package keeper

import (
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// RegisterFrontend registers or updates an integrator and its fee (spec §23.2).
func (k Keeper) RegisterFrontend(ctx sdk.Context, addr string, feeBps uint32) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if feeBps > params.BuilderFeeMaxBps {
		return errorsmod.Wrapf(types.ErrFrontendFeeTooHigh, "max %d bps", params.BuilderFeeMaxBps)
	}
	return k.Frontends.Set(ctx, addr, types.Frontend{Address: addr, FeeBps: feeBps})
}

// ApproveFrontend records a user's consent to an integrator fee cap.
func (k Keeper) ApproveFrontend(ctx sdk.Context, user, frontend string, maxFeeBps uint32) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if has, err := k.Frontends.Has(ctx, frontend); err != nil {
		return err
	} else if !has {
		return types.ErrFrontendNotFound.Wrap(frontend)
	}
	if exists, err := k.Approvals.Has(ctx, collections.Join(user, frontend)); err != nil {
		return err
	} else if !exists {
		n := uint32(0)
		if err := k.Approvals.Walk(ctx, collections.NewPrefixedPairRange[string, string](user),
			func(_ collections.Pair[string, string], _ types.FrontendApproval) (bool, error) {
				n++
				return false, nil
			}); err != nil {
			return err
		}
		if n >= params.MaxFrontendApprovals {
			return errorsmod.Wrapf(types.ErrTooManyApprovals, "max %d", params.MaxFrontendApprovals)
		}
	}
	return k.Approvals.Set(ctx, collections.Join(user, frontend), types.FrontendApproval{User: user, Frontend: frontend, MaxFeeBps: maxFeeBps})
}

// RevokeFrontend removes a user's approval.
func (k Keeper) RevokeFrontend(ctx sdk.Context, user, frontend string) error {
	return k.Approvals.Remove(ctx, collections.Join(user, frontend))
}

// frontendApproved reports whether the integrator may charge the user
// (registered and approved with a cap at or above its fee).
func (k Keeper) frontendApproved(ctx sdk.Context, user, frontend string) (bool, error) {
	fe, err := k.Frontends.Get(ctx, frontend)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	ap, err := k.Approvals.Get(ctx, collections.Join(user, frontend))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return ap.MaxFeeBps >= fe.FeeBps, nil
}
