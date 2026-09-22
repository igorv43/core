package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

type msgServer struct {
	k Keeper
}

var _ types.MsgServer = msgServer{}

// NewMsgServerImpl returns the x/batch MsgServer implementation.
func NewMsgServerImpl(keeper Keeper) types.MsgServer { return &msgServer{k: keeper} }

func (ms msgServer) SubmitIntent(goCtx context.Context, msg *types.MsgSubmitIntent) (*types.MsgSubmitIntentResponse, error) {
	id, batch, err := ms.k.SubmitIntent(sdk.UnwrapSDKContext(goCtx), msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgSubmitIntentResponse{IntentId: id, BatchId: batch}, nil
}

func (ms msgServer) CancelIntent(goCtx context.Context, msg *types.MsgCancelIntent) (*types.MsgCancelIntentResponse, error) {
	if _, err := ms.k.CancelIntent(sdk.UnwrapSDKContext(goCtx), msg.Sender, msg.IntentId); err != nil {
		return nil, err
	}
	return &types.MsgCancelIntentResponse{}, nil
}

func (ms msgServer) RegisterSolver(goCtx context.Context, msg *types.MsgRegisterSolver) (*types.MsgRegisterSolverResponse, error) {
	if err := ms.k.RegisterSolver(sdk.UnwrapSDKContext(goCtx), msg.Sender, msg.Bond); err != nil {
		return nil, err
	}
	return &types.MsgRegisterSolverResponse{}, nil
}

func (ms msgServer) UnbondSolver(goCtx context.Context, msg *types.MsgUnbondSolver) (*types.MsgUnbondSolverResponse, error) {
	h, err := ms.k.UnbondSolver(sdk.UnwrapSDKContext(goCtx), msg.Sender)
	if err != nil {
		return nil, err
	}
	return &types.MsgUnbondSolverResponse{RefundHeight: h}, nil
}

func (ms msgServer) DepositSolverEscrow(goCtx context.Context, msg *types.MsgDepositSolverEscrow) (*types.MsgDepositSolverEscrowResponse, error) {
	if err := ms.k.DepositSolverEscrow(sdk.UnwrapSDKContext(goCtx), msg.Sender, msg.Amount); err != nil {
		return nil, err
	}
	return &types.MsgDepositSolverEscrowResponse{}, nil
}

func (ms msgServer) WithdrawSolverEscrow(goCtx context.Context, msg *types.MsgWithdrawSolverEscrow) (*types.MsgWithdrawSolverEscrowResponse, error) {
	if err := ms.k.WithdrawSolverEscrow(sdk.UnwrapSDKContext(goCtx), msg.Sender, msg.Amount); err != nil {
		return nil, err
	}
	return &types.MsgWithdrawSolverEscrowResponse{}, nil
}

func (ms msgServer) CommitBid(goCtx context.Context, msg *types.MsgCommitBid) (*types.MsgCommitBidResponse, error) {
	if err := ms.k.CommitBid(sdk.UnwrapSDKContext(goCtx), msg); err != nil {
		return nil, err
	}
	return &types.MsgCommitBidResponse{}, nil
}

func (ms msgServer) RevealBid(goCtx context.Context, msg *types.MsgRevealBid) (*types.MsgRevealBidResponse, error) {
	if err := ms.k.RevealBid(sdk.UnwrapSDKContext(goCtx), msg); err != nil {
		return nil, err
	}
	return &types.MsgRevealBidResponse{}, nil
}

func (ms msgServer) RegisterFrontend(goCtx context.Context, msg *types.MsgRegisterFrontend) (*types.MsgRegisterFrontendResponse, error) {
	if err := ms.k.RegisterFrontend(sdk.UnwrapSDKContext(goCtx), msg.Sender, msg.FeeBps); err != nil {
		return nil, err
	}
	return &types.MsgRegisterFrontendResponse{}, nil
}

func (ms msgServer) ApproveFrontend(goCtx context.Context, msg *types.MsgApproveFrontend) (*types.MsgApproveFrontendResponse, error) {
	if err := ms.k.ApproveFrontend(sdk.UnwrapSDKContext(goCtx), msg.Sender, msg.Frontend, msg.MaxFeeBps); err != nil {
		return nil, err
	}
	return &types.MsgApproveFrontendResponse{}, nil
}

func (ms msgServer) RevokeFrontend(goCtx context.Context, msg *types.MsgRevokeFrontend) (*types.MsgRevokeFrontendResponse, error) {
	if err := ms.k.RevokeFrontend(sdk.UnwrapSDKContext(goCtx), msg.Sender, msg.Frontend); err != nil {
		return nil, err
	}
	return &types.MsgRevokeFrontendResponse{}, nil
}

func (ms msgServer) authorized(authority string) error {
	if ms.k.authority != authority {
		return errorsmod.Wrapf(govtypes.ErrInvalidSigner, "expected %s, got %s", ms.k.authority, authority)
	}
	return nil
}

func (ms msgServer) CreateMarket(goCtx context.Context, msg *types.MsgCreateMarket) (*types.MsgCreateMarketResponse, error) {
	if err := ms.authorized(msg.Authority); err != nil {
		return nil, err
	}
	if err := ms.k.CreateMarket(sdk.UnwrapSDKContext(goCtx), msg.Market); err != nil {
		return nil, err
	}
	return &types.MsgCreateMarketResponse{}, nil
}

func (ms msgServer) SetMarketEnabled(goCtx context.Context, msg *types.MsgSetMarketEnabled) (*types.MsgSetMarketEnabledResponse, error) {
	if err := ms.authorized(msg.Authority); err != nil {
		return nil, err
	}
	if err := ms.k.SetMarketEnabled(sdk.UnwrapSDKContext(goCtx), msg.MarketId, msg.Enabled); err != nil {
		return nil, err
	}
	return &types.MsgSetMarketEnabledResponse{}, nil
}

func (ms msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if err := ms.authorized(msg.Authority); err != nil {
		return nil, err
	}
	if err := ms.k.SetParams(sdk.UnwrapSDKContext(goCtx), msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
