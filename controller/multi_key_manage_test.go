package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestManageMultiKeysDisableAllDisablesChannelSelection(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)

	channel := model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		Name:   "multi-key-disable-all",
		Models: "gpt-4o",
		Group:  "default",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
		},
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-4o",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/channel/multi_key/manage",
		strings.NewReader(`{"channel_id":`+strconv.Itoa(channel.Id)+`,"action":"disable_all_keys"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	ManageMultiKeys(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)

	var reloaded model.Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	require.NotEqual(t, common.ChannelStatusEnabled, reloaded.Status)

	var ability model.Ability
	require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&ability).Error)
	require.False(t, ability.Enabled)
}
