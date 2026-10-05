package keeper_test

import (
	"errors"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/liquidstake/keeper"
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// Tests for the conformance rows L-03, L-08, L-09 and L-15
// (docs/testnet/SPEC-CONFORMANCE-2026-10-04.md, spec §24).

// reward allocates amount uluna of rewards to the validator.
func (f *fixture) reward(t *testing.T, val sdk.ValAddress, amount int64) {
	t.Helper()
	coins := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(amount)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", coins))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToModule(f.ctx, "mint", "distribution", coins))
	v, err := f.app.StakingKeeper.GetValidator(f.ctx, val)
	require.NoError(t, err)
	require.NoError(t, f.app.DistrKeeper.AllocateTokensToValidator(f.ctx, v, sdk.NewDecCoinsFromCoins(coins...)))
}

// addValidator creates and bonds a second validator with the same
// self-bond as the fixture's.
func (f *fixture) addValidator(t *testing.T) sdk.ValAddress {
	t.Helper()
	priv := ed25519.GenPrivKey()
	valAddr := sdk.ValAddress(priv.PubKey().Address())
	coins := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(2_000_000_000_000)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", coins))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", sdk.AccAddress(valAddr), coins))
	msg, err := stakingtypes.NewMsgCreateValidator(
		valAddr.String(), priv.PubKey(),
		sdk.NewCoin("uluna", math.NewInt(1_000_000_000_000)),
		stakingtypes.NewDescription("val2", "", "", "", ""),
		stakingtypes.NewCommissionRates(math.LegacyNewDecWithPrec(5, 2), math.LegacyNewDecWithPrec(20, 2), math.LegacyNewDecWithPrec(1, 2)),
		math.OneInt(),
	)
	require.NoError(t, err)
	_, err = stakingkeeper.NewMsgServerImpl(f.app.StakingKeeper).CreateValidator(f.ctx, msg)
	require.NoError(t, err)
	_, err = f.app.StakingKeeper.ApplyAndReturnValidatorSetUpdates(f.ctx)
	require.NoError(t, err)
	return valAddr
}

func (f *fixture) setStaking(t *testing.T, unbonding time.Duration, maxEntries uint32) {
	t.Helper()
	sp, err := f.app.StakingKeeper.GetParams(f.ctx)
	require.NoError(t, err)
	sp.UnbondingTime, sp.MaxEntries = unbonding, maxEntries
	require.NoError(t, f.app.StakingKeeper.SetParams(f.ctx, sp))
}

func (f *fixture) epochNumber(t *testing.T) uint64 {
	t.Helper()
	e, err := f.app.LiquidStakeKeeper.GetEpoch(f.ctx)
	require.NoError(t, err)
	return e.Number
}

// requireRateComponents recomputes the rate from the query fields and
// checks the identities documented in query.proto (row L-15).
func (f *fixture) requireRateComponents(t *testing.T) *types.QueryExchangeRateResponse {
	t.Helper()
	q, err := keeper.NewQueryServerImpl(f.app.LiquidStakeKeeper).ExchangeRate(f.ctx, &types.QueryExchangeRateRequest{})
	require.NoError(t, err)
	p, err := f.app.LiquidStakeKeeper.GetParams(f.ctx)
	require.NoError(t, err)

	require.Equal(t, q.PendingRewards.Sub(p.FeeRate.MulInt(q.PendingRewards).TruncateInt()).String(), q.PendingRewardsNet.String(), "pending rewards net of fee_rate")
	require.Equal(t, q.Owed.String(), q.OwedQueued.Add(q.OwedUnbonding).Add(q.OwedLiquid).String(), "owed = queued + unbonding + liquid")
	assets := q.Delegated.Add(q.Unbonding).Add(q.Balance).Add(q.PendingRewardsNet).Sub(q.Owed)
	require.Equal(t, assets.String(), q.Assets.String(), "assets = delegated + unbonding + balance + pending_net - owed")
	require.True(t, math.LegacyNewDecFromInt(assets).Quo(math.LegacyNewDecFromInt(q.StSupply.Amount)).Equal(q.ExchangeRate),
		"rate %s not reproduced by the components", q.ExchangeRate)
	buffer := q.Balance.Sub(q.OwedLiquid)
	if buffer.IsNegative() {
		buffer = math.ZeroInt()
	}
	require.Equal(t, buffer.String(), q.Buffer.String(), "buffer = max(0, balance - owed_liquid)")
	bank := f.app.BankKeeper.GetBalance(f.ctx, f.app.LiquidStakeKeeper.ModuleAddress(), "uluna").Amount
	require.Equal(t, bank.String(), q.Balance.String(), "balance is the module's bank balance")
	return q
}

