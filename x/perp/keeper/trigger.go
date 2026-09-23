package keeper

import (
	"errors"
	"sort"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	batchkeeper "github.com/classic-terra/core/v4/x/batch/keeper"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Resident trigger orders (spec §14.5 layer 4): reduce-only stops and
// take-profits kept on chain and evaluated every EndBlock against the mark.

// SubmitTrigger registers a trigger for the account's open position.
func (k Keeper) SubmitTrigger(ctx sdk.Context, msg *types.MsgSubmitTriggerOrder) (uint64, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return 0, err
	}
	m, err := k.GetMarket(ctx, msg.MarketId)
	if err != nil {
		return 0, err
	}
	pos, exists, err := k.GetPosition(ctx, msg.Sender, m.Id)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, errorsmod.Wrap(types.ErrInvalidTrigger, "no open position in the market")
	}
	if msg.Qty.IsPositive() && msg.Qty.GT(pos.Qty) {
		return 0, errorsmod.Wrap(types.ErrInvalidTrigger, "qty exceeds the position")
	}
	n := uint32(0)
	if err := k.TriggersByAccount.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](msg.Sender),
		func(_ collections.Pair[string, uint64]) (bool, error) { n++; return false, nil }); err != nil {
		return 0, err
	}
	if n >= params.MaxTriggersPerAccount {
		return 0, errorsmod.Wrapf(types.ErrTooManyTriggers, "max %d", params.MaxTriggersPerAccount)
	}
	side := batchtypes.SIDE_SELL
	if pos.Side == batchtypes.SIDE_SELL {
		side = batchtypes.SIDE_BUY
	}
	slippage := msg.Slippage
	if slippage.IsZero() {
		slippage = params.TriggerSlippageDefault
	}
	id, err := k.TriggerSeq.Next(ctx)
	if err != nil {
		return 0, err
	}
	t := types.TriggerOrder{Id: id, Account: msg.Sender, MarketId: m.Id, Side: side, TriggerPrice: msg.TriggerPrice,
		FireAbove: msg.FireAbove, Qty: msg.Qty, Slippage: slippage, CreatedHeight: ctx.BlockHeight()}
	return id, k.setTrigger(ctx, t)
}

func (k Keeper) setTrigger(ctx sdk.Context, t types.TriggerOrder) error {
	if err := k.Triggers.Set(ctx, t.Id, t); err != nil {
		return err
	}
	if err := k.TriggersByAccount.Set(ctx, collections.Join(t.Account, t.Id)); err != nil {
		return err
	}
	return k.TriggersByMarket.Set(ctx, collections.Join(t.MarketId, t.Id))
}

func (k Keeper) removeTrigger(ctx sdk.Context, t types.TriggerOrder) error {
	if err := k.Triggers.Remove(ctx, t.Id); err != nil {
		return err
	}
	if err := k.TriggersByAccount.Remove(ctx, collections.Join(t.Account, t.Id)); err != nil {
		return err
	}
	return k.TriggersByMarket.Remove(ctx, collections.Join(t.MarketId, t.Id))
}

// CancelTrigger removes a trigger of the sender.
func (k Keeper) CancelTrigger(ctx sdk.Context, sender string, id uint64) error {
	t, err := k.Triggers.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.ErrTriggerNotFound.Wrapf("%d", id)
		}
		return err
	}
	if t.Account != sender {
		return types.ErrUnauthorized.Wrap("not the trigger owner")
	}
	return k.removeTrigger(ctx, t)
}

// TriggersOfAccount lists the triggers of an account.
func (k Keeper) TriggersOfAccount(ctx sdk.Context, account string) ([]types.TriggerOrder, error) {
	var out []types.TriggerOrder
	err := k.TriggersByAccount.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](account),
		func(key collections.Pair[string, uint64]) (bool, error) {
			t, err := k.Triggers.Get(ctx, key.K2())
			if err != nil {
				return true, err
			}
			out = append(out, t)
			return false, nil
		})
	return out, err
}

// fireTriggers evaluates the triggers of a market against the mark and
// injects reduce-only orders for the ones that fire, in deterministic order
// (distance to the mark, then account), bounded per block. Nothing fires in
// a PAUSED market.
func (k Keeper) fireTriggers(ctx sdk.Context, params types.Params, m types.Market) error {
	if !m.Enabled || m.State == types.ORACLE_STATE_PAUSED || !m.MarkPrice.IsPositive() {
		return nil
	}
	var fired []types.TriggerOrder
	if err := k.TriggersByMarket.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](m.Id),
		func(key collections.Pair[string, uint64]) (bool, error) {
			t, err := k.Triggers.Get(ctx, key.K2())
			if err != nil {
				return true, err
			}
			if (t.FireAbove && m.MarkPrice.GTE(t.TriggerPrice)) || (!t.FireAbove && m.MarkPrice.LTE(t.TriggerPrice)) {
				fired = append(fired, t)
			}
			return false, nil
		}); err != nil {
		return err
	}
	sort.SliceStable(fired, func(i, j int) bool {
		di, dj := m.MarkPrice.Sub(fired[i].TriggerPrice).Abs(), m.MarkPrice.Sub(fired[j].TriggerPrice).Abs()
		if !di.Equal(dj) {
			return di.LT(dj)
		}
		return fired[i].Account < fired[j].Account
	})
	bp, err := k.batchKeeper.GetParams(ctx)
	if err != nil {
		return err
	}
	for i, t := range fired {
		if i >= int(params.MaxTriggersPerBlock) {
			break
		}
		if err := k.removeTrigger(ctx, t); err != nil {
			return err
		}
		pos, exists, err := k.GetPosition(ctx, t.Account, m.Id)
		if err != nil {
			return err
		}
		if !exists || pos.Side == t.Side {
			continue
		}
		qty := pos.Qty
		if t.Qty.IsPositive() && t.Qty.LT(qty) {
			qty = t.Qty
		}
		limit := t.TriggerPrice.Mul(math.LegacyOneDec().Sub(t.Slippage))
		if t.Side == batchtypes.SIDE_BUY {
			limit = t.TriggerPrice.Mul(math.LegacyOneDec().Add(t.Slippage))
		}
		limit = roundToTick(limit, m.TickSize, t.Side == batchtypes.SIDE_SELL)
		id, _, err := k.batchKeeper.SubmitPerpIntent(ctx, batchkeeper.PerpOrder{
			Sender: t.Account, MarketID: m.Id, Side: t.Side, Qty: qty, LimitPrice: limit,
			ExpiryHeight: ctx.BlockHeight() + bp.CommitWindow + 3, ReduceOnly: true,
		})
		if err != nil {
			k.Logger(ctx).Error("trigger order rejected", "trigger", t.Id, "err", err)
			continue
		}
		if err := ctx.EventManager().EmitTypedEvent(&types.EventTriggerFired{TriggerId: t.Id, Account: t.Account, MarketId: m.Id, MarkPrice: m.MarkPrice.String(), IntentId: id}); err != nil {
			return err
		}
	}
	return nil
}
