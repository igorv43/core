package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	terraapp "github.com/classic-terra/core/v4/app"
	apptesting "github.com/classic-terra/core/v4/app/testing"
	batchkeeper "github.com/classic-terra/core/v4/x/batch/keeper"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	oracletypes "github.com/classic-terra/core/v4/x/oracle/types"
	"github.com/classic-terra/core/v4/x/perp/keeper"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

const (
	chainID  = "perp-test"
	marketID = "ubtc-perp/uusd"
	asset    = "ubtc"
)

type fixture struct {
	app    *terraapp.TerraApp
	ctx    sdk.Context
	k      keeper.Keeper
	bk     *batchkeeper.Keeper
	long   sdk.AccAddress
	short  sdk.AccAddress
	params types.Params
	bp     batchtypes.Params
}

// setup boots the app, initialises x/batch and x/perp like the upgrades do,
// publishes a BTC price through the oracle asset path and registers an
// enabled BTC perp market with two funded traders.
func setup(t *testing.T) *fixture {
	t.Helper()
	h := &apptesting.KeeperTestHelper{}
	h.SetT(t)
	h.Setup(t, chainID)
	app, ctx := h.App, h.Ctx
	require.NoError(t, app.BatchKeeper.InitGenesis(ctx, batchtypes.DefaultGenesisState()))
	require.NoError(t, app.PerpKeeper.InitGenesis(ctx, types.DefaultGenesisState()))
	f := &fixture{app: app, ctx: ctx, k: app.PerpKeeper, bk: &app.BatchKeeper}
	f.params, _ = f.k.GetParams(ctx)
	f.bp, _ = f.bk.GetParams(ctx)
	f.at(10)
	f.setPrice(t, math.LegacyNewDec(60_000), math.LegacyZeroDec())

	require.NoError(t, f.k.CreateMarket(f.ctx, types.Market{
		Id: marketID, OracleAsset: asset, MaxLeverage: math.LegacyNewDec(3), OiCap: math.NewInt(1_000_000), Alpha: math.LegacyNewDecWithPrec(25, 2),
		Stress: math.LegacyOneDec(), ListingMinBlocks: 0, VenuesAttested: true, MinQty: math.NewInt(1_000), TickSize: math.LegacyOneDec(),
	}))
	f.forceEnable(t)

	f.long = sdk.AccAddress([]byte("perp-long------------"))
	f.short = sdk.AccAddress([]byte("perp-short-----------"))
	for _, a := range []sdk.AccAddress{f.long, f.short} {
		h.FundAcc(a, sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(100_000_000)), sdk.NewCoin("uusd", math.NewInt(1_000_000_000))))
		require.NoError(t, f.k.Deposit(f.ctx, a, sdk.NewCoin("uusd", math.NewInt(100_000_000)))) // 100 USD
	}
	return f
}

func (f *fixture) at(height int64) {
	h := f.ctx.BlockHeader()
	h.Height = height
	f.ctx = f.ctx.WithBlockHeader(h).WithBlockHeight(height)
}

// setPrice publishes the asset price and a fresh rate sample (with depth) as
// the oracle EndBlock would at the end of the current vote period.
func (f *fixture) setPrice(t *testing.T, price, dispersion math.LegacyDec) {
	t.Helper()
	ok := f.app.OracleKeeper
	ok.SetAssetTarget(f.ctx, oracletypes.Asset{Name: asset})
	ok.SetAssetPrice(f.ctx, asset, price)
	vp := ok.VotePeriod(f.ctx)
	ok.SetRateSample(f.ctx, oracletypes.RateSample{Denom: asset, ExchangeRate: price, Dispersion: dispersion,
		VotePeriod: uint64(f.ctx.BlockHeight()) / vp, Height: f.ctx.BlockHeight(), Time: f.ctx.BlockTime(),
		Depth: math.NewInt(1_000_000_000_000_000)})
}

// forceEnable enables the market on both keepers without the listing check.
func (f *fixture) forceEnable(t *testing.T) {
	t.Helper()
	m, err := f.k.GetMarket(f.ctx, marketID)
	require.NoError(t, err)
	m.Enabled = true
	require.NoError(t, f.k.Markets.Set(f.ctx, m.Id, m))
	require.NoError(t, f.bk.SetMarketEnabled(f.ctx, marketID, true))
	// refresh mark and state
	require.NoError(t, f.k.EndBlocker(f.ctx))
}

