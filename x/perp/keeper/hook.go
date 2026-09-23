package keeper

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MarginHook implements batchtypes.MarginHook (spec §14.3): x/batch calls
// Reserve when a perp order enters the book, Release when it leaves (or
// right before a fill) and Fill for every cleared quantity.
type MarginHook struct{ k Keeper }

var _ batchtypes.MarginHook = MarginHook{}

// NewMarginHook returns the hook to register in x/batch.
func NewMarginHook(k Keeper) MarginHook { return MarginHook{k: k} }

// reserveAmount is the margin an order of qty at limit reserves: IM at the
// governance leverage (stable across risk steps so Release mirrors Reserve).
func reserveAmount(market types.Market, qty math.Int, limit math.LegacyDec) math.Int {
	return types.RequiredMargin(qty, limit, types.InitialMargin(market.MaxLeverage))
}

// Reserve implements batchtypes.MarginHook.
func (h MarginHook) Reserve(ctx sdk.Context, account sdk.AccAddress, bm batchtypes.Market, side batchtypes.Side, qty math.Int, limit math.LegacyDec) error {
	k := h.k
	market, err := k.GetMarket(ctx, bm.Id)
	if err != nil {
		return err
	}
	if !market.Enabled {
		return types.ErrMarketDisabled.Wrap(market.Id)
	}
	switch market.State {
	case types.ORACLE_STATE_PAUSED:
		return types.ErrMarketPaused.Wrap(market.Id)
	case types.ORACLE_STATE_REDUCE_ONLY:
		return errorsmod.Wrap(types.ErrReduceOnly, "market in reduce-only state: only reduce_only orders are accepted")
	}
	amount := reserveAmount(market, qty, limit)
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	avail, err := k.AvailableFor(ctx, params, account.String(), market, k.stValuation(ctx, params))
	if err != nil {
		return err
	}
	if avail.LT(amount) {
		return errorsmod.Wrapf(types.ErrInsufficientFree, "available %s, reservation %s", avail, amount)
	}
	cur, err := k.reservation(ctx, account.String(), market.Id)
	if err != nil {
		return err
	}
	_ = side
	return k.setReservation(ctx, account.String(), market.Id, cur.Add(amount))
}

// Release implements batchtypes.MarginHook.
func (h MarginHook) Release(ctx sdk.Context, account sdk.AccAddress, bm batchtypes.Market, _ batchtypes.Side, qty math.Int, limit math.LegacyDec) error {
	k := h.k
	market, err := k.GetMarket(ctx, bm.Id)
	if err != nil {
		return err
	}
	cur, err := k.reservation(ctx, account.String(), market.Id)
	if err != nil {
		return err
	}
	next := cur.Sub(reserveAmount(market, qty, limit))
	if next.IsNegative() {
		next = math.ZeroInt()
	}
	return k.setReservation(ctx, account.String(), market.Id, next)
}

