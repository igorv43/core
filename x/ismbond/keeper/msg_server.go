package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"github.com/classic-terra/core/v4/x/ismbond/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

type msgServer struct{ k Keeper }

var _ types.MsgServer = msgServer{}

// NewMsgServerImpl returns the x/ismbond MsgServer implementation.
func NewMsgServerImpl(k Keeper) types.MsgServer { return &msgServer{k: k} }

func (ms msgServer) BondOperator(goCtx context.Context, msg *types.MsgBondOperator) (*types.MsgBondOperatorResponse, error) {
	if err := ms.k.Bond(sdk.UnwrapSDKContext(goCtx), msg.Operator, msg.Validator, msg.Bond); err != nil {
		return nil, err
	}
	return &types.MsgBondOperatorResponse{}, nil
}

func (ms msgServer) UnbondOperator(goCtx context.Context, msg *types.MsgUnbondOperator) (*types.MsgUnbondOperatorResponse, error) {
	h, err := ms.k.Unbond(sdk.UnwrapSDKContext(goCtx), msg.Operator)
	if err != nil {
		return nil, err
	}
	return &types.MsgUnbondOperatorResponse{ClaimHeight: h}, nil
}

func (ms msgServer) ClaimBond(goCtx context.Context, msg *types.MsgClaimBond) (*types.MsgClaimBondResponse, error) {
	if err := ms.k.Claim(sdk.UnwrapSDKContext(goCtx), msg.Operator); err != nil {
		return nil, err
	}
	return &types.MsgClaimBondResponse{}, nil
}

func (ms msgServer) SubmitEvidence(goCtx context.Context, msg *types.MsgSubmitEvidence) (*types.MsgSubmitEvidenceResponse, error) {
	validator, slashed, reward, err := ms.k.SubmitEvidence(sdk.UnwrapSDKContext(goCtx), msg.Submitter, msg.Kind, msg.CheckpointA, msg.CheckpointB)
	if err != nil {
		return nil, err
	}
	return &types.MsgSubmitEvidenceResponse{Validator: validator, Slashed: slashed.String(), Reward: reward.String()}, nil
}

func (ms msgServer) DistributeRewards(goCtx context.Context, msg *types.MsgDistributeRewards) (*types.MsgDistributeRewardsResponse, error) {
	if err := ms.k.DistributeRewards(sdk.UnwrapSDKContext(goCtx), sdk.MustAccAddressFromBech32(msg.Sender), msg.Amount); err != nil {
		return nil, err
	}
	return &types.MsgDistributeRewardsResponse{}, nil
}

func (ms msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg.Authority != ms.k.authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "expected %s, got %s", ms.k.authority, msg.Authority)
	}
	if err := ms.k.SetParams(sdk.UnwrapSDKContext(goCtx), msg.Params); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidParams, err.Error())
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