func (f *fixture) market(t *testing.T) types.Market {
	t.Helper()
	m, err := f.k.GetMarket(f.ctx, marketID)
	require.NoError(t, err)
	return m
}

func (f *fixture) submit(t *testing.T, sender sdk.AccAddress, side batchtypes.Side, qty int64, limit math.LegacyDec, reduceOnly bool) uint64 {
	t.Helper()
	id, _, err := f.bk.SubmitPerpIntent(f.ctx, batchkeeper.PerpOrder{
		Sender: sender.String(), MarketID: marketID, Side: side, Qty: math.NewInt(qty), LimitPrice: limit,
		ExpiryHeight: f.ctx.BlockHeight() + 100, ReduceOnly: reduceOnly, ChargeFee: true,
	})
	require.NoError(t, err)
	return id
}

// endBlocks runs the batch and perp EndBlockers (module order) at the given height.
func (f *fixture) endBlocks(t *testing.T, height int64) {
	t.Helper()
	f.at(height)
	require.NoError(t, f.bk.EndBlocker(f.ctx))
	require.NoError(t, f.k.EndBlocker(f.ctx))
	if msg, broken := f.k.CheckInvariants(f.ctx); broken {
		t.Fatalf("invariant broken at height %d: %s", height, msg)
	}
}

// trade opens a long for f.long and a short for f.short of qty at price
// through a full batch cycle.
func (f *fixture) trade(t *testing.T, qty int64, price math.LegacyDec) {
	t.Helper()
	batch := f.ctx.BlockHeight()
	f.submit(t, f.long, batchtypes.SIDE_BUY, qty, price, false)
	f.submit(t, f.short, batchtypes.SIDE_SELL, qty, price, false)
	f.endBlocks(t, batch+f.bp.CommitWindow+1)
}

func TestOpenPositionsThroughTheAuction(t *testing.T) {
	f := setup(t)
	price := math.LegacyNewDec(60_000)
	// 1,000 base units at 60,000 → notional 60,000,000 (60 USD); IM at 3x = 20 USD
	f.trade(t, 1_000, price)

	long, ok, err := f.k.GetPosition(f.ctx, f.long.String(), marketID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, batchtypes.SIDE_BUY, long.Side)
	require.Equal(t, "1000", long.Qty.String())
	require.Equal(t, "20000000", long.Collateral.String())
	require.Equal(t, price.String(), long.EntryPrice.String())

	short, ok, err := f.k.GetPosition(f.ctx, f.short.String(), marketID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, batchtypes.SIDE_SELL, short.Side)

	// free collateral: 100 − 20 IM − 5 bps fee (30,000)
	free, err := f.k.FreeCollateral(f.ctx, f.long.String())
	require.NoError(t, err)
	require.Equal(t, "79970000", free.String())
	// reservations released
	reserved, err := f.k.ReservedTotal(f.ctx, f.long.String())
	require.NoError(t, err)
	require.True(t, reserved.IsZero())
	// fees to the insurance fund (below target) and revenue
	l, err := f.k.GetLedger(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "60000", l.Insurance.Add(l.Revenue).String())
	require.True(t, l.Insurance.IsPositive())

	m := f.market(t)
	require.Equal(t, "1000", m.OiLong.String())
	require.Equal(t, "1000", m.OiShort.String())
	require.Equal(t, price.String(), m.MarkPrice.String())

	// derived view
	view := f.k.View(f.ctx, m, long)
	require.Equal(t, "20000000", view.Equity.String())
	require.True(t, view.LiquidationPrice.LT(price))
}

func TestReduceOnlyClosesWithRealizedPnl(t *testing.T) {
	f := setup(t)
	f.trade(t, 1_000, math.LegacyNewDec(60_000))

	// price rises 10%: the long closes with a reduce-only sell, the short
	// buys back (reduce-only) at the new price
	f.at(f.ctx.BlockHeight() + 1)
	f.setPrice(t, math.LegacyNewDec(66_000), math.LegacyZeroDec())
	f.endBlocks(t, f.ctx.BlockHeight())
	batch := f.ctx.BlockHeight()
	f.submit(t, f.long, batchtypes.SIDE_SELL, 1_000, math.LegacyNewDec(66_000), true)
	f.submit(t, f.short, batchtypes.SIDE_BUY, 1_000, math.LegacyNewDec(66_000), true)
	f.endBlocks(t, batch+f.bp.CommitWindow+1)

	_, ok, err := f.k.GetPosition(f.ctx, f.long.String(), marketID)
	require.NoError(t, err)
	require.False(t, ok)
	free, err := f.k.FreeCollateral(f.ctx, f.long.String())
	require.NoError(t, err)
	// 100 − fees (30,000 + 33,000) + pnl 6,000,000
	require.Equal(t, "105937000", free.String())
	freeShort, err := f.k.FreeCollateral(f.ctx, f.short.String())
	require.NoError(t, err)
	require.Equal(t, "93937000", freeShort.String())
	m := f.market(t)
	require.True(t, m.OiLong.IsZero() && m.OiShort.IsZero())
}

