package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/classic-terra/core/v2/x/market/types"
	oracletypes "github.com/classic-terra/core/v2/x/oracle/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the market MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

func (k msgServer) Swap(goCtx context.Context, msg *types.MsgSwap) (*types.MsgSwapResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	addr, err := sdk.AccAddressFromBech32(msg.Trader)
	if err != nil {
		return nil, err
	}

	return k.handleSwapRequest(ctx, addr, addr, msg.OfferCoin, msg.AskDenom)
}

func (k msgServer) SwapSend(goCtx context.Context, msg *types.MsgSwapSend) (*types.MsgSwapSendResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	fromAddr, err := sdk.AccAddressFromBech32(msg.FromAddress)
	if err != nil {
		return nil, err
	}

	toAddr, err := sdk.AccAddressFromBech32(msg.ToAddress)
	if err != nil {
		return nil, err
	}

	res, err := k.handleSwapRequest(ctx, fromAddr, toAddr, msg.OfferCoin, msg.AskDenom)
	if err != nil {
		return nil, err
	}

	return &types.MsgSwapSendResponse{
		SwapCoin: res.SwapCoin,
		SwapFee:  res.SwapFee,
	}, nil
}

// handleMsgSwap handles the logic of a MsgSwap
// This function does not repeat checks that have already been performed in msg.ValidateBasic()
// Ex) assert(offerCoin.Denom != askDenom)
func (k msgServer) handleSwapRequest(ctx sdk.Context,
	trader sdk.AccAddress, receiver sdk.AccAddress,
	offerCoin sdk.Coin, askDenom string,
) (*types.MsgSwapResponse, error) {
	// Compute exchange rates between the ask and offer
	swapDecCoin, spread, err := k.ComputeSwap(ctx, offerCoin, askDenom)
	if err != nil {
		return nil, err
	}

	var errSup = k.ValidateSupplyMaximum(ctx, sdk.NewDecCoin(swapDecCoin.Denom, swapDecCoin.Amount.TruncateInt()))

	if errSup != nil {
		return nil, errSup
	}
	// Charge a spread if applicable; the spread is burned
	var feeDecCoin sdk.DecCoin
	if spread.IsPositive() {
		feeDecCoin = sdk.NewDecCoinFromDec(swapDecCoin.Denom, spread.Mul(swapDecCoin.Amount))
	} else {
		feeDecCoin = sdk.NewDecCoin(swapDecCoin.Denom, sdk.ZeroInt())
	}

	// Subtract fee from the swap coin
	swapDecCoin.Amount = swapDecCoin.Amount.Sub(feeDecCoin.Amount)

	// Update pool delta
	err = k.ApplySwapToPool(ctx, offerCoin, swapDecCoin)
	if err != nil {
		return nil, err
	}

	// Send offer coins to module account
	offerCoins := sdk.NewCoins(offerCoin)
	err = k.BankKeeper.SendCoinsFromAccountToModule(ctx, trader, types.ModuleName, offerCoins)
	if err != nil {
		return nil, err
	}

	// Burn offered coins and subtract from the trader's account
	err = k.BankKeeper.BurnCoins(ctx, types.ModuleName, offerCoins)
	if err != nil {
		return nil, err
	}

	// Mint asked coins and credit Trader's account
	swapCoin, decimalCoin := swapDecCoin.TruncateDecimal()

	// Ensure to fail the swap tx when zero swap coin
	if !swapCoin.IsPositive() {
		return nil, types.ErrZeroSwapCoin
	}

	feeDecCoin = feeDecCoin.Add(decimalCoin) // add truncated decimalCoin to swapFee
	feeCoin, _ := feeDecCoin.TruncateDecimal()

	mintCoins := sdk.NewCoins(swapCoin.Add(feeCoin))
	err = k.BankKeeper.MintCoins(ctx, types.ModuleName, mintCoins)
	if err != nil {
		return nil, err
	}

	// Send swap coin to the trader
	swapCoins := sdk.NewCoins(swapCoin)
	err = k.BankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, receiver, swapCoins)
	if err != nil {
		return nil, err
	}

	// Send swap fee to oracle account
	if feeCoin.IsPositive() {
		feeCoins := sdk.NewCoins(feeCoin)
		err = k.BankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, oracletypes.ModuleName, feeCoins)
		if err != nil {
			return nil, err
		}
	}

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventSwap,
			sdk.NewAttribute(types.AttributeKeyOffer, offerCoin.String()),
			sdk.NewAttribute(types.AttributeKeyTrader, trader.String()),
			sdk.NewAttribute(types.AttributeKeyRecipient, receiver.String()),
			sdk.NewAttribute(types.AttributeKeySwapCoin, swapCoin.String()),
			sdk.NewAttribute(types.AttributeKeySwapFee, feeCoin.String()),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.AttributeValueCategory),
		),
	})

	return &types.MsgSwapResponse{
		SwapCoin: swapCoin,
		SwapFee:  feeCoin,
	}, nil
}
func (k Keeper) ValidateSupplyMaximum(ctx sdk.Context, coin sdk.DecCoin) error {

	var ok, amount = isExists(coin.Denom, k.GetMaxSupplyCoin(ctx))
	var totalSupply = k.BankKeeper.GetSupply(ctx, coin.Denom)
	if ok {

		if totalSupply.Amount.Add(coin.Amount.TruncateInt()).GT(amount) {
			return sdkerrors.Wrap(types.ErrZeroSwapCoin, "The value to be minted exceeded the maximum supply value "+amount.String()+coin.Denom)

		}
		// if (totalSupply.Amount.Int64() + coin.Amount.TruncateInt().Int64()) > amount.Int64() {
		// 	//var decoin = sdk.NewDecCoin(retDecCoin.Denom, amount)
		// 	return sdkerrors.Wrap(types.ErrZeroSwapCoin, "The value to be minted exceeded the maximum supply value "+amount.String()+coin.Denom)

		// }
	} else {
		return sdkerrors.Wrap(types.ErrZeroSwapCoin, "maximum supply not configured for currency "+coin.Denom)
	}
	return nil
}
func isExists(demom string, coins sdk.Coins) (result bool, amount sdk.Int) {
	result = false

	for _, coin := range coins {
		if coin.Denom == demom {
			amount = coin.Amount
			result = true
			break
		}
	}

	return result, amount
}
