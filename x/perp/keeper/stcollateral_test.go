package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	batchkeeper "github.com/classic-terra/core/v4/x/batch/keeper"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	lstypes "github.com/classic-terra/core/v4/x/liquidstake/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// stSetup prepares stLUNC as collateral (spec §21.5): liquid staking at rate
// 1, LUNC priced at 0.0001 USD, the LUNC/USD spot market (rule 8), the
// governance caps, the BTC market allowing stLUNC, and 1,000,000 LUNC staked
// by the long. One stLUNC is then worth 0.0001·(1 − 0.35) = 0.000065 USD.
func (f *fixture) stSetup(t *testing.T) {
	t.Helper()
	require.NoError(t, f.app.LiquidStakeKeeper.InitGenesis(f.ctx, lstypes.DefaultGenesisState()))
	// the liquid staking module cap is a share of the bonded stake: bond one validator
	require.NoError(t, f.app.SlashingKeeper.SetParams(f.ctx, slashingtypes.DefaultParams()))
	priv := ed25519.GenPrivKey()
	valAddr := sdk.ValAddress(priv.PubKey().Address())
	valCoins := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(10_000_000_000_000)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", valCoins))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", sdk.AccAddress(valAddr), valCoins))
	cv, err := stakingtypes.NewMsgCreateValidator(valAddr.String(), priv.PubKey(), sdk.NewCoin("uluna", math.NewInt(5_000_000_000_000)),
		stakingtypes.NewDescription("val", "", "", "", ""),
		stakingtypes.NewCommissionRates(math.LegacyNewDecWithPrec(5, 2), math.LegacyNewDecWithPrec(20, 2), math.LegacyNewDecWithPrec(1, 2)), math.OneInt())
	require.NoError(t, err)
	_, err = stakingkeeper.NewMsgServerImpl(f.app.StakingKeeper).CreateValidator(f.ctx, cv)
	require.NoError(t, err)
	_, err = f.app.StakingKeeper.ApplyAndReturnValidatorSetUpdates(f.ctx)
	require.NoError(t, err)
	lp, _ := f.app.LiquidStakeKeeper.GetParams(f.ctx)
	lp.MaxShare, lp.ValidatorCap = math.LegacyOneDec(), math.LegacyOneDec()
	require.NoError(t, f.app.LiquidStakeKeeper.SetParams(f.ctx, lp))
	f.app.OracleKeeper.SetLunaExchangeRate(f.ctx, "uusd", math.LegacyNewDecWithPrec(1, 4))
	require.NoError(t, f.bk.CreateMarket(f.ctx, batchtypes.Market{
		Id: "uluna/uusd", BaseDenom: "uluna", QuoteDenom: "uusd", Type: batchtypes.MARKET_TYPE_SPOT, OracleDenom: "uusd",
		Enabled: true, MinQty: math.NewInt(1_000_000), TickSize: math.LegacyNewDecWithPrec(1, 6),
	}))
	p := f.params
	p.SpotMarketId = "uluna/uusd"
	p.StGlobalCap = math.NewInt(10_000_000_000_000)
	p.StGlobalCapIfRatio = math.LegacyNewDec(100)
	p.StUnwindCapPerEpoch = math.NewInt(1_000_000_000_000)
	require.NoError(t, f.k.SetParams(f.ctx, p))
	f.params = p
	m := f.market(t)
	m.StCollateralAllowed = true
	require.NoError(t, f.k.Markets.Set(f.ctx, m.Id, m))
	// the long stakes 1,000,000 LUNC and gets 1,000,000 stLUNC at rate 1
	coins := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(1_000_000_000_000)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", coins))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", f.long, coins))
	minted, _, err := f.app.LiquidStakeKeeper.Stake(f.ctx, f.long, sdk.NewCoin("uluna", math.NewInt(1_000_000_000_000)))
	require.NoError(t, err)
	require.Equal(t, "1000000000000", minted.Amount.String())
	// the fund core is seeded so the value cap and the advances have room
	require.NoError(t, f.k.FundInsurance(f.ctx, f.short, sdk.NewCoin("uusd", math.NewInt(200_000_000))))
}

func (f *fixture) stFree(t *testing.T, acc sdk.AccAddress) math.Int {
	t.Helper()
	v, err := f.k.FreeSt(f.ctx, acc.String())
	require.NoError(t, err)
	return v
}

