package keeper

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InvariantError reports which invariant of spec §24.9 is broken.
type InvariantError struct {
	Invariant uint32
	Detail    string
}

func (e *InvariantError) Error() string {
	return fmt.Sprintf("invariant %d: %s", e.Invariant, e.Detail)
}

func broken(n uint32, format string, args ...any) error {
	return &InvariantError{Invariant: n, Detail: fmt.Sprintf(format, args...)}
}

// CheckInvariants verifies the module invariants of spec §24.9:
//
//  1. solvency and the rate identity: the gross assets (delegated +
//     unbonding + balance + pending rewards net of the fee) cover the owed
//     uluna — Assets() clamps at zero and would otherwise hide the loss —
//     and exchange_rate · supply reproduces gross assets − owed within the
//     rounding of one uluna;
//  2. queue conservation: owed equals the sum of every unfilled request,
//     owed_queued the sum of the requests not undelegated yet and
//     owed_unbonding the sum of the undelegated requests that have not
//     matured, so every accepted MsgUnstake is in exactly one bucket (paid
//     requests are removed);
//  3. the module holds at most max_share of the bonded stake (with the
//     tolerance of stake moving after entry: only violations by more than
//     the buffer are reported).
//
// It walks every request, so it runs once per epoch (spec §25.1 item 6),
// like the undelegation batch that already walks the queue.
func (k Keeper) CheckInvariants(ctx sdk.Context) error {
	rate, totals, err := k.ExchangeRate(ctx)
	if err != nil {
		return err
	}
	gross := totals.GrossAssets()
	if gross.LT(totals.Owed) {
		return broken(1, "gross assets %s below owed %s", gross, totals.Owed)
	}
	if totals.StSupply.IsPositive() {
		implied := rate.MulInt(totals.StSupply)
		if implied.Sub(math.LegacyNewDecFromInt(gross.Sub(totals.Owed))).Abs().GT(math.LegacyOneDec()) {
			return broken(1, "exchange rate %s × supply %s = %s differs from assets %s", rate, totals.StSupply, implied, gross.Sub(totals.Owed))
		}
	}

	sum, queued, unbonding := math.ZeroInt(), math.ZeroInt(), math.ZeroInt()
	now := ctx.BlockTime()
	if err := k.Requests.Walk(ctx, nil, func(_ uint64, r types.UnstakeRequest) (bool, error) {
		sum = sum.Add(r.Amount)
		switch {
		case !r.Undelegated:
			queued = queued.Add(r.Amount)
		case r.CompletionTime.After(now):
			unbonding = unbonding.Add(r.Amount)
		}
		return false, nil
	}); err != nil {
		return err
	}
	if !sum.Equal(totals.Owed) {
		return broken(2, "owed %s != sum of requests %s", totals.Owed, sum)
	}
	if !queued.Equal(totals.OwedQueued) {
		return broken(2, "owed_queued %s != sum of queued requests %s", totals.OwedQueued, queued)
	}
	if !unbonding.Equal(totals.OwedUnbonding) {
		return broken(2, "owed_unbonding %s != sum of unbonding requests %s", totals.OwedUnbonding, unbonding)
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
		return broken(3, "delegated %s above module cap %s", totals.Delegated, cap)
	}
	return nil
}

// assertInvariants runs CheckInvariants at the end of an epoch and reports
// a violation with EventInvariantBroken and an error log. Like the warp
// ledger invariant (x/warpledger EndBlocker) it never halts the chain.
func (k Keeper) assertInvariants(ctx sdk.Context, epoch uint64) error {
	err := k.CheckInvariants(ctx)
	if err == nil {
		return nil
	}
	var inv *InvariantError
	if !errors.As(err, &inv) {
		// the check itself failed (store read): report it as unverifiable
		inv = &InvariantError{Invariant: 0, Detail: "invariants could not be evaluated: " + err.Error()}
	}
	k.Logger(ctx).Error("liquidstake invariant broken", "invariant", inv.Invariant, "detail", inv.Detail, "epoch", epoch)
	return ctx.EventManager().EmitTypedEvent(&types.EventInvariantBroken{
		Epoch: epoch, Height: ctx.BlockHeight(), Invariant: inv.Invariant, Detail: inv.Detail,
	})
}
