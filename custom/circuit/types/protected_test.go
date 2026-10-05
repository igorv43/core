package types_test

import (
	"context"
	"testing"

	customcircuittypes "github.com/classic-terra/core/v4/custom/circuit/types"
	liquidstaketypes "github.com/classic-terra/core/v4/x/liquidstake/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

// The constants must track the real message type URLs of x/liquidstake.
func TestProtectedTypeURLsMatchLiquidStake(t *testing.T) {
	require.Equal(t, sdk.MsgTypeURL(&liquidstaketypes.MsgUnstake{}), customcircuittypes.MsgUnstakeTypeURL)
	require.Equal(t, sdk.MsgTypeURL(&liquidstaketypes.MsgClaim{}), customcircuittypes.MsgClaimTypeURL)
	require.False(t, customcircuittypes.IsProtected(sdk.MsgTypeURL(&liquidstaketypes.MsgStake{})),
		"MsgStake must stay pausable (spec §24.5)")

	// The returned slice is a copy: mutating it does not change the policy.
	urls := customcircuittypes.ProtectedMsgTypeURLs()
	urls[0] = "/x"
	require.True(t, customcircuittypes.IsProtected(customcircuittypes.MsgUnstakeTypeURL))
}

func TestCheckNoProtected(t *testing.T) {
	require.NoError(t, customcircuittypes.CheckNoProtected([]string{"/hyperlane.warp.v1.MsgRemoteTransfer"}))
	require.NoError(t, customcircuittypes.CheckNoProtected(nil))
	err := customcircuittypes.CheckNoProtected([]string{"/hyperlane.warp.v1.MsgRemoteTransfer", customcircuittypes.MsgClaimTypeURL})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
}

// stubBreaker disallows everything it is asked about.
type stubBreaker struct{ asked []string }

func (s *stubBreaker) IsAllowed(_ context.Context, typeURL string) (bool, error) {
	s.asked = append(s.asked, typeURL)
	return false, nil
}

func TestProtectedCircuitBreaker(t *testing.T) {
	inner := &stubBreaker{}
	b := customcircuittypes.NewProtectedCircuitBreaker(inner)

	for _, u := range customcircuittypes.ProtectedMsgTypeURLs() {
		ok, err := b.IsAllowed(context.Background(), u)
		require.NoError(t, err)
		require.True(t, ok, "%s must always be allowed", u)
	}
	require.Empty(t, inner.asked, "protected URLs never reach the stored disabled list")

	ok, err := b.IsAllowed(context.Background(), "/terra.liquidstake.v1.MsgStake")
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, []string{"/terra.liquidstake.v1.MsgStake"}, inner.asked)
}
