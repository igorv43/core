package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
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
	if err := qs.k.Gateways.Walk(ctx, nil, func(_ collections.Pair[uint64, uint32], g types.Gateway) (bool, error) {
		res.Gateways = append(res.Gateways, g)
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

// ConversionReceipts lists the receipts of an account, newest first (spec §14.7.5).
func (qs queryServer) ConversionReceipts(goCtx context.Context, req *types.QueryConversionReceiptsRequest) (*types.QueryConversionReceiptsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	if _, err := sdk.AccAddressFromBech32(req.Address); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid address: %v", err)
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	page := req.Pagination
	if page == nil {
		page = &query.PageRequest{}
	}
	page.Reverse = true
	receipts, pageRes, err := query.CollectionPaginate(ctx, qs.k.Receipts, page,
		func(_ collections.Pair[string, uint64], r types.ConversionReceipt) (types.ConversionReceipt, error) {
			return r, nil
		},
		query.WithCollectionPaginationPairPrefix[string, uint64](req.Address))
	if err != nil {
		return nil, err
	}
	if receipts == nil {
		receipts = []types.ConversionReceipt{}
	}
	return &types.QueryConversionReceiptsResponse{Receipts: receipts, Pagination: pageRes}, nil
}

// Executors lists the FabricExecutors with their expected collateral (spec §11.5).
func (qs queryServer) Executors(goCtx context.Context, _ *types.QueryExecutorsRequest) (*types.QueryExecutorsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params, err := qs.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	res := &types.QueryExecutorsResponse{Executors: []types.ExecutorView{}}
	if err := qs.k.Executors.Walk(ctx, nil, func(_ uint32, ex types.Executor) (bool, error) {
		ledger, expected, err := qs.k.ExpectedCollateral(ctx, ex)
		if err != nil {
			return true, err
		}
		res.Executors = append(res.Executors, types.ExecutorView{Executor: ex, LedgerCollateral: ledger, ExpectedCollateral: expected})
		return false, nil
	}); err != nil {
		return nil, err
	}
	h := ctx.BlockHeight()
	res.NextEpochHeight = h + params.RebalanceEpochBlocks - h%params.RebalanceEpochBlocks
	return res, nil
}

// Ports lists the port-of-entry chains (spec §11.6).
func (qs queryServer) Ports(goCtx context.Context, _ *types.QueryPortsRequest) (*types.QueryPortsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	res := &types.QueryPortsResponse{Ports: []types.Port{}}
	err := qs.k.Ports.Walk(ctx, nil, func(_ uint32, p types.Port) (bool, error) {
		res.Ports = append(res.Ports, p)
		return false, nil
	})
	return res, err
}
