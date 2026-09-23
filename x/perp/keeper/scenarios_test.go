package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Mandatory scenarios of spec §25.2 that are executable as keeper tests.

// Oracle stopped: the market pauses, nothing is liquidated and no trigger
// fires until votes resume (§21.2, §14.5).
func TestScenarioOracleStoppedSuspendsLiquidationsAndTriggers(t *testing.T) {
	f := setup(t)
	f.trade(t, 1_000, math.LegacyNewDec(60_000))
	_, err := f.k.SubmitTrigger(f.ctx, &types.MsgSubmitTriggerOrder{
		Sender: f.long.String(), MarketId: marketID,
		TriggerPrice: math.LegacyNewDec(50_000), FireAbove: false, Qty: math.ZeroInt(), Slippage: math.LegacyNewDecWithPrec(1, 2),
	})
	require.NoError(t, err)
	// the price collapses in the last sample, then the oracle stops for two periods
	f.at(f.ctx.BlockHeight() + 1)
	f.app.OracleKeeper.SetAssetPrice(f.ctx, asset, math.LegacyNewDec(40_000))
	f.at(f.ctx.BlockHeight() + 3*int64(f.app.OracleKeeper.VotePeriod(f.ctx)))
	f.endBlocks(t, f.ctx.BlockHeight())
	require.Equal(t, types.ORACLE_STATE_PAUSED, f.market(t).State)
	_, ok, _ := f.k.GetPosition(f.ctx, f.long.String(), marketID)
	require.True(t, ok, "no liquidation while paused")
	triggers, _ := f.k.TriggersOfAccount(f.ctx, f.long.String())
	require.Len(t, triggers, 1, "no trigger fires while paused")
	// votes resume at the low price: the sweep runs and the position goes
	f.setPrice(t, math.LegacyNewDec(40_000), math.LegacyZeroDec())
	f.endBlocks(t, f.ctx.BlockHeight())
	require.Equal(t, types.ORACLE_STATE_NORMAL, f.market(t).State)
	_, ok, _ = f.k.GetPosition(f.ctx, f.long.String(), marketID)
	require.False(t, ok)
}

// Cascading liquidation with the fund below target: several positions cross
// the mark in one block; the sweep is bounded per block and the fund's
// inventory cap sends the excess to ADL instead of concentrating risk (§18).
func TestScenarioCascadeWithFundBelowTarget(t *testing.T) {
	f := setup(t)
	p := f.params
	p.MaxLiquidationsPerBlock = 2
	p.IfMaxInventory = math.LegacyNewDecWithPrec(25, 2) // ≤ 2,500 units of a 10,000 cap... cap is 1e6 → 250,000; use OI cap small below
	require.NoError(t, f.k.SetParams(f.ctx, p))
	m := f.market(t)
	m.OiCap, m.EffectiveOiCap = math.NewInt(8_000), math.NewInt(8_000) // fund may hold at most 2,000 units
	require.NoError(t, f.k.Markets.Set(f.ctx, m.Id, m))
	// four longs of 1,000 against the short
	longs := make([]sdk.AccAddress, 4)
	for i := range longs {
		longs[i] = sdk.AccAddress([]byte("perp-cascade-long-" + string(rune('a'+i)) + "-"))
		coins := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(100_000_000)), sdk.NewCoin("uusd", math.NewInt(100_000_000)))
		require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", coins))
		require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", longs[i], coins))
		require.NoError(t, f.k.Deposit(f.ctx, longs[i], sdk.NewCoin("uusd", math.NewInt(100_000_000))))
	}
	require.NoError(t, f.k.Deposit(f.ctx, f.short, sdk.NewCoin("uusd", math.NewInt(900_000_000))))
	batch := f.ctx.BlockHeight()
	for _, l := range longs {
		f.submit(t, l, batchtypes.SIDE_BUY, 1_000, math.LegacyNewDec(60_000), false)
	}
	f.submit(t, f.short, batchtypes.SIDE_SELL, 4_000, math.LegacyNewDec(60_000), false)
	f.endBlocks(t, batch+f.bp.CommitWindow+1)
	for _, l := range longs {
		_, ok, _ := f.k.GetPosition(f.ctx, l.String(), marketID)
		require.True(t, ok)
	}
	// crash: all four are under water; two per block; the fund takes at most 2,000, the rest is ADL
	f.at(f.ctx.BlockHeight() + 1)
	f.setPrice(t, math.LegacyNewDec(45_000), math.LegacyZeroDec())
	f.endBlocks(t, f.ctx.BlockHeight())
	alive := 0
	for _, l := range longs {
		if _, ok, _ := f.k.GetPosition(f.ctx, l.String(), marketID); ok {
			alive++
		}
	}
	require.Equal(t, 2, alive, "bounded to max_liquidations_per_block")
	f.endBlocks(t, f.ctx.BlockHeight()+1)
	for _, l := range longs {
		_, ok, _ := f.k.GetPosition(f.ctx, l.String(), marketID)
		require.False(t, ok)
	}
	fund, ok, _ := f.k.GetPosition(f.ctx, types.InsuranceFundAddress(), marketID)
	require.True(t, ok)
	require.True(t, fund.Qty.LTE(math.NewInt(2_000)), "fund inventory capped at if_max_inventory: %s", fund.Qty)
	short, ok, _ := f.k.GetPosition(f.ctx, f.short.String(), marketID)
	require.True(t, ok)
	require.True(t, short.Qty.LT(math.NewInt(4_000)), "the short was deleveraged for the remainder")
	require.Equal(t, fund.Qty.String(), short.Qty.String(), "Σ long = Σ short after the cascade")
}

