package keeper

import (
	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// Perpetual orders (spec §14.3): the same engine, a different collateral
// class. A perp intent escrows nothing; the margin hook of x/perp reserves
// IM·notional at the limit price (unless reduce_only) and settles fills by
// novation with the protocol. Perp intents are quantity orders on both
// sides (`remaining` in base units) and have no min_out.

// PerpOrder is the input of SubmitPerpIntent.
type PerpOrder struct {
	Sender       string
	MarketID     string
	Side         types.Side
	Qty          math.Int
	LimitPrice   math.LegacyDec
	ExpiryHeight int64
	ReduceOnly   bool
	Frontend     string
	// ChargeFee charges the anti-spam intent fee to the sender (user orders);
	// protocol-originated orders (triggers, insurance fund unwinds) skip it.
	ChargeFee bool
}

// SubmitPerpIntent records a margin-reserved order for a perp market. Called
// by x/perp (MsgSubmitPerpIntent, triggers and insurance-fund unwinds).
func (k Keeper) SubmitPerpIntent(ctx sdk.Context, o PerpOrder) (uint64, uint64, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return 0, 0, err
	}
	market, err := k.GetMarket(ctx, o.MarketID)
	if err != nil {
		return 0, 0, err
	}
	if !market.Enabled {
		return 0, 0, types.ErrMarketDisabled.Wrap(market.Id)
	}
	if market.Type != types.MARKET_TYPE_PERP {
		return 0, 0, errorsmod.Wrap(types.ErrInvalidIntent, "not a perpetual market")
	}
	if k.marginHook == nil {
		return 0, 0, errorsmod.Wrap(types.ErrInvalidIntent, "perpetual markets need x/perp (margin hook not registered)")
	}
	if o.Side != types.SIDE_BUY && o.Side != types.SIDE_SELL {
		return 0, 0, errorsmod.Wrap(types.ErrInvalidIntent, "side must be buy or sell")
	}
	if o.LimitPrice.IsNil() || !o.LimitPrice.IsPositive() || !o.LimitPrice.Quo(market.TickSize).IsInteger() {
		return 0, 0, errorsmod.Wrapf(types.ErrInvalidIntent, "limit_price must be a positive multiple of the tick size %s", market.TickSize)
	}
	if o.Qty.IsNil() || o.Qty.LT(market.MinQty) {
		return 0, 0, errorsmod.Wrapf(types.ErrInvalidIntent, "quantity below the market minimum %s", market.MinQty)
	}
	sender, err := sdk.AccAddressFromBech32(o.Sender)
	if err != nil {
		return 0, 0, err
	}
	if err := k.checkIntentBounds(ctx, params, market, o.Sender, o.ExpiryHeight); err != nil {
		return 0, 0, err
	}
	frontend, err := k.attribution(ctx, o.Sender, o.Frontend)
	if err != nil {
		return 0, 0, err
	}
	if o.ChargeFee && params.IntentFee.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sender, authtypes.FeeCollectorName, sdk.NewCoins(params.IntentFee)); err != nil {
			return 0, 0, err
		}
	}
	if !o.ReduceOnly {
		if err := k.marginHook.Reserve(ctx, sender, market, o.Side, o.Qty, o.LimitPrice); err != nil {
			return 0, 0, err
		}
	}
	id, err := k.IntentSeq.Next(ctx)
	if err != nil {
		return 0, 0, err
	}
	height := ctx.BlockHeight()
	intent := types.Intent{
		Id: id, MarketId: market.Id, Sender: o.Sender, Side: o.Side,
		AmountIn: sdk.NewCoin(market.QuoteDenom, math.ZeroInt()), Remaining: o.Qty, LimitPrice: o.LimitPrice,
		MinOut: math.ZeroInt(), Received: math.ZeroInt(), ExpiryHeight: o.ExpiryHeight, CreatedHeight: height,
		Frontend: frontend, ReduceOnly: o.ReduceOnly,
	}
	if err := k.setIntent(ctx, intent); err != nil {
		return 0, 0, err
	}
	batchID := uint64(height)
	if err := ctx.EventManager().EmitTypedEvent(&types.EventIntentSubmitted{
		IntentId: id, MarketId: market.Id, Sender: o.Sender, Side: o.Side.String(),
		AmountIn: o.Qty.String() + " (perp qty)", LimitPrice: o.LimitPrice.String(), ExpiryHeight: o.ExpiryHeight, BatchId: batchID,
	}); err != nil {
		return 0, 0, err
	}
	return id, batchID, nil
}

// SubmitIntentInternal submits a spot intent on behalf of a protocol account
// (e.g. the buyback of x/perp) without the anti-spam fee. The escrow is taken
// from the sender like any intent.
func (k Keeper) SubmitIntentInternal(ctx sdk.Context, msg *types.MsgSubmitIntent) (uint64, uint64, error) {
	return k.submitIntent(ctx, msg, false)
}

// HasOpenIntent reports whether the account has an open intent in the market.
func (k Keeper) HasOpenIntent(ctx sdk.Context, account, marketID string) (bool, error) {
	found := false
	err := k.IntentsByAccount.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](account),
		func(key collections.Pair[string, uint64]) (bool, error) {
			in, err := k.Intents.Get(ctx, key.K2())
			if err != nil {
				return true, err
			}
			if in.MarketId == marketID {
				found = true
				return true, nil
			}
			return false, nil
		})
	return found, err
}

// releasePerpReservation frees the margin reserved for the unfilled part of
// a perp intent (cancel, expiry, completion).
func (k Keeper) releasePerpReservation(ctx sdk.Context, market types.Market, in types.Intent) error {
	if in.ReduceOnly || k.marginHook == nil || !in.Remaining.IsPositive() {
		return nil
	}
	return k.marginHook.Release(ctx, sdk.MustAccAddressFromBech32(in.Sender), market, in.Side, in.Remaining, in.LimitPrice)
}
