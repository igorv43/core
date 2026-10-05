package keeper

import (
	"cosmossdk.io/math"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Protocol revenue received in kind (spec §23.1, §24.6). The allocation
// cascade works in the settlement asset, but two revenue sources arrive in
// uluna: spot fees charged on the received asset when the received asset is
// LUNC (§23 table: spot fees are protocol revenue, "50% to the fund until
// the target; the rest per §23.1"), and the x/liquidstake fee remainder
// after the direct burn (§24.6: "divided between the insurance fund and
// operations per §23.1"). The insurance fund is settlement only (§18.5,
// §21.5 rule 1), so that uluna is held in the ledger (Ledger.revenue_uluna)
// and sold for settlement in the internal LUNC/settlement spot market by
// gradual limit orders capped per allocation epoch (revenue_sell_cap) — the
// conversion venue and execution discipline the spec prescribes for every
// LUNC-to-settlement path (§21.5 rules 7-8, §23.1 burn mechanics in reverse,
// §18.3). The proceeds take the same path as a settlement spot fee
// (routeFee: if_fee_share to the fund while it is below target, the rest to
// the epoch revenue), so the cascade, its fund-first rule and the
// non-governable guard of §26.3 apply to them unchanged. Revenue held in
// kind is never burned and never counts toward the fund.
//
// One sale of the module's uluna is open at a time: the revenue sale, the
// stLUNC tranche sale and the buyback exclude each other, because each one
// books what comes back to the module account by balance difference.

// lunaDenom is the denom of LUNC, the base of the internal spot market.
const lunaDenom = "uluna"

// creditRevenueInKind books uluna already moved into the module account as
// protocol revenue held in kind.
func (k Keeper) creditRevenueInKind(ctx sdk.Context, from string, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}
	l, err := k.GetLedger(ctx)
	if err != nil {
		return err
	}
	l.RevenueUluna = l.RevenueUluna.Add(amount)
	l.RevenueUlunaEpoch = l.RevenueUlunaEpoch.Add(amount)
	if err := k.Ledger.Set(ctx, l); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventRevenueInKind{From: from, Amount: amount.String()})
}

// untrackedUluna is the module uluna not accounted for by the tranche or by
// the revenue held in kind (uluna bought back and held while the fund is
// below its target, donations, and the refund of a closed revenue sale).
func (k Keeper) untrackedUluna(ctx sdk.Context, l types.Ledger) math.Int {
	bal := k.bankKeeper.GetBalance(ctx, k.ModuleAddress(), lunaDenom).Amount
	return bal.Sub(l.TrancheUluna).Sub(l.RevenueUluna)
}

// settleRevenueSale books a closed revenue sale: the unfilled uluna comes
// back to the revenue held in kind and the settlement received takes the
// spot-fee path (routeFee). It runs before the other module pipelines of the
// block so that none of them mistakes the refund or the proceeds for its own.
func (k Keeper) settleRevenueSale(ctx sdk.Context, params types.Params) error {
	l, err := k.GetLedger(ctx)
	if err != nil {
		return err
	}
	if l.RevenueSellIntentId == 0 {
		return nil
	}
	if _, err := k.batchKeeper.GetIntent(ctx, l.RevenueSellIntentId); err == nil {
		return nil // still open
	}
	id, offered := l.RevenueSellIntentId, l.RevenueSellOffered
	refund := math.MaxInt(math.ZeroInt(), k.untrackedUluna(ctx, l).Sub(l.RevenueUlunaMark))
	refund = math.MinInt(refund, l.RevenueSellOffered)
	sold := l.RevenueSellOffered.Sub(refund)
	l.RevenueUluna = l.RevenueUluna.Add(refund)
	l.RevenueSoldEpoch = math.MaxInt(math.ZeroInt(), l.RevenueSoldEpoch.Sub(refund))
	l.RevenueSellIntentId, l.RevenueUlunaMark, l.RevenueSellOffered = 0, math.ZeroInt(), math.ZeroInt()
	expected, err := k.ledgerTotal(ctx, l)
	if err != nil {
		return err
	}
	proceeds := math.MaxInt(math.ZeroInt(), k.bankKeeper.GetBalance(ctx, k.ModuleAddress(), params.SettlementDenom).Amount.Sub(expected))
	if err := k.Ledger.Set(ctx, l); err != nil {
		return err
	}
	if err := k.routeFee(ctx, proceeds, false); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventRevenueSale{IntentId: id, Offered: offered.String(), Sold: sold.String(), Proceeds: proceeds.String()})
}

// placeRevenueSale keeps one sell intent of the revenue held in kind open in
// the internal spot market, within revenue_sell_cap per allocation epoch,
// priced at the lower edge of the oracle band so it clears whenever there is
// opposite volume (the tranche sale uses the same price, §21.5 rule 7).
func (k Keeper) placeRevenueSale(ctx sdk.Context, params types.Params) error {
	l, err := k.GetLedger(ctx)
	if err != nil {
		return err
	}
	if l.RevenueSellIntentId != 0 || l.TrancheSellIntentId != 0 || l.BuybackIntentId != 0 ||
		!l.RevenueUluna.IsPositive() || params.SpotMarketId == "" || params.RevenueSellCap.IsNil() {
		return nil
	}
	amount := math.MinInt(l.RevenueUluna, params.RevenueSellCap.Sub(l.RevenueSoldEpoch))
	if !amount.IsPositive() {
		return nil
	}
	spot, err := k.batchKeeper.GetMarket(ctx, params.SpotMarketId)
	if err != nil || !spot.Enabled || spot.BaseDenom != lunaDenom || spot.QuoteDenom != params.SettlementDenom || amount.LT(spot.MinQty) {
		return nil
	}
	pref, err := k.oracleKeeper.GetPrice(ctx, spot.OracleDenom)
	if err != nil || !pref.IsPositive() {
		return nil
	}
	bp, err := k.batchKeeper.GetParams(ctx)
	if err != nil {
		return err
	}
	limit := roundToTick(pref.Mul(math.LegacyOneDec().Sub(bp.PriceBand)), spot.TickSize, true)
	id, _, err := k.batchKeeper.SubmitIntentInternal(ctx, &batchtypes.MsgSubmitIntent{
		Sender: k.ModuleAddress().String(), MarketId: spot.Id, Side: batchtypes.SIDE_SELL,
		AmountIn: sdk.NewCoin(lunaDenom, amount), LimitPrice: limit, MinOut: math.ZeroInt(),
		ExpiryHeight: ctx.BlockHeight() + bp.CommitWindow + 3,
	})
	if err != nil {
		k.Logger(ctx).Error("revenue sale rejected", "err", err)
		return nil
	}
	l.RevenueUluna = l.RevenueUluna.Sub(amount)
	l.RevenueSoldEpoch = l.RevenueSoldEpoch.Add(amount)
	l.RevenueSellIntentId, l.RevenueSellOffered = id, amount
	l.RevenueUlunaMark = k.untrackedUluna(ctx, l)
	if err := k.Ledger.Set(ctx, l); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventRevenueSale{IntentId: id, Offered: amount.String(), Sold: "0", Proceeds: "0"})
}
