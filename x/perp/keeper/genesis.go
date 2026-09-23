package keeper

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// sidesFor expands an unspecified side into both sides.
func sidesFor(side batchtypes.Side) []batchtypes.Side {
	if side == batchtypes.SIDE_BUY || side == batchtypes.SIDE_SELL {
		return []batchtypes.Side{side}
	}
	return []batchtypes.Side{batchtypes.SIDE_BUY, batchtypes.SIDE_SELL}
}

// InitGenesis initialises the module state.
func (k Keeper) InitGenesis(ctx sdk.Context, gs *types.GenesisState) error {
	if err := gs.Validate(); err != nil {
		return err
	}
	if err := k.Params.Set(ctx, gs.Params); err != nil {
		return err
	}
	markets := map[string]types.Market{}
	for _, m := range gs.Markets {
		if err := k.Markets.Set(ctx, m.Id, m); err != nil {
			return err
		}
		markets[m.Id] = m
	}
	for _, p := range gs.Positions {
		if err := k.setPosition(ctx, markets[p.MarketId], p); err != nil {
			return err
		}
	}
	for _, c := range gs.Collateral {
		if err := k.Collateral.Set(ctx, c.Account, c.Amount); err != nil {
			return err
		}
	}
	for _, r := range gs.Reservations {
		if err := k.Reservations.Set(ctx, collections.Join(r.Account, r.MarketId), r.Amount); err != nil {
			return err
		}
	}
	for _, t := range gs.Triggers {
		if err := k.setTrigger(ctx, t); err != nil {
			return err
		}
	}
	if err := k.TriggerSeq.Set(ctx, gs.NextTriggerId); err != nil {
		return err
	}
	for _, a := range gs.AutoTopUpAccounts {
		if err := k.AutoTopUp.Set(ctx, a); err != nil {
			return err
		}
	}
	if err := k.Ledger.Set(ctx, gs.Ledger); err != nil {
		return err
	}
	for _, r := range gs.Allocations {
		if err := k.Allocations.Set(ctx, r.Epoch, r); err != nil {
			return err
		}
	}
	for _, u := range gs.UnwindIntents {
		if err := k.UnwindIntents.Set(ctx, u.MarketId, u.IntentId); err != nil {
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
	if gs.Markets, err = k.AllMarkets(ctx); err != nil {
		return nil, err
	}
	if err := k.Positions.Walk(ctx, nil, func(_ collections.Pair[string, string], p types.Position) (bool, error) {
		gs.Positions = append(gs.Positions, p)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Collateral.Walk(ctx, nil, func(a string, v math.Int) (bool, error) {
		gs.Collateral = append(gs.Collateral, types.CollateralEntry{Account: a, Amount: v})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Reservations.Walk(ctx, nil, func(key collections.Pair[string, string], v math.Int) (bool, error) {
		gs.Reservations = append(gs.Reservations, types.ReservationEntry{Account: key.K1(), MarketId: key.K2(), Amount: v})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Triggers.Walk(ctx, nil, func(_ uint64, t types.TriggerOrder) (bool, error) {
		gs.Triggers = append(gs.Triggers, t)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if gs.NextTriggerId, err = k.TriggerSeq.Peek(ctx); err != nil {
		return nil, err
	}
	if err := k.AutoTopUp.Walk(ctx, nil, func(a string) (bool, error) {
		gs.AutoTopUpAccounts = append(gs.AutoTopUpAccounts, a)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if gs.Ledger, err = k.GetLedger(ctx); err != nil {
		return nil, err
	}
	if err := k.Allocations.Walk(ctx, nil, func(_ uint64, r types.AllocationRecord) (bool, error) {
		gs.Allocations = append(gs.Allocations, r)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.UnwindIntents.Walk(ctx, nil, func(m string, id uint64) (bool, error) {
		gs.UnwindIntents = append(gs.UnwindIntents, types.UnwindIntent{MarketId: m, IntentId: id})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if gs.Markets == nil {
		gs.Markets = []types.Market{}
	}
	return gs, nil
}

// CheckInvariants verifies the executable invariants of spec §25.1 for the
// perpetuals: Σ long = Σ short per market (fund included), the ledger sum
// equals the module's settlement balance, and reservations never exceed
// free collateral. It returns a description of the first violation.
func (k Keeper) CheckInvariants(ctx sdk.Context) (string, bool) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err.Error(), true
	}
	markets, err := k.AllMarkets(ctx)
	if err != nil {
		return err.Error(), true
	}
	for _, m := range markets {
		long, short := math.ZeroInt(), math.ZeroInt()
		positions, err := k.positionsOfMarket(ctx, m.Id)
		if err != nil {
			return err.Error(), true
		}
		for _, p := range positions {
			if p.Side == batchtypes.SIDE_BUY {
				long = long.Add(p.Qty)
			} else {
				short = short.Add(p.Qty)
			}
		}
		if !long.Equal(short) {
			return "perp conservation broken in " + m.Id + ": long " + long.String() + " short " + short.String(), true
		}
		if !long.Equal(m.OiLong) || !short.Equal(m.OiShort) {
			return "open interest counters diverge in " + m.Id, true
		}
	}
	l, err := k.GetLedger(ctx)
	if err != nil {
		return err.Error(), true
	}
	expected, err := k.ledgerTotal(ctx, l)
	if err != nil {
		return err.Error(), true
	}
	actual := k.bankKeeper.GetBalance(ctx, k.ModuleAddress(), params.SettlementDenom).Amount
	if actual.LT(expected) {
		return "module balance " + actual.String() + " below the ledger total " + expected.String(), true
	}
	broken := ""
	_ = k.Collateral.Walk(ctx, nil, func(account string, free math.Int) (bool, error) {
		reserved, err := k.ReservedTotal(ctx, account)
		if err != nil {
			broken = err.Error()
			return true, nil
		}
		if reserved.GT(free) {
			broken = "reservations of " + account + " exceed free collateral"
			return true, nil
		}
		return false, nil
	})
	return broken, broken != ""
}
