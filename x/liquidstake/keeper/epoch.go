package keeper

import (
	"fmt"
	"sort"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// eligibleValidator is a validator that passed the objective whitelist.
type eligibleValidator struct {
	Validator stakingtypes.Validator
	Address   sdk.ValAddress
}

// EpochDue reports whether the epoch boundary was reached at this height.
func (k Keeper) EpochDue(ctx sdk.Context) (bool, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return false, err
	}
	epoch, err := k.GetEpoch(ctx)
	if err != nil {
		return false, err
	}
	if epoch.Number == 0 && epoch.StartHeight == 0 {
		// first block after genesis/upgrade: open epoch 1 without processing
		return false, k.Epoch.Set(ctx, types.Epoch{Number: 1, StartHeight: ctx.BlockHeight(), StartTime: ctx.BlockTime()})
	}
	return ctx.BlockHeight()-epoch.StartHeight >= params.EpochBlocks, nil
}

// Eligibility evaluates the objective whitelist (spec §24.2) over the bonded
// set, bounded by params.MaxValidators, and stores the result for queries.
func (k Keeper) Eligibility(ctx sdk.Context) ([]eligibleValidator, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	bonded, err := k.stakingKeeper.GetBondedValidatorsByPower(ctx)
	if err != nil {
		return nil, err
	}
	window, err := k.slashingKeeper.SignedBlocksWindow(ctx)
	if err != nil {
		return nil, err
	}

	// forget last epoch's verdicts
	if err := k.Validators.Clear(ctx, nil); err != nil {
		return nil, err
	}

	var eligible []eligibleValidator
	for i, val := range bonded {
		if i >= int(params.MaxValidators) {
			break
		}
		valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		if err != nil {
			return nil, err
		}
		reason := k.eligibilityReason(ctx, val, params, window)
		state := types.ValidatorState{OperatorAddress: val.OperatorAddress, Eligible: reason == "", Reason: reason}
		if err := k.Validators.Set(ctx, val.OperatorAddress, state); err != nil {
			return nil, err
		}
		if reason == "" {
			eligible = append(eligible, eligibleValidator{Validator: val, Address: valAddr})
		}
	}
	return eligible, nil
}

// eligibilityReason returns "" when the validator passes every criterion,
// otherwise the failed criterion.
func (k Keeper) eligibilityReason(ctx sdk.Context, val stakingtypes.Validator, params types.Params, window int64) string {
	if !val.IsBonded() {
		return "not bonded"
	}
	if val.IsJailed() {
		return "jailed"
	}
	if val.GetCommission().GT(params.MaxCommission) {
		return fmt.Sprintf("commission %s above max %s", val.GetCommission(), params.MaxCommission)
	}
	consAddr, err := val.GetConsAddr()
	if err != nil {
		return "invalid consensus address"
	}
	if k.slashingKeeper.IsTombstoned(ctx, sdk.ConsAddress(consAddr)) {
		return "tombstoned"
	}
	info, err := k.slashingKeeper.GetValidatorSigningInfo(ctx, sdk.ConsAddress(consAddr))
	if err != nil {
		// no signing info yet (fresh validator): uptime cannot be judged, allow
		return ""
	}
	if window > 0 {
		signed := math.LegacyNewDec(window - info.MissedBlocksCounter).Quo(math.LegacyNewDec(window))
		if signed.LT(params.MinUptime) {
			return fmt.Sprintf("uptime %s below min %s", signed, params.MinUptime)
		}
	}
	return ""
}

// EpochReport summarises what an epoch did.
type EpochReport struct {
	Rewards, Fee, Burned, Delegated, Undelegated, Redelegated math.Int
	Eligible                                                  int
	RateBefore, RateAfter                                     math.LegacyDec
}

