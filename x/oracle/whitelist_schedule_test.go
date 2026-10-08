package oracle_test

import (
	"testing"
	"time"

	core "github.com/classic-terra/core/v4/types"
	"github.com/classic-terra/core/v4/x/oracle"
	"github.com/classic-terra/core/v4/x/oracle/keeper"
	"github.com/classic-terra/core/v4/x/oracle/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// oracle asset denoms of the stage-2 whitelist tests (shared with abci_test.go)
const (
	denomBTC = "ubtc"
	denomETH = "ueth"
)

// activateApprovedWhitelist runs the spec §26.3 schedule for the whitelist in
// the parameters: it must be pending (not active) right after the approval
// and active once the delay has elapsed. The vote targets are then synced by
// the next EndBlocker of the caller.
func activateApprovedWhitelist(t *testing.T, input keeper.TestInput) {
	t.Helper()
	k := input.OracleKeeper
	params := k.GetParams(input.Ctx)

	active := k.UpdateWhitelistSchedule(input.Ctx, params)
	require.False(t, active.Matches(params.Whitelist, params.AssetWhitelist), "a change must not be active at approval")
	pending, ok := k.GetPendingWhitelist(input.Ctx)
	require.True(t, ok)

	due := input.Ctx.WithBlockHeight(pending.ActivationHeight).WithBlockTime(pending.ActivationTime)
	active = k.UpdateWhitelistSchedule(due, params)
	require.True(t, active.Matches(params.Whitelist, params.AssetWhitelist), "the change must be active after the delay")
	_, ok = k.GetPendingWhitelist(input.Ctx)
	require.False(t, ok)
}

func hasEvent(ctx sdk.Context, eventType string) bool {
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type == eventType {
			return true
		}
	}
	return false
}

// The §26.3 limits are code constants pinned to the spec: 7 days, i.e.
// 100800 blocks at the 6 s target, and 168 h of block time.
func TestWhitelistActivationDelayConstants(t *testing.T) {
	require.Equal(t, int64(100800), types.WhitelistActivationDelay)
	require.Equal(t, int64(7*24*3600/6), types.WhitelistActivationDelay)
	require.Equal(t, 7*24*time.Hour, types.WhitelistActivationPeriod)
}

