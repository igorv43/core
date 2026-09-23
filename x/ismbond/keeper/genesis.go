package keeper

import (
	"cosmossdk.io/collections"
	"github.com/classic-terra/core/v4/x/ismbond/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis initialises the module state.
func (k Keeper) InitGenesis(ctx sdk.Context, gs *types.GenesisState) error {
	if err := gs.Validate(); err != nil {
		return err
	}
	if err := k.Params.Set(ctx, gs.Params); err != nil {
		return err
	}
	for _, o := range gs.Operators {
		if err := k.Operators.Set(ctx, o.Address, o); err != nil {
			return err
		}
		if err := k.ByValidator.Set(ctx, o.Validator, o.Address); err != nil {
			return err
		}
	}
	for _, r := range gs.Roots {
		if err := k.Roots.Set(ctx, collections.Join(r.MailboxId.GetInternalId(), r.Index), r); err != nil {
			return err
		}
	}
	return nil
}

// ExportGenesis exports the module state.
func (k Keeper) ExportGenesis(ctx sdk.Context) (*types.GenesisState, error) {
	gs := types.DefaultGenesisState()
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	gs.Params = params
	if gs.Operators, err = k.AllOperators(ctx); err != nil {
		return nil, err
	}
	if err := k.Roots.Walk(ctx, nil, func(_ collections.Pair[uint64, uint32], r types.RootRecord) (bool, error) {
		gs.Roots = append(gs.Roots, r)
		return false, nil
	}); err != nil {
		return nil, err
	}
	return gs, nil
}