// L-03: the rate never falls at an epoch without a slash. Rewards arrive
// every few blocks; the rate is read before and after every epoch.
func TestRateIsMonotoneAcrossEpochsWithoutSlash(t *testing.T) {
	f := setup(t)
	k := f.app.LiquidStakeKeeper
	_, _, err := k.Stake(f.ctx, f.user, sdk.NewCoin("uluna", math.NewInt(100_000_000)))
	require.NoError(t, err)
	f.advance(t, testEpochBlocks, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx))

	prev, _, err := k.ExchangeRate(f.ctx)
	require.NoError(t, err)
	for epoch := 0; epoch < 3; epoch++ {
		if epoch == 1 {
			// a queued redemption so the epoch also runs an undelegation batch
			res, err := k.Unstake(f.ctx, f.user, sdk.NewCoin(types.StDenom, math.NewInt(30_000_000)))
			require.NoError(t, err)
			require.False(t, res.Instant)
		}
		for i := 0; i < 3; i++ {
			f.advance(t, 10, 15*time.Second)
			f.reward(t, f.valAddr, 466_024+int64(i)*1000)
			rate, _, err := k.ExchangeRate(f.ctx)
			require.NoError(t, err)
			require.True(t, rate.GTE(prev), "rate fell between epochs: %s -> %s", prev, rate)
			prev = rate
		}
		before := f.epochNumber(t)
		f.advance(t, testEpochBlocks-30, 15*time.Second)
		require.NoError(t, k.EndBlocker(f.ctx))
		require.Equal(t, before+1, f.epochNumber(t), "the epoch ran")
		rate, totals, err := k.ExchangeRate(f.ctx)
		require.NoError(t, err)
		require.True(t, totals.PendingRewards.IsZero(), "rewards withdrawn at the epoch")
		require.True(t, rate.GTE(prev), "rate fell at epoch %d with no slash: %s -> %s", before, prev, rate)
		prev = rate
	}
	require.True(t, prev.GT(math.LegacyOneDec()))
	require.Nil(t, f.event("terra.liquidstake.v1.EventSlashAbsorbed"))
	require.Nil(t, f.event("terra.liquidstake.v1.EventInvariantBroken"))
}

// L-09: the 2% buffer serves a small redemption while a queued request is
// still unbonding, and the epoch delegates the surplus again.
func TestBufferServesSmallRedemptionsWhileQueueUnbonds(t *testing.T) {
	f := setup(t)
	k := f.app.LiquidStakeKeeper
	_, _, err := k.Stake(f.ctx, f.user, sdk.NewCoin("uluna", math.NewInt(100_000_000)))
	require.NoError(t, err)
	f.advance(t, testEpochBlocks, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx)) // 98M delegated, 2M balance

	res, err := k.Unstake(f.ctx, f.user, sdk.NewCoin(types.StDenom, math.NewInt(50_000_000)))
	require.NoError(t, err)
	require.False(t, res.Instant)
	_, totals, err := k.ExchangeRate(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "50000000", totals.OwedQueued.String())
	require.Equal(t, "2000000", totals.Buffer().String(), "a queued request is backed by delegations, not by the balance")

	f.advance(t, testEpochBlocks, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx))
	q := f.requireRateComponents(t)
	require.Equal(t, "50000000", q.Unbonding.String())
	require.Equal(t, "50000000", q.OwedUnbonding.String())
	require.True(t, q.OwedQueued.IsZero())
	require.True(t, q.OwedLiquid.IsZero())
	// assets 50M → target buffer 1M; the other 1M of the balance was delegated
	require.Equal(t, "1000000", q.Balance.String())
	require.Equal(t, "1000000", q.Buffer.String())
	require.Equal(t, "49000000", q.Delegated.String())

	before := f.balance("uluna")
	res, err = k.Unstake(f.ctx, f.user, sdk.NewCoin(types.StDenom, math.NewInt(500_000)))
	require.NoError(t, err)
	require.True(t, res.Instant, "the buffer pays at once while the queue is unbonding")
	require.Equal(t, before.Add(math.NewInt(500_000)).String(), f.balance("uluna").String())

	// maturity: the unbonded uluna is owed_liquid and reserved for the claim
	f.advance(t, 1, testUnbonding+time.Second)
	_, err = f.app.StakingKeeper.CompleteUnbonding(f.ctx, k.ModuleAddress(), f.valAddr)
	require.NoError(t, err)
	q = f.requireRateComponents(t)
	require.Equal(t, "50000000", q.OwedLiquid.String())
	require.True(t, q.OwedUnbonding.IsZero())
	require.Equal(t, "500000", q.Buffer.String())
	res, err = k.Unstake(f.ctx, f.user, sdk.NewCoin(types.StDenom, math.NewInt(600_000)))
	require.NoError(t, err)
	require.False(t, res.Instant, "the matured claim's uluna is not spent on new redemptions")
	paid, _, err := k.Claim(f.ctx, f.user)
	require.NoError(t, err)
	require.Equal(t, "50000000", paid.Amount.String())
	require.NoError(t, k.CheckInvariants(f.ctx))
}