// ProcessEpoch runs the epoch pipeline (spec §24.4): rewards and fee,
// redelegation away from ineligible validators, one pro-rata undelegation
// batch for the queue, delegation of the buffer surplus with equal weights
// under the per-validator cap, and the epoch roll-over.
func (k Keeper) ProcessEpoch(ctx sdk.Context) (EpochReport, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return EpochReport{}, err
	}
	denom, err := k.bondDenom(ctx)
	if err != nil {
		return EpochReport{}, err
	}
	rep := EpochReport{Rewards: math.ZeroInt(), Fee: math.ZeroInt(), Burned: math.ZeroInt(), Delegated: math.ZeroInt(), Undelegated: math.ZeroInt(), Redelegated: math.ZeroInt()}
	rep.RateBefore, _, err = k.ExchangeRate(ctx)
	if err != nil {
		return EpochReport{}, err
	}

	eligible, err := k.Eligibility(ctx)
	if err != nil {
		return EpochReport{}, err
	}
	rep.Eligible = len(eligible)
	eligibleSet := make(map[string]struct{}, len(eligible))
	for _, e := range eligible {
		eligibleSet[e.Validator.OperatorAddress] = struct{}{}
	}

	// 1. rewards and fee
	dels, err := k.delegations(ctx)
	if err != nil {
		return EpochReport{}, err
	}
	for _, d := range dels {
		valAddr, _ := sdk.ValAddressFromBech32(d.Delegation.ValidatorAddress)
		coins, err := k.distrKeeper.WithdrawDelegationRewards(ctx, k.ModuleAddress(), valAddr)
		if err != nil {
			return EpochReport{}, err
		}
		rep.Rewards = rep.Rewards.Add(coins.AmountOf(denom))
	}
	if rep.Rewards.IsPositive() {
		rep.Fee = params.FeeRate.MulInt(rep.Rewards).TruncateInt()
		rep.Burned = params.BurnShare.MulInt(rep.Fee).TruncateInt()
		if rep.Burned.IsPositive() {
			if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, k.burnModuleName, sdk.NewCoins(sdk.NewCoin(denom, rep.Burned))); err != nil {
				return EpochReport{}, err
			}
		}
		// the remainder of the fee funds the community pool until the
		// insurance fund / allocation cascade of spec §23.1 exists
		if rest := rep.Fee.Sub(rep.Burned); rest.IsPositive() {
			if err := k.distrKeeper.FundCommunityPool(ctx, sdk.NewCoins(sdk.NewCoin(denom, rest)), k.ModuleAddress()); err != nil {
				return EpochReport{}, err
			}
		}
	}

	// 2. redelegate away from validators that left the whitelist
	for _, d := range dels {
		if _, ok := eligibleSet[d.Delegation.ValidatorAddress]; ok || !d.Tokens.IsPositive() {
			continue
		}
		moved, err := k.redelegateOut(ctx, d, eligible, params)
		if err != nil {
			k.Logger(ctx).Error("redelegation failed", "validator", d.Delegation.ValidatorAddress, "err", err)
			continue
		}
		rep.Redelegated = rep.Redelegated.Add(moved)
		if err := ctx.EventManager().EmitTypedEvent(&types.EventValidatorRemoved{OperatorAddress: d.Delegation.ValidatorAddress, Reason: k.reason(ctx, d.Delegation.ValidatorAddress)}); err != nil {
			return EpochReport{}, err
		}
	}

	// 3. one undelegation batch for the queue, pro-rata over delegations
	epoch, err := k.GetEpoch(ctx)
	if err != nil {
		return EpochReport{}, err
	}
	rep.Undelegated, err = k.undelegateBatch(ctx, epoch.Number)
	if err != nil {
		return EpochReport{}, err
	}

	// 4. delegate the buffer surplus with equal weights under the cap
	rep.Delegated, err = k.delegateSurplus(ctx, eligible, params)
	if err != nil {
		return EpochReport{}, err
	}

	// 5. roll the epoch
	epoch.Number++
	epoch.StartHeight = ctx.BlockHeight()
	epoch.StartTime = ctx.BlockTime()
	if err := k.Epoch.Set(ctx, epoch); err != nil {
		return EpochReport{}, err
	}

	rep.RateAfter, _, err = k.ExchangeRate(ctx)
	if err != nil {
		return EpochReport{}, err
	}
	if rep.RateAfter.LT(rep.RateBefore) {
		// the rate only falls through slashing (or the fee on an epoch whose
		// pending rewards were already priced in); record it either way
		if err := ctx.EventManager().EmitTypedEvent(&types.EventSlashAbsorbed{Epoch: epoch.Number - 1, ExchangeRateBefore: rep.RateBefore.String(), ExchangeRateAfter: rep.RateAfter.String()}); err != nil {
			return EpochReport{}, err
		}
	}
	return rep, ctx.EventManager().EmitTypedEvent(&types.EventEpochProcessed{
		Epoch: epoch.Number - 1, Height: ctx.BlockHeight(),
		Rewards: rep.Rewards.String(), Fee: rep.Fee.String(), Burned: rep.Burned.String(),
		Delegated: rep.Delegated.String(), Undelegated: rep.Undelegated.String(), Redelegated: rep.Redelegated.String(),
		EligibleValidators: uint32(rep.Eligible), ExchangeRate: rep.RateAfter.String(),
	})
}

