package keeper

import (
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Migrator runs the in-place store migrations of x/liquidstake.
type Migrator struct {
	keeper Keeper
}

// NewMigrator returns a Migrator for the keeper.
func NewMigrator(k Keeper) Migrator {
	return Migrator{keeper: k}
}

// Migrate1to2 derives the owed_queued / owed_unbonding aggregates from the
// stored requests and fills Params.ExpectedBlockTime (new in v2) with the
// mainnet default. A chain whose params break the epoch sizing rule is not
// halted: the rule is logged and the epoch time floor (MinEpochDuration)
// keeps the MaxEntries bound at runtime until governance fixes the params.
func (m Migrator) Migrate1to2(ctx sdk.Context) error {
	k := m.keeper
	if err := k.rebuildOwedAggregates(ctx); err != nil {
		return err
	}
	p, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if p.ExpectedBlockTime == 0 {
		p.ExpectedBlockTime = types.DefaultExpectedBlockTime
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if err := k.validateEpochSizing(ctx, p); err != nil {
		k.Logger(ctx).Error("liquidstake params break the epoch sizing rule; the epoch time floor applies", "err", err)
	}
	return k.Params.Set(ctx, p)
}
