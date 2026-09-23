package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Gate G-04 (spec §12.3, §19.5): with many open positions, a price move that
// crosses a fraction of them must cost O(k log n): the sweep walks only the
// crossed entries of the liquidation index, bounded per block. Run with
// `go test ./x/perp/keeper -run TestBenchmarkLiquidationSweep -v`.
func TestBenchmarkLiquidationSweep(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	f := setup(t)
	const n = 2_000
	require.NoError(t, f.k.FundInsurance(f.ctx, f.short, sdk.NewCoin("uusd", math.NewInt(900_000_000))))
	// n longs opened directly through the fill path at increasing leverage-independent prices
	// (the auction is benchmarked in x/batch); the short is the counterparty of all
	longs := make([]sdk.AccAddress, n)
	batch := f.ctx.BlockHeight()
	for i := range longs {
		longs[i] = sdk.AccAddress([]byte(fmt.Sprintf("perp-bench-long-%05d", i)))
		coins := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(100_000_000)), sdk.NewCoin("uusd", math.NewInt(30_000_000)))
		require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", coins))
		require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", longs[i], coins))
		require.NoError(t, f.k.Deposit(f.ctx, longs[i], sdk.NewCoin("uusd", math.NewInt(30_000_000))))
	}
	shortCoins := sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(100_000_000_000)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", shortCoins))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", f.short, shortCoins))
	require.NoError(t, f.k.Deposit(f.ctx, f.short, sdk.NewCoin("uusd", math.NewInt(100_000_000_000))))
	m := f.market(t)
	m.OiCap, m.EffectiveOiCap = math.NewInt(100_000_000), math.NewInt(100_000_000)
	require.NoError(t, f.k.Markets.Set(f.ctx, m.Id, m))
	bp := f.bp
	bp.MaxIntentsPerBatch = 5_000
	require.NoError(t, f.bk.SetParams(f.ctx, bp))
	for _, l := range longs {
		f.submit(t, l, batchtypes.SIDE_BUY, 1_000, math.LegacyNewDec(60_000), false)
	}
	f.submit(t, f.short, batchtypes.SIDE_SELL, int64(n)*1_000, math.LegacyNewDec(60_000), false)
	start := time.Now()
	f.at(batch + f.bp.CommitWindow + 1)
	require.NoError(t, f.bk.EndBlocker(f.ctx))
	require.NoError(t, f.k.EndBlocker(f.ctx))
	t.Logf("G-04 perp: %d positions opened in one batch, EndBlock(batch+perp)=%s", n, time.Since(start))
	opened := 0
	for _, l := range longs {
		if _, ok, _ := f.k.GetPosition(f.ctx, l.String(), marketID); ok {
			opened++
		}
	}
	require.Equal(t, n, opened)

	// a small move that crosses nobody: the sweep must be O(log n)
	f.at(f.ctx.BlockHeight() + 1)
	f.setPrice(t, math.LegacyNewDec(59_000), math.LegacyZeroDec())
	start = time.Now()
	require.NoError(t, f.k.EndBlocker(f.ctx))
	noCross := time.Since(start)
	// a crash that crosses everybody: bounded by max_liquidations_per_block (100)
	f.at(f.ctx.BlockHeight() + 1)
	f.setPrice(t, math.LegacyNewDec(45_000), math.LegacyZeroDec())
	start = time.Now()
	require.NoError(t, f.k.EndBlocker(f.ctx))
	crash := time.Since(start)
	t.Logf("G-04 perp sweep: no-cross=%s crash(100 liquidations)=%s", noCross, crash)
	require.Less(t, noCross, 200*time.Millisecond)
	require.Less(t, crash, 1200*time.Millisecond)
}
