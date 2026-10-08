package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	terracore "github.com/classic-terra/core/v4/types"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/perp/keeper"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Guards of the allocation cascade (spec §23.1 rule 1, §26.3): nothing is
// bought back or burned while IF < IF_target, the burn budget and returned
// buyback escrow go to the fund first, and the fee split at fill time uses
// the target of the current block.

// lunaUsdSpotMarket is the LUNC/USD spot market id the perp keeper tests
// register for the buyback and the stLUNC tranche sales.
const lunaUsdSpotMarket = "uluna/uusdc.lf"

// buybackSetup registers the LUNC/USD spot market used by the buyback and
// enables the buyback (the allocation epoch stays long so it does not run).
func (f *fixture) buybackSetup(t *testing.T) {
	t.Helper()
	f.app.OracleKeeper.SetLunaExchangeRate(f.ctx, "uusd", math.LegacyNewDecWithPrec(1, 4))
	require.NoError(t, f.bk.CreateMarket(f.ctx, batchtypes.Market{
		Id: lunaUsdSpotMarket, BaseDenom: terracore.MicroLunaDenom, QuoteDenom: types.DefaultSettlementDenom, Type: batchtypes.MARKET_TYPE_SPOT, OracleDenom: terracore.MicroUSDDenom,
		Enabled: true, MinQty: math.NewInt(1_000_000), TickSize: math.LegacyNewDecWithPrec(1, 6),
	}))
	p := f.params
	p.SpotMarketId = lunaUsdSpotMarket
	p.BurnBuyCap = math.NewInt(1_000_000_000)
	require.NoError(t, f.k.SetParams(f.ctx, p))
	f.params = p
}

// mintToModule credits coins to the perp module account (bank side of a
// ledger balance set directly by the test).
func (f *fixture) mintToModule(t *testing.T, coins sdk.Coins) {
	t.Helper()
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", coins))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToModule(f.ctx, "mint", types.ModuleName, coins))
}

// setLedger sets the fund and the burn budget and backs them with settlement.
func (f *fixture) setLedger(t *testing.T, insurance, burnBudget math.Int) {
	t.Helper()
	l, err := f.k.GetLedger(f.ctx)
	require.NoError(t, err)
	add := insurance.Sub(l.Insurance).Add(burnBudget.Sub(l.BurnBudget))
	l.Insurance, l.BurnBudget = insurance, burnBudget
	require.NoError(t, f.k.Ledger.Set(f.ctx, l))
	if add.IsPositive() {
		f.mintToModule(t, sdk.NewCoins(sdk.NewCoin("uusdc.lf", add)))
	}
}

// perpEndBlock runs the perp EndBlocker at the next height with a fresh
// event manager and checks the module invariants.
func (f *fixture) perpEndBlock(t *testing.T) {
	t.Helper()
	f.at(f.ctx.BlockHeight() + 1)
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.k.EndBlocker(f.ctx))
	if msg, broken := f.k.CheckInvariants(f.ctx); broken {
		t.Fatalf("invariant broken: %s", msg)
	}
}

func (f *fixture) skippedEvents() []sdk.Event {
	var out []sdk.Event
	for _, e := range f.ctx.EventManager().Events() {
		if e.Type == "terra.perp.v1.EventBuybackSkipped" {
			out = append(out, e)
		}
	}
	return out
}

func attr(e sdk.Event, key string) string {
	for _, a := range e.Attributes {
		if a.Key == key {
			return a.Value
		}
	}
	return ""
}

func (f *fixture) raiseTarget(t *testing.T) {
	t.Helper()
	m := f.market(t)
	m.OiCap, m.EffectiveOiCap = m.OiCap.MulRaw(2), m.EffectiveOiCap.MulRaw(2)
	require.NoError(t, f.k.Markets.Set(f.ctx, m.Id, m))
}

func (f *fixture) burned(t *testing.T) math.Int {
	t.Helper()
	return f.app.BankKeeper.GetBalance(f.ctx, f.app.AccountKeeper.GetModuleAddress("burn"), "uluna").Amount
}

func (f *fixture) moduleUluna(t *testing.T) math.Int {
	t.Helper()
	return f.app.BankKeeper.GetBalance(f.ctx, f.k.ModuleAddress(), "uluna").Amount
}

