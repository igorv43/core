package keeper

import (
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// SubmitIntent escrows amount_in and records a limit order for the batch
// sealed at this height (spec §14). Returns the intent id and batch id.
func (k Keeper) SubmitIntent(ctx sdk.Context, msg *types.MsgSubmitIntent) (uint64, uint64, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return 0, 0, err
	}
	market, err := k.GetMarket(ctx, msg.MarketId)
	if err != nil {
		return 0, 0, err
	}
	if !market.Enabled {
		return 0, 0, types.ErrMarketDisabled.Wrap(market.Id)
	}
	if market.Type != types.MARKET_TYPE_SPOT {
		return 0, 0, errorsmod.Wrap(types.ErrInvalidIntent, "use x/perp for perpetual markets")
	}
	sender := sdk.MustAccAddressFromBech32(msg.Sender)

	// escrow denom must match the side
	wantDenom := market.QuoteDenom
	if msg.Side == types.SIDE_SELL {
		wantDenom = market.BaseDenom
	}
	if msg.AmountIn.Denom != wantDenom {
		return 0, 0, errorsmod.Wrapf(types.ErrInvalidIntent, "amount_in must be %s for this side", wantDenom)
	}
	// tick and minimum quantity
	if !msg.LimitPrice.Quo(market.TickSize).IsInteger() {
		return 0, 0, errorsmod.Wrapf(types.ErrInvalidIntent, "limit_price must be a multiple of the tick size %s", market.TickSize)
	}
	qty := msg.AmountIn.Amount
	if msg.Side == types.SIDE_BUY {
		qty = math.LegacyNewDecFromInt(msg.AmountIn.Amount).Quo(msg.LimitPrice).TruncateInt()
	}
	if qty.LT(market.MinQty) {
		return 0, 0, errorsmod.Wrapf(types.ErrInvalidIntent, "quantity %s below the market minimum %s", qty, market.MinQty)
	}
	// expiry within the TTL
	height := ctx.BlockHeight()
	if msg.ExpiryHeight <= height || msg.ExpiryHeight > height+params.IntentTtlBlocks {
		return 0, 0, errorsmod.Wrapf(types.ErrInvalidIntent, "expiry_height must be within (%d, %d]", height, height+params.IntentTtlBlocks)
	}
	// per-account and per-market bounds (spec §12)
	open := 0
	if err := k.IntentsByAccount.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](msg.Sender),
		func(_ collections.Pair[string, uint64]) (bool, error) {
			open++
			return open >= types.MaxIntentsPerAccount, nil
		}); err != nil {
		return 0, 0, err
	}
	if open >= types.MaxIntentsPerAccount {
		return 0, 0, errorsmod.Wrapf(types.ErrTooManyIntents, "max %d open intents per account", types.MaxIntentsPerAccount)
	}
	if n, err := k.ActiveIntentCount(ctx, market.Id); err != nil {
		return 0, 0, err
	} else if n >= uint64(params.MaxIntentsPerBatch) {
		return 0, 0, errorsmod.Wrapf(types.ErrTooManyIntents, "market %s is at max_intents_per_batch (%d)", market.Id, params.MaxIntentsPerBatch)
	}
	// integrator attribution only when approved by the sender (spec §23.2 rule 3)
	frontend := ""
	if msg.Frontend != "" {
		if approved, err := k.frontendApproved(ctx, msg.Sender, msg.Frontend); err != nil {
			return 0, 0, err
		} else if approved {
			frontend = msg.Frontend
		}
	}

	// anti-spam fee to the chain fee collector, then the escrow
	if params.IntentFee.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sender, authtypes.FeeCollectorName, sdk.NewCoins(params.IntentFee)); err != nil {
			return 0, 0, err
		}
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sender, types.ModuleName, sdk.NewCoins(msg.AmountIn)); err != nil {
		return 0, 0, err
	}

	id, err := k.IntentSeq.Next(ctx)
	if err != nil {
		return 0, 0, err
	}
	intent := types.Intent{
		Id:            id,
		MarketId:      market.Id,
		Sender:        msg.Sender,
		Side:          msg.Side,
		AmountIn:      msg.AmountIn,
		Remaining:     msg.AmountIn.Amount,
		LimitPrice:    msg.LimitPrice,
		MinOut:        msg.MinOut,
		Received:      math.ZeroInt(),
		ExpiryHeight:  msg.ExpiryHeight,
		CreatedHeight: height,
		Frontend:      frontend,
	}
	if err := k.setIntent(ctx, intent); err != nil {
		return 0, 0, err
	}
	batchID := uint64(height)
	if err := ctx.EventManager().EmitTypedEvent(&types.EventIntentSubmitted{
		IntentId: id, MarketId: market.Id, Sender: msg.Sender, Side: msg.Side.String(),
		AmountIn: msg.AmountIn.String(), LimitPrice: msg.LimitPrice.String(), ExpiryHeight: msg.ExpiryHeight, BatchId: batchID,
	}); err != nil {
		return 0, 0, err
	}
	return id, batchID, nil
}

