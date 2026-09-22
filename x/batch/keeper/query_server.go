package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type queryServer struct {
	k Keeper
}

var _ types.QueryServer = queryServer{}

// NewQueryServerImpl returns the x/batch QueryServer implementation.
func NewQueryServerImpl(keeper Keeper) types.QueryServer { return queryServer{k: keeper} }

func (qs queryServer) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	p, err := qs.k.GetParams(sdk.UnwrapSDKContext(goCtx))
	if err != nil {
		return nil, err
	}
	return &types.QueryParamsResponse{Params: p}, nil
}

func (qs queryServer) Markets(goCtx context.Context, _ *types.QueryMarketsRequest) (*types.QueryMarketsResponse, error) {
	out := []types.Market{}
	err := qs.k.Markets.Walk(sdk.UnwrapSDKContext(goCtx), nil, func(_ string, m types.Market) (bool, error) {
		out = append(out, m)
		return false, nil
	})
	return &types.QueryMarketsResponse{Markets: out}, err
}

func (qs queryServer) Market(goCtx context.Context, req *types.QueryMarketRequest) (*types.QueryMarketResponse, error) {
	if req == nil || req.MarketId == "" {
		return nil, status.Error(codes.InvalidArgument, "market_id is required")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	m, err := qs.k.GetMarket(ctx, req.MarketId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	pref, err := qs.k.ReferencePrice(ctx, m)
	if err != nil {
		pref = math.LegacyZeroDec()
	}
	n, err := qs.k.ActiveIntentCount(ctx, m.Id)
	if err != nil {
		return nil, err
	}
	last := types.BatchResult{ClearingPrice: math.LegacyZeroDec(), Volume: math.ZeroInt(), ReferencePrice: math.LegacyZeroDec()}
	_ = qs.k.Results.Walk(ctx, nil, func(key collections.Pair[uint64, string], r types.BatchResult) (bool, error) {
		if key.K2() == m.Id && r.BatchId >= last.BatchId {
			last = r
		}
		return false, nil
	})
	return &types.QueryMarketResponse{Market: m, ReferencePrice: pref, ActiveIntents: n, LastResult: last}, nil
}

func (qs queryServer) Intent(goCtx context.Context, req *types.QueryIntentRequest) (*types.QueryIntentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	in, err := qs.k.GetIntent(sdk.UnwrapSDKContext(goCtx), req.IntentId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &types.QueryIntentResponse{Intent: in}, nil
}

func (qs queryServer) IntentsByAccount(goCtx context.Context, req *types.QueryIntentsByAccountRequest) (*types.QueryIntentsByAccountResponse, error) {
	if req == nil || req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address is required")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	out := []types.Intent{}
	err := qs.k.IntentsByAccount.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](req.Address),
		func(key collections.Pair[string, uint64]) (bool, error) {
			in, err := qs.k.Intents.Get(ctx, key.K2())
			if err != nil {
				return true, err
			}
			out = append(out, in)
			return false, nil
		})
	return &types.QueryIntentsByAccountResponse{Intents: out}, err
}

func (qs queryServer) Batch(goCtx context.Context, req *types.QueryBatchRequest) (*types.QueryBatchResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	results := []types.BatchResult{}
	if err := qs.k.Results.Walk(ctx, collections.NewPrefixedPairRange[uint64, string](req.BatchId),
		func(_ collections.Pair[uint64, string], r types.BatchResult) (bool, error) {
			results = append(results, r)
			return false, nil
		}); err != nil {
		return nil, err
	}
	commits := []types.Commit{}
	if err := qs.k.Commits.Walk(ctx, collections.NewPrefixedTripleRange[uint64, string, string](req.BatchId),
		func(_ collections.Triple[uint64, string, string], c types.Commit) (bool, error) {
			if !c.Revealed {
				c.Bid = &types.Bid{} // sealed
			}
			commits = append(commits, c)
			return false, nil
		}); err != nil {
		return nil, err
	}
	return &types.QueryBatchResponse{Results: results, Commits: commits}, nil
}

func (qs queryServer) Solver(goCtx context.Context, req *types.QuerySolverRequest) (*types.QuerySolverResponse, error) {
	if req == nil || req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address is required")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	s, err := qs.k.GetSolver(ctx, req.Address)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	escrow, err := qs.k.SolverEscrowBalances(ctx, req.Address)
	if err != nil {
		return nil, err
	}
	if escrow == nil {
		escrow = sdk.Coins{}
	}
	return &types.QuerySolverResponse{Solver: s, Escrow: escrow}, nil
}

func (qs queryServer) Solvers(goCtx context.Context, _ *types.QuerySolversRequest) (*types.QuerySolversResponse, error) {
	out := []types.Solver{}
	err := qs.k.Solvers.Walk(sdk.UnwrapSDKContext(goCtx), nil, func(_ string, s types.Solver) (bool, error) {
		out = append(out, s)
		return false, nil
	})
	return &types.QuerySolversResponse{Solvers: out}, err
}

func (qs queryServer) Frontend(goCtx context.Context, req *types.QueryFrontendRequest) (*types.QueryFrontendResponse, error) {
	if req == nil || req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address is required")
	}
	fe, err := qs.k.Frontends.Get(sdk.UnwrapSDKContext(goCtx), req.Address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "frontend not registered")
		}
		return nil, err
	}
	return &types.QueryFrontendResponse{Frontend: fe}, nil
}

func (qs queryServer) Approvals(goCtx context.Context, req *types.QueryApprovalsRequest) (*types.QueryApprovalsResponse, error) {
	if req == nil || req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address is required")
	}
	out := []types.FrontendApproval{}
	err := qs.k.Approvals.Walk(sdk.UnwrapSDKContext(goCtx), collections.NewPrefixedPairRange[string, string](req.Address),
		func(_ collections.Pair[string, string], a types.FrontendApproval) (bool, error) {
			out = append(out, a)
			return false, nil
		})
	return &types.QueryApprovalsResponse{Approvals: out}, err
}

func (qs queryServer) Pipeline(goCtx context.Context, _ *types.QueryPipelineRequest) (*types.QueryPipelineResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	p, err := qs.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	h := ctx.BlockHeight()
	res := &types.QueryPipelineResponse{Height: h, Collecting: uint64(h)}
	for b := h - p.CommitWindow; b < h; b++ {
		if b >= 1 {
			res.Committing = append(res.Committing, uint64(b))
		}
	}
	if b := h - p.CommitWindow - 1; b >= 1 {
		res.Revealing = uint64(b)
		res.Resolving = uint64(b)
	}
	return res, nil
}