// Spec §26.3: a whitelist change approved by governance (a parameter write)
// does not touch the vote targets before 100800 blocks AND 7 days of block
// time have elapsed since the approval; once both have, it is applied at the
// end of the vote period.
func TestWhitelistChangeActivatesOnlyAfterDelay(t *testing.T) {
	input, _ := setup(t)
	k := input.OracleKeeper
	t0 := input.Ctx.BlockTime()

	// first EndBlock records the lists in force as the active whitelist
	oracle.EndBlocker(input.Ctx.WithBlockHeight(10), k)
	_, ok := k.GetActiveWhitelist(input.Ctx)
	require.True(t, ok)
	_, ok = k.GetPendingWhitelist(input.Ctx)
	require.False(t, ok)
	require.Empty(t, k.GetAssetTargets(input.Ctx))

	// governance approves a new asset at height 11
	params := k.GetParams(input.Ctx)
	params.AssetWhitelist = types.AssetList{{Name: denomBTC}}
	k.SetParams(input.Ctx, params)
	approval := input.Ctx.WithBlockHeight(11).WithBlockTime(t0).WithEventManager(sdk.NewEventManager())
	oracle.EndBlocker(approval, k)
	require.True(t, hasEvent(approval, types.EventTypeWhitelistChangeScheduled))
	require.Empty(t, k.GetAssetTargets(input.Ctx), "not active at approval")

	pending, ok := k.GetPendingWhitelist(input.Ctx)
	require.True(t, ok)
	require.Equal(t, int64(11), pending.ApprovedHeight)
	require.Equal(t, int64(11)+types.WhitelistActivationDelay, pending.ActivationHeight)
	require.True(t, pending.ActivationTime.Equal(t0.Add(types.WhitelistActivationPeriod)))
	require.Equal(t, types.AssetList{{Name: denomBTC}}, pending.AssetWhitelist)

	// the query exposes the pending change and the constants
	res, err := keeper.NewQuerier(k).PendingWhitelist(input.Ctx, &types.QueryPendingWhitelistRequest{})
	require.NoError(t, err)
	require.NotNil(t, res.Pending)
	require.Equal(t, types.AssetList{{Name: denomBTC}}, res.Pending.AssetWhitelist)
	require.Equal(t, pending.ActivationHeight, res.Pending.ActivationHeight)
	require.NotNil(t, res.Active)
	require.Empty(t, res.Active.AssetWhitelist)
	require.Equal(t, types.WhitelistActivationDelay, res.ActivationDelayBlocks)
	require.Equal(t, types.WhitelistActivationPeriod, res.ActivationPeriod)

	// one block short of the delay (time already past 7 days): still pending
	oracle.EndBlocker(input.Ctx.WithBlockHeight(pending.ActivationHeight-1).WithBlockTime(t0.Add(8*24*time.Hour)), k)
	require.Empty(t, k.GetAssetTargets(input.Ctx))
	_, ok = k.GetPendingWhitelist(input.Ctx)
	require.True(t, ok)

	// height reached but one second short of 7 days (faster blocks): still pending
	oracle.EndBlocker(input.Ctx.WithBlockHeight(pending.ActivationHeight).WithBlockTime(pending.ActivationTime.Add(-time.Second)), k)
	require.Empty(t, k.GetAssetTargets(input.Ctx))
	_, ok = k.GetPendingWhitelist(input.Ctx)
	require.True(t, ok)

	// both bounds reached: active, applied at the end of the vote period
	due := input.Ctx.WithBlockHeight(pending.ActivationHeight).WithBlockTime(pending.ActivationTime).WithEventManager(sdk.NewEventManager())
	oracle.EndBlocker(due, k)
	require.True(t, hasEvent(due, types.EventTypeWhitelistActivated))
	require.Equal(t, types.AssetList{{Name: denomBTC}}, k.GetAssetTargets(input.Ctx))
	_, ok = k.GetPendingWhitelist(input.Ctx)
	require.False(t, ok)
	active, ok := k.GetActiveWhitelist(input.Ctx)
	require.True(t, ok)
	require.Equal(t, types.AssetList{{Name: denomBTC}}, active.AssetWhitelist)
}

// The denom whitelist follows the same schedule: removing a denom keeps it as
// a vote target until the change is active.
func TestDenomWhitelistRemovalWaitsForDelay(t *testing.T) {
	input, _ := setup(t)
	k := input.OracleKeeper

	oracle.EndBlocker(input.Ctx.WithBlockHeight(10), k)
	before := k.GetVoteTargets(input.Ctx)
	require.Greater(t, len(before), 1)

	params := k.GetParams(input.Ctx)
	params.Whitelist = params.Whitelist[:1]
	k.SetParams(input.Ctx, params)
	oracle.EndBlocker(input.Ctx.WithBlockHeight(11), k)
	require.Equal(t, before, k.GetVoteTargets(input.Ctx), "removal not active at approval")

	pending, ok := k.GetPendingWhitelist(input.Ctx)
	require.True(t, ok)
	oracle.EndBlocker(input.Ctx.WithBlockHeight(pending.ActivationHeight).WithBlockTime(pending.ActivationTime), k)
	require.Equal(t, []string{params.Whitelist[0].Name}, k.GetVoteTargets(input.Ctx))
}

