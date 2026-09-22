package ante_test

import (
	"testing"

	circuittypes "cosmossdk.io/x/circuit/types"
	customante "github.com/classic-terra/core/v4/custom/auth/ante"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
)

func TestCircuitResetGuardDecorator(t *testing.T) {
	govAuthority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	other := sdk.AccAddress([]byte("emergency-account----")).String()

	// next handler records that the chain continued
	called := false
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		called = true
		return ctx, nil
	}

	decorator := customante.NewCircuitResetGuardDecorator(govAuthority)

	cases := []struct {
		name    string
		msgs    []sdk.Msg
		wantErr bool
	}{
		{
			name:    "reset by governance is allowed",
			msgs:    []sdk.Msg{&circuittypes.MsgResetCircuitBreaker{Authority: govAuthority}},
			wantErr: false,
		},
		{
			name:    "reset by any other account is rejected",
			msgs:    []sdk.Msg{&circuittypes.MsgResetCircuitBreaker{Authority: other}},
			wantErr: true,
		},
		{
			name:    "trip of a specific message by an emergency account is not restricted here",
			msgs:    []sdk.Msg{&circuittypes.MsgTripCircuitBreaker{Authority: other, MsgTypeUrls: []string{"/hyperlane.warp.v1.MsgRemoteTransfer"}}},
			wantErr: false,
		},
		{
			name:    "trip of a protected user-exit message is rejected",
			msgs:    []sdk.Msg{&circuittypes.MsgTripCircuitBreaker{Authority: other, MsgTypeUrls: []string{"/terra.liquidstake.v1.MsgUnstake"}}},
			wantErr: true,
		},
		{
			name:    "trip of all messages is rejected",
			msgs:    []sdk.Msg{&circuittypes.MsgTripCircuitBreaker{Authority: other}},
			wantErr: true,
		},
		{
			name: "reset nested in authz MsgExec is rejected",
			msgs: []sdk.Msg{mustMsgExec(t, other,
				&circuittypes.MsgResetCircuitBreaker{Authority: other})},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called = false
			tx := stubTx{msgs: tc.msgs}
			_, err := decorator.AnteHandle(sdk.Context{}, tx, false, next)
			if tc.wantErr {
				require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
				require.False(t, called)
				return
			}
			require.NoError(t, err)
			require.True(t, called)
		})
	}
}

func mustMsgExec(t *testing.T, grantee string, msgs ...sdk.Msg) *authz.MsgExec {
	t.Helper()
	exec := authz.NewMsgExec(sdk.MustAccAddressFromBech32(grantee), msgs)
	return &exec
}

// stubTx is a minimal sdk.Tx carrying only messages.
type stubTx struct {
	msgs []sdk.Msg
}

func (s stubTx) GetMsgs() []sdk.Msg { return s.msgs }

func (s stubTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
