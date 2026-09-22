package keeper

import (
	"bytes"
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CommitBid records a sealed bid for a batch (spec §15.1, commit window W).
func (k Keeper) CommitBid(ctx sdk.Context, msg *types.MsgCommitBid) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if !CommitWindowOpen(msg.BatchId, ctx.BlockHeight(), params.CommitWindow) {
		return errorsmod.Wrapf(types.ErrCommitWindowClosed, "batch %d at height %d", msg.BatchId, ctx.BlockHeight())
	}
	s, err := k.GetSolver(ctx, msg.Solver)
	if err != nil {
		return err
	}
	if s.UnbondHeight != 0 {
		return types.ErrSolverUnbonding.Wrap(msg.Solver)
	}
	if s.SuspendedUntil > ctx.BlockHeight() {
		return errorsmod.Wrapf(types.ErrSolverSuspended, "until height %d", s.SuspendedUntil)
	}
	market, err := k.GetMarket(ctx, msg.MarketId)
	if err != nil {
		return err
	}
	if !market.Enabled {
		return types.ErrMarketDisabled.Wrap(market.Id)
	}
	key := collections.Join3(msg.BatchId, msg.MarketId, msg.Solver)
	if has, err := k.Commits.Has(ctx, key); err != nil {
		return err
	} else if has {
		return types.ErrCommitExists.Wrap(msg.Solver)
	}
	// bound the solvers per batch and market
	n := uint32(0)
	if err := k.Commits.Walk(ctx, collections.NewSuperPrefixedTripleRange[uint64, string, string](msg.BatchId, msg.MarketId),
		func(_ collections.Triple[uint64, string, string], _ types.Commit) (bool, error) {
			n++
			return false, nil
		}); err != nil {
		return err
	}
	if n >= params.MaxSolversPerBatch {
		return errorsmod.Wrapf(types.ErrCommitWindowClosed, "max_solvers_per_batch (%d) reached", params.MaxSolversPerBatch)
	}
	idx, err := k.CommitSeq.Next(ctx)
	if err != nil {
		return err
	}
	if err := k.Commits.Set(ctx, key, types.Commit{
		BatchId: msg.BatchId, Solver: msg.Solver, MarketId: msg.MarketId, Commitment: msg.Commitment, TxIndex: idx,
	}); err != nil {
		return err
	}
	s.Commits++
	if err := k.Solvers.Set(ctx, msg.Solver, s); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventBidCommitted{BatchId: msg.BatchId, Solver: msg.Solver, MarketId: msg.MarketId})
}

// RevealBid opens a commitment in the reveal block and checks escrow
// coverage of every level; uncovered levels are discarded with a slash
// (spec §15.2, §16.2).
func (k Keeper) RevealBid(ctx sdk.Context, msg *types.MsgRevealBid) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if !RevealBlock(msg.BatchId, ctx.BlockHeight(), params.CommitWindow) {
		return errorsmod.Wrapf(types.ErrRevealWindowClosed, "batch %d reveals at height %d", msg.BatchId, int64(msg.BatchId)+params.CommitWindow+1)
	}
	key := collections.Join3(msg.BatchId, msg.Bid.MarketId, msg.Solver)
	c, err := k.Commits.Get(ctx, key)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.ErrCommitNotFound.Wrap(msg.Solver)
		}
		return err
	}
	if c.Revealed {
		return types.ErrCommitExists.Wrap("already revealed")
	}
	if uint32(len(msg.Bid.Levels)) > params.MaxLevelsPerBid {
		return errorsmod.Wrapf(types.ErrRevealMismatch, "more than max_levels_per_bid (%d) levels", params.MaxLevelsPerBid)
	}
	expected, err := types.Commitment(msg.Bid, msg.Salt, msg.Solver, msg.BatchId)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, c.Commitment) {
		// an inconsistent reveal is worth twice the no-reveal penalty (spec §16.2)
		if err := k.slashSolver(ctx, msg.Solver, sdk.NewCoin(params.SlashNoReveal.Denom, params.SlashNoReveal.Amount.MulRaw(2)), "reveal inconsistent with commitment"); err != nil {
			return err
		}
		return types.ErrRevealMismatch.Wrap(msg.Solver)
	}
	market, err := k.GetMarket(ctx, msg.Bid.MarketId)
	if err != nil {
		return err
	}

	var kept []types.Level
	discarded := uint32(0)
	if market.Type == types.MARKET_TYPE_PERP {
		// perp levels reserve initial margin in x/perp instead of escrow (spec §14.3)
		kept, discarded, err = k.reservePerpLevels(ctx, msg.Solver, market, msg.Bid.Levels)
		if err != nil {
			return err
		}
		return k.finishReveal(ctx, params, key, c, msg, kept, discarded)
	}

	// keep only the levels the escrow covers, in order; slash once if any is dropped
	quoteFree, err := k.escrowBalance(ctx, msg.Solver, market.QuoteDenom)
	if err != nil {
		return err
	}
	baseFree, err := k.escrowBalance(ctx, msg.Solver, market.BaseDenom)
	if err != nil {
		return err
	}
	reservedQuote, err := k.reservedEscrow(ctx, msg.Solver, market.QuoteDenom)
	if err != nil {
		return err
	}
	reservedBase, err := k.reservedEscrow(ctx, msg.Solver, market.BaseDenom)
	if err != nil {
		return err
	}
	quoteFree, baseFree = quoteFree.Sub(reservedQuote), baseFree.Sub(reservedBase)
	for _, l := range msg.Bid.Levels {
		if !l.Price.Quo(market.TickSize).IsInteger() || l.Qty.LT(market.MinQty) {
			discarded++
			continue
		}
		need := bidRequirement(market, types.Bid{Levels: []types.Level{l}}, market.QuoteDenom)
		if l.Side == types.SIDE_SELL {
			need = l.Qty
			if baseFree.LT(need) {
				discarded++
				continue
			}
			baseFree = baseFree.Sub(need)
		} else {
			if quoteFree.LT(need) {
				discarded++
				continue
			}
			quoteFree = quoteFree.Sub(need)
		}
		kept = append(kept, l)
	}
	return k.finishReveal(ctx, params, key, c, msg, kept, discarded)
}