// A different approval while a change is pending replaces it and restarts the
// clock; approving the active lists again cancels the pending change.
func TestWhitelistChangeReplacedAndCancelled(t *testing.T) {
	input, _ := setup(t)
	k := input.OracleKeeper
	t0 := input.Ctx.BlockTime()

	oracle.EndBlocker(input.Ctx.WithBlockHeight(10), k)
	params := k.GetParams(input.Ctx)

	params.AssetWhitelist = types.AssetList{{Name: denomBTC}}
	k.SetParams(input.Ctx, params)
	oracle.EndBlocker(input.Ctx.WithBlockHeight(11).WithBlockTime(t0), k)
	first, ok := k.GetPendingWhitelist(input.Ctx)
	require.True(t, ok)

	// the same approved lists on later blocks do not restart the clock
	oracle.EndBlocker(input.Ctx.WithBlockHeight(500).WithBlockTime(t0.Add(time.Hour)), k)
	same, ok := k.GetPendingWhitelist(input.Ctx)
	require.True(t, ok)
	require.Equal(t, first.ActivationHeight, same.ActivationHeight)

	// a new approval at 1000 replaces the change and restarts the clock
	params.AssetWhitelist = types.AssetList{{Name: denomBTC}, {Name: denomETH}}
	k.SetParams(input.Ctx, params)
	t1 := t0.Add(2 * time.Hour)
	oracle.EndBlocker(input.Ctx.WithBlockHeight(1000).WithBlockTime(t1), k)
	second, ok := k.GetPendingWhitelist(input.Ctx)
	require.True(t, ok)
	require.Equal(t, int64(1000)+types.WhitelistActivationDelay, second.ActivationHeight)
	require.True(t, second.ActivationTime.Equal(t1.Add(types.WhitelistActivationPeriod)))

	// the first schedule's activation point no longer activates anything
	oracle.EndBlocker(input.Ctx.WithBlockHeight(first.ActivationHeight).WithBlockTime(first.ActivationTime), k)
	require.Empty(t, k.GetAssetTargets(input.Ctx))

	// governance approves the active (empty) asset list again: cancelled
	params.AssetWhitelist = types.AssetList{}
	k.SetParams(input.Ctx, params)
	cancel := input.Ctx.WithBlockHeight(first.ActivationHeight + 1).WithBlockTime(first.ActivationTime).WithEventManager(sdk.NewEventManager())
	oracle.EndBlocker(cancel, k)
	require.True(t, hasEvent(cancel, types.EventTypeWhitelistChangeCancelled))
	_, ok = k.GetPendingWhitelist(input.Ctx)
	require.False(t, ok)
	oracle.EndBlocker(input.Ctx.WithBlockHeight(second.ActivationHeight).WithBlockTime(second.ActivationTime), k)
	require.Empty(t, k.GetAssetTargets(input.Ctx))
}

