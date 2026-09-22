package keeper

import (
	"cosmossdk.io/collections"
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis initialises the module state and the stLUNC bank metadata.
func (k Keeper) InitGenesis(ctx sdk.Context, gs *types.GenesisState) error {
	if err := gs.Validate(); err != nil {
		return err
	}
	if err := k.Params.Set(ctx, gs.Params); err != nil {
		return err
	}
	if err := k.Epoch.Set(ctx, gs.Epoch); err != nil {
		return err
	}
	if err := k.RequestSeq.Set(ctx, gs.NextRequestId); err != nil {
		return err
	}
	if err := k.Owed.Set(ctx, gs.Owed); err != nil {
		return err
	}
	for _, r := range gs.UnstakeRequests {
		if err := k.Requests.Set(ctx, r.Id, r); err != nil {
			return err
		}
		if err := k.RequestsByAddr.Set(ctx, collections.Join(r.Address, r.Id)); err != nil {
			return err
		}
	}
	for _, v := range gs.Validators {
		if err := k.Validators.Set(ctx, v.OperatorAddress, v); err != nil {
			return err
		}
	}
	if !k.bankKeeper.HasDenomMetaData(ctx, types.StDenom) {
		k.bankKeeper.SetDenomMetaData(ctx, types.StDenomMetadata())
	}
	return nil
}

// ExportGenesis exports the module state.
func (k Keeper) ExportGenesis(ctx sdk.Context) (*types.GenesisState, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	epoch, err := k.GetEpoch(ctx)
	if err != nil {
		return nil, err
	}
	next, err := k.RequestSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	owed, err := k.GetOwed(ctx)
	if err != nil {
		return nil, err
	}
	reqs := []types.UnstakeRequest{}
	if err := k.Requests.Walk(ctx, nil, func(_ uint64, r types.UnstakeRequest) (bool, error) {
		reqs = append(reqs, r)
		return false, nil
	}); err != nil {
		return nil, err
	}
	vals := []types.ValidatorState{}
	if err := k.Validators.Walk(ctx, nil, func(_ string, v types.ValidatorState) (bool, error) {
		vals = append(vals, v)
		return false, nil
	}); err != nil {
		return nil, err
	}
	return &types.GenesisState{Params: params, Epoch: epoch, UnstakeRequests: reqs, NextRequestId: next, Owed: owed, Validators: vals}, nil
}
