package keeper

import (
	"cosmossdk.io/collections"
	"github.com/classic-terra/core/v4/x/remote/types"
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
	for _, a := range gs.Apps {
		if err := k.Apps.Set(ctx, a.Id.GetInternalId(), a); err != nil {
			return err
		}
	}
	for _, g := range gs.Gateways {
		if err := k.Gateways.Set(ctx, collections.Join(g.AppId.GetInternalId(), g.Domain), g.Address.Bytes()); err != nil {
			return err
		}
	}
	for _, a := range gs.Accounts {
		if err := k.Accounts.Set(ctx, a.Address, a); err != nil {
			return err
		}
	}
	for _, s := range gs.Sessions {
		if err := k.Sessions.Set(ctx, collections.Join(s.Account, s.SessionKey), s); err != nil {
			return err
		}
	}
	for _, w := range gs.Withdrawals {
		if err := k.Withdrawals.Set(ctx, collections.Join(w.Account, w.Seq), w); err != nil {
			return err
		}
		if cur, err := k.WithdrawSeq.Get(ctx, w.Account); err != nil || cur < w.Seq {
			if err := k.WithdrawSeq.Set(ctx, w.Account, w.Seq); err != nil {
				return err
			}
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
	if err := k.Apps.Walk(ctx, nil, func(_ uint64, a types.RemoteApp) (bool, error) {
		gs.Apps = append(gs.Apps, a)
		return false, nil
	}); err != nil {
		return nil, err
	}
	apps, err := k.RemoteAppsAndGateways(ctx)
	if err != nil {
		return nil, err
	}
	gs.Gateways = apps.Gateways
	if err := k.Accounts.Walk(ctx, nil, func(_ string, a types.RemoteAccount) (bool, error) {
		gs.Accounts = append(gs.Accounts, a)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Sessions.Walk(ctx, nil, func(_ collections.Pair[string, string], s types.Session) (bool, error) {
		gs.Sessions = append(gs.Sessions, s)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Withdrawals.Walk(ctx, nil, func(_ collections.Pair[string, uint64], w types.Withdrawal) (bool, error) {
		gs.Withdrawals = append(gs.Withdrawals, w)
		return false, nil
	}); err != nil {
		return nil, err
	}
	return gs, nil
}

// RemoteAppsAndGateways lists apps and gateways (shared by the query and the export).
func (k Keeper) RemoteAppsAndGateways(ctx sdk.Context) (*types.QueryRemoteAppsResponse, error) {
	return NewQueryServerImpl(k).RemoteApps(ctx, &types.QueryRemoteAppsRequest{})
}
