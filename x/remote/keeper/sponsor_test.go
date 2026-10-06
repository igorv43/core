package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	perptypes "github.com/classic-terra/core/v4/x/perp/types"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/stretchr/testify/require"
)

const spotMarket = "uluna/" + settle

// spotIntent is a buy of LUNC with 1 USDC of the settlement asset.
func (f *fixture) spotIntent(sender sdk.AccAddress) *batchtypes.MsgSubmitIntent {
	return &batchtypes.MsgSubmitIntent{
		Sender: sender.String(), MarketId: spotMarket, Side: batchtypes.SIDE_BUY,
		AmountIn: sdk.NewCoin(settle, math.NewInt(1_000_000)), LimitPrice: math.LegacyNewDecWithPrec(1, 4),
		MinOut: math.ZeroInt(), ExpiryHeight: f.ctx.BlockHeight() + 10,
	}
}

// Spec v0.9.10 §14.2 rule 5: by default the intent fee is in the settlement
// asset, so an account funded only with USDC through a gateway pays it
// itself and the paymaster (whose cap is in uluna) is not involved.
func TestSessionKeyIntentFeeInSettlementPaidByAccount(t *testing.T) {
	f := setup(t)
	pm := types.PaymasterAddress()
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(100_000_000))))
	require.NoError(t, f.app.BatchKeeper.CreateMarket(f.ctx, batchtypes.Market{
		Id: spotMarket, BaseDenom: "uluna", QuoteDenom: settle, Type: batchtypes.MARKET_TYPE_SPOT, OracleDenom: "uusd",
		Enabled: true, MinQty: math.NewInt(1_000_000), TickSize: math.LegacyNewDecWithPrec(1, 6),
	}))
	bp, err := f.app.BatchKeeper.GetParams(f.ctx)
	require.NoError(t, err)
	require.Equal(t, settle, bp.IntentFee.Denom)

	controller := util.CreateMockHexAddress("evm-user", 4)
	acct := types.DeriveAddress(originDom, controller)
	require.NoError(t, f.app.BankKeeper.SendCoins(f.ctx, f.owner, acct, sdk.NewCoins(sdk.NewCoin(settle, math.NewInt(50_000_000)))))
	session := sdk.AccAddress([]byte("remote-session-key-y-"))
	f.deliver(t, controller, f.payload(t, nil,
		&perptypes.MsgDepositCollateral{Sender: acct.String(), Amount: sdk.NewCoin(settle, math.NewInt(20_000_000))},
		&types.MsgGrantSessionKey{Controller: acct.String(), SessionKey: session.String()},
	))
	require.Empty(t, f.rejected(t))
	pmBefore := f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount
	m := authz.NewMsgExec(session, []sdk.Msg{f.spotIntent(acct)})
	_, err = f.app.AuthzKeeper.Exec(f.ctx, &m)
	require.NoError(t, err)
	// escrow (1 USDC) and intent fee left the account's own settlement balance
	require.Equal(t, math.NewInt(29_000_000).Sub(bp.IntentFee.Amount).String(), f.app.BankKeeper.GetBalance(f.ctx, acct, settle).Amount.String())
	require.Equal(t, pmBefore.String(), f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount.String(), "the paymaster paid nothing")
	_, err = f.k.IntentFeeBudgets.Get(f.ctx, acct.String())
	require.Error(t, err, "no sponsored budget consumed")
}

