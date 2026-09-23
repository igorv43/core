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
	return nil
}
