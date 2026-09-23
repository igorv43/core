package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type queryServer struct{ k Keeper }

var _ types.QueryServer = queryServer{}

// NewQueryServerImpl returns the x/remote QueryServer implementation.
func NewQueryServerImpl(k Keeper) types.QueryServer { return queryServer{k: k} }

func (qs queryServer) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	p, err := qs.k.GetParams(sdk.UnwrapSDKContext(goCtx))
	if err != nil {
		return nil, err
	}
	return &types.QueryParamsResponse{Params: p}, nil
}

func (qs queryServer) RemoteApps(goCtx context.Context, _ *types.QueryRemoteAppsRequest) (*types.QueryRemoteAppsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	res := &types.QueryRemoteAppsResponse{Apps: []types.RemoteApp{}, Gateways: []types.Gateway{}}
	if err := qs.k.Apps.Walk(ctx, nil, func(_ uint64, a types.RemoteApp) (bool, error) {
		res.Apps = append(res.Apps, a)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := qs.k.Gateways.Walk(ctx, nil, func(key collections.Pair[uint64, uint32], bz []byte) (bool, error) {
		h, err := hexFromBytes(bz)
		if err != nil {
			return true, err
		}
		for _, a := range res.Apps {
			if a.Id.GetInternalId() == key.K1() {
				res.Gateways = append(res.Gateways, types.Gateway{AppId: a.Id, Domain: key.K2(), Address: h})
			}
		}
		return false, nil
	}); err != nil {
		return nil, err
	}
	return res, nil
}

func parseController(s string) (util.HexAddress, error) {
	h, err := util.DecodeHexAddress(s)
	if err != nil {
		return util.HexAddress{}, status.Error(codes.InvalidArgument, "controller must be a 32-byte hex address")
	}
	return h, nil
}

func (qs queryServer) DeriveAddress(_ context.Context, req *types.QueryDeriveAddressRequest) (*types.QueryDeriveAddressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	c, err := parseController(req.Controller)
	if err != nil {
		return nil, err
	}
	return &types.QueryDeriveAddressResponse{Address: types.DeriveAddress(req.Domain, c).String()}, nil
}

func (qs queryServer) RemoteAccount(goCtx context.Context, req *types.QueryRemoteAccountRequest) (*types.QueryRemoteAccountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	c, err := parseController(req.Controller)
	if err != nil {
		return nil, err
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	addr := types.DeriveAddress(req.Domain, c)
	account, err := qs.k.GetAccount(ctx, addr.String())
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	sessions, err := qs.k.SessionsOf(ctx, addr.String())
	if err != nil {
		return nil, err
	}
	return &types.QueryRemoteAccountResponse{Account: account, Balances: qs.k.bankKeeper.GetAllBalances(ctx, addr), Sessions: sessions}, nil
}

func (qs queryServer) Sessions(goCtx context.Context, req *types.QuerySessionsRequest) (*types.QuerySessionsResponse, error) {
	if req == nil || req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address is required")
	}
	sessions, err := qs.k.SessionsOf(sdk.UnwrapSDKContext(goCtx), req.Address)
	if err != nil {
		return nil, err
	}
	return &types.QuerySessionsResponse{Sessions: sessions}, nil
}

func (qs queryServer) Withdrawals(goCtx context.Context, req *types.QueryWithdrawalsRequest) (*types.QueryWithdrawalsResponse, error) {
	if req == nil || req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address is required")
	}
	w, err := qs.k.WithdrawalsOf(sdk.UnwrapSDKContext(goCtx), req.Address)
	if err != nil {
		return nil, err
	}
	return &types.QueryWithdrawalsResponse{Withdrawals: w}, nil
}

func (qs queryServer) Paymaster(goCtx context.Context, _ *types.QueryPaymasterRequest) (*types.QueryPaymasterResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	pm := types.PaymasterAddress()
	return &types.QueryPaymasterResponse{Address: pm.String(), Balance: qs.k.bankKeeper.GetAllBalances(ctx, pm)}, nil
}

func (qs queryServer) Beacons(goCtx context.Context, _ *types.QueryBeaconsRequest) (*types.QueryBeaconsResponse, error) {
	b, err := qs.k.AllBeacons(sdk.UnwrapSDKContext(goCtx))
	if err != nil {
		return nil, err
	}
	return &types.QueryBeaconsResponse{Beacons: b}, nil
}

func (qs queryServer) BeaconBody(goCtx context.Context, req *types.QueryBeaconBodyRequest) (*types.QueryBeaconBodyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	b, err := qs.k.Beacons.Get(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, "beacon not found")
	}
	body, err := qs.k.BeaconBody(ctx, b)
	if err != nil {
		return nil, err
	}
	return &types.QueryBeaconBodyResponse{Body: body}, nil
}