func TestStDepositValuationAndRules(t *testing.T) {
	f := setup(t)
	// closed by default: st_global_cap = 0
	err := f.k.Deposit(f.ctx, f.long, sdk.NewCoin("stluna", math.NewInt(1)))
	require.ErrorIs(t, err, types.ErrInvalidCollateral)
	f.stSetup(t)

	require.NoError(t, f.k.Deposit(f.ctx, f.long, sdk.NewCoin("stluna", math.NewInt(1_000_000_000_000))))
	require.Equal(t, "1000000000000", f.stFree(t, f.long).String())
	val := f.k.Valuation(f.ctx)
	require.True(t, val.Available)
	require.Equal(t, "0.000065000000000000", val.UnitValue.String())
	require.Equal(t, "65000000", val.Value(math.NewInt(1_000_000_000_000)).String()) // 65 USD after the 35% haircut
	if msg, broken := f.k.CheckInvariants(f.ctx); broken {
		t.Fatal(msg)
	}

	// global cap in units: checked before any transfer
	p := f.params
	p.StGlobalCap = math.NewInt(1_000_000_000_000)
	require.NoError(t, f.k.SetParams(f.ctx, p))
	err = f.k.Deposit(f.ctx, f.long, sdk.NewCoin("stluna", math.NewInt(1)))
	require.ErrorIs(t, err, types.ErrInvalidCollateral)

	// rule 3: a LUNC-priced market can never allow stLUNC
	err = f.k.CreateMarket(f.ctx, types.Market{
		Id: "uluna-perp/uusd", OracleAsset: "uusd", MaxLeverage: math.LegacyNewDec(2), OiCap: math.NewInt(1_000_000), Alpha: math.LegacyNewDecWithPrec(10, 2),
		Stress: math.LegacyOneDec(), MinQty: math.NewInt(1_000), TickSize: math.LegacyOneDec(), StCollateralAllowed: true,
	})
	require.ErrorIs(t, err, types.ErrInvalidMarket)

	// withdraw stLUNC back
	require.NoError(t, f.k.Withdraw(f.ctx, f.long, sdk.NewCoin("stluna", math.NewInt(1_000_000_000_000))))
	require.True(t, f.stFree(t, f.long).IsZero())
	require.Equal(t, "1000000000000", f.app.BankKeeper.GetBalance(f.ctx, f.long, "stluna").Amount.String())
}

func TestStMarginAllocationWithinShareCap(t *testing.T) {
	f := setup(t)
	f.stSetup(t)
	require.NoError(t, f.k.Deposit(f.ctx, f.long, sdk.NewCoin("stluna", math.NewInt(1_000_000_000_000))))
	// leave the long with exactly 10 USD of settlement: IM at 3x on 60 USD is 20 USD → 10 settlement + 10 stLUNC
	require.NoError(t, f.k.Withdraw(f.ctx, f.long, sdk.NewCoin("uusd", math.NewInt(90_000_000))))

	f.trade(t, 1_000, math.LegacyNewDec(60_000))
	long, ok, _ := f.k.GetPosition(f.ctx, f.long.String(), marketID)
	require.True(t, ok, "opened with mixed collateral")
	require.Equal(t, "10000000", long.Collateral.String())
	require.Equal(t, "153846153847", long.CollateralSt.String()) // ceil(10,000,000 / 0.000065)
	view := f.k.View(f.ctx, f.market(t), long)
	require.True(t, view.CollateralValue.GTE(math.NewInt(20_000_000)))
	// no settlement was left for the fee (30,000): it was seized from stLUNC into the
	// tranche, the core advanced it, and the same EndBlock already redeemed the tranche
	l, _ := f.k.GetLedger(f.ctx)
	require.True(t, l.TrancheAdvanced.IsPositive(), "fee advanced against seized stLUNC")
	require.True(t, l.TrancheSt.IsZero() && (l.TrancheUluna.IsPositive() || l.TrancheSellIntentId != 0), "tranche redeemed")
	if msg, broken := f.k.CheckInvariants(f.ctx); broken {
		t.Fatal(msg)
	}

	// the collateral query reports both legs
	res, err := f.k.Valuation(f.ctx), error(nil)
	require.NoError(t, err)
	require.Equal(t, "0.350000000000000000", res.Haircut.String())

	// a second account with only 5 USD of settlement cannot open: settlement must cover 50% of the margin
	poor := sdk.AccAddress([]byte("perp-poor-st---------"))
	stake := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(1_000_000_000_000)), sdk.NewCoin("uusd", math.NewInt(5_000_000)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", stake))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", poor, stake))
	_, _, err = f.app.LiquidStakeKeeper.Stake(f.ctx, poor, sdk.NewCoin("uluna", math.NewInt(1_000_000_000_000)))
	require.NoError(t, err)
	require.NoError(t, f.k.Deposit(f.ctx, poor, sdk.NewCoin("stluna", math.NewInt(1_000_000_000_000))))
	require.NoError(t, f.k.Deposit(f.ctx, poor, sdk.NewCoin("uusd", math.NewInt(5_000_000))))
	// the reservation already applies rule 5: 5 settlement + at most 5 of stLUNC capacity < 20 needed
	_, _, err = f.bk.SubmitPerpIntent(f.ctx, batchkeeper.PerpOrder{Sender: poor.String(), MarketID: marketID, Side: batchtypes.SIDE_BUY,
		Qty: math.NewInt(1_000), LimitPrice: math.LegacyNewDec(60_000), ExpiryHeight: f.ctx.BlockHeight() + 100})
	require.ErrorIs(t, err, types.ErrInsufficientFree)
}

