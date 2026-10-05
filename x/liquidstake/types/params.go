package types

import (
	"fmt"
	"time"

	"cosmossdk.io/math"
)

// DefaultEpochBlocks is ~3.5 days at 6 s blocks: above the spec minimum
// UnbondingTime / MaxEntries = 21 d / 7 = 3 d (spec §24.4).
const DefaultEpochBlocks = 50_400

// DefaultExpectedBlockTime is the mainnet block time assumed by the epoch
// sizing rule.
const DefaultExpectedBlockTime = 6 * time.Second

// Bounds of Params.ExpectedBlockTime (spec §26.3: non-governable limits are
// constants). They keep governance from defeating the epoch sizing rule
// with an unrealistic block time; the runtime time floor of the epoch
// (MinEpochDuration) holds whatever the parameter says.
const (
	MinExpectedBlockTime = 100 * time.Millisecond
	MaxExpectedBlockTime = 60 * time.Second
)

// DefaultParams returns the initial parameters of spec Annex B.
func DefaultParams() Params {
	return Params{
		EpochBlocks:   DefaultEpochBlocks,
		FeeRate:       math.LegacyNewDecWithPrec(5, 2),  // 5% of rewards
		BurnShare:     math.LegacyNewDecWithPrec(20, 2), // 20% of the fee burned directly
		ValidatorCap:  math.LegacyNewDecWithPrec(10, 2), // 10% of the module total per validator
		MaxShare:      math.LegacyNewDecWithPrec(20, 2), // 20% of the chain's bonded stake
		BufferRatio:   math.LegacyNewDecWithPrec(2, 2),  // 2% kept undelegated
		MaxCommission: math.LegacyNewDecWithPrec(20, 2), // validators above 20% commission are excluded
		MinUptime:     math.LegacyNewDecWithPrec(90, 2), // ≥ 90% signed in the slashing window
		MaxValidators: 100,

		ExpectedBlockTime: DefaultExpectedBlockTime,
	}
}

// Validate validates the parameters.
func (p Params) Validate() error {
	if p.EpochBlocks <= 0 {
		return fmt.Errorf("epoch_blocks must be positive")
	}
	if p.ExpectedBlockTime < MinExpectedBlockTime || p.ExpectedBlockTime > MaxExpectedBlockTime {
		return fmt.Errorf("expected_block_time %s must be within [%s, %s]", p.ExpectedBlockTime, MinExpectedBlockTime, MaxExpectedBlockTime)
	}
	for name, d := range map[string]math.LegacyDec{
		"fee_rate":       p.FeeRate,
		"burn_share":     p.BurnShare,
		"validator_cap":  p.ValidatorCap,
		"max_share":      p.MaxShare,
		"buffer_ratio":   p.BufferRatio,
		"max_commission": p.MaxCommission,
		"min_uptime":     p.MinUptime,
	} {
		if d.IsNil() || d.IsNegative() || d.GT(math.LegacyOneDec()) {
			return fmt.Errorf("%s must be within [0, 1]", name)
		}
	}
	if p.ValidatorCap.IsZero() {
		return fmt.Errorf("validator_cap must be positive")
	}
	if p.MaxValidators == 0 || p.MaxValidators > MaxValidatorsAbsolute {
		return fmt.Errorf("max_validators must be within [1, %d]", MaxValidatorsAbsolute)
	}
	// with a per-validator cap c, at least ceil(1/c) validators are needed to
	// place the whole module total; max_validators must allow that
	minValidators := math.LegacyOneDec().Quo(p.ValidatorCap).Ceil().TruncateInt64()
	if int64(p.MaxValidators) < minValidators {
		return fmt.Errorf("max_validators (%d) is below 1/validator_cap (%d)", p.MaxValidators, minValidators)
	}
	return nil
}

// MinEpochDuration returns UnbondingTime / MaxEntries rounded up: the
// shortest epoch with which one undelegation batch per epoch never needs
// more than MaxEntries simultaneous unbonding entries per validator
// (spec §24.4). With maxEntries 0 the staking module allows no entry at
// all and the whole unbonding time is returned.
func MinEpochDuration(unbondingTime time.Duration, maxEntries uint32) time.Duration {
	if maxEntries == 0 {
		return unbondingTime
	}
	n := time.Duration(maxEntries)
	return (unbondingTime + n - 1) / n
}

// ValidateEpochSizing checks the sizing rule of spec §24.4 against the
// x/staking params: epoch_blocks × expected_block_time × MaxEntries ≥
// UnbondingTime (the product form avoids rounding the division).
func (p Params) ValidateEpochSizing(unbondingTime time.Duration, maxEntries uint32) error {
	if maxEntries == 0 {
		return fmt.Errorf("x/staking max_entries is 0: no undelegation is possible")
	}
	epoch := math.NewInt(p.EpochBlocks).Mul(math.NewInt(int64(p.ExpectedBlockTime)))
	if epoch.MulRaw(int64(maxEntries)).LT(math.NewInt(int64(unbondingTime))) {
		return fmt.Errorf(
			"epoch_blocks %d × expected_block_time %s = %s is below unbonding_time %s / max_entries %d = %s (spec §24.4)",
			p.EpochBlocks, p.ExpectedBlockTime, time.Duration(epoch.Int64()), unbondingTime, maxEntries, MinEpochDuration(unbondingTime, maxEntries))
	}
	return nil
}
