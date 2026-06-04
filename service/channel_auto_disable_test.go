package service

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestShouldDisableChannelErrorSeparatesMultiKeySwitch(t *testing.T) {
	originalDisableChannel := common.AutomaticDisableChannelEnabled
	originalDisableKey := common.AutomaticDisableChannelKeyEnabled
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = originalDisableChannel
		common.AutomaticDisableChannelKeyEnabled = originalDisableKey
	})

	err := types.NewError(errors.New("invalid key"), types.ErrorCodeChannelInvalidKey)
	singleKeyChannel := types.ChannelError{IsMultiKey: false}
	multiKeyChannel := types.ChannelError{IsMultiKey: true}

	common.AutomaticDisableChannelEnabled = true
	common.AutomaticDisableChannelKeyEnabled = false
	require.True(t, ShouldDisableChannelError(err, singleKeyChannel))
	require.False(t, ShouldDisableChannelError(err, multiKeyChannel))

	common.AutomaticDisableChannelEnabled = false
	common.AutomaticDisableChannelKeyEnabled = true
	require.False(t, ShouldDisableChannelError(err, singleKeyChannel))
	require.True(t, ShouldDisableChannelError(err, multiKeyChannel))
}