// Trigger storm: every stop of a market fires in the same block; the
// module fires at most max_triggers_per_block and the rest the next block,
// deterministically ordered (§14.5, §12).
func TestScenarioTriggerStormIsBounded(t *testing.T) {
	f := setup(t)
	p := f.params
	p.MaxTriggersPerBlock = 3
	require.NoError(t, f.k.SetParams(f.ctx, p))
	accounts := make([]sdk.AccAddress, 5)
	for i := range accounts {
		accounts[i] = sdk.AccAddress([]byte("perp-storm-long-" + string(rune('a'+i)) + "---"))
		coins := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(100_000_000)), sdk.NewCoin("uusd", math.NewInt(100_000_000)))
		require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", coins))
		require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", accounts[i], coins))
		require.NoError(t, f.k.Deposit(f.ctx, accounts[i], sdk.NewCoin("uusd", math.NewInt(100_000_000))))
	}
	require.NoError(t, f.k.Deposit(f.ctx, f.short, sdk.NewCoin("uusd", math.NewInt(900_000_000))))
	batch := f.ctx.BlockHeight()
	for _, a := range accounts {
		f.submit(t, a, batchtypes.SIDE_BUY, 1_000, math.LegacyNewDec(60_000), false)
	}
	f.submit(t, f.short, batchtypes.SIDE_SELL, 5_000, math.LegacyNewDec(60_000), false)
	f.endBlocks(t, batch+f.bp.CommitWindow+1)
	for _, a := range accounts {
		_, err := f.k.SubmitTrigger(f.ctx, &types.MsgSubmitTriggerOrder{
			Sender: a.String(), MarketId: marketID,
			TriggerPrice: math.LegacyNewDec(58_000), FireAbove: false, Qty: math.ZeroInt(), Slippage: math.LegacyNewDecWithPrec(1, 2),
		})
		require.NoError(t, err)
	}
	f.at(f.ctx.BlockHeight() + 1)
	f.setPrice(t, math.LegacyNewDec(57_000), math.LegacyZeroDec())
	f.endBlocks(t, f.ctx.BlockHeight())
	fired := 0
	for _, a := range accounts {
		if has, _ := f.bk.HasOpenIntent(f.ctx, a.String(), marketID); has {
			fired++
		}
	}
	require.Equal(t, 3, fired, "max_triggers_per_block")
	f.endBlocks(t, f.ctx.BlockHeight()+1)
	fired = 0
	for _, a := range accounts {
		if has, _ := f.bk.HasOpenIntent(f.ctx, a.String(), marketID); has {
			fired++
		}
	}
	require.Equal(t, 5, fired, "the rest fired next block")
}
