package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultParams returns the initial parameters of spec Annex B.
func DefaultParams() Params {
	return Params{
		SessionTtlSeconds:       30 * 24 * 3600,                                 // 30 days
		MaxSessions:             3,                                              // param_remote_max_sessions
		MsgFee:                  sdk.NewCoin("uluna", math.NewInt(10_000_000)),  // param_remote_msg_fee: ~2x a simple tx at 28.325 uluna/gas, in the paymaster's denom
		MaxMsgsPerPayload:       5,                                              // max in code 10
		PaymasterDailyCap:       sdk.NewCoin("uluna", math.NewInt(200_000_000)), // 200 LUNC of gas per period (~17 orders at 400k gas)
		PaymasterMinCollateral:  math.NewInt(10_000_000),                        // 10 USD
		PaymasterPeriodSeconds:  24 * 3600,
		WithdrawFeeCap:          sdk.NewCoin("uluna", math.NewInt(200_000_000)), // interchain gas of one withdrawal
		BeaconFeeCap:            sdk.NewCoin("uluna", math.NewInt(200_000_000)), // interchain gas of one beacon
		MaxBeaconsPerBlock:      4,
		RebalanceEpochBlocks:    600,                                            // ~1 h at 6 s (param_rebalance_epoch)
		DynamicFeeBandBps:       50,                                             // up to 0.5 % (param_dynamic_fee_band)
		ControlFeeCap:           sdk.NewCoin("uluna", math.NewInt(200_000_000)), // interchain gas of one control order
		MaxControlMsgsPerBlock:  8,
		MaxPendingPerAccount:    3,
		PendingTtlBlocks:        600, // ~1 h at 6 s
		MaxPendingExecsPerBlock: 10,
		MaxAutoReturnsPerEpoch:  50,
		PortExitFeeToleranceBps: 10, // 0.1 %: allowance for the vault chain's CCTP minimum fee in a default port exit
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
	if p.RebalanceEpochBlocks < 1 {
		return fmt.Errorf("rebalance_epoch_blocks must be positive")
	}
	if p.DynamicFeeBandBps > 1_000 {
		return fmt.Errorf("dynamic_fee_band_bps must be at most 1000")
	}
	if err := p.ControlFeeCap.Validate(); err != nil {
		return fmt.Errorf("control_fee_cap: %w", err)
	}
	if p.MaxControlMsgsPerBlock == 0 || p.MaxControlMsgsPerBlock > 50 {
		return fmt.Errorf("max_control_msgs_per_block must be within [1, 50]")
	}
	if p.MaxBeaconsPerBlock == 0 || p.MaxBeaconsPerBlock > 50 {
		return fmt.Errorf("max_beacons_per_block must be within [1, 50]")
	}
	if p.MaxPendingPerAccount == 0 || p.MaxPendingPerAccount > MaxPendingPerAccountAbsolute {
		return fmt.Errorf("max_pending_per_account must be within [1, %d]", MaxPendingPerAccountAbsolute)
	}
	if p.PendingTtlBlocks < 1 || p.PendingTtlBlocks > MaxPendingTTLBlocksAbsolute {
		return fmt.Errorf("pending_ttl_blocks must be within [1, %d]", MaxPendingTTLBlocksAbsolute)
	}
	if p.MaxPendingExecsPerBlock == 0 || p.MaxPendingExecsPerBlock > MaxPendingExecsPerBlockAbsolute {
		return fmt.Errorf("max_pending_execs_per_block must be within [1, %d]", MaxPendingExecsPerBlockAbsolute)
	}
	if p.MaxAutoReturnsPerEpoch == 0 || p.MaxAutoReturnsPerEpoch > MaxAutoReturnsPerEpochAbsolute {
		return fmt.Errorf("max_auto_returns_per_epoch must be within [1, %d]", MaxAutoReturnsPerEpochAbsolute)
	}
	if p.PortExitFeeToleranceBps > MaxPortExitFeeToleranceBpsAbsolute {
		return fmt.Errorf("port_exit_fee_tolerance_bps must be at most %d", MaxPortExitFeeToleranceBpsAbsolute)
	}
	return nil
}

// PortExitMinAccepted is the min_accepted of the default exit of a port
// account (spec §11.6, v0.9.12): net - ceil(net x toleranceBps / 10,000).
//
// The exit on the vault chain burns all its USDC by CCTP with maxFee =
// TokenMessengerV2.getMinFeeAmount(net) and checks net - maxFee >= min_accepted;
// the user receives net minus the fee the issuer actually executes (at most
// maxFee). min_accepted moves no value: it is the user's guaranteed floor and
// the condition under which the exit can settle at all.
//
// Rounding: the fee allowance is rounded UP, so the floor is rounded DOWN.
// The rule "never round in the user's favour" means the protocol never
// promises or pays the user more than it can guarantee. Rounding the floor up
// would promise a minimum that a CCTP fee exactly at the tolerance cannot
// honour (Circle's getMinFeeAmount is floor(amount x minFee / 1e7) but at
// least 1 unit: with net = 999 and 10 bps a truncated allowance of 0 would
// demand 999 while the issuer charges 1), so the exit would revert and, for a
// non-EVM controller, the USDC would be stranded in it. Rounding the floor
// down widens the user's protection by less than one base unit and never
// changes what the user is paid. This is the convention of the module's other
// amounts: every fee or allowance rounds up (withdrawFee), every promised
// amount rounds down.
func PortExitMinAccepted(net math.Int, toleranceBps uint32) math.Int {
	if toleranceBps == 0 || !net.IsPositive() {
		return net
	}
	allowance := net.MulRaw(int64(toleranceBps)).AddRaw(9_999).QuoRaw(10_000)
	return net.Sub(allowance)
}
