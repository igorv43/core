package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	batchkeeper "github.com/classic-terra/core/v4/x/batch/keeper"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

type msgServer struct{ k Keeper }

var _ types.MsgServer = msgServer{}

// NewMsgServerImpl returns the x/perp MsgServer implementation.
func NewMsgServerImpl(k Keeper) types.MsgServer { return &msgServer{k: k} }

func (ms msgServer) DepositCollateral(goCtx context.Context, msg *types.MsgDepositCollateral) (*types.MsgDepositCollateralResponse, error) {
	if err := ms.k.Deposit(sdk.UnwrapSDKContext(goCtx), sdk.MustAccAddressFromBech32(msg.Sender), msg.Amount); err != nil {
		return nil, err
	}
	return &types.MsgDepositCollateralResponse{}, nil
}

func (ms msgServer) WithdrawCollateral(goCtx context.Context, msg *types.MsgWithdrawCollateral) (*types.MsgWithdrawCollateralResponse, error) {
	if err := ms.k.Withdraw(sdk.UnwrapSDKContext(goCtx), sdk.MustAccAddressFromBech32(msg.Sender), msg.Amount); err != nil {
		return nil, err
	}
	return &types.MsgWithdrawCollateralResponse{}, nil
}

func (ms msgServer) SubmitPerpIntent(goCtx context.Context, msg *types.MsgSubmitPerpIntent) (*types.MsgSubmitPerpIntentResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	m, err := ms.k.GetMarket(ctx, msg.MarketId)
	if err != nil {
		return nil, err
	}
	if !m.Enabled {
		return nil, types.ErrMarketDisabled.Wrap(m.Id)
	}
	id, batch, err := ms.k.batchKeeper.SubmitPerpIntent(ctx, batchkeeper.PerpOrder{
		Sender: msg.Sender, MarketID: msg.MarketId, Side: msg.Side, Qty: msg.Qty, LimitPrice: msg.LimitPrice,
		ExpiryHeight: msg.ExpiryHeight, ReduceOnly: msg.ReduceOnly, Frontend: msg.Frontend, ChargeFee: true,
	})
	if err != nil {
		return nil, err
	}
	return &types.MsgSubmitPerpIntentResponse{IntentId: id, BatchId: batch}, nil
}

func (ms msgServer) SubmitTriggerOrder(goCtx context.Context, msg *types.MsgSubmitTriggerOrder) (*types.MsgSubmitTriggerOrderResponse, error) {
	id, err := ms.k.SubmitTrigger(sdk.UnwrapSDKContext(goCtx), msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgSubmitTriggerOrderResponse{TriggerId: id}, nil
}

func (ms msgServer) CancelTriggerOrder(goCtx context.Context, msg *types.MsgCancelTriggerOrder) (*types.MsgCancelTriggerOrderResponse, error) {
	if err := ms.k.CancelTrigger(sdk.UnwrapSDKContext(goCtx), msg.Sender, msg.TriggerId); err != nil {
		return nil, err
	}
	return &types.MsgCancelTriggerOrderResponse{}, nil
}

func (ms msgServer) SetAutoTopUp(goCtx context.Context, msg *types.MsgSetAutoTopUp) (*types.MsgSetAutoTopUpResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	var err error
	if msg.Enabled {
		err = ms.k.AutoTopUp.Set(ctx, msg.Sender)
	} else {
		err = ms.k.AutoTopUp.Remove(ctx, msg.Sender)
	}
	if err != nil {
		return nil, err
	}
	return &types.MsgSetAutoTopUpResponse{}, nil
}

func (ms msgServer) FundInsurance(goCtx context.Context, msg *types.MsgFundInsurance) (*types.MsgFundInsuranceResponse, error) {
	if err := ms.k.FundInsurance(sdk.UnwrapSDKContext(goCtx), sdk.MustAccAddressFromBech32(msg.Sender), msg.Amount); err != nil {
		return nil, err
	}
	return &types.MsgFundInsuranceResponse{}, nil
}

func (ms msgServer) authority(msg string) error {
	if msg != ms.k.authority {
		return errorsmod.Wrapf(govtypes.ErrInvalidSigner, "expected %s, got %s", ms.k.authority, msg)
	}
	return nil
}

func (ms msgServer) CreateMarket(goCtx context.Context, msg *types.MsgCreateMarket) (*types.MsgCreateMarketResponse, error) {
	if err := ms.authority(msg.Authority); err != nil {
		return nil, err
	}
	if err := ms.k.CreateMarket(sdk.UnwrapSDKContext(goCtx), msg.Market()); err != nil {
		return nil, err
	}
	return &types.MsgCreateMarketResponse{}, nil
}

func (ms msgServer) UpdateMarket(goCtx context.Context, msg *types.MsgUpdateMarket) (*types.MsgUpdateMarketResponse, error) {
	if err := ms.authority(msg.Authority); err != nil {
		return nil, err
	}
	if err := ms.k.UpdateMarket(sdk.UnwrapSDKContext(goCtx), msg); err != nil {
		return nil, err
	}
	return &types.MsgUpdateMarketResponse{}, nil
}

func (ms msgServer) EnableMarket(goCtx context.Context, msg *types.MsgEnableMarket) (*types.MsgEnableMarketResponse, error) {
	if err := ms.authority(msg.Authority); err != nil {
		return nil, err
	}
	if err := ms.k.EnableMarket(sdk.UnwrapSDKContext(goCtx), msg.MarketId, msg.Enabled); err != nil {
		return nil, err
	}
	return &types.MsgEnableMarketResponse{}, nil
}

func (ms msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if err := ms.authority(msg.Authority); err != nil {
		return nil, err
	}
	if err := ms.k.SetParams(sdk.UnwrapSDKContext(goCtx), msg.Params); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidParams, err.Error())
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
