package keeper

import (
	"fmt"
	"strconv"
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/auction"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ResolveBatch resolves batch b for every enabled market: builds the book
// from open intents created at or before b and the revealed levels, runs the
// uniform-price auction inside the oracle band, settles atomically (DvP for
// spot) and records the aggregate. Unrevealed commits are slashed.
func (k Keeper) ResolveBatch(ctx sdk.Context, batch uint64) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	markets, err := k.EnabledMarkets(ctx)
	if err != nil {
		return err
	}
	for _, market := range markets {
		if err := k.resolveMarket(ctx, params, market, batch); err != nil {
			// a market failure must not halt the chain nor the other markets
			k.Logger(ctx).Error("batch resolution failed", "batch", batch, "market", market.Id, "err", err)
		}
	}
	// slash unrevealed commits and prune the batch's commits
	return k.finishCommits(ctx, params, batch)
}

func (k Keeper) resolveMarket(ctx sdk.Context, params types.Params, market types.Market, batch uint64) error {
	result := types.BatchResult{BatchId: batch, MarketId: market.Id, Height: ctx.BlockHeight(),
		ClearingPrice: math.LegacyZeroDec(), Volume: math.ZeroInt(), ReferencePrice: math.LegacyZeroDec()}

	pref, err := k.ReferencePrice(ctx, market)
	if err != nil {
		result.ClearingPrice = math.LegacyZeroDec()
		if err := k.Results.Set(ctx, collections.Join(batch, market.Id), result); err != nil {
			return err
		}
		return ctx.EventManager().EmitTypedEvent(&types.EventBatchNotExecuted{BatchId: batch, MarketId: market.Id, Reason: "reference price unavailable"})
	}
	result.ReferencePrice = pref

	intents, err := k.IntentsOfMarket(ctx, market.Id, int(params.MaxIntentsPerBatch))
	if err != nil {
		return err
	}
	commits, err := k.commitsOf(ctx, batch, market.Id)
	if err != nil {
		return err
	}

	var orders []auction.Order
	intentByKey := make(map[string]types.Intent)
	for _, in := range intents {
		if in.CreatedHeight > int64(batch) || in.ExpiryHeight <= ctx.BlockHeight() {
			continue
		}
		o := auction.Order{Key: "i:" + strconv.FormatUint(in.Id, 10), Kind: auction.KindIntent, Side: in.Side, Limit: in.LimitPrice,
			MinOut: in.MinOut, AmountIn: in.AmountIn.Amount}
		if in.Side == types.SIDE_BUY {
			o.Quote = in.Remaining
		} else {
			o.Qty = in.Remaining
		}
		orders = append(orders, o)
		intentByKey[o.Key] = in
	}
	levelByKey := make(map[string]struct {
		commit types.Commit
		level  types.Level
	})
	for _, c := range commits {
		if !c.Revealed || c.Bid == nil {
			continue
		}
		for i, l := range c.Bid.Levels {
			key := fmt.Sprintf("s:%s:%d", c.Solver, i)
			orders = append(orders, auction.Order{Key: key, Kind: auction.KindSolver, Side: l.Side, Limit: l.Price, Qty: l.Qty, SolverIndex: c.TxIndex})
			levelByKey[key] = struct {
				commit types.Commit
				level  types.Level
			}{c, l}
		}
	}

	res := auction.Resolve(orders, pref, params.PriceBand, params.MaxResolutionPasses)
	if !res.Executed {
		if err := k.Results.Set(ctx, collections.Join(batch, market.Id), result); err != nil {
			return err
		}
		return ctx.EventManager().EmitTypedEvent(&types.EventBatchNotExecuted{BatchId: batch, MarketId: market.Id, Reason: res.Reason})
	}

	if err := k.settle(ctx, params, market, batch, res, intentByKey, func(key string) (string, bool) {
		l, ok := levelByKey[key]
		return l.commit.Solver, ok
	}); err != nil {
		return err
	}

	result.ClearingPrice = res.Price
	result.Volume = res.Volume
	result.Fills = uint32(len(res.Fills))
	result.Executed = true
	if err := k.Results.Set(ctx, collections.Join(batch, market.Id), result); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventBatchCleared{
		BatchId: batch, MarketId: market.Id, ClearingPrice: res.Price.String(), ReferencePrice: pref.String(),
		Volume: res.Volume.String(), Fills: uint32(len(res.Fills)), Passes: res.Passes,
	})
}