// reservePerpLevels reserves margin for each valid level through the hook;
// levels the account cannot cover are discarded.
func (k Keeper) reservePerpLevels(ctx sdk.Context, solver string, market types.Market, levels []types.Level) ([]types.Level, uint32, error) {
	if k.marginHook == nil {
		return nil, uint32(len(levels)), nil
	}
	addr := sdk.MustAccAddressFromBech32(solver)
	var kept []types.Level
	discarded := uint32(0)
	for _, l := range levels {
		if !l.Price.Quo(market.TickSize).IsInteger() || l.Qty.LT(market.MinQty) {
			discarded++
			continue
		}
		if err := k.marginHook.Reserve(ctx, addr, market, l.Side, l.Qty, l.Price); err != nil {
			discarded++
			continue
		}
		kept = append(kept, l)
	}
	return kept, discarded, nil
}

// finishReveal records the kept levels, slashes once when any level was
// dropped and counts the reveal.
func (k Keeper) finishReveal(ctx sdk.Context, params types.Params, key collections.Triple[uint64, string, string], c types.Commit, msg *types.MsgRevealBid, kept []types.Level, discarded uint32) error {
	if discarded > 0 {
		if err := k.slashSolver(ctx, msg.Solver, params.SlashNoReveal, "level without coverage"); err != nil {
			return err
		}
	}
	c.Revealed = true
	c.Bid = &types.Bid{MarketId: msg.Bid.MarketId, Levels: kept}
	if err := k.Commits.Set(ctx, key, c); err != nil {
		return err
	}
	s, err := k.GetSolver(ctx, msg.Solver)
	if err != nil {
		return err
	}
	s.Reveals++
	if err := k.Solvers.Set(ctx, msg.Solver, s); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventBidRevealed{
		BatchId: msg.BatchId, Solver: msg.Solver, MarketId: msg.Bid.MarketId, Levels: uint32(len(kept)), DiscardedLevels: discarded,
	})
}

// commitsOf returns the commits of (batch, market) sorted by tx index.
func (k Keeper) commitsOf(ctx sdk.Context, batch uint64, marketID string) ([]types.Commit, error) {
	var out []types.Commit
	err := k.Commits.Walk(ctx, collections.NewSuperPrefixedTripleRange[uint64, string, string](batch, marketID),
		func(_ collections.Triple[uint64, string, string], c types.Commit) (bool, error) {
			out = append(out, c)
			return false, nil
		})
	// insertion order of the walk is by solver address; sort by tx index
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].TxIndex > out[j].TxIndex; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out, err
}
