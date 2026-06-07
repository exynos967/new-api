package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openVideoProxyTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled
	originalMemoryCacheEnabled := common.MemoryCacheEnabled

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	service.InitHttpClient()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.RedisEnabled = originalRedisEnabled
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})

	return db
}

func TestVideoProxyOpenAISoraUsesStoredTaskKeyAndUpstreamTaskID(t *testing.T) {
	db := openVideoProxyTestDB(t)

	restoreFetchSetting := disableVideoProxySSRFProtection(t)
	defer restoreFetchSetting()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/videos/video_upstream/content", r.URL.Path)
		require.Equal(t, "Bearer selected-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("video-bytes"))
	}))
	t.Cleanup(upstream.Close)

	baseURL := upstream.URL
	channel := model.Channel{
		Type:    constant.ChannelTypeSora,
		Key:     "key-a\nkey-b",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
		Name:    "sora-multikey",
	}
	require.NoError(t, db.Create(&channel).Error)

	task := model.Task{
		TaskID:    "task_public",
		UserId:    7,
		ChannelId: channel.Id,
		Platform:  constant.TaskPlatform(fmt.Sprintf("%d", constant.ChannelTypeSora)),
		Status:    model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			Key:            "selected-key",
			UpstreamTaskID: "video_upstream",
		},
	}
	require.NoError(t, db.Create(&task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task_public/content", nil)
	ctx.Params = gin.Params{{Key: "task_id", Value: "task_public"}}
	ctx.Set("id", 7)

	VideoProxy(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	require.Equal(t, "video-bytes", recorder.Body.String())
}

func TestVideoProxyForwardsRangeAndAllowsPartialContent(t *testing.T) {
	db := openVideoProxyTestDB(t)

	restoreFetchSetting := disableVideoProxySSRFProtection(t)
	defer restoreFetchSetting()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "bytes=0-4", r.Header.Get("Range"))
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Range", "bytes 0-4/11")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("video"))
	}))
	t.Cleanup(upstream.Close)

	baseURL := upstream.URL
	channel := model.Channel{
		Type:    constant.ChannelTypeSora,
		Key:     "single-key",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
		Name:    "sora-range",
	}
	require.NoError(t, db.Create(&channel).Error)

	task := model.Task{
		TaskID:    "task_public",
		UserId:    7,
		ChannelId: channel.Id,
		Platform:  constant.TaskPlatform(fmt.Sprintf("%d", constant.ChannelTypeSora)),
		Status:    model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "video_upstream",
		},
	}
	require.NoError(t, db.Create(&task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task_public/content", nil)
	ctx.Request.Header.Set("Range", "bytes=0-4")
	ctx.Params = gin.Params{{Key: "task_id", Value: "task_public"}}
	ctx.Set("id", 7)

	VideoProxy(ctx)

	require.Equal(t, http.StatusPartialContent, recorder.Code)
	require.Equal(t, "bytes 0-4/11", recorder.Header().Get("Content-Range"))
	require.Equal(t, "video", recorder.Body.String())
}

func disableVideoProxySSRFProtection(t *testing.T) func() {
	t.Helper()

	fetchSetting := system_setting.GetFetchSetting()
	originalSSRFProtection := fetchSetting.EnableSSRFProtection
	fetchSetting.EnableSSRFProtection = false
	return func() {
		fetchSetting.EnableSSRFProtection = originalSSRFProtection
	}
}
