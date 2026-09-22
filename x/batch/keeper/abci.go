package keeper

import (
	"cosmossdk.io/collections"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EndBlocker drives the pipeline of spec §15.1: expire intents, refund
// unbonded solvers, resolve the batch whose reveal block is this one,
// prune old aggregates, seal the batch collected in this block. Every
// step is bounded by the parameters of §12.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if err := k.ExpireIntents(ctx); err != nil {
		k.Logger(ctx).Error("expire intents failed", "err", err)
	}
	if err := k.refundUnbondedSolvers(ctx); err != nil {
		k.Logger(ctx).Error("refund unbonded solvers failed", "err", err)
	}
	if batch, ok := BatchToResolve(ctx.BlockHeight(), params.CommitWindow); ok {
		if err := k.ResolveBatch(ctx, batch); err != nil {
			k.Logger(ctx).Error("resolve batch failed", "batch", batch, "err", err)
		}
	}
	if err := k.pruneResults(ctx, params); err != nil {
		k.Logger(ctx).Error("prune results failed", "err", err)
	}
	if err := k.rollSolverWindows(ctx, params); err != nil {
		k.Logger(ctx).Error("solver windows failed", "err", err)
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventBatchSealed{BatchId: uint64(ctx.BlockHeight()), Height: ctx.BlockHeight()})
}

// pruneResults removes aggregates older than prune_delay_blocks (spec §12.4).
func (k Keeper) pruneResults(ctx sdk.Context, params types.Params) error {
	cutoff := ctx.BlockHeight() - params.PruneDelayBlocks
	if cutoff <= 0 {
		return nil
	}
	var keys []collections.Pair[uint64, string]
	if err := k.Results.Walk(ctx, nil, func(key collections.Pair[uint64, string], r types.BatchResult) (bool, error) {
		if r.Height < cutoff {
			keys = append(keys, key)
		}
		return len(keys) >= types.MaxExpiredPerBlock || r.Height >= cutoff, nil
	}); err != nil {
		return err
	}
	for _, key := range keys {
		if err := k.Results.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// rollSolverWindows applies the reveal-rate rule to every solver whose
// window elapsed (spec §16.2).
func (k Keeper) rollSolverWindows(ctx sdk.Context, params types.Params) error {
	var updated []types.Solver
	if err := k.Solvers.Walk(ctx, nil, func(_ string, s types.Solver) (bool, error) {
		if ctx.BlockHeight()-s.WindowStart >= types.RevealRateWindowBlocks {
			if err := k.rollSolverWindow(ctx, &s, params.SolverSuspensionBlocks); err != nil {
				return true, err
			}
			updated = append(updated, s)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for _, s := range updated {
		if err := k.Solvers.Set(ctx, s.Address, s); err != nil {
			return err
		}
	}
	return nil
}
