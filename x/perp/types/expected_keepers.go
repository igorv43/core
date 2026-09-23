package types

import (
	"context"

	"cosmossdk.io/math"
	batchkeeper "github.com/classic-terra/core/v4/x/batch/keeper"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	oracletypes "github.com/classic-terra/core/v4/x/oracle/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper is the subset of x/bank used by the module.
type BankKeeper interface {
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
}

// OracleKeeper is the subset of x/oracle used for P_ref and the oracle state.
type OracleKeeper interface {
	// GetPrice returns the USD price of an asset target or the LUNC rate in a denom.
	GetPrice(ctx sdk.Context, name string) (math.LegacyDec, error)
	// GetRateSample returns the latest sample (dispersion, depth, vote period).
	GetRateSample(ctx sdk.Context, name string) (oracletypes.RateSample, error)
	// RateHistory returns up to `periods` samples, newest first.
	RateHistory(ctx sdk.Context, name string, periods uint64) []oracletypes.RateSample
	VotePeriod(ctx sdk.Context) uint64
}

// BatchKeeper is the subset of x/batch used by the module: market
// registration and the two ways to enter the auction.
type BatchKeeper interface {
	GetMarket(ctx sdk.Context, id string) (batchtypes.Market, error)
	CreateMarket(ctx sdk.Context, m batchtypes.Market) error
	SetMarketEnabled(ctx sdk.Context, id string, enabled bool) error
	SubmitPerpIntent(ctx sdk.Context, o batchkeeper.PerpOrder) (uint64, uint64, error)
	SubmitIntentInternal(ctx sdk.Context, msg *batchtypes.MsgSubmitIntent) (uint64, uint64, error)
	GetIntent(ctx sdk.Context, id uint64) (batchtypes.Intent, error)
	HasOpenIntent(ctx sdk.Context, account, marketID string) (bool, error)
	GetParams(ctx sdk.Context) (batchtypes.Params, error)
}

// DistributionKeeper funds the community pool (allocation cascade).
type DistributionKeeper interface {
	FundCommunityPool(ctx context.Context, amount sdk.Coins, sender sdk.AccAddress) error
}