// L-15: the query components reproduce the rate with queued, unbonding and
// matured requests and pending rewards all present.
func TestExchangeRateComponentsReproduceTheRate(t *testing.T) {
	f := setup(t)
	k := f.app.LiquidStakeKeeper
	_, _, err := k.Stake(f.ctx, f.user, sdk.NewCoin("uluna", math.NewInt(100_000_000)))
	require.NoError(t, err)
	f.advance(t, testEpochBlocks, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx))
	f.requireRateComponents(t)

	_, err = k.Unstake(f.ctx, f.user, sdk.NewCoin(types.StDenom, math.NewInt(30_000_000)))
	require.NoError(t, err)
	f.advance(t, testEpochBlocks, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx)) // 30M unbonding
	f.advance(t, 1, time.Second)
	f.reward(t, f.valAddr, 7_777_777)
	_, err = k.Unstake(f.ctx, f.user, sdk.NewCoin(types.StDenom, math.NewInt(20_000_000)))
	require.NoError(t, err) // 20M queued
	q := f.requireRateComponents(t)
	require.True(t, q.PendingRewards.IsPositive())
	require.True(t, q.OwedQueued.IsPositive())
	require.Equal(t, "30000000", q.OwedUnbonding.String())
	require.True(t, q.ExchangeRate.GT(math.LegacyOneDec()))
	require.NoError(t, k.CheckInvariants(f.ctx))

	// the 30M batch matures: owed_liquid, before the claim
	f.advance(t, 1, testUnbonding+time.Second)
	_, err = f.app.StakingKeeper.CompleteUnbonding(f.ctx, k.ModuleAddress(), f.valAddr)
	require.NoError(t, err)
	q = f.requireRateComponents(t)
	require.Equal(t, "30000000", q.OwedLiquid.String())
	require.True(t, q.Unbonding.IsZero())
	require.NoError(t, k.CheckInvariants(f.ctx))
}

// L-08: epoch_blocks × expected_block_time ≥ UnbondingTime / MaxEntries is
// enforced by MsgUpdateParams and InitGenesis against the x/staking params.
func TestEpochSizingRuleIsEnforced(t *testing.T) {
	f := setup(t)
	k := f.app.LiquidStakeKeeper
	ms := keeper.NewMsgServerImpl(k)
	withEpoch := func(blocks int64, blockTime time.Duration) types.Params {
		p, err := k.GetParams(f.ctx)
		require.NoError(t, err)
		p.EpochBlocks, p.ExpectedBlockTime = blocks, blockTime
		return p
	}
	update := func(p types.Params) error {
		_, err := ms.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: k.GetAuthority(), Params: p})
		return err
	}

	// testenv: 300 s unbonding, 7 entries, 1 s blocks → at least 43 blocks
	err := update(withEpoch(30, time.Second))
	require.ErrorContains(t, err, "below unbonding_time")
	require.Error(t, update(withEpoch(42, time.Second)))
	require.NoError(t, update(withEpoch(43, time.Second)))
	require.NoError(t, update(withEpoch(45, time.Second)))
	// or keep 30 blocks with unbonding ≤ 210 s
	f.setStaking(t, 210*time.Second, 7)
	require.NoError(t, update(withEpoch(30, time.Second)))
	f.setStaking(t, 180*time.Second, 7)
	require.NoError(t, update(withEpoch(30, time.Second)))
	// mainnet: 21 d / 7 = 3 d; the default 50400 blocks × 6 s = 3.5 d passes, 43200 is the minimum
	f.setStaking(t, 21*24*time.Hour, 7)
	require.NoError(t, update(withEpoch(types.DefaultEpochBlocks, types.DefaultExpectedBlockTime)))
	require.NoError(t, update(withEpoch(43_200, 6*time.Second)))
	require.Error(t, update(withEpoch(43_199, 6*time.Second)))

	// the block time assumption is bounded by constants
	require.ErrorContains(t, withEpoch(50_400, 0).Validate(), "expected_block_time")
	require.ErrorContains(t, withEpoch(50_400, 61*time.Second).Validate(), "expected_block_time")

	// InitGenesis checks the rule as well
	gs := types.DefaultGenesisState()
	gs.Params = withEpoch(30, time.Second)
	f.setStaking(t, testUnbonding, 7)
	require.ErrorContains(t, k.InitGenesis(f.ctx, gs), "below unbonding_time")
}

