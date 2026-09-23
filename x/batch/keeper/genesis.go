package keeper

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/types"
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
	for _, m := range gs.Markets {
		if err := k.Markets.Set(ctx, m.Id, m); err != nil {
			return err
		}
	}
	if err := k.IntentSeq.Set(ctx, gs.NextIntentId); err != nil {
		return err
	}
	for _, in := range gs.Intents {
		if err := k.setIntent(ctx, in); err != nil {
			return err
		}
	}
	for _, s := range gs.Solvers {
		if err := k.Solvers.Set(ctx, s.Address, s); err != nil {
			return err
		}
	}
	for _, e := range gs.SolverEscrows {
		if err := k.SolverEscrow.Set(ctx, collections.Join(e.Solver, e.Balance.Denom), e.Balance.Amount); err != nil {
			return err
		}
	}
	for _, f := range gs.Frontends {
		if err := k.Frontends.Set(ctx, f.Address, f); err != nil {
			return err
		}
	}
	for _, a := range gs.FrontendApprovals {
		if err := k.Approvals.Set(ctx, collections.Join(a.User, a.Frontend), a); err != nil {
			return err
		}
	}
	return nil
}

// ExportGenesis exports the module state (open commits are not exported:
// they belong to in-flight batches that a restarted chain re-runs).
func (k Keeper) ExportGenesis(ctx sdk.Context) (*types.GenesisState, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	gs := &types.GenesisState{
		Params: params, Markets: []types.Market{}, Intents: []types.Intent{}, Solvers: []types.Solver{},
		SolverEscrows: []types.SolverEscrowEntry{}, Frontends: []types.Frontend{}, FrontendApprovals: []types.FrontendApproval{},
	}
	if err := k.Markets.Walk(ctx, nil, func(_ string, m types.Market) (bool, error) { gs.Markets = append(gs.Markets, m); return false, nil }); err != nil {
		return nil, err
	}
	if err := k.Intents.Walk(ctx, nil, func(_ uint64, in types.Intent) (bool, error) { gs.Intents = append(gs.Intents, in); return false, nil }); err != nil {
		return nil, err
	}
	next, err := k.IntentSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	gs.NextIntentId = next
	if err := k.Solvers.Walk(ctx, nil, func(_ string, s types.Solver) (bool, error) { gs.Solvers = append(gs.Solvers, s); return false, nil }); err != nil {
		return nil, err
	}
	if err := k.SolverEscrow.Walk(ctx, nil, func(key collections.Pair[string, string], v math.Int) (bool, error) {
		gs.SolverEscrows = append(gs.SolverEscrows, types.SolverEscrowEntry{Solver: key.K1(), Balance: sdk.NewCoin(key.K2(), v)})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Frontends.Walk(ctx, nil, func(_ string, f types.Frontend) (bool, error) {
		gs.Frontends = append(gs.Frontends, f)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Approvals.Walk(ctx, nil, func(_ collections.Pair[string, string], a types.FrontendApproval) (bool, error) {
		gs.FrontendApprovals = append(gs.FrontendApprovals, a)
		return false, nil
	}); err != nil {
		return nil, err
	}
	return gs, nil
}
