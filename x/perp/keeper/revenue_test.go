package keeper_test

import (
	"strconv"
	"strings"
	"testing"

	"cosmossdk.io/math"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/perp/keeper"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Protocol revenue received in uluna (spec §23 table + §23.1, §24.6): spot
// fees charged in LUNC and the x/liquidstake fee remainder enter the
// allocation cascade, held in kind and sold for settlement in the internal
// spot market; never burned, never part of IF, never sent to the community
// pool directly.

// depositUlunaFee hands `amount` uluna to the perp FeeSink as x/batch does
// with a spot fee charged on the received LUNC.
func (f *fixture) depositUlunaFee(t *testing.T, amount int64) {
	t.Helper()
	coins := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(amount)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", coins))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToModule(f.ctx, "mint", batchtypes.ModuleName, coins))
	require.NoError(t, keeper.NewFeeSink(f.k).Deposit(f.ctx, batchtypes.ModuleName, coins))
}

func (f *fixture) communityPoolUluna(t *testing.T) math.LegacyDec {
	t.Helper()
	fp, err := f.app.DistrKeeper.FeePool.Get(f.ctx)
	require.NoError(t, err)
	return fp.CommunityPool.AmountOf("uluna")
}

func (f *fixture) eventsOf(typ string) []sdk.Event {
	var out []sdk.Event
	for _, e := range f.ctx.EventManager().Events() {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

// buyLuna submits a user buy of LUNC in the internal spot market that
// crosses the revenue sale, and runs the auction and the settlement step.
func (f *fixture) buyLuna(t *testing.T, quote int64) {
	t.Helper()
	batch := f.ctx.BlockHeight()
	_, _, err := f.bk.SubmitIntent(f.ctx, &batchtypes.MsgSubmitIntent{
		Sender: f.short.String(), MarketId: lunaUsdSpotMarket, Side: batchtypes.SIDE_BUY,
		AmountIn: sdk.NewCoin("uusdc.lf", math.NewInt(quote)), LimitPrice: math.LegacyNewDecWithPrec(1, 4), MinOut: math.ZeroInt(), ExpiryHeight: batch + 50,
	})
	require.NoError(t, err)
	f.endBlocks(t, batch+f.bp.CommitWindow+1)
	f.endBlocks(t, f.ctx.BlockHeight()+1) // the closed sale is settled at the next step
}

// A spot fee charged in uluna no longer bypasses the cascade: it is held in
// kind (not burned, not sent to the community pool), sold in the internal
// spot market, and the settlement proceeds join the revenue path like a
// settlement fee (fund at target: all to the epoch revenue).
func TestSpotFeeInUlunaEntersTheCascade(t *testing.T) {
	f := setup(t)
	f.buybackSetup(t)
	target, err := f.k.InsuranceTarget(f.ctx)
	require.NoError(t, err)
	f.setLedger(t, target, math.ZeroInt())
	cpBefore, burnBefore := f.communityPoolUluna(t), f.burned(t)

	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	f.depositUlunaFee(t, 50_000_000) // 50 LUNC
	ev := f.eventsOf("terra.perp.v1.EventRevenueInKind")
	require.Len(t, ev, 1)
	require.Equal(t, `"batch"`, attr(ev[0], "from"))
	l, _ := f.k.GetLedger(f.ctx)
	require.Equal(t, "50000000", l.RevenueUluna.String())
	require.Equal(t, "50000000", l.RevenueUlunaEpoch.String())
	require.Equal(t, cpBefore.String(), f.communityPoolUluna(t).String(), "no longer sent to the community pool")
	revenueBefore := l.Revenue

	// the next EndBlock offers it in the internal spot market; nothing is burned
	f.perpEndBlock(t)
	l, _ = f.k.GetLedger(f.ctx)
	require.NotZero(t, l.RevenueSellIntentId, "revenue sale placed")
	require.True(t, l.RevenueUluna.IsZero())
	require.Equal(t, "50000000", l.RevenueSellOffered.String())
	sale, err := f.bk.GetIntent(f.ctx, l.RevenueSellIntentId)
	require.NoError(t, err)
	require.Equal(t, batchtypes.SIDE_SELL, sale.Side)
	require.Equal(t, "50000000uluna", sale.AmountIn.String())
	require.Equal(t, burnBefore.String(), f.burned(t).String(), "revenue in kind is never burned")

	// a LUNC buyer meets the sale: the proceeds join the revenue (fund at target)
	f.buyLuna(t, 200_000_000)
	l, _ = f.k.GetLedger(f.ctx)
	require.Zero(t, l.RevenueSellIntentId, "sale settled")
	// the buyer's own spot fee (10 bps of the 50 LUNC it received) is new revenue in kind
	require.Equal(t, "50000", l.RevenueUluna.String())
	require.Equal(t, "50000", f.moduleUluna(t).String())
	require.Equal(t, "50000000", l.RevenueSoldEpoch.String())
	proceeds := l.Revenue.Sub(revenueBefore)
	require.True(t, proceeds.IsPositive(), "settlement proceeds joined the epoch revenue")
	// 50 LUNC at a clearing price within the band around 0.0001 USD
	require.True(t, proceeds.GTE(math.NewInt(4_900)) && proceeds.LTE(math.NewInt(5_100)), "proceeds %s", proceeds)
	require.Equal(t, target.String(), l.Insurance.String())
	require.Equal(t, burnBefore.String(), f.burned(t).String())
	require.Equal(t, cpBefore.String(), f.communityPoolUluna(t).String())
	if msg, broken := f.k.CheckInvariants(f.ctx); broken {
		t.Fatal(msg)
	}
}

// Rule 1 of §23.1 and F-13 hold for revenue received in kind: while the fund
// is below its target the proceeds go to the fund first (if_fee_share at
// once, the rest at the epoch), uluna held in kind is never burned even when
// the buyback burns the uluna it bought, and nothing reaches OP/CP/burn.
func TestRevenueInKindKeepsTheFundFirst(t *testing.T) {
	f := setup(t)
	f.buybackSetup(t)
	target, err := f.k.InsuranceTarget(f.ctx)
	require.NoError(t, err)
	f.setLedger(t, target.SubRaw(1_000_000), math.ZeroInt())
	burnBefore := f.burned(t)
	f.depositUlunaFee(t, 50_000_000)
	f.mintToModule(t, sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(3_000_000)))) // bought back earlier, held
	f.perpEndBlock(t)
	l, _ := f.k.GetLedger(f.ctx)
	require.NotZero(t, l.RevenueSellIntentId, "the sale also runs below target: its proceeds feed the fund")
	require.Equal(t, burnBefore.String(), f.burned(t).String())

	before := l.Insurance
	f.buyLuna(t, 200_000_000)
	l, _ = f.k.GetLedger(f.ctx)
	require.Zero(t, l.RevenueSellIntentId)
	gained := l.Insurance.Sub(before)
	require.True(t, gained.IsPositive(), "if_fee_share of the proceeds went to the fund")
	require.True(t, l.Insurance.LT(target))
	require.Equal(t, burnBefore.String(), f.burned(t).String(), "nothing burned below target")
	// the held buyback uluna plus the buyer's 10 bps spot fee in LUNC (revenue in kind)
	require.Equal(t, "50000", l.RevenueUluna.String())
	require.Equal(t, "3050000", f.moduleUluna(t).String())

	// the allocation epoch closes below target: everything to the fund, nothing passed on
	p := f.params
	p.AllocEpochBlocks = 1
	require.NoError(t, f.k.SetParams(f.ctx, p))
	f.perpEndBlock(t)
	f.perpEndBlock(t)
	recs, err := f.k.AllocationRecords(f.ctx, 1)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	r := recs[0]
	require.True(t, r.ToOraclePool.IsZero() && r.ToCommunityPool.IsZero() && r.ToBurnBudget.IsZero(), "nothing to OP/CP/burn while IF < target: %+v", r)
	require.Equal(t, r.Revenue.String(), r.ToInsurance.String())
	require.Equal(t, burnBefore.String(), f.burned(t).String())

	// uluna in kind is not burned when the fund is back at target either:
	// only the uluna the buyback bought is
	f.depositUlunaFee(t, 7_000_000)
	l, _ = f.k.GetLedger(f.ctx)
	require.NoError(t, f.k.FundInsurance(f.ctx, f.long, sdk.NewCoin("uusdc.lf", target.Sub(l.Insurance))))
	p.SpotMarketId = "" // no sale: the revenue stays in kind
	require.NoError(t, f.k.SetParams(f.ctx, p))
	f.perpEndBlock(t)
	require.Equal(t, burnBefore.AddRaw(3_000_000).String(), f.burned(t).String(), "held buyback uluna burned at target")
	l, _ = f.k.GetLedger(f.ctx)
	require.Equal(t, "7050000", l.RevenueUluna.String())
	require.Equal(t, "7050000", f.moduleUluna(t).String(), "revenue in kind kept")
}

// QueryAllocation exposes, per epoch, the oracle LUNC price used and the
// revenue received and sold in kind, so reports can express every step in
// LUNC as well (USDC ~ USD, spec §21.6).
func TestAllocationRecordCarriesTheLunaPrice(t *testing.T) {
	f := setup(t)
	f.buybackSetup(t) // LUNC = 0.0001 USD in the oracle
	p := f.params
	p.AllocEpochBlocks = 1
	p.SpotMarketId = "" // keep the revenue in kind
	require.NoError(t, f.k.SetParams(f.ctx, p))
	f.perpEndBlock(t) // opens the first epoch
	f.depositUlunaFee(t, 9_000_000)
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	f.at(f.ctx.BlockHeight() + 1)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	recs, err := f.k.AllocationRecords(f.ctx, 1)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	require.Equal(t, math.LegacyNewDecWithPrec(1, 4).String(), recs[0].LunaPrice.String())
	require.Equal(t, "9000000", recs[0].RevenueUlunaReceived.String())
	require.True(t, recs[0].RevenueUlunaSold.IsZero())
	ev := f.eventsOf("terra.perp.v1.EventAllocationExecuted")
	require.Len(t, ev, 1)
	require.Equal(t, `"`+math.LegacyNewDecWithPrec(1, 4).String()+`"`, attr(ev[0], "luna_price"))
	l, _ := f.k.GetLedger(f.ctx)
	require.True(t, l.RevenueUlunaEpoch.IsZero(), "epoch counters reset")
	require.Equal(t, "9000000", l.RevenueUluna.String(), "the revenue in kind carries over")

	// the query serves the same record
	res, err := keeper.NewQueryServerImpl(f.k).Allocations(f.ctx, &types.QueryAllocationsRequest{})
	require.NoError(t, err)
	require.Equal(t, recs[0].LunaPrice.String(), res.Allocations[0].LunaPrice.String())
}

// A settlement spot fee keeps its path; a denom with no conversion venue in
// the spec still funds the community pool.
func TestFeeSinkOtherDenoms(t *testing.T) {
	f := setup(t)
	cp := func(denom string) math.LegacyDec {
		fp, err := f.app.DistrKeeper.FeePool.Get(f.ctx)
		require.NoError(t, err)
		return fp.CommunityPool.AmountOf(denom)
	}
	before := cp("ukrw")
	coins := sdk.NewCoins(sdk.NewCoin("ukrw", math.NewInt(1_000)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", coins))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToModule(f.ctx, "mint", batchtypes.ModuleName, coins))
	require.NoError(t, keeper.NewFeeSink(f.k).Deposit(f.ctx, batchtypes.ModuleName, coins))
	require.Equal(t, before.Add(math.LegacyNewDec(1_000)).String(), cp("ukrw").String())
	l, _ := f.k.GetLedger(f.ctx)
	require.True(t, l.RevenueUluna.IsZero())
}

// An unfilled revenue sale expires: the uluna comes back to the revenue held
// in kind (never to the tranche or the burn), the epoch cap is released and
// the next sale offers it again.
func TestRevenueSaleRefundReturnsInKind(t *testing.T) {
	f := setup(t)
	f.buybackSetup(t)
	target, err := f.k.InsuranceTarget(f.ctx)
	require.NoError(t, err)
	f.setLedger(t, target, math.ZeroInt())
	burnBefore := f.burned(t)
	f.depositUlunaFee(t, 20_000_000)
	f.perpEndBlock(t)
	l, _ := f.k.GetLedger(f.ctx)
	id := l.RevenueSellIntentId
	require.NotZero(t, id)
	// no buyer: the intent expires in x/batch and its escrow is refunded
	for i := 0; i < int(f.bp.CommitWindow)+5; i++ {
		f.endBlocks(t, f.ctx.BlockHeight()+1)
	}
	_, err = f.bk.GetIntent(f.ctx, id)
	require.Error(t, err, "expired")
	l, _ = f.k.GetLedger(f.ctx)
	require.NotEqual(t, id, l.RevenueSellIntentId)
	require.True(t, l.TrancheUluna.IsZero(), "refund not taken by the tranche")
	inSale := math.ZeroInt()
	if l.RevenueSellIntentId != 0 {
		inSale = l.RevenueSellOffered // offered again
	}
	require.Equal(t, "20000000", l.RevenueUluna.Add(inSale).String())
	require.Equal(t, inSale.String(), l.RevenueSoldEpoch.String(), "only the open sale counts against the cap")
	require.Equal(t, burnBefore.String(), f.burned(t).String())
}

// Spec §16.2 "Slash destinado ao fundo de seguro": a solver slash goes 100% to
// the insurance fund, not through if_fee_share and the cascade — even below
// target, where a fee would only put if_fee_share (50%) in the fund.
func TestSolverSlashGoesEntirelyToTheInsuranceFund(t *testing.T) {
	f := setup(t)
	f.setLedger(t, math.ZeroInt(), math.ZeroInt()) // fund below target
	before, err := f.k.GetLedger(f.ctx)
	require.NoError(t, err)

	slash := sdk.NewCoins(sdk.NewCoin("uusdc.lf", math.NewInt(1_000_000_000)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", slash))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToModule(f.ctx, "mint", batchtypes.ModuleName, slash))
	require.NoError(t, keeper.NewFeeSink(f.k).DepositInsurance(f.ctx, batchtypes.ModuleName, slash))

	after, err := f.k.GetLedger(f.ctx)
	require.NoError(t, err)
	require.Equal(t, before.Insurance.Add(math.NewInt(1_000_000_000)).String(), after.Insurance.String(), "the whole slash is in the fund")
	require.Equal(t, before.Revenue.String(), after.Revenue.String(), "nothing of the slash enters the cascade revenue")

	// contrast: the same amount as a fee below target only puts if_fee_share in the fund
	fee := sdk.NewCoins(sdk.NewCoin("uusdc.lf", math.NewInt(1_000_000_000)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", fee))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToModule(f.ctx, "mint", batchtypes.ModuleName, fee))
	require.NoError(t, keeper.NewFeeSink(f.k).Deposit(f.ctx, batchtypes.ModuleName, fee))
	l, err := f.k.GetLedger(f.ctx)
	require.NoError(t, err)
	require.True(t, l.Revenue.GT(after.Revenue), "a fee does reach the cascade revenue")
}

// EventRevenueSale.proceeds and the amount routed are what the sale intent's
// fills paid the module (the sum of its EventIntentFilled.received), not the
// module settlement balance minus the ledger: settlement that reaches the
// module account without being booked (here a 7 USDC donation) stays out.
func TestRevenueSaleProceedsAreTheSaleFills(t *testing.T) {
	f := setup(t)
	f.buybackSetup(t)
	target, err := f.k.InsuranceTarget(f.ctx)
	require.NoError(t, err)
	f.setLedger(t, target, math.ZeroInt())
	f.depositUlunaFee(t, 50_000_000)
	f.perpEndBlock(t)
	l, _ := f.k.GetLedger(f.ctx)
	id := l.RevenueSellIntentId
	require.NotZero(t, id)
	require.True(t, l.RevenueSellProceeds.IsZero())
	revenueBefore := l.Revenue

	donation := math.NewInt(7_000_000)
	f.mintToModule(t, sdk.NewCoins(sdk.NewCoin("uusdc.lf", donation)))
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	f.buyLuna(t, 200_000_000)

	unquote := func(s string) string { return strings.Trim(s, `"`) }
	filled, fees := math.ZeroInt(), math.ZeroInt()
	for _, e := range f.eventsOf("terra.batch.v1.EventIntentFilled") {
		if unquote(attr(e, "intent_id")) != strconv.FormatUint(id, 10) {
			continue
		}
		r, ok := math.NewIntFromString(unquote(attr(e, "received")))
		require.True(t, ok)
		pf, ok := math.NewIntFromString(unquote(attr(e, "protocol_fee")))
		require.True(t, ok)
		filled, fees = filled.Add(r), fees.Add(pf)
	}
	require.True(t, filled.IsPositive(), "the sale was filled")

	var proceeds string
	for _, e := range f.eventsOf("terra.perp.v1.EventRevenueSale") {
		if unquote(attr(e, "intent_id")) == strconv.FormatUint(id, 10) && unquote(attr(e, "proceeds")) != "0" {
			proceeds = unquote(attr(e, "proceeds"))
		}
	}
	require.Equal(t, filled.String(), proceeds, "the event reports what the fills paid")
	l, _ = f.k.GetLedger(f.ctx)
	require.Zero(t, l.RevenueSellIntentId)
	require.True(t, l.RevenueSellProceeds.IsZero(), "reset for the next sale")
	// fund at target: the proceeds and the sale's settlement spot fee (plus at
	// most the quote dust of the batch) are the new revenue; the donation is not
	gained := l.Revenue.Sub(revenueBefore)
	require.True(t, gained.GTE(filled.Add(fees)), "gained %s, fills %s + fees %s", gained, filled, fees)
	require.True(t, gained.LT(filled.Add(fees).AddRaw(10)), "gained %s", gained)
	require.True(t, gained.LT(donation), "the unbooked donation is not revenue")
}