func (k Keeper) setIntent(ctx sdk.Context, in types.Intent) error {
	if err := k.Intents.Set(ctx, in.Id, in); err != nil {
		return err
	}
	if err := k.IntentsByAccount.Set(ctx, collections.Join(in.Sender, in.Id)); err != nil {
		return err
	}
	return k.IntentsByMarket.Set(ctx, collections.Join(in.MarketId, in.Id))
}

func (k Keeper) removeIntent(ctx sdk.Context, in types.Intent) error {
	if err := k.Intents.Remove(ctx, in.Id); err != nil {
		return err
	}
	if err := k.IntentsByAccount.Remove(ctx, collections.Join(in.Sender, in.Id)); err != nil {
		return err
	}
	return k.IntentsByMarket.Remove(ctx, collections.Join(in.MarketId, in.Id))
}

// GetIntent returns an intent.
func (k Keeper) GetIntent(ctx sdk.Context, id uint64) (types.Intent, error) {
	in, err := k.Intents.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Intent{}, types.ErrIntentNotFound.Wrapf("%d", id)
		}
		return types.Intent{}, err
	}
	return in, nil
}

// CancelIntent refunds the remaining escrow to the sender.
func (k Keeper) CancelIntent(ctx sdk.Context, sender string, id uint64) (sdk.Coin, error) {
	in, err := k.GetIntent(ctx, id)
	if err != nil {
		return sdk.Coin{}, err
	}
	if in.Sender != sender {
		return sdk.Coin{}, types.ErrUnauthorized.Wrap("not the intent owner")
	}
	refund, err := k.closeIntent(ctx, in)
	if err != nil {
		return sdk.Coin{}, err
	}
	return refund, ctx.EventManager().EmitTypedEvent(&types.EventIntentCancelled{IntentId: id, Refunded: refund.String()})
}

// closeIntent removes an intent and refunds its remaining escrow.
func (k Keeper) closeIntent(ctx sdk.Context, in types.Intent) (sdk.Coin, error) {
	refund := sdk.NewCoin(in.AmountIn.Denom, in.Remaining)
	if refund.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdk.MustAccAddressFromBech32(in.Sender), sdk.NewCoins(refund)); err != nil {
			return sdk.Coin{}, err
		}
	}
	return refund, k.removeIntent(ctx, in)
}

// ExpireIntents refunds intents past their expiry (bounded per block).
func (k Keeper) ExpireIntents(ctx sdk.Context) error {
	height := ctx.BlockHeight()
	var expired []types.Intent
	if err := k.Intents.Walk(ctx, nil, func(_ uint64, in types.Intent) (bool, error) {
		if in.ExpiryHeight <= height {
			expired = append(expired, in)
		}
		return len(expired) >= types.MaxExpiredPerBlock, nil
	}); err != nil {
		return err
	}
	for _, in := range expired {
		refund, err := k.closeIntent(ctx, in)
		if err != nil {
			return err
		}
		if err := ctx.EventManager().EmitTypedEvent(&types.EventIntentExpired{IntentId: in.Id, Refunded: refund.String()}); err != nil {
			return err
		}
	}
	return nil
}

// IntentsOfMarket returns the open intents of a market in id order, up to limit.
func (k Keeper) IntentsOfMarket(ctx sdk.Context, marketID string, limit int) ([]types.Intent, error) {
	var out []types.Intent
	err := k.IntentsByMarket.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](marketID),
		func(key collections.Pair[string, uint64]) (bool, error) {
			in, err := k.Intents.Get(ctx, key.K2())
			if err != nil {
				return true, err
			}
			out = append(out, in)
			return len(out) >= limit, nil
		})
	return out, err
}