// L-08: whatever the params say, an epoch never closes before
// UnbondingTime / MaxEntries of block time (x/staking params can change
// after the liquidstake params were validated).
func TestEpochWaitsForTheTimeFloor(t *testing.T) {
	f := setup(t)
	k := f.app.LiquidStakeKeeper
	start := f.epochNumber(t)

	f.advance(t, testEpochBlocks, 30*time.Second)
	due, err := k.EpochDue(f.ctx)
	require.NoError(t, err)
	require.False(t, due, "45 blocks in 30 s: below 300 s / 7")
	f.advance(t, 0, 12*time.Second) // 42 s
	due, err = k.EpochDue(f.ctx)
	require.NoError(t, err)
	require.False(t, due)
	f.advance(t, 0, time.Second) // 43 s ≥ 42.857 s
	due, err = k.EpochDue(f.ctx)
	require.NoError(t, err)
	require.True(t, due)

	// governance doubles the unbonding time in x/staking: the floor follows
	f.setStaking(t, 2*testUnbonding, 7)
	due, err = k.EpochDue(f.ctx)
	require.NoError(t, err)
	require.False(t, due)
	f.advance(t, 0, 43*time.Second) // 86 s ≥ 85.71 s
	require.NoError(t, k.EndBlocker(f.ctx))
	require.Equal(t, start+1, f.epochNumber(t))
}

// L-08: a validator already at MaxEntries is left out of the batch before
// the pro-rata split, so the others carry the whole batch and the request
// is undelegated in this epoch.
func TestUndelegationBatchSkipsValidatorsAtMaxEntries(t *testing.T) {
	f := setup(t)
	k := f.app.LiquidStakeKeeper
	valB := f.addValidator(t)
	_, _, err := k.Stake(f.ctx, f.user, sdk.NewCoin("uluna", math.NewInt(100_000_000)))
	require.NoError(t, err)
	f.advance(t, testEpochBlocks, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx))
	for _, v := range []sdk.ValAddress{f.valAddr, valB} {
		d, err := f.app.StakingKeeper.GetDelegation(f.ctx, k.ModuleAddress(), v)
		require.NoError(t, err)
		require.Equal(t, "49000000", d.Shares.TruncateInt().String())
	}

	// x/staking now allows 2 entries; fill both on the first validator
	f.setStaking(t, testUnbonding, 2)
	for i := 0; i < 2; i++ {
		f.advance(t, 1, time.Second)
		_, _, err := f.app.StakingKeeper.Undelegate(f.ctx, k.ModuleAddress(), f.valAddr, math.LegacyNewDec(1))
		require.NoError(t, err)
	}
	full, err := f.app.StakingKeeper.HasMaxUnbondingDelegationEntries(f.ctx, k.ModuleAddress(), f.valAddr)
	require.NoError(t, err)
	require.True(t, full)

	res, err := k.Unstake(f.ctx, f.user, sdk.NewCoin(types.StDenom, math.NewInt(40_000_000)))
	require.NoError(t, err)
	require.False(t, res.Instant)

	// time floor with 2 entries: 150 s
	f.advance(t, testEpochBlocks, 151*time.Second)
	require.NoError(t, k.EndBlocker(f.ctx))
	req, err := k.Requests.Get(f.ctx, res.RequestID)
	require.NoError(t, err)
	require.True(t, req.Undelegated, "the batch went to the validator with a free entry")
	ubdA, err := f.app.StakingKeeper.GetUnbondingDelegation(f.ctx, k.ModuleAddress(), f.valAddr)
	require.NoError(t, err)
	require.Len(t, ubdA.Entries, 2, "no entry beyond MaxEntries")
	ubdB, err := f.app.StakingKeeper.GetUnbondingDelegation(f.ctx, k.ModuleAddress(), valB)
	require.NoError(t, err)
	require.Len(t, ubdB.Entries, 1)
	require.Equal(t, "40000000", ubdB.Entries[0].Balance.String())
	owedUnbonding, err := k.GetOwedUnbonding(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "40000000", owedUnbonding.String())
	require.Nil(t, f.event("terra.liquidstake.v1.EventInvariantBroken"))
}