func TestReserveRejectsWithoutCollateralAndInReduceOnlyState(t *testing.T) {
	f := setup(t)
	poor := sdk.AccAddress([]byte("perp-poor------------"))
	_, _, err := f.bk.SubmitPerpIntent(f.ctx, batchkeeper.PerpOrder{Sender: poor.String(), MarketID: marketID, Side: batchtypes.SIDE_BUY,
		Qty: math.NewInt(1_000), LimitPrice: math.LegacyNewDec(60_000), ExpiryHeight: f.ctx.BlockHeight() + 10})
	require.ErrorIs(t, err, types.ErrInsufficientFree)

	// high dispersion → reduce-only: non reduce-only orders refused
	f.setPrice(t, math.LegacyNewDec(60_000), math.LegacyNewDecWithPrec(2, 2))
	f.endBlocks(t, f.ctx.BlockHeight())
	require.Equal(t, types.ORACLE_STATE_REDUCE_ONLY, f.market(t).State)
	_, _, err = f.bk.SubmitPerpIntent(f.ctx, batchkeeper.PerpOrder{Sender: f.long.String(), MarketID: marketID, Side: batchtypes.SIDE_BUY,
		Qty: math.NewInt(1_000), LimitPrice: math.LegacyNewDec(60_000), ExpiryHeight: f.ctx.BlockHeight() + 10})
	require.ErrorIs(t, err, types.ErrReduceOnly)

	// stale for two periods → paused; restricted halves the leverage in force
	f.at(f.ctx.BlockHeight() + 3*int64(f.app.OracleKeeper.VotePeriod(f.ctx)))
	f.endBlocks(t, f.ctx.BlockHeight())
	require.Equal(t, types.ORACLE_STATE_PAUSED, f.market(t).State)
}

func TestLiquidationTransfersToInsuranceFund(t *testing.T) {
	f := setup(t)
	f.trade(t, 1_000, math.LegacyNewDec(60_000))
	// long: collateral 20 USD on 60 USD notional (3x), MM = 1/6:
	// liquidation at (60,000·1000 − 20,000,000) / (1000·(1 − 1/6)) = 48,000
	long, _, _ := f.k.GetPosition(f.ctx, f.long.String(), marketID)
	m := f.market(t)
	liq := long.LiquidationPrice(m.FundingIndex)
	require.True(t, liq.Sub(math.LegacyNewDec(48_000)).Abs().LT(math.LegacyNewDecWithPrec(1, 3)), liq.String())

	before, _ := f.k.GetLedger(f.ctx)
	f.at(f.ctx.BlockHeight() + 1)
	f.setPrice(t, math.LegacyNewDec(45_000), math.LegacyZeroDec())
	f.endBlocks(t, f.ctx.BlockHeight())

	_, ok, err := f.k.GetPosition(f.ctx, f.long.String(), marketID)
	require.NoError(t, err)
	require.False(t, ok, "long liquidated")
	fund, ok, err := f.k.GetPosition(f.ctx, types.InsuranceFundAddress(), marketID)
	require.NoError(t, err)
	require.True(t, ok, "fund holds the inventory")
	require.Equal(t, batchtypes.SIDE_BUY, fund.Side)
	require.Equal(t, "1000", fund.Qty.String())
	after, _ := f.k.GetLedger(f.ctx)
	// penalty 1% of 45,000,000 = 450,000 credited
	require.Equal(t, before.Insurance.AddRaw(450_000).String(), after.Insurance.String())
	// the fund placed its unwind order
	id, err := f.k.UnwindIntents.Get(f.ctx, marketID)
	require.NoError(t, err)
	in, err := f.bk.GetIntent(f.ctx, id)
	require.NoError(t, err)
	require.True(t, in.ReduceOnly)
	require.Equal(t, batchtypes.SIDE_SELL, in.Side)
	// the short is untouched
	_, ok, _ = f.k.GetPosition(f.ctx, f.short.String(), marketID)
	require.True(t, ok)
}

