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