// L-15: invariant 1 is solvency (gross assets ≥ owed) and the rate
// identity; a violation found at the epoch emits EventInvariantBroken and
// never halts the chain.
func TestInvariantsAreAssertedAtTheEpoch(t *testing.T) {
	f := setup(t)
	k := f.app.LiquidStakeKeeper
	_, _, err := k.Stake(f.ctx, f.user, sdk.NewCoin("uluna", math.NewInt(100_000_000)))
	require.NoError(t, err)
	f.advance(t, testEpochBlocks, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx))
	require.Nil(t, f.event("terra.liquidstake.v1.EventInvariantBroken"))

	// owed above every uluna the module controls: invariant 1
	require.NoError(t, k.Owed.Set(f.ctx, math.NewInt(1_000_000_000)))
	var inv *keeper.InvariantError
	require.True(t, errors.As(k.CheckInvariants(f.ctx), &inv))
	require.Equal(t, uint32(1), inv.Invariant)

	// owed without a request: invariant 2, reported at the epoch
	require.NoError(t, k.Owed.Set(f.ctx, math.NewInt(1)))
	require.True(t, errors.As(k.CheckInvariants(f.ctx), &inv))
	require.Equal(t, uint32(2), inv.Invariant)
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	f.advance(t, testEpochBlocks, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx), "a broken invariant never halts the chain")
	ev := f.event("terra.liquidstake.v1.EventInvariantBroken")
	require.NotNil(t, ev)
	require.Equal(t, "2", attr(ev, "invariant"))
	require.Contains(t, attr(ev, "detail"), "sum of requests")
}

// Migrate1to2 derives the owed aggregates from the requests and fills the
// new expected_block_time param; InitGenesis does the same from a genesis.
func TestMigrate1to2AndGenesisRebuildOwedAggregates(t *testing.T) {
	f := setup(t)
	k := f.app.LiquidStakeKeeper
	_, _, err := k.Stake(f.ctx, f.user, sdk.NewCoin("uluna", math.NewInt(100_000_000)))
	require.NoError(t, err)
	f.advance(t, testEpochBlocks, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx))
	_, err = k.Unstake(f.ctx, f.user, sdk.NewCoin(types.StDenom, math.NewInt(30_000_000)))
	require.NoError(t, err)
	f.advance(t, testEpochBlocks, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx))
	_, err = k.Unstake(f.ctx, f.user, sdk.NewCoin(types.StDenom, math.NewInt(20_000_000)))
	require.NoError(t, err)
	require.NoError(t, k.CheckInvariants(f.ctx))

	// a v1 store: no aggregates, no expected_block_time
	wipe := func() {
		require.NoError(t, k.OwedQueued.Remove(f.ctx))
		require.NoError(t, k.OwedUnbonding.Clear(f.ctx, nil))
	}
	wipe()
	p, err := k.GetParams(f.ctx)
	require.NoError(t, err)
	p.ExpectedBlockTime = 0
	require.NoError(t, k.Params.Set(f.ctx, p))
	require.Error(t, k.CheckInvariants(f.ctx))

	require.NoError(t, keeper.NewMigrator(k).Migrate1to2(f.ctx))
	require.NoError(t, k.CheckInvariants(f.ctx))
	p, err = k.GetParams(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.DefaultExpectedBlockTime, p.ExpectedBlockTime)
	q, err := k.GetOwedQueued(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "20000000", q.String())
	u, err := k.GetOwedUnbonding(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "30000000", u.String())

	gs, err := k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	wipe()
	require.NoError(t, k.InitGenesis(f.ctx, gs))
	require.NoError(t, k.CheckInvariants(f.ctx))
}

