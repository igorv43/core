package keeper

import (
	"fmt"
	"strconv"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/auction"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// perpLevel is a revealed solver level of a perp market keyed like an order.
type perpLevel struct {
	solver string
	level  types.Level
}

// resolvePerpMarket resolves a perpetual market (spec §14.3, §19.4): the same
// call auction on quantity orders, settled by novation through the margin
// hook. Fills whose account no longer covers the initial margin at p* are
// excluded and the batch is re-resolved, up to max_resolution_passes. The
// margin reserved by revealed levels is always released at the end.
func (k Keeper) resolvePerpMarket(ctx sdk.Context, params types.Params, market types.Market, batch uint64) error {
	result := types.BatchResult{BatchId: batch, MarketId: market.Id, Height: ctx.BlockHeight(),
		ClearingPrice: math.LegacyZeroDec(), Volume: math.ZeroInt(), ReferencePrice: math.LegacyZeroDec()}
	commits, err := k.commitsOf(ctx, batch, market.Id)
	if err != nil {
		return err
	}
	levelByKey := map[string]perpLevel{}
	var levelOrders []auction.Order
	for _, c := range commits {
		if !c.Revealed || c.Bid == nil {
			continue
		}
		for i, l := range c.Bid.Levels {
			key := fmt.Sprintf("s:%s:%d", c.Solver, i)
			levelByKey[key] = perpLevel{solver: c.Solver, level: l}
			levelOrders = append(levelOrders, auction.Order{Key: key, Kind: auction.KindSolver, Side: l.Side, Limit: l.Price, Qty: l.Qty, SolverIndex: c.TxIndex})
		}
	}
	// whatever happens below, revealed levels give their reservation back
	released := map[string]bool{}
	defer func() {
		for key, pl := range levelByKey {
			if released[key] || k.marginHook == nil {
				continue
			}
			_ = k.marginHook.Release(ctx, sdk.MustAccAddressFromBech32(pl.solver), market, pl.level.Side, pl.level.Qty, pl.level.Price)
		}
	}()

	notExecuted := func(reason string) error {
		if err := k.Results.Set(ctx, collections.Join(batch, market.Id), result); err != nil {
			return err
		}
		return ctx.EventManager().EmitTypedEvent(&types.EventBatchNotExecuted{BatchId: batch, MarketId: market.Id, Reason: reason})
	}
	if k.marginHook == nil {
		return notExecuted("margin hook not registered")
	}
	pref, err := k.ReferencePrice(ctx, market)
	if err != nil {
		return notExecuted("reference price unavailable")
	}
	result.ReferencePrice = pref

	intents, err := k.IntentsOfMarket(ctx, market.Id, int(params.MaxIntentsPerBatch))
	if err != nil {
		return err
	}
	intentByKey := map[string]types.Intent{}
	var orders []auction.Order
	for _, in := range intents {
		if in.CreatedHeight > int64(batch) || in.ExpiryHeight <= ctx.BlockHeight() {
			continue
		}
		key := "i:" + strconv.FormatUint(in.Id, 10)
		orders = append(orders, auction.Order{Key: key, Kind: auction.KindIntent, Side: in.Side, Limit: in.LimitPrice, Qty: in.Remaining})
		intentByKey[key] = in
	}
	orders = append(orders, levelOrders...)

	var excluded []string
	for pass := uint32(1); pass <= params.MaxResolutionPasses; pass++ {
		active := orders
		if len(excluded) > 0 {
			drop := map[string]bool{}
			for _, key := range excluded {
				drop[key] = true
			}
			active = active[:0:0]
			for _, o := range orders {
				if !drop[o.Key] {
					active = append(active, o)
				}
			}
		}
		res := auction.Resolve(active, pref, params.PriceBand, params.MaxResolutionPasses)
		if !res.Executed {
			return notExecuted(res.Reason)
		}
		cacheCtx, write := ctx.CacheContext()
		failed, releasedNow, err := k.settlePerp(cacheCtx, params, market, batch, res, intentByKey, levelByKey)
		if err != nil {
			return err
		}
		if len(failed) == 0 {
			write()
			for key := range releasedNow {
				released[key] = true
			}
			result.ClearingPrice, result.Volume, result.Fills, result.Executed = res.Price, res.Volume, uint32(len(res.Fills)), true
			if err := k.Results.Set(ctx, collections.Join(batch, market.Id), result); err != nil {
				return err
			}
			return ctx.EventManager().EmitTypedEvent(&types.EventBatchCleared{
				BatchId: batch, MarketId: market.Id, ClearingPrice: res.Price.String(), ReferencePrice: pref.String(),
				Volume: res.Volume.String(), Fills: uint32(len(res.Fills)), Passes: pass,
			})
		}
		excluded = append(excluded, failed...)
	}
	return notExecuted("margin failures persisted")
}

// settlePerp applies the fills of a perp market through the margin hook and
// returns the keys whose Fill failed (to be excluded) and the level keys
// whose reservation was released.
func (k Keeper) settlePerp(ctx sdk.Context, params types.Params, market types.Market, batch uint64, res auction.Result,
	intents map[string]types.Intent, levels map[string]perpLevel,
) (failed []string, released map[string]bool, err error) {
	released = map[string]bool{}
	for _, f := range res.Fills {
		if f.Kind == auction.KindSolver {
			pl, ok := levels[f.Key]
			if !ok {
				return nil, nil, fmt.Errorf("unknown solver level %s", f.Key)
			}
			addr := sdk.MustAccAddressFromBech32(pl.solver)
			if err := k.marginHook.Release(ctx, addr, market, pl.level.Side, pl.level.Qty, pl.level.Price); err != nil {
				return nil, nil, err
			}
			released[f.Key] = true
			if err := k.marginHook.Fill(ctx, types.PerpFill{Batch: batch, Account: addr, Market: market, Side: f.Side, Qty: f.Qty, Price: f.Price, Solver: true, BuilderFee: math.ZeroInt()}); err != nil {
				k.Logger(ctx).Info("perp fill excluded", "batch", batch, "market", market.Id, "key", f.Key, "err", err)
				failed = append(failed, f.Key)
			}
			continue
		}
		in, ok := intents[f.Key]
		if !ok {
			return nil, nil, fmt.Errorf("unknown intent %s", f.Key)
		}
		addr := sdk.MustAccAddressFromBech32(in.Sender)
		if !in.ReduceOnly {
			if err := k.marginHook.Release(ctx, addr, market, in.Side, f.Qty, in.LimitPrice); err != nil {
				return nil, nil, err
			}
		}
		builderFee := math.ZeroInt()
		if in.Frontend != "" {
			if fe, err := k.Frontends.Get(ctx, in.Frontend); err == nil {
				builderFee = math.LegacyNewDecFromInt(f.Qty).Mul(f.Price).MulInt64(int64(fe.FeeBps)).QuoInt64(10_000).TruncateInt()
			}
		}
		if err := k.marginHook.Fill(ctx, types.PerpFill{Batch: batch, Account: addr, Market: market, Side: f.Side, Qty: f.Qty, Price: f.Price,
			ReduceOnly: in.ReduceOnly, Frontend: in.Frontend, BuilderFee: builderFee}); err != nil {
			k.Logger(ctx).Info("perp fill excluded", "batch", batch, "market", market.Id, "key", f.Key, "err", err)
			failed = append(failed, f.Key)
			continue
		}
		in.Remaining = in.Remaining.Sub(f.Qty)
		in.Received = in.Received.Add(math.LegacyNewDecFromInt(f.Qty).Mul(f.Price).TruncateInt())
		complete := in.Remaining.LT(market.MinQty)
		if complete {
			if _, err := k.closeIntent(ctx, in); err != nil {
				return nil, nil, err
			}
		} else if err := k.Intents.Set(ctx, in.Id, in); err != nil {
			return nil, nil, err
		}
		if err := ctx.EventManager().EmitTypedEvent(&types.EventIntentFilled{
			IntentId: in.Id, BatchId: batch, MarketId: market.Id, Qty: f.Qty.String(), Price: f.Price.String(),
			Paid: f.In.String(), Received: f.Out.String(), ProtocolFee: "0", BuilderFee: builderFee.String(), Complete: complete,
		}); err != nil {
			return nil, nil, err
		}
	}
	_ = params
	return failed, released, nil
}
