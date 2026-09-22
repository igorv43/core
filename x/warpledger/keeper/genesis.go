package keeper

import (
	"github.com/classic-terra/core/v4/x/warpledger/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis initialises the module state from genesis.
func (k Keeper) InitGenesis(ctx sdk.Context, genState *types.GenesisState) error {
	if err := genState.Validate(); err != nil {
		return err
	}
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}
	for _, ledger := range genState.Ledgers {
		if err := k.Ledgers.Set(ctx, ledgerKey(ledger.TokenId, ledger.Domain), ledger); err != nil {
			return err
		}
	}
	return nil
}

// ExportGenesis exports the module state.
func (k Keeper) ExportGenesis(ctx sdk.Context) (*types.GenesisState, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	ledgers := []types.DomainLedger{}
	if err := k.IterateLedgers(ctx, func(ledger types.DomainLedger) (bool, error) {
		ledgers = append(ledgers, ledger)
		return false, nil
	}); err != nil {
		return nil, err
	}
	return &types.GenesisState{Params: params, Ledgers: ledgers}, nil
}