func TestAutoTopUpRescues(t *testing.T) {
	f := setup(t)
	require.NoError(t, f.k.AutoTopUp.Set(f.ctx, f.long.String()))
	f.trade(t, 1_000, math.LegacyNewDec(60_000))
	f.at(f.ctx.BlockHeight() + 1)
	f.setPrice(t, math.LegacyNewDec(45_000), math.LegacyZeroDec())
	f.endBlocks(t, f.ctx.BlockHeight())
	long, ok, err := f.k.GetPosition(f.ctx, f.long.String(), marketID)
	require.NoError(t, err)
	require.True(t, ok, "rescued by the top-up")
	require.True(t, long.Collateral.GT(math.NewInt(20_000_000)))
}

func TestADLWhenFundInventoryCapped(t *testing.T) {
	f := setup(t)
	p := f.params
	p.IfMaxInventory = math.LegacyZeroDec() // any inventory exceeds the cap → ADL
	require.NoError(t, f.k.SetParams(f.ctx, p))
	f.trade(t, 1_000, math.LegacyNewDec(60_000))
	shortBefore, _ := f.k.FreeCollateral(f.ctx, f.short.String())

	f.at(f.ctx.BlockHeight() + 1)
	f.setPrice(t, math.LegacyNewDec(45_000), math.LegacyZeroDec())
	f.endBlocks(t, f.ctx.BlockHeight())

	_, ok, _ := f.k.GetPosition(f.ctx, f.long.String(), marketID)
	require.False(t, ok)
	_, ok, _ = f.k.GetPosition(f.ctx, f.short.String(), marketID)
	require.False(t, ok, "counterparty deleveraged")
	_, ok, _ = f.k.GetPosition(f.ctx, types.InsuranceFundAddress(), marketID)
	require.False(t, ok, "fund took nothing")
	shortAfter, _ := f.k.FreeCollateral(f.ctx, f.short.String())
	// the short realised its gain at the long's bankruptcy price (40,000): 20 USD of pnl + 20 USD margin back
	require.Equal(t, shortBefore.AddRaw(40_000_000).String(), shortAfter.String())
}

func TestTriggerFiresReduceOnlyOrder(t *testing.T) {
	f := setup(t)
	f.trade(t, 1_000, math.LegacyNewDec(60_000))
	id, err := f.k.SubmitTrigger(f.ctx, &types.MsgSubmitTriggerOrder{Sender: f.long.String(), MarketId: marketID,
		TriggerPrice: math.LegacyNewDec(58_000), FireAbove: false, Qty: math.ZeroInt(), Slippage: math.LegacyNewDecWithPrec(1, 2)})
	require.NoError(t, err)
	// not fired above the trigger
	f.endBlocks(t, f.ctx.BlockHeight()+1)
	_, err = f.k.Triggers.Get(f.ctx, id)
	require.NoError(t, err)
	// fires at 57,000 and injects a reduce-only sell at 58,000·0.99
	f.at(f.ctx.BlockHeight() + 1)
	f.setPrice(t, math.LegacyNewDec(57_000), math.LegacyZeroDec())
	f.endBlocks(t, f.ctx.BlockHeight())
	_, err = f.k.Triggers.Get(f.ctx, id)
	require.Error(t, err)
	has, err := f.bk.HasOpenIntent(f.ctx, f.long.String(), marketID)
	require.NoError(t, err)
	require.True(t, has)
}

func TestListingCheckGatesEnable(t *testing.T) {
	f := setup(t)
	m := f.market(t)
	m.Enabled = false
	m.VenuesAttested = false
	require.NoError(t, f.k.Markets.Set(f.ctx, m.Id, m))
	require.NoError(t, f.bk.SetMarketEnabled(f.ctx, marketID, false))
	err := f.k.EnableMarket(f.ctx, marketID, true)
	require.ErrorIs(t, err, types.ErrListingCriteria)
	m.VenuesAttested = true
	require.NoError(t, f.k.Markets.Set(f.ctx, m.Id, m))
	// insurance below target: fund it
	target, err := f.k.InsuranceTarget(f.ctx, m)
	require.NoError(t, err)
	l, _ := f.k.GetLedger(f.ctx)
	l.Insurance = target
	require.NoError(t, f.k.Ledger.Set(f.ctx, l))
	require.NoError(t, f.k.EnableMarket(f.ctx, marketID, true))
	require.True(t, f.market(t).Enabled)
	check, err := f.k.ListingCheck(f.ctx, f.market(t))
	require.NoError(t, err)
	require.True(t, check.Listable)
}

