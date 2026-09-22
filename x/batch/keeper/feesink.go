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
// It is the default until the insurance fund of x/perp exists (spec §23).
type CommunityPoolSink struct {
	distr DistributionKeeper
}

var _ types.FeeSink = CommunityPoolSink{}

// NewCommunityPoolSink creates the default fee sink.
func NewCommunityPoolSink(distr DistributionKeeper) CommunityPoolSink {
	return CommunityPoolSink{distr: distr}
}

// Deposit implements types.FeeSink.
func (s CommunityPoolSink) Deposit(ctx sdk.Context, fromModule string, coins sdk.Coins) error {
	if coins.IsZero() {
		return nil
	}
	return s.distr.FundCommunityPool(ctx, coins, authtypes.NewModuleAddress(fromModule))
}
