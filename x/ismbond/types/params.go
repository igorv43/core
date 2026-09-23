package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultParams returns the initial parameters (spec Annex B: bond minimum
// and fee share "to be defined with the operators"; the window must exceed
// the finality of the slowest domain).
func DefaultParams() Params {
	return Params{
		BondMin:             sdk.NewCoin("uluna", math.NewInt(1_000_000_000_000)), // 1,000,000 LUNC
		UnbondDelayBlocks:   100_800,                                              // ~7 days at 6 s
		DisputeWindowBlocks: 50_400,                                               // ~3.5 days at 6 s
		EvidenceReward:      math.LegacyNewDecWithPrec(20, 2),                     // 20%
	}
}

// Validate validates the parameters.
func (p Params) Validate() error {
	if err := p.BondMin.Validate(); err != nil || !p.BondMin.IsPositive() {
		return fmt.Errorf("bond_min must be a positive coin")
	}
	if p.DisputeWindowBlocks < 1 {
		return fmt.Errorf("dispute_window_blocks must be positive")
	}
	if p.UnbondDelayBlocks < p.DisputeWindowBlocks {
		return fmt.Errorf("unbond_delay_blocks must be at least the dispute window")
	}
	if p.EvidenceReward.IsNil() || p.EvidenceReward.IsNegative() || p.EvidenceReward.GT(math.LegacyOneDec()) {
		return fmt.Errorf("evidence_reward must be within [0, 1]")
	}
	return nil
}
