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
		if err := k.Gateways.Set(ctx, collections.Join(g.AppId.GetInternalId(), g.Domain), g); err != nil {
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
	for _, b := range gs.Beacons {
		if err := k.Beacons.Set(ctx, b.Id, b); err != nil {
			return err
		}
	}
	if err := k.BeaconSeq.Set(ctx, gs.NextBeaconId); err != nil {
		return err
	}
	for _, r := range gs.Receipts {
		if err := k.storeReceipt(ctx, r); err != nil {
			return err
		}
	}
	for _, ex := range gs.Executors {
		if err := k.Executors.Set(ctx, ex.Domain, ex); err != nil {
			return err
		}
	}
	for _, p := range gs.Ports {
		if err := k.Ports.Set(ctx, p.PortDomain, p); err != nil {
			return err
		}
	}
	for _, p := range gs.PendingPayloads {
		if err := k.Pending.Set(ctx, p.Id, p); err != nil {
			return err
		}
		if err := k.PendingByAccount.Set(ctx, collections.Join(p.Account, p.Id)); err != nil {
			return err
		}
		// re-check every pending payload against the credits after import
		if err := k.PendingReady.Set(ctx, p.Id); err != nil {
			return err
		}
	}
	if err := k.PendingSeq.Set(ctx, gs.NextPendingId); err != nil {
		return err
	}
	for _, c := range gs.DepositCredits {
		key := creditKey(c.Account, c.TokenId, c.Origin)
		if err := k.Credits.Set(ctx, key, c); err != nil {
			return err
		}
		if err := k.CreditExpiry.Set(ctx, collections.Join(c.Height, key)); err != nil {
			return err
		}
	}
	for _, ar := range gs.AutoReturns {
		if err := k.AutoReturns.Set(ctx, ar.Account, ar); err != nil {
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
	if err := k.Receipts.Walk(ctx, nil, func(_ collections.Pair[string, uint64], r types.ConversionReceipt) (bool, error) {
		gs.Receipts = append(gs.Receipts, r)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Executors.Walk(ctx, nil, func(_ uint32, ex types.Executor) (bool, error) {
		gs.Executors = append(gs.Executors, ex)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Ports.Walk(ctx, nil, func(_ uint32, p types.Port) (bool, error) {
		gs.Ports = append(gs.Ports, p)
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
	if err := k.Pending.Walk(ctx, nil, func(_ uint64, p types.PendingPayload) (bool, error) {
		gs.PendingPayloads = append(gs.PendingPayloads, p)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if gs.NextPendingId, err = k.PendingSeq.Peek(ctx); err != nil {
		return nil, err
	}
	if err := k.Credits.Walk(ctx, nil, func(_ string, c types.DepositCredit) (bool, error) {
		gs.DepositCredits = append(gs.DepositCredits, c)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.AutoReturns.Walk(ctx, nil, func(_ string, ar types.AutoReturn) (bool, error) {
		gs.AutoReturns = append(gs.AutoReturns, ar)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if gs.Beacons, err = k.AllBeacons(ctx); err != nil {
		return nil, err
	}
	if gs.NextBeaconId, err = k.BeaconSeq.Peek(ctx); err != nil {
		return nil, err
	}
	return gs, nil
}

// RemoteAppsAndGateways lists apps and gateways (shared by the query and the export).
func (k Keeper) RemoteAppsAndGateways(ctx sdk.Context) (*types.QueryRemoteAppsResponse, error) {
	return NewQueryServerImpl(k).RemoteApps(ctx, &types.QueryRemoteAppsRequest{})
}
