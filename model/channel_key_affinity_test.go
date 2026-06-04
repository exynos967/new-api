package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestGetEnabledKeyByIndex(t *testing.T) {
	channel := &Channel{
		Id:  998001,
		Key: "key-a\nkey-b\nkey-c",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeyMode: constant.MultiKeyModeRandom,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusManuallyDisabled,
				1: common.ChannelStatusAutoDisabled,
			},
		},
	}

	key, index, ok := channel.GetEnabledKeyByIndex(2)
	require.True(t, ok)
	require.Equal(t, "key-c", key)
	require.Equal(t, 2, index)

	_, _, ok = channel.GetEnabledKeyByIndex(1)
	require.False(t, ok)

	_, _, ok = channel.GetEnabledKeyByIndex(0)
	require.False(t, ok)

	_, _, ok = channel.GetEnabledKeyByIndex(3)
	require.False(t, ok)
}

func TestGetNextEnabledKeyPollingUsesLatestDisabledStatus(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalChannelsIDM := channelsIDM
	originalGroup2Model2Channels := group2model2channels
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	channelsIDM = map[int]*Channel{
		998002: {
			Id:  998002,
			Key: "key-a\nkey-b",
			ChannelInfo: ChannelInfo{
				IsMultiKey:           true,
				MultiKeyMode:         constant.MultiKeyModePolling,
				MultiKeyPollingIndex: 0,
				MultiKeyStatusList: map[int]int{
					0: common.ChannelStatusManuallyDisabled,
				},
			},
		},
	}
	group2model2channels = map[string]map[string][]int{}
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		channelsIDM = originalChannelsIDM
		group2model2channels = originalGroup2Model2Channels
		channelSyncLock.Unlock()
	})

	staleChannel := &Channel{
		Id:  998002,
		Key: "key-a\nkey-b",
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 0,
		},
	}

	key, index, err := staleChannel.GetNextEnabledKey()
	require.Nil(t, err)
	require.Equal(t, "key-b", key)
	require.Equal(t, 1, index)
}
