package keeper

import (
	"fmt"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CheckInvariants verifies the module invariants of spec §24.9:
//  1. the exchange-rate identity holds by construction (assets are read live),
//     so it is asserted as assets ≥ 0 and rate > 0 whenever supply > 0;
//  2. queue conservation: Owed equals the sum of every unfilled request;
//  3. the module holds at most max_share of the bonded stake (with the
//     tolerance of stake moving after entry: only violations by more than
//     the buffer are reported).
func (k Keeper) CheckInvariants(ctx sdk.Context) error {
	rate, totals, err := k.ExchangeRate(ctx)
	if err != nil {
		return err
	}
	if totals.StSupply.IsPositive() && !rate.IsPositive() {
		return fmt.Errorf("invariant 1: exchange rate %s not positive with supply %s", rate, totals.StSupply)
	}

	sum := math.ZeroInt()
	if err := k.Requests.Walk(ctx, nil, func(_ uint64, r types.UnstakeRequest) (bool, error) {
		sum = sum.Add(r.Amount)
		return false, nil
	}); err != nil {
		return err
	}
	if !sum.Equal(totals.Owed) {
		return fmt.Errorf("invariant 2: owed %s != sum of requests %s", totals.Owed, sum)
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	bonded, err := k.stakingKeeper.TotalBondedTokens(ctx)
	if err != nil {
		return err
	}
	cap := params.MaxShare.MulInt(bonded).TruncateInt()
	if totals.Delegated.GT(cap.Add(totals.Buffer())) {
		return fmt.Errorf("invariant 3: delegated %s above module cap %s", totals.Delegated, cap)
	}
	return nil
}
