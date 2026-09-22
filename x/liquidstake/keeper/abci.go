package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EndBlocker processes the epoch when its boundary is reached. All work is
// bounded by params.MaxValidators (≤ MaxValidatorsAbsolute) and by the size
// of the module's delegation set; nothing runs on other blocks.
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
	return nil
}
