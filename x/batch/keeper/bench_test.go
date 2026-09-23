package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Gate G-04 (spec §12.3): resolve a batch with the limits saturated —
// max_intents_per_batch intents and max_solvers_per_batch × max_levels_per_bid
// levels in one market — and report the EndBlock time. Run with
// `go test ./x/batch/keeper -run TestBenchmarkSaturatedBatch -v`.
func TestBenchmarkSaturatedBatch(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	f := setup(t)
	f.registerSolver(t)
	pref := math.LegacyNewDecWithPrec(1, 4)
	nIntents, nSolvers, nLevels := int(f.params.MaxIntentsPerBatch), int(f.params.MaxSolversPerBatch), int(f.params.MaxLevelsPerBid)

	f.at(10)
	batch := uint64(f.ctx.BlockHeight())
	// intents: alternating buys and sells around P_ref inside the band
	for i := 0; i < nIntents; i++ {
		acc := sdk.AccAddress([]byte(fmt.Sprintf("bench-intent-%06d----", i)))
		coins := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(100_000_000_000)), sdk.NewCoin("uusd", math.NewInt(10_000_000)))
		require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", coins))
		require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", acc, coins))
		side, amount := types.SIDE_BUY, sdk.NewCoin("uusd", math.NewInt(1_000_000))
		if i%2 == 1 {
			side, amount = types.SIDE_SELL, sdk.NewCoin("uluna", math.NewInt(10_000_000_000))
		}
		limit := pref.Mul(math.LegacyOneDec().Add(math.LegacyNewDecWithPrec(int64(i%20)-10, 4))) // ±0.1%
		limit = limit.Quo(math.LegacyNewDecWithPrec(1, 6)).TruncateDec().Mul(math.LegacyNewDecWithPrec(1, 6))
		_, _, err := f.k.SubmitIntent(f.ctx, &types.MsgSubmitIntent{
			Sender: acc.String(), MarketId: marketID, Side: side, AmountIn: amount,
			LimitPrice: limit, MinOut: math.ZeroInt(), ExpiryHeight: f.ctx.BlockHeight() + 100,
		})
		require.NoError(t, err)
	}
	// solvers: each commits and reveals max levels
	solvers := make([]sdk.AccAddress, nSolvers)
	bids := make([]types.Bid, nSolvers)
	for s := range solvers {
		solvers[s] = sdk.AccAddress([]byte(fmt.Sprintf("bench-solver-%06d----", s)))
		coins := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(1_000_000_000_000)), sdk.NewCoin("uusd", math.NewInt(100_000_000_000)))
		require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", coins))
		require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", solvers[s], coins))
		require.NoError(t, f.k.RegisterSolver(f.ctx, solvers[s].String(), f.params.SolverBondMin))
		require.NoError(t, f.k.DepositSolverEscrow(f.ctx, solvers[s].String(), sdk.NewCoin("uluna", math.NewInt(500_000_000_000))))
		require.NoError(t, f.k.DepositSolverEscrow(f.ctx, solvers[s].String(), sdk.NewCoin("uusd", math.NewInt(50_000_000))))
		levels := make([]types.Level, nLevels)
		for l := range levels {
			side := types.SIDE_SELL
			if l%2 == 1 {
				side = types.SIDE_BUY
			}
			levels[l] = types.Level{Side: side, Price: pref, Qty: math.NewInt(1_000_000_000)}
		}
		bids[s] = types.Bid{MarketId: marketID, Levels: levels}
	}
	f.at(int64(batch) + 1)
	for s := range solvers {
		c, err := types.Commitment(bids[s], []byte("salt"), solvers[s].String(), batch)
		require.NoError(t, err)
		require.NoError(t, f.k.CommitBid(f.ctx, &types.MsgCommitBid{Solver: solvers[s].String(), BatchId: batch, MarketId: marketID, Commitment: c}))
	}
	f.at(int64(batch) + f.params.CommitWindow + 1)
	for s := range solvers {
		require.NoError(t, f.k.RevealBid(f.ctx, &types.MsgRevealBid{Solver: solvers[s].String(), BatchId: batch, Bid: bids[s], Salt: []byte("salt")}))
	}

	start := time.Now()
	require.NoError(t, f.k.EndBlocker(f.ctx))
	elapsed := time.Since(start)
	res, err := f.k.Results.Get(f.ctx, collectionsJoin(batch, marketID))
	require.NoError(t, err)
	t.Logf("G-04 batch: %d intents + %d solvers × %d levels → executed=%v fills=%d passes=1 EndBlock=%s", nIntents, nSolvers, nLevels, res.Executed, res.Fills, elapsed)
	require.Less(t, elapsed, 1200*time.Millisecond, "EndBlock must stay ≤ 20%% of a 6 s block")
}
