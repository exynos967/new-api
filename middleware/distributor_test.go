package middleware

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupDistributorTestDB(t *testing.T) {
	t.Helper()

	originalSQLitePath := common.SQLitePath
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalIsMasterNode := common.IsMasterNode
	originalDB := model.DB
	originalLogDB := model.LOG_DB

	t.Setenv("SQL_DSN", "")
	common.SQLitePath = filepath.Join(t.TempDir(), "new-api-test.db")
	common.MemoryCacheEnabled = false
	common.IsMasterNode = true
	common.UsingSQLite = false
	common.UsingMySQL = false
	common.UsingPostgreSQL = false

	require.NoError(t, model.InitDB())
	model.LOG_DB = model.DB

	t.Cleanup(func() {
		if db, err := model.DB.DB(); err == nil {
			_ = db.Close()
		}
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SQLitePath = originalSQLitePath
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.IsMasterNode = originalIsMasterNode
	})
}

func TestDistributeAbortsWhenMultiKeyChannelHasNoEnabledKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupDistributorTestDB(t)

	priority := int64(10)
	channel := &model.Channel{
		Type:        constant.ChannelTypeOpenAI,
		Name:        "all-disabled-multi-key",
		Key:         "disabled-a\ndisabled-b",
		Status:      common.ChannelStatusEnabled,
		Group:       "default",
		Models:      "gpt-4o",
		CreatedTime: common.GetTimestamp(),
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusManuallyDisabled,
				1: common.ChannelStatusAutoDisabled,
			},
		},
	}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-4o",
		ChannelId: channel.Id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    100,
	}).Error)

	router := gin.New()
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		c.Next()
	}, Distribute(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"selected_key": common.GetContextKeyString(c, constant.ContextKeyChannelKey),
		})
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[]}`),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.NotEqual(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), string(types.ErrorCodeChannelNoAvailableKey))
}
