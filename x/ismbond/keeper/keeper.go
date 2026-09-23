package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/keeper"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	pdkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/keeper"
	pdtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/types"
	corekeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/keeper"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	"github.com/classic-terra/core/v4/x/ismbond/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper implements the economic guarantee of the ISM (spec §6.6, D-17):
// operators bond LUNC against the checkpoints they sign; evidence of
// equivocation or of an outbound attestation that contradicts the local
// merkle tree hook is judged on chain, without a committee.
type Keeper struct {
	cdc       codec.Codec
	authority string

	bankKeeper  types.BankKeeper
	coreKeeper  types.CoreKeeper
	distrKeeper types.DistributionKeeper

	Schema      collections.Schema
	Params      collections.Item[types.Params]
	Operators   collections.Map[string, types.Operator]
	ByValidator collections.Map[string, string]
	Roots       collections.Map[collections.Pair[uint64, uint32], types.RootRecord]
}

// NewKeeper creates the x/ismbond keeper.
func NewKeeper(
	cdc codec.Codec,
	storeService storetypes.KVStoreService,
	authority string,
	bankKeeper types.BankKeeper,
	coreKeeper types.CoreKeeper,
	distrKeeper types.DistributionKeeper,
) Keeper {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic(fmt.Errorf("invalid ismbond authority address: %w", err))
	}
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		cdc: cdc, authority: authority, bankKeeper: bankKeeper, coreKeeper: coreKeeper, distrKeeper: distrKeeper,
		Params:      collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Operators:   collections.NewMap(sb, types.OperatorsKey, "operators", collections.StringKey, codec.CollValue[types.Operator](cdc)),
		ByValidator: collections.NewMap(sb, types.ByValidatorKey, "by_validator", collections.StringKey, collections.StringValue),
		Roots: collections.NewMap(sb, types.RootsKey, "roots",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint32Key), codec.CollValue[types.RootRecord](cdc)),
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

// CoreAdapter exposes the Hyperlane core keeper through the query servers of
// its sub-keepers (their stores are private).
type CoreAdapter struct{ core *corekeeper.Keeper }

var _ types.CoreKeeper = CoreAdapter{}

// NewCoreAdapter wraps the Hyperlane core keeper.
func NewCoreAdapter(core *corekeeper.Keeper) CoreAdapter { return CoreAdapter{core: core} }

// GetMailbox implements types.CoreKeeper.
func (a CoreAdapter) GetMailbox(ctx context.Context, id util.HexAddress) (coretypes.Mailbox, error) {
	return a.core.GetMailbox(ctx, id)
}

// MerkleTreeHookQuery implements types.CoreKeeper.
func (a CoreAdapter) MerkleTreeHookQuery(ctx context.Context, req *pdtypes.QueryMerkleTreeHookRequest) (*pdtypes.QueryMerkleTreeHookResponse, error) {
	return pdkeeper.NewQueryServerImpl(&a.core.PostDispatchKeeper).MerkleTreeHook(ctx, req)
}

// IsmQuery implements types.CoreKeeper.
func (a CoreAdapter) IsmQuery(ctx context.Context, req *ismtypes.QueryIsmRequest) (*ismtypes.QueryIsmResponse, error) {
	return ismkeeper.NewQueryServerImpl(&a.core.IsmKeeper).Ism(ctx, req)
}
