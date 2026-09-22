package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type queryServer struct {
	k Keeper
}

var _ types.QueryServer = queryServer{}

// NewQueryServerImpl returns the x/liquidstake QueryServer implementation.
func NewQueryServerImpl(keeper Keeper) types.QueryServer {
	return queryServer{k: keeper}
}

// Params implements types.QueryServer.
func (qs queryServer) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	p, err := qs.k.GetParams(sdk.UnwrapSDKContext(goCtx))
	if err != nil {
		return nil, err
	}
	return &types.QueryParamsResponse{Params: p}, nil
}

// ExchangeRate implements types.QueryServer.
func (qs queryServer) ExchangeRate(goCtx context.Context, _ *types.QueryExchangeRateRequest) (*types.QueryExchangeRateResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	rate, t, err := qs.k.ExchangeRate(ctx)
	if err != nil {
		return nil, err
	}
	buffer, err := qs.k.RewardBuffer(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryExchangeRateResponse{
		ExchangeRate:   rate,
		Delegated:      t.Delegated,
		Unbonding:      t.Unbonding,
		Buffer:         t.Buffer(),
		PendingRewards: t.PendingRewards,
		Owed:           t.Owed,
		StSupply:       sdk.NewCoin(types.StDenom, t.StSupply),
		RewardBuffer:   buffer,
		Height:         ctx.BlockHeight(),
	}, nil
}

// Delegations implements types.QueryServer.
func (qs queryServer) Delegations(goCtx context.Context, _ *types.QueryDelegationsRequest) (*types.QueryDelegationsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params, err := qs.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	dels, err := qs.k.delegations(ctx)
	if err != nil {
		return nil, err
	}
	total := math.ZeroInt()
	for _, d := range dels {
		total = total.Add(d.Tokens)
	}
	entries := make([]types.DelegationEntry, 0, len(dels))
	for _, d := range dels {
		share := math.LegacyZeroDec()
		if total.IsPositive() {
			share = math.LegacyNewDecFromInt(d.Tokens).Quo(math.LegacyNewDecFromInt(total))
		}
		st, err := qs.k.Validators.Get(ctx, d.Delegation.ValidatorAddress)
		eligible, reason := false, "not evaluated"
		if err == nil {
			eligible, reason = st.Eligible, st.Reason
		}
		entries = append(entries, types.DelegationEntry{
			OperatorAddress: d.Delegation.ValidatorAddress, Tokens: d.Tokens, Share: share, Eligible: eligible, Reason: reason,
		})
	}
	bonded, err := qs.k.stakingKeeper.TotalBondedTokens(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryDelegationsResponse{
		Delegations:  entries,
		Total:        total,
		ValidatorCap: capPerValidator(params, total),
		ModuleCap:    params.MaxShare.MulInt(bonded).TruncateInt(),
	}, nil
}

// UnstakeQueue implements types.QueryServer.
func (qs queryServer) UnstakeQueue(goCtx context.Context, req *types.QueryUnstakeQueueRequest) (*types.QueryUnstakeQueueResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	var out []types.UnstakeRequest
	queued, owed := math.ZeroInt(), math.ZeroInt()
	collect := func(r types.UnstakeRequest) {
		out = append(out, r)
		if r.Undelegated {
			owed = owed.Add(r.Amount)
		} else {
			queued = queued.Add(r.Amount)
		}
	}
	if req.Address != "" {
		if _, err := sdk.AccAddressFromBech32(req.Address); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid address: %v", err)
		}
		rng := collections.NewPrefixedPairRange[string, uint64](req.Address)
		if err := qs.k.RequestsByAddr.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
			r, err := qs.k.Requests.Get(ctx, key.K2())
			if err != nil {
				return true, err
			}
			collect(r)
			return false, nil
		}); err != nil {
			return nil, err
		}
	} else if err := qs.k.Requests.Walk(ctx, nil, func(_ uint64, r types.UnstakeRequest) (bool, error) {
		collect(r)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if out == nil {
		out = []types.UnstakeRequest{}
	}
	return &types.QueryUnstakeQueueResponse{Requests: out, TotalQueued: queued, TotalOwed: owed}, nil
}

// Epoch implements types.QueryServer.
func (qs queryServer) Epoch(goCtx context.Context, _ *types.QueryEpochRequest) (*types.QueryEpochResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	e, err := qs.k.GetEpoch(ctx)
	if err != nil {
		return nil, err
	}
	p, err := qs.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryEpochResponse{Epoch: e, NextEpochHeight: e.StartHeight + p.EpochBlocks, CurrentHeight: ctx.BlockHeight()}, nil
}
