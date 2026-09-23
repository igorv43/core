package keeper

import (
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	"github.com/classic-terra/core/v4/x/warpledger/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper keeps the per (warp token, remote domain) exposure ledger, enforces
// domain caps on outbound transfers, asserts the local solvency invariant in
// EndBlock and executes migration sweeps (spec §9, §10).
type Keeper struct {
	cdc codec.BinaryCodec

	// authority is the address allowed to update params and caps (x/gov).
	authority string

	bankKeeper    types.BankKeeper
	warpKeeper    *warpkeeper.Keeper
	circuitKeeper types.CircuitKeeper
	// router dispatches the MsgRemoteTransfer built by SweepMigration through
	// the registered (wrapped) warp message server, so caps and accounting apply.
	router baseapp.MessageRouter
	// ismBond is optional: when set, caps above bonded_cap_threshold require a bonded ISM (spec §7.2).
	ismBond types.IsmBondKeeper

	Schema  collections.Schema
	Params  collections.Item[types.Params]
	Ledgers collections.Map[collections.Pair[uint64, uint32], types.DomainLedger]
	// Baskets holds the internal ids of the synthetic tokens that are
	// multi-origin settlement baskets (spec §11.4 D-29).
	Baskets collections.KeySet[uint64]
}

// NewKeeper creates a new x/warpledger keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	authority string,
	bankKeeper types.BankKeeper,
	warpKeeper *warpkeeper.Keeper,
	circuitKeeper types.CircuitKeeper,
	router baseapp.MessageRouter,
) Keeper {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic(fmt.Errorf("invalid warpledger authority address: %w", err))
	}
	if warpKeeper == nil {
		panic("warpledger requires the warp keeper")
	}

	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		cdc:           cdc,
		authority:     authority,
		bankKeeper:    bankKeeper,
		warpKeeper:    warpKeeper,
		circuitKeeper: circuitKeeper,
		router:        router,
		Params:        collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Ledgers: collections.NewMap(sb, types.LedgersKey, "ledgers",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint32Key),
			codec.CollValue[types.DomainLedger](cdc)),
		Baskets: collections.NewKeySet(sb, types.BasketsKey, "baskets", collections.Uint64Key),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// GetAuthority returns the module authority.
func (k Keeper) GetAuthority() string {
	return k.authority
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// GetParams returns the module parameters.
func (k Keeper) GetParams(ctx sdk.Context) (types.Params, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.DefaultParams(), nil
		}
		return types.Params{}, err
	}
	return params, nil
}

// SetParams sets the module parameters.
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) error {
	if err := params.Validate(); err != nil {
		return err
	}
	return k.Params.Set(ctx, params)
}

// SetIsmBondKeeper registers x/ismbond for the bonded-cap rule of spec §7.2.
func (k *Keeper) SetIsmBondKeeper(b types.IsmBondKeeper) { k.ismBond = b }