// F-13: with the fund below target the buyback neither buys nor burns; the
// burn budget goes to the fund up to the gap and bought uluna is held until
// the fund is back at its target.
func TestBuybackSkippedWhileInsuranceBelowTarget(t *testing.T) {
	f := setup(t)
	f.buybackSetup(t)
	target, err := f.k.InsuranceTarget(f.ctx)
	require.NoError(t, err)
	require.True(t, target.GT(math.NewInt(100_000)))
	f.setLedger(t, target.SubRaw(100_000), math.NewInt(30_000))
	f.mintToModule(t, sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(5_000_000)))) // bought, not yet burned
	burnBefore := f.burned(t)

	f.perpEndBlock(t)
	l, err := f.k.GetLedger(f.ctx)
	require.NoError(t, err)
	require.Zero(t, l.BuybackIntentId, "no buyback order while IF < IF_target")
	require.True(t, l.BurnBudget.IsZero(), "burn budget routed to the fund: %s", l.BurnBudget)
	require.Equal(t, target.SubRaw(70_000).String(), l.Insurance.String())
	require.Equal(t, burnBefore.String(), f.burned(t).String(), "nothing burned while IF < IF_target")
	require.Equal(t, "5000000", f.moduleUluna(t).String(), "bought uluna held")
	ev := f.skippedEvents()
	require.Len(t, ev, 1)
	require.Contains(t, attr(ev[0], "reason"), "insurance fund below target")
	require.Equal(t, `"30000"`, attr(ev[0], "routed_to_insurance"))
	require.Equal(t, `"5000000"`, attr(ev[0], "held_uluna"))

	// a quiet block below target emits nothing more and still burns nothing
	f.perpEndBlock(t)
	require.Empty(t, f.skippedEvents())
	require.Equal(t, burnBefore.String(), f.burned(t).String())

	// the fund is refilled: the held uluna is burned at the next EndBlock
	require.NoError(t, f.k.FundInsurance(f.ctx, f.long, sdk.NewCoin("uusdc.lf", math.NewInt(70_000))))
	f.perpEndBlock(t)
	require.Equal(t, burnBefore.AddRaw(5_000_000).String(), f.burned(t).String())
	require.True(t, f.moduleUluna(t).IsZero())
	l, _ = f.k.GetLedger(f.ctx)
	require.Zero(t, l.BuybackIntentId)
}

// F-13: part of the burn budget fills the gap and the rest is bought back
// once the fund reached its target in the same step.
func TestBuybackRoutesOnlyTheGapThenBuysBack(t *testing.T) {
	f := setup(t)
	f.buybackSetup(t)
	target, err := f.k.InsuranceTarget(f.ctx)
	require.NoError(t, err)
	f.setLedger(t, target.SubRaw(4_000), math.NewInt(20_500))
	f.perpEndBlock(t)
	l, _ := f.k.GetLedger(f.ctx)
	require.Equal(t, target.String(), l.Insurance.String(), "gap filled first")
	require.NotZero(t, l.BuybackIntentId, "surplus bought back")
	in, err := f.bk.GetIntent(f.ctx, l.BuybackIntentId)
	require.NoError(t, err)
	require.Equal(t, "16500uusdc.lf", in.AmountIn.String())
	ev := f.skippedEvents()
	require.Len(t, ev, 1)
	require.Equal(t, `"4000"`, attr(ev[0], "routed_to_insurance"))
}

// F-13: an open buyback intent is withdrawn when the fund falls below its
// target, and the returned escrow goes to the fund, not to the burn budget.
func TestBuybackIntentCancelledWhenFundFallsBelowTarget(t *testing.T) {
	f := setup(t)
	f.buybackSetup(t)
	target, err := f.k.InsuranceTarget(f.ctx)
	require.NoError(t, err)
	f.setLedger(t, target, math.NewInt(16_500))
	burnBefore := f.burned(t)
	f.perpEndBlock(t)
	l, _ := f.k.GetLedger(f.ctx)
	id := l.BuybackIntentId
	require.NotZero(t, id)
	require.True(t, l.BurnBudget.IsZero())

	f.raiseTarget(t) // IF_target doubles: the fund is now below it
	f.perpEndBlock(t)
	_, err = f.bk.GetIntent(f.ctx, id)
	require.Error(t, err, "buyback intent withdrawn")
	l, _ = f.k.GetLedger(f.ctx)
	require.Zero(t, l.BuybackIntentId)
	require.True(t, l.BurnBudget.IsZero(), "refund not returned to the burn budget: %s", l.BurnBudget)
	require.Equal(t, target.AddRaw(16_500).String(), l.Insurance.String(), "refund routed to the fund")
	require.Equal(t, burnBefore.String(), f.burned(t).String())
	ev := f.skippedEvents()
	require.Len(t, ev, 1)
	require.Equal(t, `"16500"`, attr(ev[0], "routed_to_insurance"))
	require.Equal(t, `"`+uintStr(id)+`"`, attr(ev[0], "cancelled_intent_id"))
}

func uintStr(v uint64) string { return math.NewIntFromUint64(v).String() }

