package controller

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldRetryBypassesAffinitySkipOnceAfterKeyDisable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("channel_affinity_skip_retry_on_failure", true)
	err := types.NewError(errors.New("invalid key"), types.ErrorCodeChannelInvalidKey)

	require.False(t, shouldRetry(ctx, err, 1))

	markChannelKeyDisabledForRetry(ctx, 123)
	require.True(t, shouldRetry(ctx, err, 1))
	require.False(t, shouldRetry(ctx, err, 1))
}

func TestGetChannelRetriesSameMultiKeyChannelAfterKeyDisable(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)

	channel := model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		Name:   "multi-key-retry",
		Models: "gpt-4o",
		Group:  "default",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
			},
		},
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-4o",
		ChannelId: channel.Id,
		Enabled:   true,
		Weight:    100,
	}).Error)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	markChannelKeyDisabledForRetry(ctx, channel.Id)

	relayInfo := &relaycommon.RelayInfo{
		TokenGroup:      "default",
		OriginModelName: "gpt-4o",
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}
	retryParam := &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-4o",
		Retry:      common.GetPointer(0),
	}

	selected, err := getChannel(ctx, relayInfo, retryParam)
	require.Nil(t, err)
	require.NotNil(t, selected)
	require.Equal(t, channel.Id, selected.Id)
	require.Equal(t, "key-b", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
	require.Equal(t, 1, common.GetContextKeyInt(ctx, constant.ContextKeyChannelMultiKeyIndex))
}
