package warp_test

import (
	"context"
	"errors"
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	apptesting "github.com/classic-terra/core/v4/app/testing"
	customwarp "github.com/classic-terra/core/v4/custom/warp"
	"github.com/classic-terra/core/v4/x/warpledger/keeper"
	warpledgertypes "github.com/classic-terra/core/v4/x/warpledger/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

// fakeUpstream records whether the upstream RemoteTransfer handler ran and
// lets the test choose its outcome.
type fakeUpstream struct {
	warptypes.UnimplementedMsgServer
	called bool
	err    error
}

func (f *fakeUpstream) RemoteTransfer(_ context.Context, _ *warptypes.MsgRemoteTransfer) (*warptypes.MsgRemoteTransferResponse, error) {
	f.called = true
	if f.err != nil {
		return nil, f.err
	}
	return &warptypes.MsgRemoteTransferResponse{MessageId: util.CreateMockHexAddress("msg", 1)}, nil
}

func TestWrappedRemoteTransfer(t *testing.T) {
	app := apptesting.SetupApp(t, "warp-wrapper-test")
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: "warp-wrapper-test", Height: 1})

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
	const domain = uint32(56)

	msg := func(amount int64) *warptypes.MsgRemoteTransfer {
		return &warptypes.MsgRemoteTransfer{
			Sender:            sdk.AccAddress([]byte("sender---------------")).String(),
			TokenId:           tokenId,
			DestinationDomain: domain,
			Recipient:         util.CreateMockHexAddress("recipient", 1),
			Amount:            math.NewInt(amount),
			GasLimit:          math.ZeroInt(),
			MaxFee:            sdk.NewCoin("uluna", math.ZeroInt()),
		}
	}

	t.Run("cap is enforced before upstream runs", func(t *testing.T) {
		up := &fakeUpstream{}
		srv := customwarp.NewMsgServer(up, ledger)

		_, err := srv.RemoteTransfer(ctx, msg(1))
		require.ErrorIs(t, err, warpledgertypes.ErrDomainCapExceeded)
		require.False(t, up.called, "upstream must not run when the cap rejects the transfer")
	})

	require.NoError(t, ledger.SetDomainCap(ctx, tokenId, domain, math.NewInt(100)))

	t.Run("upstream failure leaves the ledger untouched", func(t *testing.T) {
		up := &fakeUpstream{err: errors.New("boom")}
		srv := customwarp.NewMsgServer(up, ledger)

		_, err := srv.RemoteTransfer(ctx, msg(10))
		require.Error(t, err)
		require.True(t, up.called)

		l, _, err := ledger.GetLedger(ctx, tokenId, domain)
		require.NoError(t, err)
		require.True(t, l.Sent.IsZero())
	})

	t.Run("successful transfer records sent", func(t *testing.T) {
		up := &fakeUpstream{}
		srv := customwarp.NewMsgServer(up, ledger)

		res, err := srv.RemoteTransfer(ctx, msg(60))
		require.NoError(t, err)
		require.True(t, up.called)
		require.False(t, res.MessageId.IsZeroAddress())

		l, _, err := ledger.GetLedger(ctx, tokenId, domain)
		require.NoError(t, err)
		require.Equal(t, "60", l.Sent.String())
		require.Equal(t, "60", keeper.Exposure(l).String())

		// 60 + 41 > 100
		_, err = srv.RemoteTransfer(ctx, msg(41))
		require.ErrorIs(t, err, warpledgertypes.ErrDomainCapExceeded)
		// 60 + 40 == 100
		_, err = srv.RemoteTransfer(ctx, msg(40))
		require.NoError(t, err)
	})
}
