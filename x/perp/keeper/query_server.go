package keeper

import (
	"context"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type queryServer struct{ k Keeper }

var _ types.QueryServer = queryServer{}

// NewQueryServerImpl returns the x/perp QueryServer implementation.
func NewQueryServerImpl(k Keeper) types.QueryServer { return queryServer{k: k} }

func (qs queryServer) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	p, err := qs.k.GetParams(sdk.UnwrapSDKContext(goCtx))
	if err != nil {
		return nil, err
	}
	return &types.QueryParamsResponse{Params: p}, nil
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
	params, err := qs.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	view, err := qs.k.MarketView(ctx, params, m)
	if err != nil {
		return nil, err
	}
	return &types.QueryMarketResponse{Market: view}, nil
}

func (qs queryServer) Markets(goCtx context.Context, _ *types.QueryMarketsRequest) (*types.QueryMarketsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params, err := qs.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	markets, err := qs.k.AllMarkets(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]types.MarketView, 0, len(markets))
	for _, m := range markets {
		v, err := qs.k.MarketView(ctx, params, m)
		if err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return &types.QueryMarketsResponse{Markets: views}, nil
}

func (qs queryServer) Position(goCtx context.Context, req *types.QueryPositionRequest) (*types.QueryPositionResponse, error) {
	if req == nil || req.Account == "" || req.MarketId == "" {
		return nil, status.Error(codes.InvalidArgument, "account and market_id are required")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	m, err := qs.k.GetMarket(ctx, req.MarketId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	p, exists, err := qs.k.GetPosition(ctx, req.Account, req.MarketId)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, status.Error(codes.NotFound, types.ErrPositionNotFound.Error())
	}
	return &types.QueryPositionResponse{Position: qs.k.View(ctx, m, p)}, nil
}

func (qs queryServer) Positions(goCtx context.Context, req *types.QueryPositionsRequest) (*types.QueryPositionsResponse, error) {
	if req == nil || req.Account == "" {
		return nil, status.Error(codes.InvalidArgument, "account is required")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	positions, err := qs.k.PositionsOfAccount(ctx, req.Account)
	if err != nil {
		return nil, err
	}
	views := make([]types.PositionView, 0, len(positions))
	for _, p := range positions {
		m, err := qs.k.GetMarket(ctx, p.MarketId)
		if err != nil {
			return nil, err
		}
		views = append(views, qs.k.View(ctx, m, p))
	}
	return &types.QueryPositionsResponse{Positions: views}, nil
}

func (qs queryServer) Collateral(goCtx context.Context, req *types.QueryCollateralRequest) (*types.QueryCollateralResponse, error) {
	if req == nil || req.Account == "" {
		return nil, status.Error(codes.InvalidArgument, "account is required")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	free, err := qs.k.FreeCollateral(ctx, req.Account)
	if err != nil {
		return nil, err
	}
	reserved, err := qs.k.ReservedTotal(ctx, req.Account)
	if err != nil {
		return nil, err
	}
	positions, err := qs.k.PositionsOfAccount(ctx, req.Account)
	if err != nil {
		return nil, err
	}
	inPositions := free.Sub(free) // zero
	for _, p := range positions {
		inPositions = inPositions.Add(p.Collateral)
	}
	auto, err := qs.k.AutoTopUp.Has(ctx, req.Account)
	if err != nil {
		return nil, err
	}
	params, err := qs.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	val := qs.k.stValuation(ctx, params)
	freeSt, err := qs.k.FreeSt(ctx, req.Account)
	if err != nil {
		return nil, err
	}
	inPositionsSt := math.ZeroInt()
	for _, p := range positions {
		if !p.CollateralSt.IsNil() {
			inPositionsSt = inPositionsSt.Add(p.CollateralSt)
		}
	}
	feeInLuna, err := qs.k.FeeInLuna.Has(ctx, req.Account)
	if err != nil {
		return nil, err
	}
	return &types.QueryCollateralResponse{Free: free, Reserved: reserved, InPositions: inPositions, AutoTopUp: auto,
		FreeSt: freeSt, StValue: val.Value(freeSt), HaircutEff: val.Haircut, InPositionsSt: inPositionsSt, FeeInLuna: feeInLuna}, nil
}

func (qs queryServer) Triggers(goCtx context.Context, req *types.QueryTriggersRequest) (*types.QueryTriggersResponse, error) {
	if req == nil || req.Account == "" {
		return nil, status.Error(codes.InvalidArgument, "account is required")
	}
	triggers, err := qs.k.TriggersOfAccount(sdk.UnwrapSDKContext(goCtx), req.Account)
	if err != nil {
		return nil, err
	}
	if triggers == nil {
		triggers = []types.TriggerOrder{}
	}
	return &types.QueryTriggersResponse{Triggers: triggers}, nil
}

func (qs queryServer) InsuranceFund(goCtx context.Context, _ *types.QueryInsuranceFundRequest) (*types.QueryInsuranceFundResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	l, err := qs.k.GetLedger(ctx)
	if err != nil {
		return nil, err
	}
	target, err := qs.k.InsuranceTarget(ctx)
	if err != nil {
		return nil, err
	}
	inventory, err := qs.k.PositionsOfAccount(ctx, types.InsuranceFundAddress())
	if err != nil {
		return nil, err
	}
	if inventory == nil {
		inventory = []types.Position{}
	}
	return &types.QueryInsuranceFundResponse{Balance: l.Insurance, Target: target, Inventory: inventory, Ledger: l}, nil
}

func (qs queryServer) ADLRank(goCtx context.Context, req *types.QueryADLRankRequest) (*types.QueryADLRankResponse, error) {
	if req == nil || req.MarketId == "" {
		return nil, status.Error(codes.InvalidArgument, "market_id is required")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	m, err := qs.k.GetMarket(ctx, req.MarketId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	entries := []types.ADLEntry{}
	for _, side := range sidesFor(req.Side) {
		e, _, err := qs.k.adlRank(ctx, m, side)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e...)
	}
	return &types.QueryADLRankResponse{Entries: entries}, nil
}

func (qs queryServer) ListingCheck(goCtx context.Context, req *types.QueryListingCheckRequest) (*types.QueryListingCheckResponse, error) {
	if req == nil || req.MarketId == "" {
		return nil, status.Error(codes.InvalidArgument, "market_id is required")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	m, err := qs.k.GetMarket(ctx, req.MarketId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	res, err := qs.k.ListingCheck(ctx, m)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (qs queryServer) Allocations(goCtx context.Context, req *types.QueryAllocationsRequest) (*types.QueryAllocationsResponse, error) {
	limit := 0
	if req != nil {
		limit = int(req.Limit)
	}
	recs, err := qs.k.AllocationRecords(sdk.UnwrapSDKContext(goCtx), limit)
	if err != nil {
		return nil, err
	}
	if recs == nil {
		recs = []types.AllocationRecord{}
	}
	return &types.QueryAllocationsResponse{Allocations: recs}, nil
}