// spec §14.4 item 4: when governance prices the intent fee in uluna, an
// account funded only with the settlement asset through a gateway holds no
// uluna; its session key can still submit spot intents because the
// paymaster pays the x/batch intent fee, within paymaster_daily_cap per
// account and period. Local users pay themselves.
func TestSessionKeyIntentFeeSponsoredForSettlementOnlyAccount(t *testing.T) {
	f := setup(t)
	pm := types.PaymasterAddress()
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(100_000_000))))
	require.NoError(t, f.app.BatchKeeper.CreateMarket(f.ctx, batchtypes.Market{
		Id: spotMarket, BaseDenom: "uluna", QuoteDenom: settle, Type: batchtypes.MARKET_TYPE_SPOT, OracleDenom: "uusd",
		Enabled: true, MinQty: math.NewInt(1_000_000), TickSize: math.LegacyNewDecWithPrec(1, 6),
	}))
	bp, err := f.app.BatchKeeper.GetParams(f.ctx)
	require.NoError(t, err)
	bp.IntentFee = sdk.NewCoin("uluna", math.NewInt(2_000_000))
	require.NoError(t, f.app.BatchKeeper.SetParams(f.ctx, bp))
	intentFee := bp.IntentFee
	require.Equal(t, "uluna", intentFee.Denom)
	require.True(t, intentFee.IsPositive())

	controller := util.CreateMockHexAddress("evm-user", 3)
	acct := types.DeriveAddress(originDom, controller)
	require.NoError(t, f.app.BankKeeper.SendCoins(f.ctx, f.owner, acct, sdk.NewCoins(sdk.NewCoin(settle, math.NewInt(50_000_000)))))
	session := sdk.AccAddress([]byte("remote-session-key-x-"))
	f.deliver(t, controller, f.payload(t, nil,
		&perptypes.MsgDepositCollateral{Sender: acct.String(), Amount: sdk.NewCoin(settle, math.NewInt(20_000_000))},
		&types.MsgGrantSessionKey{Controller: acct.String(), SessionKey: session.String()},
	))
	require.Empty(t, f.rejected(t))
	require.True(t, f.app.BankKeeper.GetBalance(f.ctx, acct, "uluna").IsZero(), "settlement-only account")

	// two sponsored intents per period, then the account pays itself (it cannot)
	p, err := f.k.GetParams(f.ctx)
	require.NoError(t, err)
	p.PaymasterDailyCap = sdk.NewCoin("uluna", intentFee.Amount.MulRaw(2))
	require.NoError(t, f.k.SetParams(f.ctx, p))

	exec := func() error {
		m := authz.NewMsgExec(session, []sdk.Msg{f.spotIntent(acct)})
		_, err := f.app.AuthzKeeper.Exec(f.ctx, &m)
		return err
	}
	pmBefore := f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount
	require.NoError(t, exec())
	require.NoError(t, exec())
	require.Equal(t, intentFee.Amount.MulRaw(2).String(), pmBefore.Sub(f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount).String(),
		"the paymaster paid both intent fees")
	require.True(t, f.app.BankKeeper.GetBalance(f.ctx, acct, "uluna").IsZero())
	require.Equal(t, "28000000", f.app.BankKeeper.GetBalance(f.ctx, acct, settle).Amount.String(), "only the escrow left the account")
	intents, err := f.app.BatchKeeper.IntentsOfMarket(f.ctx, spotMarket, 10)
	require.NoError(t, err)
	require.Len(t, intents, 2)
	require.Equal(t, acct.String(), intents[0].Sender)

	// the period cap is reached: the third intent falls back to the account, which holds no uluna
	err = exec()
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient funds")
	b, err := f.k.IntentFeeBudgets.Get(f.ctx, acct.String())
	require.NoError(t, err)
	require.Equal(t, intentFee.Amount.MulRaw(2).String(), b.Spent.Amount.String())

	// a new paymaster period restores the sponsorship
	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(time.Duration(p.PaymasterPeriodSeconds) * time.Second))
	require.NoError(t, exec())

	// a local user is never sponsored
	pmBefore = f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount
	ownerBefore := f.app.BankKeeper.GetBalance(f.ctx, f.owner, "uluna").Amount
	_, _, err = f.app.BatchKeeper.SubmitIntent(f.ctx, f.spotIntent(f.owner))
	require.NoError(t, err)
	require.Equal(t, pmBefore.String(), f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount.String())
	require.Equal(t, intentFee.Amount.String(), ownerBefore.Sub(f.app.BankKeeper.GetBalance(f.ctx, f.owner, "uluna").Amount).String())

	// an empty paymaster leaves the remote account to pay itself
	empty := f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna")
	require.NoError(t, f.app.BankKeeper.SendCoins(f.ctx, pm, f.owner, sdk.NewCoins(empty)))
	sponsored, err := f.k.SponsorIntentFee(f.ctx, acct, intentFee)
	require.NoError(t, err)
	require.False(t, sponsored)
}
