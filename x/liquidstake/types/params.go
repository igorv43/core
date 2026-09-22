package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// DefaultEpochBlocks is ~3.5 days at 6 s blocks: above the spec minimum
// UnbondingTime / MaxEntries = 21 d / 7 = 3 d (spec §24.4).
const DefaultEpochBlocks = 50_400

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
	}
}

// Validate validates the parameters.
func (p Params) Validate() error {
	if p.EpochBlocks <= 0 {
		return fmt.Errorf("epoch_blocks must be positive")
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
