package keeper

import (
	"errors"
	"fmt"
	"time"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
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
	// feeSink receives the fee remainder after the burn share (spec §24.6).
	// It is a shared holder so that every copy of the keeper sees the sink
	// registered by the app after x/perp exists (SetFeeSink).
	feeSink *feeSinkHolder

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
	// OwedQueued is the part of Owed whose requests were not undelegated
	// yet: it is still backed by delegations, not by the module balance.
	OwedQueued collections.Item[math.Int]
	// OwedUnbonding records, per completion time, the uluna of the requests
	// marked by one undelegation batch (every request of a batch shares the
	// completion time). Entries with a completion time after the block time
	// are the part of Owed still in x/staking unbonding; matured entries are
	// pruned at the epoch. Live entries are bounded by MaxEntries + 1
	// because an epoch never lasts less than UnbondingTime / MaxEntries.
	OwedUnbonding collections.Map[time.Time, math.Int]
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
		feeSink:        &feeSinkHolder{sink: communityPoolSink{distr: distrKeeper}},
		Params:         collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Epoch:          collections.NewItem(sb, types.EpochKey, "epoch", codec.CollValue[types.Epoch](cdc)),
		Requests:       collections.NewMap(sb, types.RequestsKey, "requests", collections.Uint64Key, codec.CollValue[types.UnstakeRequest](cdc)),
		RequestSeq:     collections.NewSequence(sb, types.RequestSeqKey, "request_seq"),
		Owed:           collections.NewItem(sb, types.OwedKey, "owed", sdk.IntValue),
		Validators:     collections.NewMap(sb, types.ValidatorsKey, "validators", collections.StringKey, codec.CollValue[types.ValidatorState](cdc)),
		RequestsByAddr: collections.NewKeySet(sb, types.RequestsByAddrIx, "requests_by_addr", collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		OwedQueued:     collections.NewItem(sb, types.OwedQueuedKey, "owed_queued", sdk.IntValue),
		OwedUnbonding:  collections.NewMap(sb, types.OwedUnbondingKey, "owed_unbonding", sdk.TimeKey, sdk.IntValue),
	}
	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema
	return k
}

// feeSinkHolder is shared by every copy of the keeper.
type feeSinkHolder struct{ sink types.FeeSink }

// communityPoolSink is the fallback sink when no allocation cascade is
// registered (a keeper built without x/perp): the fee remainder funds the
// community pool.
type communityPoolSink struct{ distr types.DistributionKeeper }

// Deposit implements types.FeeSink.
func (s communityPoolSink) Deposit(ctx sdk.Context, fromModule string, coins sdk.Coins) error {
	if coins.IsZero() {
		return nil
	}
	return s.distr.FundCommunityPool(ctx, coins, authtypes.NewModuleAddress(fromModule))
}

// SetFeeSink registers the destination of the fee remainder: the app wires
// the x/perp sink, which feeds the insurance fund and operations through the
// allocation cascade of spec §23.1 (§24.6).
func (k Keeper) SetFeeSink(s types.FeeSink) { k.feeSink.sink = s }

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

// SetParams validates and stores the module parameters. Besides the
// stateless checks it enforces the epoch sizing rule of spec §24.4 against
// the live x/staking params (UnbondingTime, MaxEntries); MsgUpdateParams
// goes through here.
func (k Keeper) SetParams(ctx sdk.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := k.validateEpochSizing(ctx, p); err != nil {
		return err
	}
	return k.Params.Set(ctx, p)
}

// validateEpochSizing checks epoch_blocks × expected_block_time ≥
// UnbondingTime / MaxEntries with the current x/staking params.
func (k Keeper) validateEpochSizing(ctx sdk.Context, p types.Params) error {
	unbonding, maxEntries, err := k.stakingLimits(ctx)
	if err != nil {
		return err
	}
	return p.ValidateEpochSizing(unbonding, maxEntries)
}

// stakingLimits returns the x/staking UnbondingTime and MaxEntries.
func (k Keeper) stakingLimits(ctx sdk.Context) (time.Duration, uint32, error) {
	unbonding, err := k.stakingKeeper.UnbondingTime(ctx)
	if err != nil {
		return 0, 0, err
	}
	maxEntries, err := k.stakingKeeper.MaxEntries(ctx)
	if err != nil {
		return 0, 0, err
	}
	return unbonding, maxEntries, nil
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

// GetOwedQueued returns the part of Owed not yet undelegated.
func (k Keeper) GetOwedQueued(ctx sdk.Context) (math.Int, error) {
	v, err := k.OwedQueued.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.Int{}, err
	}
	return v, nil
}

func (k Keeper) addOwedQueued(ctx sdk.Context, delta math.Int) error {
	v, err := k.GetOwedQueued(ctx)
	if err != nil {
		return err
	}
	v = v.Add(delta)
	if v.IsNegative() {
		return fmt.Errorf("owed_queued would become negative: %s", v)
	}
	return k.OwedQueued.Set(ctx, v)
}

// addOwedUnbonding records amount as unbonding until completion.
func (k Keeper) addOwedUnbonding(ctx sdk.Context, completion time.Time, amount math.Int) error {
	cur, err := k.OwedUnbonding.Get(ctx, completion)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		cur = math.ZeroInt()
	}
	return k.OwedUnbonding.Set(ctx, completion, cur.Add(amount))
}

// GetOwedUnbonding returns the part of Owed whose undelegation completes
// after the block time. The walk only visits live batches (bounded by
// MaxEntries + 1, see OwedUnbonding).
func (k Keeper) GetOwedUnbonding(ctx sdk.Context) (math.Int, error) {
	sum := math.ZeroInt()
	rng := new(collections.Range[time.Time]).StartExclusive(ctx.BlockTime())
	err := k.OwedUnbonding.Walk(ctx, rng, func(_ time.Time, v math.Int) (bool, error) {
		sum = sum.Add(v)
		return false, nil
	})
	return sum, err
}

// pruneOwedUnbonding drops the batches that matured at or before the block
// time: their uluna is back in the module balance (x/staking completes
// matured entries in its EndBlock, which runs before this module's).
func (k Keeper) pruneOwedUnbonding(ctx sdk.Context) error {
	rng := new(collections.Range[time.Time]).EndInclusive(ctx.BlockTime())
	return k.OwedUnbonding.Clear(ctx, rng)
}

// rebuildOwedAggregates recomputes OwedQueued and OwedUnbonding from the
// requests. Used by InitGenesis and the v1→v2 migration; it walks every
// request, so it never runs in a block.
func (k Keeper) rebuildOwedAggregates(ctx sdk.Context) error {
	if err := k.OwedUnbonding.Clear(ctx, nil); err != nil {
		return err
	}
	queued := math.ZeroInt()
	if err := k.Requests.Walk(ctx, nil, func(_ uint64, r types.UnstakeRequest) (bool, error) {
		if !r.Undelegated {
			queued = queued.Add(r.Amount)
			return false, nil
		}
		return false, k.addOwedUnbonding(ctx, r.CompletionTime, r.Amount)
	}); err != nil {
		return err
	}
	return k.OwedQueued.Set(ctx, queued)
}

// bondDenom returns the staking bond denom (uluna).
func (k Keeper) bondDenom(ctx sdk.Context) (string, error) {
	return k.stakingKeeper.BondDenom(ctx)
}
