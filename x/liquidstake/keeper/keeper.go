package keeper

import (
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper implements native liquid staking (spec §24, D-15): a non-rebasing
// stLUNC receipt whose exchange rate in uluna grows with staking rewards,
// objective validator whitelisting with equal weights and caps, epoch-batched
// delegation/undelegation and a redemption queue.
type Keeper struct {
	cdc       codec.BinaryCodec
	authority string

	accountKeeper  types.AccountKeeper
	bankKeeper     types.BankKeeper
	stakingKeeper  types.StakingKeeper
	distrKeeper    types.DistributionKeeper
	slashingKeeper types.SlashingKeeper
	// burnModuleName is the module account whose balance the treasury burns
	// every block (x/treasury "burn"); the burn share of the fee goes there.
	burnModuleName string

	Schema     collections.Schema
	Params     collections.Item[types.Params]
	Epoch      collections.Item[types.Epoch]
	Requests   collections.Map[uint64, types.UnstakeRequest]
	RequestSeq collections.Sequence
	// Owed is the uluna committed to queued or undelegated (not yet claimed)
	// requests. It is excluded from the exchange-rate assets and from the
	// instantly redeemable buffer.
	Owed       collections.Item[math.Int]
	Validators collections.Map[string, types.ValidatorState]
	// RequestsByAddr indexes request ids by owner for MsgClaim and queries.
	RequestsByAddr collections.KeySet[collections.Pair[string, uint64]]
}

// NewKeeper creates the x/liquidstake keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	authority string,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	stakingKeeper types.StakingKeeper,
	distrKeeper types.DistributionKeeper,
	slashingKeeper types.SlashingKeeper,
	burnModuleName string,
) Keeper {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic(fmt.Errorf("invalid liquidstake authority address: %w", err))
	}
	if accountKeeper.GetModuleAddress(types.ModuleName) == nil {
		panic("liquidstake module account is not registered in maccPerms")
	}

	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		cdc:            cdc,
		authority:      authority,
		accountKeeper:  accountKeeper,
		bankKeeper:     bankKeeper,
		stakingKeeper:  stakingKeeper,
		distrKeeper:    distrKeeper,
		slashingKeeper: slashingKeeper,
		burnModuleName: burnModuleName,
		Params:         collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Epoch:          collections.NewItem(sb, types.EpochKey, "epoch", codec.CollValue[types.Epoch](cdc)),
		Requests:       collections.NewMap(sb, types.RequestsKey, "requests", collections.Uint64Key, codec.CollValue[types.UnstakeRequest](cdc)),
		RequestSeq:     collections.NewSequence(sb, types.RequestSeqKey, "request_seq"),
		Owed:           collections.NewItem(sb, types.OwedKey, "owed", sdk.IntValue),
		Validators:     collections.NewMap(sb, types.ValidatorsKey, "validators", collections.StringKey, codec.CollValue[types.ValidatorState](cdc)),
		RequestsByAddr: collections.NewKeySet(sb, types.RequestsByAddrIx, "requests_by_addr", collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
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
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// ModuleAddress returns the module account address (the single delegator).
func (k Keeper) ModuleAddress() sdk.AccAddress {
	return k.accountKeeper.GetModuleAddress(types.ModuleName)
}

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

// SetParams validates and stores the module parameters.
func (k Keeper) SetParams(ctx sdk.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return k.Params.Set(ctx, p)
}

// GetEpoch returns the current epoch.
func (k Keeper) GetEpoch(ctx sdk.Context) (types.Epoch, error) {
	e, err := k.Epoch.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Epoch{}, nil
		}
		return types.Epoch{}, err
	}
	return e, nil
}

// GetOwed returns the uluna committed to unfilled requests.
func (k Keeper) GetOwed(ctx sdk.Context) (math.Int, error) {
	v, err := k.Owed.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.Int{}, err
	}
	return v, nil
}

func (k Keeper) addOwed(ctx sdk.Context, delta math.Int) error {
	owed, err := k.GetOwed(ctx)
	if err != nil {
		return err
	}
	owed = owed.Add(delta)
	if owed.IsNegative() {
		return fmt.Errorf("owed would become negative: %s", owed)
	}
	return k.Owed.Set(ctx, owed)
}

// bondDenom returns the staking bond denom (uluna).
func (k Keeper) bondDenom(ctx sdk.Context) (string, error) {
	return k.stakingKeeper.BondDenom(ctx)
}
