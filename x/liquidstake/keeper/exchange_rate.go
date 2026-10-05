package keeper

import (
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// Totals is the asset breakdown of the module (spec §24.1).
type Totals struct {
	Delegated      math.Int // tokens behind the module's delegation shares
	Unbonding      math.Int // tokens in unbonding entries of the module
	Balance        math.Int // uluna held by the module account
	PendingRewards math.Int // uluna rewards accrued and not yet withdrawn (gross)
	Owed           math.Int // uluna committed to unfilled requests (excluded)
	StSupply       math.Int
	// OwedQueued is the part of Owed not undelegated yet (still delegated).
	OwedQueued math.Int
	// OwedUnbonding is the part of Owed still in x/staking unbonding.
	OwedUnbonding math.Int
	// FeeRate is params.FeeRate: the share of the pending rewards that leaves
	// at the epoch and therefore never backs stLUNC.
	FeeRate math.LegacyDec
}

// PendingFee returns the fee the epoch will take from the pending rewards,
// computed exactly as ProcessEpoch does (fee_rate × rewards, truncated).
func (t Totals) PendingFee() math.Int {
	if t.FeeRate.IsNil() || t.PendingRewards.IsNil() {
		return math.ZeroInt()
	}
	return t.FeeRate.MulInt(t.PendingRewards).TruncateInt()
}

// PendingRewardsNet returns the pending rewards net of the epoch fee: the
// part that will join the assets. Pricing the gross amount made the rate
// fall at every epoch when the fee left (spec §24.1: the rate only falls
// by slashing).
func (t Totals) PendingRewardsNet() math.Int {
	return t.PendingRewards.Sub(t.PendingFee())
}

// GrossAssets returns every uluna the module controls before the
// liabilities: delegated + unbonding + balance + net pending rewards.
func (t Totals) GrossAssets() math.Int {
	return t.Delegated.Add(t.Unbonding).Add(t.Balance).Add(t.PendingRewardsNet())
}

// Assets returns the uluna backing the stLUNC supply (gross assets − owed).
// It is clamped at zero; CheckInvariants reports gross assets below owed.
func (t Totals) Assets() math.Int {
	a := t.GrossAssets().Sub(t.Owed)
	if a.IsNegative() {
		return math.ZeroInt()
	}
	return a
}

// OwedLiquid returns the part of Owed that the module balance must cover:
// matured requests whose uluna is back from x/staking. Queued requests are
// backed by delegations and unbonding ones by unbonding entries.
func (t Totals) OwedLiquid() math.Int {
	l := t.Owed.Sub(t.OwedQueued).Sub(t.OwedUnbonding)
	if l.IsNegative() {
		return math.ZeroInt()
	}
	return l
}

// Buffer returns the uluna available for instant redemptions and for the
// epoch delegation: the module balance minus what matured requests are
// owed. Uluna still in x/staking unbonding is not subtracted from the
// balance (it is not in it), so a queue in flight no longer blocks the
// buffer (spec §24.4).
func (t Totals) Buffer() math.Int {
	b := t.Balance.Sub(t.OwedLiquid())
	if b.IsNegative() {
		return math.ZeroInt()
	}
	return b
}

// delegation is a module delegation with its validator resolved.
type delegation struct {
	Validator  stakingtypes.Validator
	Delegation stakingtypes.Delegation
	Tokens     math.Int
}

// delegations lists the module's delegations (bounded by MaxValidatorsAbsolute).
func (k Keeper) delegations(ctx sdk.Context) ([]delegation, error) {
	dels, err := k.stakingKeeper.GetDelegatorDelegations(ctx, k.ModuleAddress(), types.MaxValidatorsAbsolute)
	if err != nil {
		return nil, err
	}
	out := make([]delegation, 0, len(dels))
	for _, d := range dels {
		valAddr, err := sdk.ValAddressFromBech32(d.ValidatorAddress)
		if err != nil {
			return nil, err
		}
		val, err := k.stakingKeeper.GetValidator(ctx, valAddr)
		if err != nil {
			return nil, err
		}
		out = append(out, delegation{
			Validator:  val,
			Delegation: d,
			Tokens:     val.TokensFromShares(d.Shares).TruncateInt(),
		})
	}
	return out, nil
}

// GetTotals computes the asset breakdown. Pending rewards are computed with
// the distribution keeper, which increments validator periods; that is the
// same side effect a rewards query has and is bounded by the number of
// delegations.
func (k Keeper) GetTotals(ctx sdk.Context) (Totals, error) {
	denom, err := k.bondDenom(ctx)
	if err != nil {
		return Totals{}, err
	}
	t := Totals{
		Delegated:      math.ZeroInt(),
		Unbonding:      math.ZeroInt(),
		PendingRewards: math.ZeroInt(),
	}

	dels, err := k.delegations(ctx)
	if err != nil {
		return Totals{}, err
	}
	for _, d := range dels {
		t.Delegated = t.Delegated.Add(d.Tokens)
		endingPeriod, err := k.distrKeeper.IncrementValidatorPeriod(ctx, d.Validator)
		if err != nil {
			return Totals{}, err
		}
		rewards, err := k.distrKeeper.CalculateDelegationRewards(ctx, d.Validator, d.Delegation, endingPeriod)
		if err != nil {
			return Totals{}, err
		}
		t.PendingRewards = t.PendingRewards.Add(rewards.AmountOf(denom).TruncateInt())
	}

	ubds, err := k.stakingKeeper.GetUnbondingDelegations(ctx, k.ModuleAddress(), types.MaxValidatorsAbsolute)
	if err != nil {
		return Totals{}, err
	}
	for _, u := range ubds {
		for _, e := range u.Entries {
			t.Unbonding = t.Unbonding.Add(e.Balance)
		}
	}

	t.Balance = k.bankKeeper.GetBalance(ctx, k.ModuleAddress(), denom).Amount
	t.Owed, err = k.GetOwed(ctx)
	if err != nil {
		return Totals{}, err
	}
	t.OwedQueued, err = k.GetOwedQueued(ctx)
	if err != nil {
		return Totals{}, err
	}
	t.OwedUnbonding, err = k.GetOwedUnbonding(ctx)
	if err != nil {
		return Totals{}, err
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return Totals{}, err
	}
	t.FeeRate = params.FeeRate
	t.StSupply = k.bankKeeper.GetSupply(ctx, types.StDenom).Amount
	return t, nil
}

// ExchangeRate returns uluna per stLUNC. With no supply the rate is 1.
func (k Keeper) ExchangeRate(ctx sdk.Context) (math.LegacyDec, Totals, error) {
	t, err := k.GetTotals(ctx)
	if err != nil {
		return math.LegacyDec{}, Totals{}, err
	}
	if t.StSupply.IsZero() {
		return math.LegacyOneDec(), t, nil
	}
	return math.LegacyNewDecFromInt(t.Assets()).Quo(math.LegacyNewDecFromInt(t.StSupply)), t, nil
}

// RewardBuffer returns the module balances in denoms other than the bond
// denom and stLUNC: rewards paid in other denoms (spec §24.8, D-23) that
// accumulate until the internal conversion venue exists.
func (k Keeper) RewardBuffer(ctx sdk.Context) (sdk.Coins, error) {
	denom, err := k.bondDenom(ctx)
	if err != nil {
		return nil, err
	}
	var out sdk.Coins
	for _, c := range k.bankKeeper.GetAllBalances(ctx, k.ModuleAddress()) {
		if c.Denom == denom || c.Denom == types.StDenom {
			continue
		}
		out = out.Add(c)
	}
	return out, nil
}