func TestFundingAppliesLazily(t *testing.T) {
	f := setup(t)
	p := f.params
	p.FundingInterval = 1
	require.NoError(t, f.k.SetParams(f.ctx, p))
	f.trade(t, 1_000, math.LegacyNewDec(60_600)) // clears 1% above P_ref → positive premium
	m := f.market(t)
	require.True(t, m.FundingIndex.IsPositive(), m.FundingIndex.String())
	long, _, _ := f.k.GetPosition(f.ctx, f.long.String(), marketID)
	require.True(t, long.FundingOwed(m.FundingIndex).IsPositive())
	short, _, _ := f.k.GetPosition(f.ctx, f.short.String(), marketID)
	require.True(t, short.FundingOwed(m.FundingIndex).IsNegative())
}

func TestGenesisRoundTrip(t *testing.T) {
	f := setup(t)
	f.trade(t, 1_000, math.LegacyNewDec(60_000))
	gs, err := f.k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, gs.Markets, 1)
	require.Len(t, gs.Positions, 2)
	require.Len(t, gs.Collateral, 2)
	require.NoError(t, gs.Validate())
}

func TestInsuranceUnwindFillsAgainstUsers(t *testing.T) {
	f := setup(t)
	f.trade(t, 1_000, math.LegacyNewDec(60_000))
	f.at(f.ctx.BlockHeight() + 1)
	f.setPrice(t, math.LegacyNewDec(45_000), math.LegacyZeroDec())
	f.endBlocks(t, f.ctx.BlockHeight()) // liquidates the long into the fund and places the unwind sell
	fund, ok, _ := f.k.GetPosition(f.ctx, types.InsuranceFundAddress(), marketID)
	require.True(t, ok)
	id, err := f.k.UnwindIntents.Get(f.ctx, marketID)
	require.NoError(t, err)
	unwind, err := f.bk.GetIntent(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, uint64(f.ctx.BlockHeight()), uint64(unwind.CreatedHeight))
	before, _ := f.k.GetLedger(f.ctx)

	// the short buys back the fund's inventory in the same batch: reduce-only buy at the ref price
	batch := f.ctx.BlockHeight()
	f.submit(t, f.short, batchtypes.SIDE_BUY, unwind.Remaining.Int64(), math.LegacyNewDec(45_000), true)
	f.endBlocks(t, batch+f.bp.CommitWindow+1)

	after, ok, _ := f.k.GetPosition(f.ctx, types.InsuranceFundAddress(), marketID)
	if ok {
		require.True(t, after.Qty.LT(fund.Qty), "inventory reduced")
	}
	l, _ := f.k.GetLedger(f.ctx)
	require.False(t, l.Insurance.Equal(before.Insurance), "the fund settled its inventory")
	_, err = f.k.UnwindIntents.Get(f.ctx, marketID)
	require.NoError(t, err) // next unwind order already placed by the EndBlock
}