func (k Keeper) reason(ctx sdk.Context, operator string) string {
	st, err := k.Validators.Get(ctx, operator)
	if err != nil {
		return "not in the evaluated set"
	}
	return st.Reason
}

// capPerValidator returns the maximum tokens one validator may hold for the
// module: validator_cap × total to be placed.
func capPerValidator(params types.Params, total math.Int) math.Int {
	return params.ValidatorCap.MulInt(total).TruncateInt()
}

// room returns how many more tokens each eligible validator can take under
// the cap, given the module's current delegation on it.
func (k Keeper) room(ctx sdk.Context, eligible []eligibleValidator, cap math.Int) ([]math.Int, error) {
	out := make([]math.Int, len(eligible))
	for i, e := range eligible {
		current := math.ZeroInt()
		if d, err := k.stakingKeeper.GetDelegation(ctx, k.ModuleAddress(), e.Address); err == nil {
			current = e.Validator.TokensFromShares(d.Shares).TruncateInt()
		}
		r := cap.Sub(current)
		if r.IsNegative() {
			r = math.ZeroInt()
		}
		out[i] = r
	}
	return out, nil
}

// spread splits amount over targets proportionally to their room, filling the
// smallest delegations first through equal-share rounds. Returns per-target amounts.
func spread(amount math.Int, room []math.Int) []math.Int {
	out := make([]math.Int, len(room))
	for i := range out {
		out[i] = math.ZeroInt()
	}
	remaining := amount
	for remaining.IsPositive() {
		open := 0
		for i := range room {
			if room[i].Sub(out[i]).IsPositive() {
				open++
			}
		}
		if open == 0 {
			break
		}
		share := remaining.QuoRaw(int64(open))
		if share.IsZero() {
			share = math.OneInt()
		}
		progressed := false
		for i := range room {
			free := room[i].Sub(out[i])
			if !free.IsPositive() || !remaining.IsPositive() {
				continue
			}
			take := math.MinInt(share, math.MinInt(free, remaining))
			out[i] = out[i].Add(take)
			remaining = remaining.Sub(take)
			progressed = true
		}
		if !progressed {
			break
		}
	}
	return out
}

// delegateSurplus delegates balance − owed − target buffer over the eligible set.
func (k Keeper) delegateSurplus(ctx sdk.Context, eligible []eligibleValidator, params types.Params) (math.Int, error) {
	totals, err := k.GetTotals(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}
	targetBuffer := params.BufferRatio.MulInt(totals.Assets()).TruncateInt()
	available := totals.Buffer().Sub(targetBuffer)
	if !available.IsPositive() || len(eligible) == 0 {
		return math.ZeroInt(), nil
	}
	cap := capPerValidator(params, totals.Delegated.Add(available))
	room, err := k.room(ctx, eligible, cap)
	if err != nil {
		return math.ZeroInt(), err
	}
	amounts := spread(available, room)

	delegated := math.ZeroInt()
	for i, e := range eligible {
		if !amounts[i].IsPositive() {
			continue
		}
		if _, err := k.stakingKeeper.Delegate(ctx, k.ModuleAddress(), amounts[i], stakingtypes.Unbonded, e.Validator, true); err != nil {
			k.Logger(ctx).Error("delegate failed", "validator", e.Validator.OperatorAddress, "err", err)
			continue
		}
		delegated = delegated.Add(amounts[i])
	}
	return delegated, nil
}

