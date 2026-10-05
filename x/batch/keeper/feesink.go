package keeper

import (
	"context"

	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// DistributionKeeper is the subset of x/distribution used by the default sink.
type DistributionKeeper interface {
	FundCommunityPool(ctx context.Context, amount sdk.Coins, sender sdk.AccAddress) error
}

// CommunityPoolSink sends protocol fees and slashes to the community pool.
// It is only the fallback of a keeper built without x/perp: the app replaces
// it with the x/perp sink, which routes them into the allocation cascade of
// spec §23.1.
type CommunityPoolSink struct {
	distr DistributionKeeper
}

var _ types.FeeSink = CommunityPoolSink{}

// NewCommunityPoolSink creates the default fee sink.
func NewCommunityPoolSink(distr DistributionKeeper) CommunityPoolSink {
	return CommunityPoolSink{distr: distr}
}

// DepositInsurance implements types.FeeSink; without x/perp there is no
// insurance fund, so slashes fall back to the community pool too.
func (s CommunityPoolSink) DepositInsurance(ctx sdk.Context, fromModule string, coins sdk.Coins) error {
	return s.Deposit(ctx, fromModule, coins)
}

// Deposit implements types.FeeSink.
func (s CommunityPoolSink) Deposit(ctx sdk.Context, fromModule string, coins sdk.Coins) error {
	if coins.IsZero() {
		return nil
	}
	return s.distr.FundCommunityPool(ctx, coins, authtypes.NewModuleAddress(fromModule))
}
