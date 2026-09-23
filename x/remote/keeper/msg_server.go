package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

type msgServer struct{ k Keeper }

var _ types.MsgServer = msgServer{}

// NewMsgServerImpl returns the x/remote MsgServer implementation.
func NewMsgServerImpl(k Keeper) types.MsgServer { return &msgServer{k: k} }

func (ms msgServer) GrantSessionKey(goCtx context.Context, msg *types.MsgGrantSessionKey) (*types.MsgGrantSessionKeyResponse, error) {
	expiry, err := ms.k.GrantSession(sdk.UnwrapSDKContext(goCtx), msg.Controller, msg.SessionKey, msg.TtlSeconds)
	if err != nil {
		return nil, err
	}
	return &types.MsgGrantSessionKeyResponse{Expiry: expiry}, nil
}

func (ms msgServer) RevokeSessionKey(goCtx context.Context, msg *types.MsgRevokeSessionKey) (*types.MsgRevokeSessionKeyResponse, error) {
	if err := ms.k.RevokeSession(sdk.UnwrapSDKContext(goCtx), msg.Controller, msg.SessionKey); err != nil {
		return nil, err
	}
	return &types.MsgRevokeSessionKeyResponse{}, nil
}

func (ms msgServer) RevokeOwnSessionKey(goCtx context.Context, msg *types.MsgRevokeOwnSessionKey) (*types.MsgRevokeOwnSessionKeyResponse, error) {
	if err := ms.k.RevokeSession(sdk.UnwrapSDKContext(goCtx), msg.Controller, msg.SessionKey); err != nil {
		return nil, err
	}
	return &types.MsgRevokeOwnSessionKeyResponse{}, nil
}

func (ms msgServer) Withdraw(goCtx context.Context, msg *types.MsgWithdraw) (*types.MsgWithdrawResponse, error) {
	id, err := ms.k.Withdraw(sdk.UnwrapSDKContext(goCtx), msg.Controller, msg.TokenId, msg.Amount, msg.TokenOut, msg.MinAccepted)
	if err != nil {
		return nil, err
	}
	return &types.MsgWithdrawResponse{MessageId: id}, nil
}

func (ms msgServer) FundPaymaster(goCtx context.Context, msg *types.MsgFundPaymaster) (*types.MsgFundPaymasterResponse, error) {
	if err := ms.k.FundPaymaster(sdk.UnwrapSDKContext(goCtx), sdk.MustAccAddressFromBech32(msg.Sender), msg.Amount); err != nil {
		return nil, err
	}
	return &types.MsgFundPaymasterResponse{}, nil
}

func (ms msgServer) authority(msg string) error {
	if msg != ms.k.authority {
		return errorsmod.Wrapf(govtypes.ErrInvalidSigner, "expected %s, got %s", ms.k.authority, msg)
	}
	return nil
}

func (ms msgServer) CreateRemoteApp(goCtx context.Context, msg *types.MsgCreateRemoteApp) (*types.MsgCreateRemoteAppResponse, error) {
	if err := ms.authority(msg.Authority); err != nil {
		return nil, err
	}
	id, err := ms.k.CreateApp(sdk.UnwrapSDKContext(goCtx), msg.Authority, msg.MailboxId, msg.IsmId)
	if err != nil {
		return nil, err
	}
	return &types.MsgCreateRemoteAppResponse{Id: id}, nil
}

func (ms msgServer) SetGateway(goCtx context.Context, msg *types.MsgSetGateway) (*types.MsgSetGatewayResponse, error) {
	if err := ms.authority(msg.Authority); err != nil {
		return nil, err
	}
	if err := ms.k.SetGateway(sdk.UnwrapSDKContext(goCtx), msg.AppId, msg.Domain, msg.Address, msg.ExitFactory, msg.ExitInitCodeHash); err != nil {
		return nil, err
	}
	return &types.MsgSetGatewayResponse{}, nil
}

func (ms msgServer) SetBeacon(goCtx context.Context, msg *types.MsgSetBeacon) (*types.MsgSetBeaconResponse, error) {
	if err := ms.authority(msg.Authority); err != nil {
		return nil, err
	}
	id, err := ms.k.SetBeacon(sdk.UnwrapSDKContext(goCtx), msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgSetBeaconResponse{Id: id}, nil
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

func (ms msgServer) SetExecutor(goCtx context.Context, msg *types.MsgSetExecutor) (*types.MsgSetExecutorResponse, error) {
	if err := ms.authority(msg.Authority); err != nil {
		return nil, err
	}
	if err := ms.k.SetExecutor(sdk.UnwrapSDKContext(goCtx), msg); err != nil {
		return nil, err
	}
	return &types.MsgSetExecutorResponse{}, nil
}

func (ms msgServer) ExecutorControl(goCtx context.Context, msg *types.MsgExecutorControl) (*types.MsgExecutorControlResponse, error) {
	if err := ms.authority(msg.Authority); err != nil {
		return nil, err
	}
	id, nonce, err := ms.k.ExecutorControl(sdk.UnwrapSDKContext(goCtx), msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgExecutorControlResponse{MessageId: id, Nonce: nonce}, nil
}

func (ms msgServer) SetPort(goCtx context.Context, msg *types.MsgSetPort) (*types.MsgSetPortResponse, error) {
	if err := ms.authority(msg.Authority); err != nil {
		return nil, err
	}
	if err := ms.k.SetPort(sdk.UnwrapSDKContext(goCtx), msg); err != nil {
		return nil, err
	}
	return &types.MsgSetPortResponse{}, nil
}
