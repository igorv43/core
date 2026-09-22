package hyperlane_test

import (
	"context"
	"math/big"
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	apptesting "github.com/classic-terra/core/v4/app/testing"
	customhyperlane "github.com/classic-terra/core/v4/custom/hyperlane"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

// fakeUpstream stands in for the hyperlane core message server: it accepts
// every message so the test can focus on the ledger interposition.
type fakeUpstream struct {
	coretypes.UnimplementedMsgServer
	called int
}

func (f *fakeUpstream) ProcessMessage(_ context.Context, _ *coretypes.MsgProcessMessage) (*coretypes.MsgProcessMessageResponse, error) {
	f.called++
	return &coretypes.MsgProcessMessageResponse{}, nil
}

func TestWrappedProcessMessageRecordsReceived(t *testing.T) {
	app := apptesting.SetupApp(t, "hyperlane-wrapper-test")
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: "hyperlane-wrapper-test", Height: 1})

	tokenId := util.GenerateHexAddress([20]byte{'w', 'a', 'r', 'p'}, uint32(warptypes.HYP_TOKEN_TYPE_COLLATERAL), 1)
	require.NoError(t, app.WarpKeeper.HypTokens.Set(ctx, tokenId.GetInternalId(), warptypes.HypToken{
		Id:                tokenId,
		Owner:             authtypes.NewModuleAddress("gov").String(),
		TokenType:         warptypes.HYP_TOKEN_TYPE_COLLATERAL,
		OriginMailbox:     util.NewZeroAddress(),
		OriginDenom:       "uluna",
		CollateralBalance: math.ZeroInt(),
	}))
	ledger := app.WarpLedgerKeeper
	const origin = uint32(56)

	recipientAcc := sdk.AccAddress([]byte("recipient------------"))
	payload, err := warptypes.NewWarpPayload(recipientAcc.Bytes(), *big.NewInt(250))
	require.NoError(t, err)

	warpMsg := util.HyperlaneMessage{
		Version:     3,
		Nonce:       1,
		Origin:      origin,
		Sender:      util.CreateMockHexAddress("remote-router", 1),
		Destination: 1325,
		Recipient:   tokenId,
		Body:        payload.Bytes(),
	}
	otherMsg := warpMsg
	otherMsg.Recipient = util.GenerateHexAddress([20]byte{'o', 't', 'h', 'e', 'r'}, 9, 1) // not a warp app id

	up := &fakeUpstream{}
	srv := customhyperlane.NewMsgServer(up, ledger)

	// a message delivered to a warp token is accounted for its origin domain
	_, err = srv.ProcessMessage(ctx, &coretypes.MsgProcessMessage{
		MailboxId: util.NewZeroAddress(), Relayer: recipientAcc.String(), Metadata: "0x", Message: warpMsg.String(),
	})
	require.NoError(t, err)
	require.Equal(t, 1, up.called)

	l, exists, err := ledger.GetLedger(ctx, tokenId, origin)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "250", l.Received.String())
	require.Equal(t, "-250", l.Sent.Sub(l.Received).String(), "exposure goes negative when more is received than sent")

	// a message for another application is delegated but not accounted
	_, err = srv.ProcessMessage(ctx, &coretypes.MsgProcessMessage{
		MailboxId: util.NewZeroAddress(), Relayer: recipientAcc.String(), Metadata: "0x", Message: otherMsg.String(),
	})
	require.NoError(t, err)
	require.Equal(t, 2, up.called)
	l, _, err = ledger.GetLedger(ctx, tokenId, origin)
	require.NoError(t, err)
	require.Equal(t, "250", l.Received.String())
}
