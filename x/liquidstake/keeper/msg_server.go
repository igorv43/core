package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

type msgServer struct {
	k Keeper
}

var _ types.MsgServer = msgServer{}

// NewMsgServerImpl returns the x/liquidstake MsgServer implementation.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

// Stake implements types.MsgServer.
func (ms msgServer) Stake(goCtx context.Context, msg *types.MsgStake) (*types.MsgStakeResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	sender, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return nil, err
	}
	minted, rate, err := ms.k.Stake(ctx, sender, msg.Amount)
	if err != nil {
		return nil, err
	}
	return &types.MsgStakeResponse{Minted: minted, ExchangeRate: rate}, nil
}

// Unstake implements types.MsgServer.
func (ms msgServer) Unstake(goCtx context.Context, msg *types.MsgUnstake) (*types.MsgUnstakeResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	sender, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return nil, err
	}
	res, err := ms.k.Unstake(ctx, sender, msg.Amount)
	if err != nil {
		return nil, err
	}
	return &types.MsgUnstakeResponse{Amount: res.Amount, Instant: res.Instant, RequestId: res.RequestID, ExchangeRate: res.Rate}, nil
}

// Claim implements types.MsgServer.
func (ms msgServer) Claim(goCtx context.Context, msg *types.MsgClaim) (*types.MsgClaimResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	sender, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return nil, err
	}
	coin, ids, err := ms.k.Claim(ctx, sender)
	if err != nil {
		return nil, err
	}
	return &types.MsgClaimResponse{Amount: coin, RequestIds: ids}, nil
}

// UpdateParams implements types.MsgServer.
func (ms msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if ms.k.authority != msg.Authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "expected %s, got %s", ms.k.authority, msg.Authority)
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := ms.k.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
