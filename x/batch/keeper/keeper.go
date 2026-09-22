package keeper

import (
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// Keeper implements the sealed-bid, uniform-price call auction (spec §13–§16)
// with escrowed intents, bonded solvers, integrator attribution (§23.2) and
// the perpetuals margin hook (§14.3).
type Keeper struct {
	cdc       codec.BinaryCodec
	authority string

	bankKeeper   types.BankKeeper
	oracleKeeper types.OracleKeeper
	feeSink      types.FeeSink
	marginHook   types.MarginHook

	Schema           collections.Schema
	Params           collections.Item[types.Params]
	Markets          collections.Map[string, types.Market]
	Intents          collections.Map[uint64, types.Intent]
	IntentSeq        collections.Sequence
	IntentsByAccount collections.KeySet[collections.Pair[string, uint64]]
	IntentsByMarket  collections.KeySet[collections.Pair[string, uint64]]
	Solvers          collections.Map[string, types.Solver]
	SolverEscrow     collections.Map[collections.Pair[string, string], math.Int]
	Commits          collections.Map[collections.Triple[uint64, string, string], types.Commit]
	CommitSeq        collections.Sequence
	Results          collections.Map[collections.Pair[uint64, string], types.BatchResult]
	Frontends        collections.Map[string, types.Frontend]
	Approvals        collections.Map[collections.Pair[string, string], types.FrontendApproval]
}

// NewKeeper creates the x/batch keeper. feeSink receives protocol fees and
// slashes; marginHook may be nil until x/perp registers one.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	authority string,
	bankKeeper types.BankKeeper,
	oracleKeeper types.OracleKeeper,
	feeSink types.FeeSink,
) Keeper {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic(fmt.Errorf("invalid batch authority address: %w", err))
	}
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		cdc:          cdc,
		authority:    authority,
		bankKeeper:   bankKeeper,
		oracleKeeper: oracleKeeper,
		feeSink:      feeSink,
		Params:       collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Markets:      collections.NewMap(sb, types.MarketsKey, "markets", collections.StringKey, codec.CollValue[types.Market](cdc)),
		Intents:      collections.NewMap(sb, types.IntentsKey, "intents", collections.Uint64Key, codec.CollValue[types.Intent](cdc)),
		IntentSeq:    collections.NewSequence(sb, types.IntentSeqKey, "intent_seq"),
		IntentsByAccount: collections.NewKeySet(sb, types.IntentsByAccountKey, "intents_by_account",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		IntentsByMarket: collections.NewKeySet(sb, types.IntentsByMarketKey, "intents_by_market",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		Solvers: collections.NewMap(sb, types.SolversKey, "solvers", collections.StringKey, codec.CollValue[types.Solver](cdc)),
		SolverEscrow: collections.NewMap(sb, types.SolverEscrowKey, "solver_escrow",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey), sdk.IntValue),
		Commits: collections.NewMap(sb, types.CommitsKey, "commits",
			collections.TripleKeyCodec(collections.Uint64Key, collections.StringKey, collections.StringKey), codec.CollValue[types.Commit](cdc)),
		CommitSeq: collections.NewSequence(sb, types.CommitSeqKey, "commit_seq"),
		Results: collections.NewMap(sb, types.ResultsKey, "results",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey), codec.CollValue[types.BatchResult](cdc)),
		Frontends: collections.NewMap(sb, types.FrontendsKey, "frontends", collections.StringKey, codec.CollValue[types.Frontend](cdc)),
		Approvals: collections.NewMap(sb, types.ApprovalsKey, "approvals",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey), codec.CollValue[types.FrontendApproval](cdc)),
	}
	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema
	return k
}

// SetMarginHook registers the perpetuals margin hook (x/perp).
func (k *Keeper) SetMarginHook(h types.MarginHook) { k.marginHook = h }

// GetAuthority returns the module authority.
func (k Keeper) GetAuthority() string { return k.authority }

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// ModuleAddress returns the escrow account of the module.
func (k Keeper) ModuleAddress() sdk.AccAddress { return authtypes.NewModuleAddress(types.ModuleName) }

// GetParams returns the module parameters.
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

// GetMarket returns a market.
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

// ReferencePrice returns P_ref of a market (quote per base) from x/oracle.
func (k Keeper) ReferencePrice(ctx sdk.Context, market types.Market) (math.LegacyDec, error) {
	rate, err := k.oracleKeeper.GetPrice(ctx, market.OracleDenom)
	if err != nil || !rate.IsPositive() {
		return math.LegacyZeroDec(), types.ErrReferencePriceUnset.Wrap(market.Id)
	}
	return rate, nil
}

// BatchToResolve returns the batch id resolved at the EndBlock of `height`
// and whether there is one: b = height − W − 1.
func BatchToResolve(height int64, commitWindow int64) (uint64, bool) {
	b := height - commitWindow - 1
	if b < 1 {
		return 0, false
	}
	return uint64(b), true
}

// CommitWindowOpen reports whether commits for batch b are accepted at height.
func CommitWindowOpen(batch uint64, height, commitWindow int64) bool {
	return height > int64(batch) && height <= int64(batch)+commitWindow
}

// RevealBlock reports whether reveals for batch b are accepted at height.
func RevealBlock(batch uint64, height, commitWindow int64) bool {
	return height == int64(batch)+commitWindow+1
}