// Fill implements batchtypes.MarginHook: novation with the protocol. A fill
// first reduces an opposite position, then opens or increases one with the
// remainder (never for reduce_only orders). The protocol fee (perp_fee_bps
// on the notional) and the integrator fee come out of free collateral.
func (h MarginHook) Fill(ctx sdk.Context, f batchtypes.PerpFill) error {
	k := h.k
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	market, err := k.GetMarket(ctx, f.Market.Id)
	if err != nil {
		return err
	}
	if !market.Enabled || market.State == types.ORACLE_STATE_PAUSED {
		return types.ErrMarketPaused.Wrap(market.Id)
	}
	account := f.Account.String()
	isFund := account == types.InsuranceFundAddress()
	val := k.stValuationEndBlock(ctx, params)

	// premium sample of the batch (spec §20.1): p* against P_ref, once per batch
	if err := k.recordPremium(ctx, params, &market, f.Batch, f.Price); err != nil {
		return err
	}

	pos, exists, err := k.GetPosition(ctx, account, market.Id)
	if err != nil {
		return err
	}
	qty := f.Qty
	if exists && pos.Side != f.Side {
		closeQty := math.MinInt(qty, pos.Qty)
		if _, _, err := k.reducePositionValued(ctx, &market, &pos, closeQty, f.Price, val); err != nil {
			return err
		}
		qty = qty.Sub(closeQty)
		if pos.Qty.IsZero() {
			if err := k.removePosition(ctx, pos); err != nil {
				return err
			}
			exists = false
		} else if err := k.setPositionValued(ctx, market, pos, val); err != nil {
			return err
		}
	}
	if qty.IsPositive() {
		if f.ReduceOnly || isFund {
			return errorsmod.Wrap(types.ErrReduceOnly, "fill would increase the position")
		}
		if market.State == types.ORACLE_STATE_REDUCE_ONLY {
			return errorsmod.Wrap(types.ErrReduceOnly, "market in reduce-only state")
		}
		cap := market.EffectiveOiCap
		if market.State == types.ORACLE_STATE_RESTRICTED {
			cap = cap.QuoRaw(2)
		}
		oi := market.OiLong
		if f.Side == batchtypes.SIDE_SELL {
			oi = market.OiShort
		}
		if oi.Add(qty).GT(cap) {
			return errorsmod.Wrapf(types.ErrOICapExceeded, "%s side at %s, cap %s", f.Side, oi, cap)
		}
		if err := k.increasePositionValued(ctx, params, &market, account, f.Side, qty, f.Price, &pos, exists, val); err != nil {
			return err
		}
		if err := k.setPositionValued(ctx, market, pos, val); err != nil {
			return err
		}
	}

	// fees (spec §23): protocol fee on the notional, builder fee on top
	fee := math.ZeroInt()
	if !isFund {
		fee = math.LegacyNewDecFromInt(f.Qty).Mul(f.Price).MulInt64(int64(params.PerpFeeBps)).QuoInt64(10_000).Ceil().TruncateInt()
		paidInLuna, err := k.tryFeeInLuna(ctx, params, val, f.Account, fee)
		if err != nil {
			return err
		}
		if !paidInLuna {
			if _, _, err := k.chargeSettlement(ctx, val, account, fee); err != nil {
				return errorsmod.Wrapf(types.ErrInsufficientFree, "protocol fee %s: %v", fee, err)
			}
			if err := k.routeFee(ctx, fee, false); err != nil {
				return err
			}
		}
		if f.Frontend != "" && !f.BuilderFee.IsNil() && f.BuilderFee.IsPositive() {
			if _, _, err := k.chargeSettlement(ctx, val, account, f.BuilderFee); err != nil {
				return errorsmod.Wrapf(types.ErrInsufficientFree, "builder fee %s: %v", f.BuilderFee, err)
			}
			fe, err := sdk.AccAddressFromBech32(f.Frontend)
			if err != nil {
				return err
			}
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, fe, sdk.NewCoins(sdk.NewCoin(params.SettlementDenom, f.BuilderFee))); err != nil {
				return err
			}
		}
	}
	if err := k.Markets.Set(ctx, market.Id, market); err != nil {
		return err
	}
	closed := pos.Qty.IsNil() || pos.Qty.IsZero()
	ev := &types.EventPositionChanged{
		Account: account, MarketId: market.Id, Side: pos.Side.String(), Qty: "0",
		EntryPrice: "0", Collateral: "0", RealizedPnl: "0", Fee: fee.String(), Closed: closed,
	}
	if !closed {
		ev.Qty, ev.EntryPrice, ev.Collateral, ev.RealizedPnl = pos.Qty.String(), pos.EntryPrice.String(), pos.Collateral.String(), pos.RealizedPnl.String()
	}
	return ctx.EventManager().EmitTypedEvent(ev)
}

// tryFeeInLuna pays the protocol fee in LUNC at the governance discount when
// the account opted in (spec §23.3, N1 option): only while the insurance
// fund is at its target (the cascade's first step never loses its share),
// from the account's bank balance, and the LUNC is sent to the burn account.
// Returns false when the option does not apply (the settlement path runs).
func (k Keeper) tryFeeInLuna(ctx sdk.Context, params types.Params, val StValuation, account sdk.AccAddress, fee math.Int) (bool, error) {
	if !params.LunaFeeDiscount.IsPositive() || !fee.IsPositive() {
		return false, nil
	}
	if has, err := k.FeeInLuna.Has(ctx, account.String()); err != nil || !has {
		return false, err
	}
	if !val.Price.IsPositive() {
		return false, nil
	}
	balance, err := k.insuranceBalance(ctx)
	if err != nil {
		return false, err
	}
	target, err := k.InsuranceTarget(ctx)
	if err != nil {
		return false, err
	}
	if balance.LT(target) {
		return false, nil
	}
	discounted := math.LegacyOneDec().Sub(params.LunaFeeDiscount).MulInt(fee)
	luna := discounted.Quo(val.Price).Ceil().TruncateInt()
	coins := sdk.NewCoins(sdk.NewCoin("uluna", luna))
	if !k.bankKeeper.GetBalance(ctx, account, "uluna").Amount.GTE(luna) {
		return false, nil
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, account, types.ModuleName, coins); err != nil {
		return false, err
	}
	if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, k.burnAccount, coins); err != nil {
		return false, err
	}
	return true, ctx.EventManager().EmitTypedEvent(&types.EventFeePaidInLuna{Account: account.String(), SettlementFee: fee.String(), LunaBurned: luna.String()})
}