func TestStLiquidationSeizesToTrancheAndConverts(t *testing.T) {
	f := setup(t)
	f.stSetup(t)
	require.NoError(t, f.k.Deposit(f.ctx, f.long, sdk.NewCoin("stluna", math.NewInt(1_000_000_000_000))))
	require.NoError(t, f.k.Withdraw(f.ctx, f.long, sdk.NewCoin("uusd", math.NewInt(89_000_000)))) // 11 USD settlement: fee comes from it
	f.trade(t, 1_000, math.LegacyNewDec(60_000))
	long, ok, _ := f.k.GetPosition(f.ctx, f.long.String(), marketID)
	require.True(t, ok)
	require.True(t, long.CollateralSt.IsPositive())
	before, _ := f.k.GetLedger(f.ctx)
	stValue := f.k.Valuation(f.ctx).Value(long.CollateralSt)

	// the drop liquidates the long: its stLUNC goes to the tranche, the core advances its haircut value
	f.at(f.ctx.BlockHeight() + 1)
	f.setPrice(t, math.LegacyNewDec(45_000), math.LegacyZeroDec())
	f.endBlocks(t, f.ctx.BlockHeight())
	_, ok, _ = f.k.GetPosition(f.ctx, f.long.String(), marketID)
	require.False(t, ok, "liquidated")
	after, _ := f.k.GetLedger(f.ctx)
	require.True(t, after.TrancheAdvanced.GTE(stValue), "core advanced the stLUNC value")
	// the tranche was redeemed in the same EndBlock (instant, from the liquid staking buffer)
	require.True(t, after.TrancheSt.IsZero())
	require.True(t, after.TrancheUluna.IsPositive() || after.TrancheSellIntentId != 0, "tranche uluna claimed or already on sale")
	require.NotZero(t, after.TrancheSellIntentId, "sale placed in the spot market")
	require.True(t, after.Insurance.LT(before.Insurance.AddRaw(1_000_000)), "core lent settlement against the tranche")

	// a buyer of LUNC meets the tranche sale; the settlement repays the advance
	sale, err := f.bk.GetIntent(f.ctx, after.TrancheSellIntentId)
	require.NoError(t, err)
	require.Equal(t, batchtypes.SIDE_SELL, sale.Side)
	batch := f.ctx.BlockHeight()
	_, _, err = f.bk.SubmitIntent(f.ctx, &batchtypes.MsgSubmitIntent{Sender: f.short.String(), MarketId: "uluna/uusd", Side: batchtypes.SIDE_BUY,
		AmountIn: sdk.NewCoin("uusd", math.NewInt(200_000_000)), LimitPrice: math.LegacyNewDecWithPrec(1, 4), MinOut: math.ZeroInt(), ExpiryHeight: batch + 50})
	require.NoError(t, err)
	f.endBlocks(t, batch+f.bp.CommitWindow+1)
	f.endBlocks(t, f.ctx.BlockHeight()+1) // the closed sale is settled at the next step
	final, _ := f.k.GetLedger(f.ctx)
	require.True(t, final.TrancheAdvanced.LT(after.TrancheAdvanced), "advance repaid by the sale: %s → %s", after.TrancheAdvanced, final.TrancheAdvanced)
	require.True(t, final.Insurance.GT(after.Insurance), "proceeds joined the core")
	if msg, broken := f.k.CheckInvariants(f.ctx); broken {
		t.Fatal(msg)
	}
}

func TestStAutoTopUpUsesStLunc(t *testing.T) {
	f := setup(t)
	f.stSetup(t)
	require.NoError(t, f.k.Deposit(f.ctx, f.long, sdk.NewCoin("stluna", math.NewInt(1_000_000_000_000))))
	require.NoError(t, f.k.AutoTopUp.Set(f.ctx, f.long.String()))
	// 21 USD of settlement: 20 goes into the position, 1 stays free (not enough to top up alone)
	require.NoError(t, f.k.Withdraw(f.ctx, f.long, sdk.NewCoin("uusd", math.NewInt(79_000_000))))
	f.trade(t, 1_000, math.LegacyNewDec(60_000))
	stBefore := f.stFree(t, f.long)
	f.at(f.ctx.BlockHeight() + 1)
	f.setPrice(t, math.LegacyNewDec(45_000), math.LegacyZeroDec())
	f.endBlocks(t, f.ctx.BlockHeight())
	long, ok, _ := f.k.GetPosition(f.ctx, f.long.String(), marketID)
	require.True(t, ok, "rescued with stLUNC")
	require.True(t, long.CollateralSt.IsPositive())
	require.True(t, f.stFree(t, f.long).LT(stBefore))
	if msg, broken := f.k.CheckInvariants(f.ctx); broken {
		t.Fatal(msg)
	}
}
