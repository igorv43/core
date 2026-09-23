package keeper

import (
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EndBlocker runs after x/batch settled the block's fills: per market, the
// oracle state and mark, funding when due, the liquidation sweep, trigger
// evaluation, the fund's unwind order and the risk steps; then the
// allocation epoch and the buyback. Every step is bounded (spec §12) and a
// market failure is logged, never fatal.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	markets, err := k.AllMarkets(ctx)
	if err != nil {
		return err
	}
	for _, m := range markets {
		if err := k.endBlockMarket(ctx, params, m); err != nil {
			k.Logger(ctx).Error("perp market end block failed", "market", m.Id, "err", err)
		}
	}
	if err := k.runAllocation(ctx, params); err != nil {
		k.Logger(ctx).Error("allocation failed", "err", err)
	}
	if err := k.runBuyback(ctx, params); err != nil {
		k.Logger(ctx).Error("buyback failed", "err", err)
	}
	return nil
}

func (k Keeper) endBlockMarket(ctx sdk.Context, params types.Params, m types.Market) error {
	if err := k.updateMark(ctx, params, &m); err != nil {
		return err
	}
	if err := k.Markets.Set(ctx, m.Id, m); err != nil {
		return err
	}
	if err := k.applyFunding(ctx, params, &m); err != nil {
		return err
	}
	if err := k.sweepLiquidations(ctx, params, &m); err != nil {
		return err
	}
	if err := k.Markets.Set(ctx, m.Id, m); err != nil {
		return err
	}
	if err := k.fireTriggers(ctx, params, m); err != nil {
		return err
	}
	if err := k.unwindInsurance(ctx, params, m); err != nil {
		return err
	}
	// reload: fills of the fund and triggers do not happen here, but OI may have
	// changed through liquidations above
	m, err := k.GetMarket(ctx, m.Id)
	if err != nil {
		return err
	}
	if err := k.applyRiskSteps(ctx, params, &m); err != nil {
		return err
	}
	return k.Markets.Set(ctx, m.Id, m)
}