// Regression for the testenv halt of 2026-10-04 (height 3242, "duplicate entry
// Validator" in the validator set update): an epoch that both undelegates a
// queued batch and delegates surplus to the same validator must keep the
// validator's tokens equal to the bonded pool and produce a valid validator
// set update. Before the fix delegateSurplus passed the validator struct
// snapshotted before undelegateBatch to x/staking Delegate, which erased the
// undelegation from validator.tokens and left a second power-index entry.
func TestEpochUndelegateThenDelegateKeepsValidatorConsistent(t *testing.T) {
	f := setup(t)
	k := f.app.LiquidStakeKeeper
	_, _, err := k.Stake(f.ctx, f.user, sdk.NewCoin("uluna", math.NewInt(100_000_000)))
	require.NoError(t, err)
	f.advance(t, testEpochBlocks, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx))

	// a queued redemption (undelegated at the next epoch) and new stake (surplus
	// delegated at the same epoch)
	res, err := k.Unstake(f.ctx, f.user, sdk.NewCoin(types.StDenom, math.NewInt(40_000_000)))
	require.NoError(t, err)
	require.False(t, res.Instant)
	_, _, err = k.Stake(f.ctx, f.user, sdk.NewCoin("uluna", math.NewInt(30_000_000)))
	require.NoError(t, err)

	f.advance(t, testEpochBlocks, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx))
	ev := f.event("terra.liquidstake.v1.EventEpochProcessed")
	require.NotNil(t, ev)

	v, err := f.app.StakingKeeper.GetValidator(f.ctx, f.valAddr)
	require.NoError(t, err)
	pool := f.app.BankKeeper.GetBalance(f.ctx, f.app.AccountKeeper.GetModuleAddress(stakingtypes.BondedPoolName), "uluna").Amount
	require.Equal(t, pool.String(), v.Tokens.String(), "validator tokens must equal the bonded pool (single bonded validator)")

	updates, err := f.app.StakingKeeper.ApplyAndReturnValidatorSetUpdates(f.ctx)
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, u := range updates {
		key := u.PubKey.String()
		require.False(t, seen[key], "duplicate validator in the set update")
		seen[key] = true
	}
	require.NoError(t, k.CheckInvariants(f.ctx))
}

// A validator jailed for downtime leaves the bonded set: the epoch must still
// record why it is ineligible (reason "jailed", not "not in the evaluated
// set"), redelegate its share away, and the delegations query must list the
// evaluated validators that hold no module delegation with zero tokens.
func TestJailedValidatorVerdictAndDelegationsQuery(t *testing.T) {
	f := setup(t)
	k := f.app.LiquidStakeKeeper
	p, err := k.GetParams(f.ctx)
	require.NoError(t, err)
	p.ValidatorCap = math.LegacyNewDecWithPrec(5, 1)
	require.NoError(t, k.SetParams(f.ctx, p))
	val2 := f.addValidator(t)
	val3 := f.addValidator(t)

	_, _, err = k.Stake(f.ctx, f.user, sdk.NewCoin("uluna", math.NewInt(300_000_000)))
	require.NoError(t, err)
	f.advance(t, testEpochBlocks, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx))
	_, err = f.app.StakingKeeper.ApplyAndReturnValidatorSetUpdates(f.ctx)
	require.NoError(t, err)
	_, err = f.app.StakingKeeper.GetDelegation(f.ctx, k.ModuleAddress(), val3)
	require.NoError(t, err, "val3 holds a module delegation after the first epoch")

	// jail val3: it leaves the bonded set at the next validator-set update
	v3, err := f.app.StakingKeeper.GetValidator(f.ctx, val3)
	require.NoError(t, err)
	cons, err := v3.GetConsAddr()
	require.NoError(t, err)
	require.NoError(t, f.app.StakingKeeper.Jail(f.ctx, sdk.ConsAddress(cons)))
	_, err = f.app.StakingKeeper.ApplyAndReturnValidatorSetUpdates(f.ctx)
	require.NoError(t, err)

	f.advance(t, testEpochBlocks, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx))
	st, err := k.Validators.Get(f.ctx, val3.String())
	require.NoError(t, err)
	require.False(t, st.Eligible)
	require.Equal(t, "jailed", st.Reason)

	q, err := keeper.NewQueryServerImpl(k).Delegations(f.ctx, &types.QueryDelegationsRequest{})
	require.NoError(t, err)
	byOp := map[string]types.DelegationEntry{}
	for _, e := range q.Delegations {
		byOp[e.OperatorAddress] = e
	}
	require.Contains(t, byOp, f.valAddr.String())
	require.Contains(t, byOp, val2.String())
	e3, ok := byOp[val3.String()]
	require.True(t, ok, "the jailed validator is listed")
	require.False(t, e3.Eligible)
	require.Equal(t, "jailed", e3.Reason)
	require.True(t, e3.Tokens.IsZero(), "its share was redelegated away: %s", e3.Tokens)
	require.NoError(t, k.CheckInvariants(f.ctx))
}
