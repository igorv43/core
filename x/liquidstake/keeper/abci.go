package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EndBlocker processes the epoch when its boundary is reached and then
// asserts the module invariants (EventInvariantBroken on violation, never a
// halt). Delegation work is bounded by params.MaxValidators (≤
// MaxValidatorsAbsolute); the undelegation batch and the invariant check
// walk the request queue. Other blocks only read the epoch and params.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	due, err := k.EpochDue(ctx)
	if err != nil {
		return err
	}
	if !due {
		return nil
	}
	rep, err := k.ProcessEpoch(ctx)
	if err != nil {
		// an epoch failure must never halt the chain: log and retry next block
		k.Logger(ctx).Error("epoch processing failed", "err", err)
		return nil
	}
	k.Logger(ctx).Info("epoch processed",
		"rewards", rep.Rewards, "fee", rep.Fee, "burned", rep.Burned,
		"delegated", rep.Delegated, "undelegated", rep.Undelegated, "redelegated", rep.Redelegated,
		"eligible", rep.Eligible, "rate", rep.RateAfter)
	// spec §25.1 item 6: the module invariants are verified at every epoch
	epoch, err := k.GetEpoch(ctx)
	if err != nil {
		return err
	}
	return k.assertInvariants(ctx, epoch.Number-1)
}
