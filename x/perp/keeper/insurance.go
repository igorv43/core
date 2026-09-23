package keeper

import (
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	batchkeeper "github.com/classic-terra/core/v4/x/batch/keeper"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// unwindInsurance places one reduce-only order per market for the fund's
// inventory, up to if_unwind_per_batch of it per batch (spec §18.3 step 3),
// priced at the edge of the oracle band so it clears whenever there is
// opposite volume.
func (k Keeper) unwindInsurance(ctx sdk.Context, params types.Params, m types.Market) error {
	if !m.Enabled || m.State == types.ORACLE_STATE_PAUSED || !m.MarkPrice.IsPositive() {
		return nil
	}
	fund := types.InsuranceFundAddress()
	pos, exists, err := k.GetPosition(ctx, fund, m.Id)
	if err != nil || !exists {
		return err
	}
	if id, err := k.UnwindIntents.Get(ctx, m.Id); err == nil {
		if _, err := k.batchKeeper.GetIntent(ctx, id); err == nil {
			return nil // still open
		}
		if err := k.UnwindIntents.Remove(ctx, m.Id); err != nil {
			return err
		}
	} else if !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	qty := params.IfUnwindPerBatch.MulInt(pos.Qty).TruncateInt()
	if qty.LT(m.MinQty) {
		qty = math.MinInt(m.MinQty, pos.Qty)
	}
	if qty.LT(m.MinQty) {
		return nil
	}
	bp, err := k.batchKeeper.GetParams(ctx)
	if err != nil {
		return err
	}
	side, limit := batchtypes.SIDE_SELL, m.RefPrice.Mul(math.LegacyOneDec().Sub(bp.PriceBand))
	if pos.Side == batchtypes.SIDE_SELL {
		side, limit = batchtypes.SIDE_BUY, m.RefPrice.Mul(math.LegacyOneDec().Add(bp.PriceBand))
	}
	limit = roundToTick(limit, m.TickSize, side == batchtypes.SIDE_SELL)
	id, _, err := k.batchKeeper.SubmitPerpIntent(ctx, batchkeeper.PerpOrder{
		Sender: fund, MarketID: m.Id, Side: side, Qty: qty, LimitPrice: limit,
		ExpiryHeight: ctx.BlockHeight() + bp.CommitWindow + 3, ReduceOnly: true,
	})
	if err != nil {
		k.Logger(ctx).Error("insurance unwind order rejected", "market", m.Id, "err", err)
		return nil
	}
	if err := k.UnwindIntents.Set(ctx, m.Id, id); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventInsuranceUnwind{MarketId: m.Id, Side: side.String(), Qty: qty.String(), IntentId: id})
}

// roundToTick rounds a price to the market tick: down for sells (more
// aggressive), up for buys.
func roundToTick(price, tick math.LegacyDec, down bool) math.LegacyDec {
	if !tick.IsPositive() {
		return price
	}
	ticks := price.Quo(tick)
	if down {
		return ticks.TruncateDec().Mul(tick)
	}
	return ticks.Ceil().Mul(tick)
}