func TestAllocationCascadeAndBuyback(t *testing.T) {
	f := setup(t)
	// spot market LUNC/USD for the buyback, priced by the oracle uusd rate
	f.app.OracleKeeper.SetLunaExchangeRate(f.ctx, "uusd", math.LegacyNewDecWithPrec(1, 4))
	require.NoError(t, f.bk.CreateMarket(f.ctx, batchtypes.Market{
		Id: "uluna/uusd", BaseDenom: "uluna", QuoteDenom: "uusd", Type: batchtypes.MARKET_TYPE_SPOT, OracleDenom: "uusd",
		Enabled: true, MinQty: math.NewInt(1_000_000), TickSize: math.LegacyNewDecWithPrec(1, 6),
	}))
	p := f.params
	p.AllocEpochBlocks = 1
	p.SpotMarketId = "uluna/uusd"
	p.BurnBuyCap = math.NewInt(1_000_000_000)
	p.OpexCap = math.NewInt(5_000)
	require.NoError(t, f.k.SetParams(f.ctx, p))
	// the fund is at its target so fees become revenue
	target, err := f.k.InsuranceTarget(f.ctx)
	require.NoError(t, err)
	l, _ := f.k.GetLedger(f.ctx)
	l.Insurance = target
	require.NoError(t, f.k.Ledger.Set(f.ctx, l))
	// mint the fund's settlement into the module so the ledger matches the bank
	coins := sdk.NewCoins(sdk.NewCoin("uusd", target))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", coins))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToModule(f.ctx, "mint", types.ModuleName, coins))

	f.trade(t, 1_000, math.LegacyNewDec(60_000)) // 2 × 30,000 of fees → revenue
	recs, err := f.k.AllocationRecords(f.ctx, 10)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	rec := recs[0]
	require.Equal(t, "60000", rec.Revenue.String())
	require.True(t, rec.ToInsurance.IsZero())
	require.Equal(t, "5000", rec.ToOpex.String())
	// surplus 55,000: 50% oracle pool, 20% community pool, 30% buyback budget
	require.Equal(t, "27500", rec.ToOraclePool.String())
	require.Equal(t, "11000", rec.ToCommunityPool.String())
	require.Equal(t, "16500", rec.ToBurnBudget.String())
	oraclePool := f.app.BankKeeper.GetBalance(f.ctx, f.app.AccountKeeper.GetModuleAddress("oracle"), "uusd")
	require.Equal(t, "27500", oraclePool.Amount.String())

	// the buyback placed a buy intent of the budget in the spot market
	l, _ = f.k.GetLedger(f.ctx)
	require.NotZero(t, l.BuybackIntentId)
	require.True(t, l.BurnBudget.IsZero())
	in, err := f.bk.GetIntent(f.ctx, l.BuybackIntentId)
	require.NoError(t, err)
	require.Equal(t, "16500uusd", in.AmountIn.String())

	// a seller of LUNC meets it; the uluna received is burned at the next EndBlock
	seller := sdk.AccAddress([]byte("perp-luna-seller-----"))
	lunas := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(1_000_000_000_000)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", lunas))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", seller, lunas))
	batch := f.ctx.BlockHeight()
	_, _, err = f.bk.SubmitIntent(f.ctx, &batchtypes.MsgSubmitIntent{Sender: seller.String(), MarketId: "uluna/uusd", Side: batchtypes.SIDE_SELL,
		AmountIn: sdk.NewCoin("uluna", math.NewInt(200_000_000)), LimitPrice: math.LegacyNewDecWithPrec(1, 4), MinOut: math.ZeroInt(), ExpiryHeight: batch + 50})
	require.NoError(t, err)
	burnBefore := f.app.BankKeeper.GetBalance(f.ctx, f.app.AccountKeeper.GetModuleAddress("burn"), "uluna").Amount
	f.endBlocks(t, batch+f.bp.CommitWindow+1)
	burnAfter := f.app.BankKeeper.GetBalance(f.ctx, f.app.AccountKeeper.GetModuleAddress("burn"), "uluna").Amount
	require.True(t, burnAfter.GT(burnBefore), "bought-back uluna sent to the burn account")
	l, _ = f.k.GetLedger(f.ctx)
	require.True(t, l.BurnedEpoch.IsPositive() || l.Epoch > rec.Epoch+1)
}

func TestFundInsuranceAndLeverageFloor(t *testing.T) {
	f := setup(t)
	before, _ := f.k.GetLedger(f.ctx)
	require.NoError(t, f.k.FundInsurance(f.ctx, f.long, sdk.NewCoin("uusd", math.NewInt(1_000_000))))
	after, _ := f.k.GetLedger(f.ctx)
	require.Equal(t, before.Insurance.AddRaw(1_000_000).String(), after.Insurance.String())
	require.Error(t, f.k.FundInsurance(f.ctx, f.long, sdk.NewCoin("uluna", math.NewInt(1))))
	if msg, broken := f.k.CheckInvariants(f.ctx); broken {
		t.Fatal(msg)
	}

	// with the fund far below target the leverage target is floored at 1x,
	// and the effective leverage steps down 10% per vote period towards it
	m := f.market(t)
	vp := int64(f.app.OracleKeeper.VotePeriod(f.ctx))
	for i := int64(1); i <= 30; i++ {
		h := f.ctx.BlockHeight() + 1
		f.at(h)
		if h%vp == 0 {
			f.setPrice(t, math.LegacyNewDec(60_000), math.LegacyZeroDec())
		}
		require.NoError(t, f.k.EndBlocker(f.ctx))
	}
	m2 := f.market(t)
	require.True(t, m2.EffectiveLeverage.LT(m.EffectiveLeverage), "leverage stepped down")
	require.True(t, m2.EffectiveLeverage.GTE(math.LegacyOneDec()), m2.EffectiveLeverage.String())
	require.True(t, m2.EffectiveOiCap.LT(m.EffectiveOiCap), "OI cap stepped down towards IF/IF_target")
}
