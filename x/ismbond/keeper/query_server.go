package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/classic-terra/core/v4/x/ismbond/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type queryServer struct{ k Keeper }

var _ types.QueryServer = queryServer{}

// NewQueryServerImpl returns the x/ismbond QueryServer implementation.
func NewQueryServerImpl(k Keeper) types.QueryServer { return queryServer{k: k} }

func (qs queryServer) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	p, err := qs.k.GetParams(sdk.UnwrapSDKContext(goCtx))
	if err != nil {
		return nil, err
	}
	return &types.QueryParamsResponse{Params: p}, nil
}

func (qs queryServer) Operator(goCtx context.Context, req *types.QueryOperatorRequest) (*types.QueryOperatorResponse, error) {
	if req == nil || req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address is required")
	}
	o, err := qs.k.GetOperator(sdk.UnwrapSDKContext(goCtx), req.Address)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &types.QueryOperatorResponse{Operator: o}, nil
}

func (qs queryServer) Operators(goCtx context.Context, _ *types.QueryOperatorsRequest) (*types.QueryOperatorsResponse, error) {
	ops, err := qs.k.AllOperators(sdk.UnwrapSDKContext(goCtx))
	if err != nil {
		return nil, err
	}
	return &types.QueryOperatorsResponse{Operators: ops}, nil
}

func (qs queryServer) IsmBonded(goCtx context.Context, req *types.QueryIsmBondedRequest) (*types.QueryIsmBondedResponse, error) {
	if req == nil || req.IsmId == "" {
		return nil, status.Error(codes.InvalidArgument, "ism_id is required")
	}
	id, err := util.DecodeHexAddress(req.IsmId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return qs.k.IsBonded(sdk.UnwrapSDKContext(goCtx), id)
}

func (qs queryServer) Root(goCtx context.Context, req *types.QueryRootRequest) (*types.QueryRootResponse, error) {
	if req == nil || req.MailboxId == "" {
		return nil, status.Error(codes.InvalidArgument, "mailbox_id is required")
	}
	id, err := util.DecodeHexAddress(req.MailboxId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	rec, err := qs.k.GetRoot(sdk.UnwrapSDKContext(goCtx), id, req.Index)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &types.QueryRootResponse{Record: rec}, nil
}

func (qs queryServer) Roots(goCtx context.Context, req *types.QueryRootsRequest) (*types.QueryRootsResponse, error) {
	if req == nil || req.MailboxId == "" {
		return nil, status.Error(codes.InvalidArgument, "mailbox_id is required")
	}
	id, err := util.DecodeHexAddress(req.MailboxId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	limit := int(req.Limit)
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	out := []types.RootRecord{}
	if err := qs.k.Roots.Walk(sdk.UnwrapSDKContext(goCtx), collections.NewPrefixedPairRange[uint64, uint32](id.GetInternalId()).Descending(),
		func(_ collections.Pair[uint64, uint32], r types.RootRecord) (bool, error) {
			out = append(out, r)
			return len(out) >= limit, nil
		}); err != nil {
		return nil, err
	}
	return &types.QueryRootsResponse{Records: out}, nil
}
