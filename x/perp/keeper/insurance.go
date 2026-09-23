package keeper

import (
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	batchkeeper "github.com/classic-terra/core/v4/x/batch/keeper"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// unwindInsurance places one reduce-only order per market for the fund's
// inventory, up to if_unwind_per_batch of it per batch (spec §18.3 step 3),
// priced at the edge of the oracle band so it clears whenever there is
// opposite volume.
func (k Keeper) unwindInsurance(ctx sdk.Context, params types.Params, m types.Market) error {
	if !m.Enabled || m.State == types.ORACLE_STATE_PAUSED || !m.MarkPrice.IsPositive() {
		return nil
	}
	fund := types.InsuranceFundAddress()
	pos, exists, err := k.GetPosition(ctx, fund, m.Id)
	if err != nil || !exists {
		return err
	}
	if id, err := k.UnwindIntents.Get(ctx, m.Id); err == nil {
		if _, err := k.batchKeeper.GetIntent(ctx, id); err == nil {
			return nil // still open
		}
		if err := k.UnwindIntents.Remove(ctx, m.Id); err != nil {
			return err
		}
	} else if !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	qty := params.IfUnwindPerBatch.MulInt(pos.Qty).TruncateInt()
	if qty.LT(m.MinQty) {
		qty = math.MinInt(m.MinQty, pos.Qty)
	}
	if qty.LT(m.MinQty) {
		return nil
	}
	bp, err := k.batchKeeper.GetParams(ctx)
	if err != nil {
		return err
	}
	side, limit := batchtypes.SIDE_SELL, m.RefPrice.Mul(math.LegacyOneDec().Sub(bp.PriceBand))
	if pos.Side == batchtypes.SIDE_SELL {
		side, limit = batchtypes.SIDE_BUY, m.RefPrice.Mul(math.LegacyOneDec().Add(bp.PriceBand))
	}
	limit = roundToTick(limit, m.TickSize, side == batchtypes.SIDE_SELL)
	id, _, err := k.batchKeeper.SubmitPerpIntent(ctx, batchkeeper.PerpOrder{
		Sender: fund, MarketID: m.Id, Side: side, Qty: qty, LimitPrice: limit,
		ExpiryHeight: ctx.BlockHeight() + bp.CommitWindow + 3, ReduceOnly: true,
	})
	if err != nil {
		k.Logger(ctx).Error("insurance unwind order rejected", "market", m.Id, "err", err)
		return nil
	}
	if err := k.UnwindIntents.Set(ctx, m.Id, id); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventInsuranceUnwind{MarketId: m.Id, Side: side.String(), Qty: qty.String(), IntentId: id})
}

// roundToTick rounds a price to the market tick: down for sells (more
// aggressive), up for buys.
func roundToTick(price, tick math.LegacyDec, down bool) math.LegacyDec {
	if !tick.IsPositive() {
		return price
	}
	ticks := price.Quo(tick)
	if down {
		return ticks.TruncateDec().Mul(tick)
	}
	return ticks.Ceil().Mul(tick)
}
