package keeper

import (
	"context"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/classic-terra/core/v4/x/warpledger/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type queryServer struct {
	k Keeper
}

var _ types.QueryServer = queryServer{}

// NewQueryServerImpl returns the x/warpledger QueryServer implementation.
func NewQueryServerImpl(keeper Keeper) types.QueryServer {
	return queryServer{k: keeper}
}

// Params implements types.QueryServer.
func (qs queryServer) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params, err := qs.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryParamsResponse{Params: params}, nil
}

// WarpLedger implements types.QueryServer.
func (qs queryServer) WarpLedger(goCtx context.Context, req *types.QueryWarpLedgerRequest) (*types.QueryWarpLedgerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	tokenId, err := util.DecodeHexAddress(req.TokenId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid token id: %v", err)
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	return qs.k.ledgerResponse(ctx, tokenId)
}

// WarpLedgers implements types.QueryServer.
func (qs queryServer) WarpLedgers(goCtx context.Context, _ *types.QueryWarpLedgersRequest) (*types.QueryWarpLedgersResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	var (
		tokenIds []util.HexAddress
		seen     = make(map[uint64]struct{})
	)
	if err := qs.k.IterateLedgers(ctx, func(ledger types.DomainLedger) (bool, error) {
		id := ledger.TokenId.GetInternalId()
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			tokenIds = append(tokenIds, ledger.TokenId)
		}
		return false, nil
	}); err != nil {
		return nil, err
	}

	ledgers := make([]types.QueryWarpLedgerResponse, 0, len(tokenIds))
	for _, tokenId := range tokenIds {
		resp, err := qs.k.ledgerResponse(ctx, tokenId)
		if err != nil {
			return nil, err
		}
		ledgers = append(ledgers, *resp)
	}
	return &types.QueryWarpLedgersResponse{Ledgers: ledgers}, nil
}

// DepositAddress implements types.QueryServer.
func (qs queryServer) DepositAddress(goCtx context.Context, req *types.QueryDepositAddressRequest) (*types.QueryDepositAddressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	tokenId, err := util.DecodeHexAddress(req.TokenId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid token id: %v", err)
	}
	recipient, err := util.DecodeHexAddress(req.Recipient)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid recipient: %v", err)
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	token, err := qs.k.GetToken(ctx, tokenId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	addr := types.DeriveDepositAddress(tokenId, req.Domain, recipient)
	balance := qs.k.bankKeeper.GetBalance(ctx, addr, token.OriginDenom).Amount

	return &types.QueryDepositAddressResponse{
		Address: addr.String(),
		Denom:   token.OriginDenom,
		Balance: balance,
	}, nil
}

// ledgerResponse assembles the solvency view of a token (spec §9.5 query).
func (k Keeper) ledgerResponse(ctx sdk.Context, tokenId util.HexAddress) (*types.QueryWarpLedgerResponse, error) {
	token, err := k.GetToken(ctx, tokenId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	ledgers, err := k.LedgersOfToken(ctx, tokenId)
	if err != nil {
		return nil, err
	}

	basket, err := k.IsBasket(ctx, tokenId)
	if err != nil {
		return nil, err
	}
	circulation := math.ZeroInt()
	for _, ledger := range ledgers {
		circulation = circulation.Add(Collateral(ledger))
	}
	sum := math.ZeroInt()
	domains := make([]types.DomainExposure, 0, len(ledgers))
	for _, ledger := range ledgers {
		cap, err := k.EffectiveCap(ctx, ledger)
		if err != nil {
			return nil, err
		}
		exposure := Exposure(ledger)
		sum = sum.Add(exposure)
		collateral := Collateral(ledger)
		share, shareCap, headroom := math.LegacyZeroDec(), math.LegacyZeroDec(), math.ZeroInt()
		if basket {
			if shareCap, err = k.EffectiveShareCap(ctx, ledger); err != nil {
				return nil, err
			}
			if circulation.IsPositive() {
				share = math.LegacyNewDecFromInt(collateral).QuoInt(circulation)
			}
			headroom = Headroom(collateral, circulation, shareCap)
		}
		domains = append(domains, types.DomainExposure{
			Domain:     ledger.Domain,
			Sent:       ledger.Sent,
			Received:   ledger.Received,
			Exposure:   exposure,
			Cap:        cap,
			HasCap:     ledger.HasCap,
			Collateral: collateral,
			Share:      share,
			ShareCap:   shareCap,
			Headroom:   headroom,
			Paused:     ledger.Paused,
		})
	}

	warpAddr := authtypes.NewModuleAddress(warptypes.ModuleName)
	bankBalance := k.bankKeeper.GetBalance(ctx, warpAddr, token.OriginDenom).Amount

	return &types.QueryWarpLedgerResponse{
		TokenId:           tokenId.String(),
		Denom:             token.OriginDenom,
		TokenType:         token.TokenType.String(),
		CollateralBalance: token.CollateralBalance,
		BankBalance:       bankBalance,
		SumExposure:       sum,
		Domains:           domains,
		Height:            ctx.BlockHeight(),
		Basket:            basket,
		Circulation:       circulation,
	}, nil
}