// settle applies fills: delivery versus payment inside the module escrow,
// protocol fee on the amount received (spec §23), builder fee on top for
// attributed intents (§23.2), quote dust to the fee sink.
func (k Keeper) settle(ctx sdk.Context, params types.Params, market types.Market, batch uint64, res auction.Result,
	intents map[string]types.Intent, solverOf func(key string) (string, bool),
) error {
	quoteIn, quoteOut := math.ZeroInt(), math.ZeroInt()
	fees := sdk.NewCoins()
	builderFees := map[string]sdk.Coins{}

	for _, f := range res.Fills {
		recvDenom, payDenom := market.BaseDenom, market.QuoteDenom
		if f.Side == types.SIDE_SELL {
			recvDenom, payDenom = market.QuoteDenom, market.BaseDenom
		}
		if f.Side == types.SIDE_BUY {
			quoteIn = quoteIn.Add(f.In)
		} else {
			quoteOut = quoteOut.Add(f.Out)
		}

		protocolFee := f.Out.MulRaw(int64(params.SpotFeeBps)).QuoRaw(10_000)
		net := f.Out.Sub(protocolFee)
		if protocolFee.IsPositive() {
			fees = fees.Add(sdk.NewCoin(recvDenom, protocolFee))
		}

		if f.Kind == auction.KindSolver {
			solver, ok := solverOf(f.Key)
			if !ok {
				return fmt.Errorf("unknown solver level %s", f.Key)
			}
			if err := k.addEscrow(ctx, solver, payDenom, f.In.Neg()); err != nil {
				return err
			}
			if err := k.addEscrow(ctx, solver, recvDenom, net); err != nil {
				return err
			}
			continue
		}

		in, ok := intents[f.Key]
		if !ok {
			return fmt.Errorf("unknown intent %s", f.Key)
		}
		builderFee := math.ZeroInt()
		if in.Frontend != "" {
			if fe, err := k.Frontends.Get(ctx, in.Frontend); err == nil {
				builderFee = f.Out.MulRaw(int64(fe.FeeBps)).QuoRaw(10_000)
				if builderFee.IsPositive() {
					builderFees[in.Frontend] = builderFees[in.Frontend].Add(sdk.NewCoin(recvDenom, builderFee))
					net = net.Sub(builderFee)
				}
			}
		}
		if net.IsPositive() {
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdk.MustAccAddressFromBech32(in.Sender), sdk.NewCoins(sdk.NewCoin(recvDenom, net))); err != nil {
				return err
			}
		}
		in.Remaining = in.Remaining.Sub(f.In)
		in.Received = in.Received.Add(f.Out)
		complete := k.intentComplete(market, in)
		if complete {
			if _, err := k.closeIntent(ctx, in); err != nil {
				return err
			}
		} else if err := k.Intents.Set(ctx, in.Id, in); err != nil {
			return err
		}
		if err := ctx.EventManager().EmitTypedEvent(&types.EventIntentFilled{
			IntentId: in.Id, BatchId: batch, MarketId: market.Id, Qty: f.Qty.String(), Price: f.Price.String(),
			Paid: f.In.String(), Received: net.String(), ProtocolFee: protocolFee.String(), BuilderFee: builderFee.String(), Complete: complete,
		}); err != nil {
			return err
		}
	}

	// quote dust: buyers paid ceil, sellers received floor
	if dust := quoteIn.Sub(quoteOut); dust.IsPositive() {
		fees = fees.Add(sdk.NewCoin(market.QuoteDenom, dust))
	}
	if !fees.IsZero() {
		if err := k.feeSink.Deposit(ctx, types.ModuleName, fees); err != nil {
			return err
		}
	}
	for fe, coins := range builderFees {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdk.MustAccAddressFromBech32(fe), coins); err != nil {
			return err
		}
	}
	return nil
}

// intentComplete reports whether the remainder can no longer form a valid order.
func (k Keeper) intentComplete(market types.Market, in types.Intent) bool {
	if !in.Remaining.IsPositive() {
		return true
	}
	if in.Side == types.SIDE_SELL {
		return in.Remaining.LT(market.MinQty)
	}
	return math.LegacyNewDecFromInt(in.Remaining).Quo(in.LimitPrice).TruncateInt().LT(market.MinQty)
}

// finishCommits slashes solvers that committed without revealing, applies
// the reveal-rate rule and removes the batch's commits.
func (k Keeper) finishCommits(ctx sdk.Context, params types.Params, batch uint64) error {
	var keys []collections.Triple[uint64, string, string]
	var unrevealed []string
	if err := k.Commits.Walk(ctx, collections.NewPrefixedTripleRange[uint64, string, string](batch),
		func(key collections.Triple[uint64, string, string], c types.Commit) (bool, error) {
			keys = append(keys, key)
			if !c.Revealed {
				unrevealed = append(unrevealed, c.Solver)
			}
			return false, nil
		}); err != nil {
		return err
	}
	for _, solver := range unrevealed {
		if err := k.slashSolver(ctx, solver, params.SlashNoReveal, "commit not revealed"); err != nil && !strings.Contains(err.Error(), "not registered") {
			return err
		}
	}
	for _, key := range keys {
		if err := k.Commits.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}