// trancheStep advances the stLUNC tranche of the fund one step per block
// (spec §21.5 rule 7): seized units are redeemed through x/liquidstake
// (instant from the buffer or queued), matured requests are claimed, and the
// uluna is sold for settlement in the internal spot market in gradual
// reduce-only-style limit orders capped per allocation epoch. Proceeds repay
// what the core advanced and the rest joins the core.
func (k Keeper) trancheStep(ctx sdk.Context, params types.Params) error {
	l, err := k.GetLedger(ctx)
	if err != nil {
		return err
	}
	module := k.ModuleAddress()
	// 1. redeem seized units
	if l.TrancheSt.IsPositive() && k.lsKeeper != nil {
		res, err := k.lsKeeper.Unstake(ctx, module, sdk.NewCoin(params.StDenom, l.TrancheSt))
		if err != nil {
			k.Logger(ctx).Error("tranche unstake failed", "err", err)
		} else {
			if res.Instant {
				l.TrancheUluna = l.TrancheUluna.Add(res.Amount.Amount)
			} else {
				l.TrancheUnbondingSt = l.TrancheUnbondingSt.Add(l.TrancheSt)
			}
			l.TrancheSt = math.ZeroInt()
		}
	}
	// 2. claim matured requests
	if l.TrancheUnbondingSt.IsPositive() && k.lsKeeper != nil {
		if coin, _, err := k.lsKeeper.Claim(ctx, module); err == nil && coin.IsPositive() {
			l.TrancheUluna = l.TrancheUluna.Add(coin.Amount)
			rate, _, err := k.lsKeeper.ExchangeRate(ctx)
			if err == nil && rate.IsPositive() {
				units := math.LegacyNewDecFromInt(coin.Amount).Quo(rate).Ceil().TruncateInt()
				l.TrancheUnbondingSt = math.MaxInt(math.ZeroInt(), l.TrancheUnbondingSt.Sub(units))
			}
		}
	}
	// 3. settle a closed sale: unfilled uluna comes back, settlement repays the advance
	if l.TrancheSellIntentId != 0 {
		if _, err := k.batchKeeper.GetIntent(ctx, l.TrancheSellIntentId); err == nil {
			return k.Ledger.Set(ctx, l) // still open
		}
		l.TrancheSellIntentId = 0
		l.TrancheUluna = k.bankKeeper.GetBalance(ctx, module, "uluna").Amount
		expected, err := k.ledgerTotal(ctx, l)
		if err != nil {
			return err
		}
		if surplus := k.bankKeeper.GetBalance(ctx, module, params.SettlementDenom).Amount.Sub(expected); surplus.IsPositive() {
			repay := math.MinInt(surplus, l.TrancheAdvanced)
			l.TrancheAdvanced = l.TrancheAdvanced.Sub(repay)
			l.Insurance = l.Insurance.Add(surplus) // the advance comes back and any excess is the core's
		}
	}
	if err := k.Ledger.Set(ctx, l); err != nil {
		return err
	}
	// 4. sell within the epoch cap
	if !l.TrancheUluna.IsPositive() || params.SpotMarketId == "" {
		return nil
	}
	room := params.StUnwindCapPerEpoch.Sub(l.TrancheSoldEpoch)
	amount := math.MinInt(l.TrancheUluna, room)
	if !amount.IsPositive() {
		return nil
	}
	spot, err := k.batchKeeper.GetMarket(ctx, params.SpotMarketId)
	if err != nil || !spot.Enabled || spot.BaseDenom != "uluna" || spot.QuoteDenom != params.SettlementDenom || amount.LT(spot.MinQty) {
		return nil
	}
	pref, err := k.oracleKeeper.GetPrice(ctx, spot.OracleDenom)
	if err != nil || !pref.IsPositive() {
		return nil
	}
	bp, err := k.batchKeeper.GetParams(ctx)
	if err != nil {
		return err
	}
	limit := roundToTick(pref.Mul(math.LegacyOneDec().Sub(bp.PriceBand)), spot.TickSize, true)
	id, _, err := k.batchKeeper.SubmitIntentInternal(ctx, &batchtypes.MsgSubmitIntent{
		Sender: module.String(), MarketId: spot.Id, Side: batchtypes.SIDE_SELL,
		AmountIn: sdk.NewCoin("uluna", amount), LimitPrice: limit, MinOut: math.ZeroInt(),
		ExpiryHeight: ctx.BlockHeight() + bp.CommitWindow + 3,
	})
	if err != nil {
		k.Logger(ctx).Error("tranche sale rejected", "err", err)
		return nil
	}
	l.TrancheSellIntentId = id
	l.TrancheUluna = l.TrancheUluna.Sub(amount)
	l.TrancheSoldEpoch = l.TrancheSoldEpoch.Add(amount)
	return k.Ledger.Set(ctx, l)
}