// redelegateOut moves a whole delegation from an ineligible validator to the
// eligible set, respecting the cap.
func (k Keeper) redelegateOut(ctx sdk.Context, d delegation, eligible []eligibleValidator, params types.Params) (math.Int, error) {
	if len(eligible) == 0 {
		return math.ZeroInt(), types.ErrNoEligibleVals
	}
	totals, err := k.GetTotals(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}
	cap := capPerValidator(params, totals.Delegated)
	room, err := k.room(ctx, eligible, cap)
	if err != nil {
		return math.ZeroInt(), err
	}
	amounts := spread(d.Tokens, room)

	moved := math.ZeroInt()
	for i, e := range eligible {
		if !amounts[i].IsPositive() {
			continue
		}
		shares, err := k.stakingKeeper.ValidateUnbondAmount(ctx, k.ModuleAddress(), sdk.ValAddress(mustValAddr(d.Delegation.ValidatorAddress)), amounts[i])
		if err != nil {
			return moved, err
		}
		if _, err := k.stakingKeeper.BeginRedelegation(ctx, k.ModuleAddress(), mustValAddr(d.Delegation.ValidatorAddress), e.Address, shares); err != nil {
			k.Logger(ctx).Error("redelegate failed", "from", d.Delegation.ValidatorAddress, "to", e.Validator.OperatorAddress, "err", err)
			continue
		}
		moved = moved.Add(amounts[i])
	}
	return moved, nil
}

// undelegateBatch undelegates the sum of queued requests up to the given
// epoch, pro-rata over the module's delegations, and marks the requests.
func (k Keeper) undelegateBatch(ctx sdk.Context, epoch uint64) (math.Int, error) {
	var queued []types.UnstakeRequest
	total := math.ZeroInt()
	if err := k.Requests.Walk(ctx, nil, func(_ uint64, r types.UnstakeRequest) (bool, error) {
		if !r.Undelegated && r.Epoch <= epoch {
			queued = append(queued, r)
			total = total.Add(r.Amount)
		}
		return false, nil
	}); err != nil {
		return math.ZeroInt(), err
	}
	if !total.IsPositive() {
		return math.ZeroInt(), nil
	}

	dels, err := k.delegations(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}
	sort.Slice(dels, func(i, j int) bool { return dels[i].Tokens.GT(dels[j].Tokens) })
	delegated := math.ZeroInt()
	for _, d := range dels {
		delegated = delegated.Add(d.Tokens)
	}
	if !delegated.IsPositive() {
		return math.ZeroInt(), nil
	}
	if total.GT(delegated) {
		total = delegated
	}

	undelegated := math.ZeroInt()
	remaining := total
	var completion = ctx.BlockTime()
	for i, d := range dels {
		if !remaining.IsPositive() {
			break
		}
		part := total.Mul(d.Tokens).Quo(delegated)
		if i == len(dels)-1 || part.GT(remaining) {
			part = remaining
		}
		if !part.IsPositive() {
			continue
		}
		valAddr := mustValAddr(d.Delegation.ValidatorAddress)
		if full, err := k.stakingKeeper.HasMaxUnbondingDelegationEntries(ctx, k.ModuleAddress(), valAddr); err != nil || full {
			// entry limit reached (MaxEntries): leave this validator for the next epoch
			continue
		}
		shares, err := k.stakingKeeper.ValidateUnbondAmount(ctx, k.ModuleAddress(), valAddr, part)
		if err != nil {
			k.Logger(ctx).Error("validate unbond failed", "validator", d.Delegation.ValidatorAddress, "err", err)
			continue
		}
		done, amt, err := k.stakingKeeper.Undelegate(ctx, k.ModuleAddress(), valAddr, shares)
		if err != nil {
			k.Logger(ctx).Error("undelegate failed", "validator", d.Delegation.ValidatorAddress, "err", err)
			continue
		}
		if done.After(completion) {
			completion = done
		}
		undelegated = undelegated.Add(amt)
		remaining = remaining.Sub(part)
	}

	// mark requests covered by what was actually undelegated (oldest first)
	sort.Slice(queued, func(i, j int) bool { return queued[i].Id < queued[j].Id })
	covered := undelegated
	for _, r := range queued {
		if covered.LT(r.Amount) {
			break
		}
		r.Undelegated = true
		r.CompletionTime = completion
		if err := k.Requests.Set(ctx, r.Id, r); err != nil {
			return math.ZeroInt(), err
		}
		covered = covered.Sub(r.Amount)
	}
	return undelegated, nil
}

func mustValAddr(s string) sdk.ValAddress {
	v, err := sdk.ValAddressFromBech32(s)
	if err != nil {
		panic(err)
	}
	return v
}
