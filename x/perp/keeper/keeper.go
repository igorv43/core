package keeper

import (
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/perp/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// Keeper implements the perpetuals of spec §17–§23: isolated-margin
// positions novated with the protocol, the insurance-fund waterfall and ADL,
// the oracle-state risk engine, resident trigger orders and the allocation
// cascade. Orders enter through the x/batch auction (margin hook).
type Keeper struct {
	cdc       codec.BinaryCodec
	authority string

	bankKeeper   types.BankKeeper
	oracleKeeper types.OracleKeeper
	batchKeeper  types.BatchKeeper
	distrKeeper  types.DistributionKeeper
	burnAccount  string

	Schema            collections.Schema
	Params            collections.Item[types.Params]
	Markets           collections.Map[string, types.Market]
	Positions         collections.Map[collections.Pair[string, string], types.Position]
	PositionsByMarket collections.KeySet[collections.Pair[string, string]]
	Collateral        collections.Map[string, math.Int]
	Reservations      collections.Map[collections.Pair[string, string], math.Int]
	Triggers          collections.Map[uint64, types.TriggerOrder]
	TriggerSeq        collections.Sequence
	TriggersByAccount collections.KeySet[collections.Pair[string, uint64]]
	TriggersByMarket  collections.KeySet[collections.Pair[string, uint64]]
	AutoTopUp         collections.KeySet[string]
	Ledger            collections.Item[types.Ledger]
	Allocations       collections.Map[uint64, types.AllocationRecord]
	UnwindIntents     collections.Map[string, uint64]
	// LiqIndex orders positions by liquidation price per market and side:
	// (market/side, price_key ‖ account) → unit (spec §19.5).
	LiqIndex collections.KeySet[collections.Pair[string, []byte]]
	// Premiums keeps the last premium_window batch premiums per market.
	Premiums collections.Map[collections.Pair[string, uint64], math.LegacyDec]
}

// NewKeeper creates the x/perp keeper. burnAccount is the treasury burn
// module account that receives the bought-back uluna.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	authority string,
	bankKeeper types.BankKeeper,
	oracleKeeper types.OracleKeeper,
	batchKeeper types.BatchKeeper,
	distrKeeper types.DistributionKeeper,
	burnAccount string,
) Keeper {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic(fmt.Errorf("invalid perp authority address: %w", err))
	}
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		cdc: cdc, authority: authority, bankKeeper: bankKeeper, oracleKeeper: oracleKeeper, batchKeeper: batchKeeper,
		distrKeeper: distrKeeper, burnAccount: burnAccount,
		Params:  collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Markets: collections.NewMap(sb, types.MarketsKey, "markets", collections.StringKey, codec.CollValue[types.Market](cdc)),
		Positions: collections.NewMap(sb, types.PositionsKey, "positions",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey), codec.CollValue[types.Position](cdc)),
		PositionsByMarket: collections.NewKeySet(sb, types.PositionsByMarketKey, "positions_by_market",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey)),
		Collateral: collections.NewMap(sb, types.CollateralKey, "collateral", collections.StringKey, sdk.IntValue),
		Reservations: collections.NewMap(sb, types.ReservationsKey, "reservations",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey), sdk.IntValue),
		Triggers:   collections.NewMap(sb, types.TriggersKey, "triggers", collections.Uint64Key, codec.CollValue[types.TriggerOrder](cdc)),
		TriggerSeq: collections.NewSequence(sb, types.TriggerSeqKey, "trigger_seq"),
		TriggersByAccount: collections.NewKeySet(sb, types.TriggersByAccountKey, "triggers_by_account",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		TriggersByMarket: collections.NewKeySet(sb, types.TriggersByMarketKey, "triggers_by_market",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		AutoTopUp:     collections.NewKeySet(sb, types.AutoTopUpKey, "auto_top_up", collections.StringKey),
		Ledger:        collections.NewItem(sb, types.LedgerKey, "ledger", codec.CollValue[types.Ledger](cdc)),
		Allocations:   collections.NewMap(sb, types.AllocationsKey, "allocations", collections.Uint64Key, codec.CollValue[types.AllocationRecord](cdc)),
		UnwindIntents: collections.NewMap(sb, types.UnwindIntentsKey, "unwind_intents", collections.StringKey, collections.Uint64Value),
		LiqIndex: collections.NewKeySet(sb, types.LiqIndexKey, "liq_index",
			collections.PairKeyCodec(collections.StringKey, collections.BytesKey)),
		Premiums: collections.NewMap(sb, types.PremiumsKey, "premiums",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key), sdk.LegacyDecValue),
	}
	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema
	return k
}

// GetAuthority returns the module authority.
func (k Keeper) GetAuthority() string { return k.authority }

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

// ModuleAddress returns the module account that holds every balance.
func (k Keeper) ModuleAddress() sdk.AccAddress { return authtypes.NewModuleAddress(types.ModuleName) }

// GetParams returns the parameters.
func (k Keeper) GetParams(ctx sdk.Context) (types.Params, error) {
	p, err := k.Params.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.DefaultParams(), nil
		}
		return types.Params{}, err
	}
	return p, nil
}

// SetParams validates and stores the parameters.
func (k Keeper) SetParams(ctx sdk.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return k.Params.Set(ctx, p)
}

// GetMarket returns a perp market.
func (k Keeper) GetMarket(ctx sdk.Context, id string) (types.Market, error) {
	m, err := k.Markets.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Market{}, types.ErrMarketNotFound.Wrap(id)
		}
		return types.Market{}, err
	}
	return m, nil
}

// GetLedger returns the module ledger.
func (k Keeper) GetLedger(ctx sdk.Context) (types.Ledger, error) {
	l, err := k.Ledger.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.DefaultLedger(), nil
		}
		return types.Ledger{}, err
	}
	return l, nil
}

// AllMarkets returns every market in id order.
func (k Keeper) AllMarkets(ctx sdk.Context) ([]types.Market, error) {
	var out []types.Market
	err := k.Markets.Walk(ctx, nil, func(_ string, m types.Market) (bool, error) {
		out = append(out, m)
		return false, nil
	})
	return out, err
}
