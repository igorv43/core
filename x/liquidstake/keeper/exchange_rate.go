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
	PendingRewards math.Int // uluna rewards accrued and not yet withdrawn
	Owed           math.Int // uluna committed to unfilled requests (excluded)
	StSupply       math.Int
}

// Assets returns the uluna backing the stLUNC supply.
func (t Totals) Assets() math.Int {
	a := t.Delegated.Add(t.Unbonding).Add(t.Balance).Add(t.PendingRewards).Sub(t.Owed)
	if a.IsNegative() {
		return math.ZeroInt()
	}
	return a
}

// Buffer returns the uluna available for instant redemptions and delegation.
func (t Totals) Buffer() math.Int {
	b := t.Balance.Sub(t.Owed)
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
