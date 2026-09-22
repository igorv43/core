package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"github.com/classic-terra/core/v4/x/warpledger/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

type msgServer struct {
	k Keeper
}

var _ types.MsgServer = msgServer{}

// NewMsgServerImpl returns the x/warpledger MsgServer implementation.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
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

// SetDomainCap implements types.MsgServer.
func (ms msgServer) SetDomainCap(goCtx context.Context, msg *types.MsgSetDomainCap) (*types.MsgSetDomainCapResponse, error) {
	if ms.k.authority != msg.Authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "expected %s, got %s", ms.k.authority, msg.Authority)
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := ms.k.SetDomainCap(ctx, msg.TokenId, msg.Domain, msg.Cap); err != nil {
		return nil, err
	}
	return &types.MsgSetDomainCapResponse{}, nil
}

// SweepMigration implements types.MsgServer.
func (ms msgServer) SweepMigration(goCtx context.Context, msg *types.MsgSweepMigration) (*types.MsgSweepMigrationResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	result, err := ms.k.SweepMigration(ctx, msg.TokenId, msg.Domain, msg.Recipient)
	if err != nil {
		return nil, err
	}
	return &types.MsgSweepMigrationResponse{
		MessageId:      result.MessageId,
		Amount:         result.Amount,
		DepositAddress: result.DepositAddress.String(),
	}, nil
}