// Suspected bug (perps): the fee split at fill time must use the IF_target of
// the current block. The per-block memo of the target could survive into a
// later block (the stLUNC valuation memo moved its height without clearing
// the target), so a fill right after the target rose sent 100% to revenue.
func TestFeeSplitUsesTheCurrentInsuranceTarget(t *testing.T) {
	f := setup(t)
	target, err := f.k.InsuranceTarget(f.ctx)
	require.NoError(t, err)
	f.setLedger(t, target, math.ZeroInt())
	f.trade(t, 1_000, math.LegacyNewDec(60_000)) // fund at target: 2 × 30,000 → revenue
	l, _ := f.k.GetLedger(f.ctx)
	require.Equal(t, target.String(), l.Insurance.String())
	require.Equal(t, "60000", l.Revenue.String())

	f.raiseTarget(t)
	f.at(f.ctx.BlockHeight() + 1)
	require.NoError(t, f.k.EndBlocker(f.ctx)) // EndBlock paths touch the valuation memo
	f.trade(t, 1_000, math.LegacyNewDec(60_000))
	l, _ = f.k.GetLedger(f.ctx)
	require.Equal(t, target.AddRaw(30_000).String(), l.Insurance.String(), "50%% of the fees to the fund while IF < IF_target")
	require.Equal(t, "90000", l.Revenue.String())
}

// Suspected bug (perps): a fund inventory below min_qty is unwound by one
// final reduce-only order instead of staying forever.
func TestInsuranceResidualBelowMinQtyIsUnwound(t *testing.T) {
	f := setup(t)
	f.trade(t, 1_541, math.LegacyNewDec(60_000))
	f.at(f.ctx.BlockHeight() + 1)
	f.setPrice(t, math.LegacyNewDec(45_000), math.LegacyZeroDec())
	f.endBlocks(t, f.ctx.BlockHeight()) // the long goes to the fund; unwind sell of min_qty
	fund, ok, _ := f.k.GetPosition(f.ctx, types.InsuranceFundAddress(), marketID)
	require.True(t, ok)
	require.Equal(t, "1541", fund.Qty.String())
	id, err := f.k.UnwindIntents.Get(f.ctx, marketID)
	require.NoError(t, err)
	unwind, err := f.bk.GetIntent(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, "1000", unwind.Remaining.String())

	// the short buys 1,000 back: the fund keeps a 541 residual below min_qty
	batch := f.ctx.BlockHeight()
	f.submit(t, f.short, batchtypes.SIDE_BUY, 1_000, math.LegacyNewDec(45_000), true)
	f.endBlocks(t, batch+f.bp.CommitWindow+1)
	fund, ok, _ = f.k.GetPosition(f.ctx, types.InsuranceFundAddress(), marketID)
	require.True(t, ok)
	require.Equal(t, "541", fund.Qty.String())

	// the final unwind order carries the whole residual
	id, err = f.k.UnwindIntents.Get(f.ctx, marketID)
	require.NoError(t, err, "final unwind order placed")
	unwind, err = f.bk.GetIntent(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, "541", unwind.Remaining.String())
	require.True(t, unwind.ReduceOnly)

	// the short closes with a reduce-only buy of min_qty: both sides flat
	batch = f.ctx.BlockHeight()
	f.submit(t, f.short, batchtypes.SIDE_BUY, 1_000, math.LegacyNewDec(45_000), true)
	f.endBlocks(t, batch+f.bp.CommitWindow+1)
	_, ok, _ = f.k.GetPosition(f.ctx, types.InsuranceFundAddress(), marketID)
	require.False(t, ok, "fund inventory fully unwound")
	_, ok, _ = f.k.GetPosition(f.ctx, f.short.String(), marketID)
	require.False(t, ok, "counterparty closed")
	m := f.market(t)
	require.True(t, m.OiLong.IsZero() && m.OiShort.IsZero(), "oi %s/%s", m.OiLong, m.OiShort)
}

// Users still cannot place orders below min_qty: the exemption is only for
// the fund's final reduce-only unwind.
func TestBelowMinQtyStillRejectedForUsers(t *testing.T) {
	f := setup(t)
	f.trade(t, 1_000, math.LegacyNewDec(60_000))
	_, err := keeper.NewMsgServerImpl(f.k).SubmitPerpIntent(f.ctx, &types.MsgSubmitPerpIntent{
		Sender: f.short.String(), MarketId: marketID, Side: batchtypes.SIDE_BUY, Qty: math.NewInt(541),
		LimitPrice: math.LegacyNewDec(60_000), ExpiryHeight: f.ctx.BlockHeight() + 100, ReduceOnly: true,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "below the market minimum")
}
