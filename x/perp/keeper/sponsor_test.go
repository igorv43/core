package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	batchkeeper "github.com/classic-terra/core/v4/x/batch/keeper"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	remotetypes "github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/stretchr/testify/require"
)

// Spec v0.9.10 §14.2 rule 5 / §14.4 item 4: the intent fee is in the
// settlement asset. A remote account that put all its USDC into margin
// submits a perp intent through its session key: the shortfall of the fee is
// taken from its available collateral (IntentFeeFunder), the paymaster pays
// nothing. When governance sets the fee in uluna instead, the x/remote
// paymaster sponsors it, as before. A local trader always pays its own.
func TestPerpIntentFeeFromCollateralOrSponsor(t *testing.T) {
	f := setup(t)
	require.NoError(t, f.app.RemoteKeeper.InitGenesis(f.ctx, remotetypes.DefaultGenesisState()))
	controller := util.CreateMockHexAddress("evm-perp-user", 1)
	acct := remotetypes.DeriveAddress(97, controller)
	require.NoError(t, f.app.RemoteKeeper.Accounts.Set(f.ctx, acct.String(), remotetypes.RemoteAccount{
		Domain: 97, Controller: controller, Address: acct.String(),
	}))
	settlement := sdk.NewCoins(sdk.NewCoin("uusdc.lf", math.NewInt(200_000_000)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", settlement))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", acct, settlement))
	require.NoError(t, f.k.Deposit(f.ctx, acct, sdk.NewCoin("uusdc.lf", math.NewInt(200_000_000))))
	pm := remotetypes.PaymasterAddress()
	float := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(50_000_000)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", float))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", pm, float))
	feeCollector := f.app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)

	session := sdk.AccAddress([]byte("perp-session-key-----"))
	_, err := f.app.RemoteKeeper.GrantSession(f.ctx, acct.String(), session.String(), 0)
	require.NoError(t, err)
	exec := func() {
		t.Helper()
		msg := authz.NewMsgExec(session, []sdk.Msg{&types.MsgSubmitPerpIntent{
			Sender: acct.String(), MarketId: marketID, Side: batchtypes.SIDE_BUY, Qty: math.NewInt(1_000),
			LimitPrice: math.LegacyNewDec(60_000), ExpiryHeight: f.ctx.BlockHeight() + 100,
		}})
		_, err := f.app.AuthzKeeper.Exec(f.ctx, &msg)
		require.NoError(t, err)
	}

	// default: 1000 uusdc.lf, paid from the margin of an account with no bank balance
	fee := f.bp.IntentFee
	require.Equal(t, "1000uusdc.lf", fee.String())
	fcBefore := f.app.BankKeeper.GetBalance(f.ctx, feeCollector, fee.Denom).Amount
	exec()
	free, err := f.k.FreeCollateral(f.ctx, acct.String())
	require.NoError(t, err)
	require.Equal(t, math.NewInt(200_000_000).Sub(fee.Amount).String(), free.String(), "the fee came out of the free collateral")
	require.True(t, f.app.BankKeeper.GetBalance(f.ctx, acct, fee.Denom).IsZero())
	require.Equal(t, fee.Amount.String(), f.app.BankKeeper.GetBalance(f.ctx, feeCollector, fee.Denom).Amount.Sub(fcBefore).String())
	require.Equal(t, float.AmountOf("uluna").String(), f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount.String(), "the paymaster paid nothing")
	if msg, broken := f.k.CheckInvariants(f.ctx); broken {
		t.Fatal(msg)
	}

	// governance prices the fee in uluna: the paymaster sponsors the remote account
	bp := f.bp
	bp.IntentFee = sdk.NewCoin("uluna", math.NewInt(2_000_000))
	require.NoError(t, f.bk.SetParams(f.ctx, bp))
	exec()
	require.True(t, f.app.BankKeeper.GetBalance(f.ctx, acct, "uluna").IsZero(), "the account never held uluna")
	require.Equal(t, float.AmountOf("uluna").Sub(bp.IntentFee.Amount).String(),
		f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount.String(), "the paymaster paid the intent fee")
	intents, err := f.bk.IntentsOfMarket(f.ctx, marketID, 10)
	require.NoError(t, err)
	require.Len(t, intents, 2)
	require.Equal(t, acct.String(), intents[0].Sender)

	// a local trader pays its own intent fee
	before := f.app.BankKeeper.GetBalance(f.ctx, f.long, "uluna").Amount
	pmBefore := f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount
	f.submit(t, f.long, batchtypes.SIDE_SELL, 1_000, math.LegacyNewDec(60_000), false)
	require.Equal(t, bp.IntentFee.Amount.String(), before.Sub(f.app.BankKeeper.GetBalance(f.ctx, f.long, "uluna").Amount).String())
	require.Equal(t, pmBefore.String(), f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount.String())
}

// The collateral path never touches margin reserved by open orders: with
// nothing available and no bank balance the intent is refused.
func TestPerpIntentFeeNeverFromReservedMargin(t *testing.T) {
	f := setup(t)
	acct := sdk.AccAddress([]byte("perp-all-in-margin---"))
	// IM of 1,000 at 60,000 at 3x is 20,000,000; the first intent fee (1,000)
	// also comes from the margin, leaving 500 available
	coins := sdk.NewCoins(sdk.NewCoin("uusdc.lf", math.NewInt(20_001_500)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", coins))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", acct, coins))
	require.NoError(t, f.k.Deposit(f.ctx, acct, coins[0]))
	f.submit(t, acct, batchtypes.SIDE_BUY, 1_000, math.LegacyNewDec(60_000), false)
	avail, err := f.k.Available(f.ctx, acct.String())
	require.NoError(t, err)
	require.True(t, avail.LT(f.bp.IntentFee.Amount), "available %s", avail)
	_, _, err = f.bk.SubmitPerpIntent(f.ctx, batchkeeper.PerpOrder{
		Sender: acct.String(), MarketID: marketID, Side: batchtypes.SIDE_BUY, Qty: math.NewInt(1_000),
		LimitPrice: math.LegacyNewDec(60_000), ExpiryHeight: f.ctx.BlockHeight() + 100, ChargeFee: true,
	})
	require.ErrorContains(t, err, "insufficient funds")
}
