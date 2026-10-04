package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// StakingHooks reports the slashes that reach the module's delegations
// (spec §24.5: stLUNC holders absorb slashing pro rata, and slashing is the
// only thing that lowers the exchange rate — the epoch fee is taken from
// rewards that were priced in, so an epoch never emits this event). It is
// registered in the staking multi-hooks after the distribution and slashing
// hooks; every other hook is a no-op.
type StakingHooks struct{ k Keeper }

var _ stakingtypes.StakingHooks = StakingHooks{}

// StakingHooks returns the module's staking hooks.
func (k Keeper) StakingHooks() StakingHooks { return StakingHooks{k: k} }

// BeforeValidatorSlashed emits EventSlashAbsorbed when the slashed validator
// holds a delegation of the module. x/staking calls it before the tokens are
// burned, so the loss is estimated from the module's shares (truncated) and
// the rate after is the one expected once the tokens are gone. Unbonding
// entries of the module with that validator may be slashed by the same call
// but are not estimated (only entries created after the infraction are
// slashed, which the hook does not know). The hook is informational: it
// never fails the slash.
func (h StakingHooks) BeforeValidatorSlashed(goCtx context.Context, valAddr sdk.ValAddress, fraction math.LegacyDec) error {
	ctx := sdk.UnwrapSDKContext(goCtx)
	del, err := h.k.stakingKeeper.GetDelegation(ctx, h.k.ModuleAddress(), valAddr)
	if err != nil {
		if !errors.Is(err, stakingtypes.ErrNoDelegation) {
			h.k.Logger(ctx).Error("slash hook: delegation lookup failed", "validator", valAddr.String(), "err", err)
		}
		return nil
	}
	val, err := h.k.stakingKeeper.GetValidator(ctx, valAddr)
	if err != nil {
		h.k.Logger(ctx).Error("slash hook: validator lookup failed", "validator", valAddr.String(), "err", err)
		return nil
	}
	loss := val.TokensFromShares(del.Shares).Mul(fraction).TruncateInt()
	before, totals, err := h.k.ExchangeRate(ctx)
	if err != nil {
		h.k.Logger(ctx).Error("slash hook: exchange rate failed", "err", err)
		return nil
	}
	after := before
	if totals.StSupply.IsPositive() {
		assets := totals.Assets().Sub(loss)
		if assets.IsNegative() {
			assets = math.ZeroInt()
		}
		after = math.LegacyNewDecFromInt(assets).Quo(math.LegacyNewDecFromInt(totals.StSupply))
	}
	var epochNumber uint64
	if epoch, err := h.k.GetEpoch(ctx); err == nil {
		epochNumber = epoch.Number
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventSlashAbsorbed{
		Epoch: epochNumber, ExchangeRateBefore: before.String(), ExchangeRateAfter: after.String(),
		Validator: valAddr.String(), Fraction: fraction.String(), Loss: loss.String(),
	})
}

func (h StakingHooks) AfterValidatorCreated(context.Context, sdk.ValAddress) error { return nil }

func (h StakingHooks) BeforeValidatorModified(context.Context, sdk.ValAddress) error { return nil }

func (h StakingHooks) AfterValidatorRemoved(context.Context, sdk.ConsAddress, sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) AfterValidatorBonded(context.Context, sdk.ConsAddress, sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) AfterValidatorBeginUnbonding(context.Context, sdk.ConsAddress, sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) BeforeDelegationCreated(context.Context, sdk.AccAddress, sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) BeforeDelegationSharesModified(context.Context, sdk.AccAddress, sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) BeforeDelegationRemoved(context.Context, sdk.AccAddress, sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) AfterDelegationModified(context.Context, sdk.AccAddress, sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) AfterUnbondingInitiated(context.Context, uint64) error { return nil }