// Genesis is not a change: the genesis whitelist is active at once, with no
// pending change. An exported pending change keeps its schedule across an
// export/import, and a genesis cannot shorten the §26.3 delay.
func TestGenesisWhitelistIsImmediate(t *testing.T) {
	input := keeper.CreateTestInput(t)
	k := input.OracleKeeper

	gs := types.DefaultGenesisState()
	gs.Params.AssetWhitelist = types.AssetList{{Name: denomBTC}}
	require.NoError(t, types.ValidateGenesis(gs))
	oracle.InitGenesis(input.Ctx, k, gs)

	require.Equal(t, types.AssetList{{Name: denomBTC}}, k.GetAssetTargets(input.Ctx))
	active, ok := k.GetActiveWhitelist(input.Ctx)
	require.True(t, ok)
	require.True(t, active.Matches(gs.Params.Whitelist, gs.Params.AssetWhitelist))
	_, ok = k.GetPendingWhitelist(input.Ctx)
	require.False(t, ok)

	oracle.EndBlocker(input.Ctx.WithBlockHeight(1), k)
	_, ok = k.GetPendingWhitelist(input.Ctx)
	require.False(t, ok)
	require.Equal(t, types.AssetList{{Name: denomBTC}}, k.GetAssetTargets(input.Ctx))

	// schedule a change, export, import into a fresh chain: same schedule
	params := k.GetParams(input.Ctx)
	params.AssetWhitelist = types.AssetList{{Name: denomBTC}, {Name: denomETH}}
	k.SetParams(input.Ctx, params)
	oracle.EndBlocker(input.Ctx.WithBlockHeight(2), k)
	exported := oracle.ExportGenesis(input.Ctx, k)
	require.NotNil(t, exported.PendingWhitelist)
	require.NotNil(t, exported.ActiveWhitelist)
	require.NoError(t, types.ValidateGenesis(exported))

	fresh := keeper.CreateTestInput(t)
	oracle.InitGenesis(fresh.Ctx, fresh.OracleKeeper, exported)
	require.Equal(t, types.AssetList{{Name: denomBTC}}, fresh.OracleKeeper.GetAssetTargets(fresh.Ctx), "import does not activate the pending change")
	imported, ok := fresh.OracleKeeper.GetPendingWhitelist(fresh.Ctx)
	require.True(t, ok)
	require.Equal(t, exported.PendingWhitelist.ActivationHeight, imported.ActivationHeight)
	require.Equal(t, exported, oracle.ExportGenesis(fresh.Ctx, fresh.OracleKeeper))

	// a pending change with a delay below the code minimum is rejected
	short := *exported.PendingWhitelist
	short.ActivationHeight = short.ApprovedHeight + types.WhitelistActivationDelay - 1
	exported.PendingWhitelist = &short
	require.Error(t, types.ValidateGenesis(exported))
	short.ActivationHeight = short.ApprovedHeight + types.WhitelistActivationDelay
	short.ActivationTime = short.ApprovedTime.Add(types.WhitelistActivationPeriod - time.Second)
	require.Error(t, types.ValidateGenesis(exported))
}

// A feeder that follows the parameters may vote a denom or asset of the
// approved change before its activation: the tuple is accepted and dropped,
// the rest of the vote counts, and a denom outside both lists is still
// rejected.
func TestVoteOnPendingWhitelistEntryIsDropped(t *testing.T) {
	input, h := setup(t)
	k := input.OracleKeeper
	enableAsset(t, input, denomBTC)
	oracle.EndBlocker(input.Ctx.WithBlockHeight(10), k) // record the active whitelist

	params := k.GetParams(input.Ctx)
	params.AssetWhitelist = types.AssetList{{Name: denomBTC}, {Name: denomETH}}
	k.SetParams(input.Ctx, params)
	oracle.EndBlocker(input.Ctx.WithBlockHeight(11), k)
	_, ok := k.GetPendingWhitelist(input.Ctx)
	require.True(t, ok)
	require.False(t, k.IsAssetTarget(input.Ctx, denomETH))

	sdr := randomExchangeRate.String() + core.MicroSDRDenom
	makeAggregatePrevoteAndVoteStr(t, input, h, 100, "65000.0ubtc,3000.0ueth,"+sdr, 0)
	vote, err := k.GetAggregateExchangeRateVote(input.Ctx, keeper.ValAddrs[0])
	require.NoError(t, err)
	require.Len(t, vote.ExchangeRateTuples, 2)
	for _, tuple := range vote.ExchangeRateTuples {
		require.NotEqual(t, denomETH, tuple.Denom)
	}

	// a denom in neither the active nor the pending whitelist is rejected
	rates := "65000.0ubtc,1.0usol," + sdr
	salt := "1"
	hash := types.GetAggregateVoteHash(salt, rates, keeper.ValAddrs[1])
	_, err = h.AggregateExchangeRatePrevote(input.Ctx.WithBlockHeight(100), types.NewMsgAggregateExchangeRatePrevote(hash, keeper.Addrs[1], keeper.ValAddrs[1]))
	require.NoError(t, err)
	_, err = h.AggregateExchangeRateVote(input.Ctx.WithBlockHeight(101), types.NewMsgAggregateExchangeRateVote(salt, rates, keeper.Addrs[1], keeper.ValAddrs[1]))
	require.ErrorIs(t, err, types.ErrUnknownDenom)
}
