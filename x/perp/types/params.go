package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultSettlementDenom is the settlement denom of the initial parameters;
// governance sets the bridged settlement denom (spec §11).
const DefaultSettlementDenom = "uusd"

// DefaultParams returns the initial parameters of spec Annex B.
func DefaultParams() Params {
	return Params{
		SettlementDenom:            DefaultSettlementDenom,
		LiqPenalty:                 math.LegacyNewDecWithPrec(1, 2),  // 1%
		IfUnwindPerBatch:           math.LegacyNewDecWithPrec(5, 2),  // 5% of inventory
		IfMaxInventory:             math.LegacyNewDecWithPrec(10, 2), // 10% of OI_cap
		IfFeeShare:                 math.LegacyNewDecWithPrec(50, 2), // 50%
		Beta:                       math.LegacyNewDec(3),
		PremiumWindow:              60,
		PremiumClamp:               math.LegacyNewDecWithPrec(5, 3), // 0.5%
		FundingInterval:            600,
		FundingRateMax:             math.LegacyNewDecWithPrec(5, 4),  // 0.05%
		DispersionRestricted:       math.LegacyNewDecWithPrec(5, 3),  // 0.5%
		DispersionReduceOnly:       math.LegacyNewDecWithPrec(15, 3), // 1.5%
		PerpFeeBps:                 5,
		MaxOpenPositionsPerAccount: 10,
		MaxTriggersPerAccount:      20,
		TriggerSlippageDefault:     math.LegacyNewDecWithPrec(1, 2),  // 1%
		RiskStep:                   math.LegacyNewDecWithPrec(10, 2), // 10% per vote period
		AllocEpochBlocks:           100_800,                          // ~7 days at 6 s
		OpexCap:                    math.ZeroInt(),
		OpexAccount:                "",
		SplitOp:                    math.LegacyNewDecWithPrec(50, 2),
		SplitCp:                    math.LegacyNewDecWithPrec(20, 2),
		SplitBurn:                  math.LegacyNewDecWithPrec(30, 2),
		BurnBuyCap:                 math.ZeroInt(),
		SpotMarketId:               "",
		MaxLiquidationsPerBlock:    MaxLiquidationsPerBlockDefault,
		MaxTriggersPerBlock:        MaxTriggersPerBlockDefault,
	}
}

func fraction(name string, d math.LegacyDec) error {
	if d.IsNil() || d.IsNegative() || d.GT(math.LegacyOneDec()) {
		return fmt.Errorf("%s must be within [0, 1]", name)
	}
	return nil
}

// Validate validates the parameters against the code limits of spec §26.3.
func (p Params) Validate() error {
	if err := sdk.ValidateDenom(p.SettlementDenom); err != nil {
		return fmt.Errorf("settlement_denom: %w", err)
	}
	for name, d := range map[string]math.LegacyDec{
		"liq_penalty": p.LiqPenalty, "if_unwind_per_batch": p.IfUnwindPerBatch, "if_max_inventory": p.IfMaxInventory,
		"if_fee_share": p.IfFeeShare, "premium_clamp": p.PremiumClamp, "funding_rate_max": p.FundingRateMax,
		"dispersion_restricted": p.DispersionRestricted, "dispersion_reduce_only": p.DispersionReduceOnly,
		"trigger_slippage_default": p.TriggerSlippageDefault, "risk_step": p.RiskStep,
		"split_op": p.SplitOp, "split_cp": p.SplitCp, "split_burn": p.SplitBurn,
	} {
		if err := fraction(name, d); err != nil {
			return err
		}
	}
	if p.Beta.IsNil() || !p.Beta.IsPositive() {
		return fmt.Errorf("beta must be positive")
	}
	if p.PremiumWindow == 0 || p.PremiumWindow > MaxPremiumWindowAbsolute {
		return fmt.Errorf("premium_window must be within [1, %d]", MaxPremiumWindowAbsolute)
	}
	if p.FundingInterval < 1 {
		return fmt.Errorf("funding_interval must be positive")
	}
	if !p.DispersionRestricted.LTE(p.DispersionReduceOnly) {
		return fmt.Errorf("dispersion_restricted (d1) must not exceed dispersion_reduce_only (d2)")
	}
	if p.PerpFeeBps > 1_000 {
		return fmt.Errorf("perp_fee_bps must not exceed 1000")
	}
	if p.MaxOpenPositionsPerAccount == 0 || p.MaxOpenPositionsPerAccount > MaxOpenPositionsAbsolute {
		return fmt.Errorf("max_open_positions_per_account must be within [1, %d]", MaxOpenPositionsAbsolute)
	}
	if p.MaxTriggersPerAccount == 0 || p.MaxTriggersPerAccount > MaxTriggersPerAccountAbsolute {
		return fmt.Errorf("max_triggers_per_account must be within [1, %d]", MaxTriggersPerAccountAbsolute)
	}
	if !p.RiskStep.IsPositive() {
		return fmt.Errorf("risk_step must be positive")
	}
	if p.AllocEpochBlocks < 1 {
		return fmt.Errorf("alloc_epoch_blocks must be positive")
	}
	if p.OpexCap.IsNil() || p.OpexCap.IsNegative() || p.BurnBuyCap.IsNil() || p.BurnBuyCap.IsNegative() {
		return fmt.Errorf("opex_cap and burn_buy_cap must be non-negative")
	}
	if p.OpexAccount != "" {
		if _, err := sdk.AccAddressFromBech32(p.OpexAccount); err != nil {
			return fmt.Errorf("opex_account: %w", err)
		}
	}
	if !p.SplitOp.Add(p.SplitCp).Add(p.SplitBurn).Equal(math.LegacyOneDec()) {
		return fmt.Errorf("split_op + split_cp + split_burn must equal 1")
	}
	if p.MaxLiquidationsPerBlock == 0 || p.MaxTriggersPerBlock == 0 {
		return fmt.Errorf("max_liquidations_per_block and max_triggers_per_block must be positive")
	}
	return nil
}
