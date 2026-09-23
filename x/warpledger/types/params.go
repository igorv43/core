package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// DefaultParams returns the default module parameters: no default cap, i.e.
// every (token, domain) pair is blocked until governance sets a cap.
func DefaultParams() Params {
	return Params{
		DefaultDomainCap:   math.ZeroInt(),
		BondedCapThreshold: math.ZeroInt(),
		SettleSourceCap:    math.LegacyNewDecWithPrec(40, 2), // 40% per origin (spec §11.4 item 4)
	}
}

// Validate validates the module parameters.
func (p Params) Validate() error {
	if p.DefaultDomainCap.IsNil() {
		return fmt.Errorf("default_domain_cap must be set")
	}
	if p.DefaultDomainCap.IsNegative() {
		return fmt.Errorf("default_domain_cap must not be negative: %s", p.DefaultDomainCap)
	}
	if !p.BondedCapThreshold.IsNil() && p.BondedCapThreshold.IsNegative() {
		return fmt.Errorf("bonded_cap_threshold must not be negative: %s", p.BondedCapThreshold)
	}
	if p.SettleSourceCap.IsNil() || p.SettleSourceCap.IsNegative() || p.SettleSourceCap.GT(math.LegacyOneDec()) {
		return fmt.Errorf("settle_source_cap must be a fraction in [0, 1]: %s", p.SettleSourceCap)
	}
	return nil
}
