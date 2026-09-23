package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultParams returns the initial parameters of spec Annex B.
func DefaultParams() Params {
	return Params{
		SessionTtlSeconds:      30 * 24 * 3600,                                 // 30 days
		MaxSessions:            3,                                              // param_remote_max_sessions
		MsgFee:                 sdk.NewCoin("uusd", math.NewInt(100_000)),      // ~$0.10 per message
		MaxMsgsPerPayload:      5,                                              // max in code 10
		PaymasterDailyCap:      sdk.NewCoin("uluna", math.NewInt(200_000_000)), // 200 LUNC of gas per period (~17 orders at 400k gas)
		PaymasterMinCollateral: math.NewInt(10_000_000),                        // 10 USD
		PaymasterPeriodSeconds: 24 * 3600,
		WithdrawFeeCap:         sdk.NewCoin("uluna", math.NewInt(200_000_000)), // interchain gas of one withdrawal
		BeaconFeeCap:           sdk.NewCoin("uluna", math.NewInt(200_000_000)), // interchain gas of one beacon
		MaxBeaconsPerBlock:     4,
	}
}

// Validate validates the parameters.
func (p Params) Validate() error {
	if p.SessionTtlSeconds < 1 {
		return fmt.Errorf("session_ttl_seconds must be positive")
	}
	if p.MaxSessions == 0 || p.MaxSessions > 10 {
		return fmt.Errorf("max_sessions must be within [1, 10]")
	}
	if err := p.MsgFee.Validate(); err != nil {
		return fmt.Errorf("msg_fee: %w", err)
	}
	if p.MaxMsgsPerPayload == 0 || p.MaxMsgsPerPayload > MaxMsgsPerPayloadAbsolute {
		return fmt.Errorf("max_msgs_per_payload must be within [1, %d]", MaxMsgsPerPayloadAbsolute)
	}
	if err := p.PaymasterDailyCap.Validate(); err != nil {
		return fmt.Errorf("paymaster_daily_cap: %w", err)
	}
	if p.PaymasterMinCollateral.IsNil() || p.PaymasterMinCollateral.IsNegative() {
		return fmt.Errorf("paymaster_min_collateral must be non-negative")
	}
	if p.PaymasterPeriodSeconds < 1 {
		return fmt.Errorf("paymaster_period_seconds must be positive")
	}
	if err := p.WithdrawFeeCap.Validate(); err != nil {
		return fmt.Errorf("withdraw_fee_cap: %w", err)
	}
	if err := p.BeaconFeeCap.Validate(); err != nil {
		return fmt.Errorf("beacon_fee_cap: %w", err)
	}
	if p.MaxBeaconsPerBlock == 0 || p.MaxBeaconsPerBlock > 50 {
		return fmt.Errorf("max_beacons_per_block must be within [1, 50]")
	}
	return nil
}
