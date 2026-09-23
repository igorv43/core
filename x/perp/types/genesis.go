package types

import (
	"fmt"

	"cosmossdk.io/math"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesisState returns the default genesis state: no markets.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:            DefaultParams(),
		Markets:           []Market{},
		Positions:         []Position{},
		Collateral:        []CollateralEntry{},
		Reservations:      []ReservationEntry{},
		Triggers:          []TriggerOrder{},
		NextTriggerId:     1,
		AutoTopUpAccounts: []string{},
		Ledger:            DefaultLedger(),
		Allocations:       []AllocationRecord{},
		UnwindIntents:     []UnwindIntent{},
	}
}

// DefaultLedger returns an empty ledger.
func DefaultLedger() Ledger {
	return Ledger{
		Insurance: math.ZeroInt(), Revenue: math.ZeroInt(), BurnBudget: math.ZeroInt(),
		BurnSpentEpoch: math.ZeroInt(), BurnedEpoch: math.ZeroInt(), Epoch: 0, EpochStartHeight: 0,
		TrancheSt: math.ZeroInt(), TrancheUnbondingSt: math.ZeroInt(), TrancheUluna: math.ZeroInt(),
		TrancheSoldEpoch: math.ZeroInt(), TrancheAdvanced: math.ZeroInt(),
	}
}

// Validate performs basic genesis validation.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	markets := map[string]struct{}{}
	for _, m := range gs.Markets {
		if err := m.Validate(); err != nil {
			return err
		}
		if _, dup := markets[m.Id]; dup {
			return fmt.Errorf("duplicated market %s", m.Id)
		}
		markets[m.Id] = struct{}{}
	}
	for _, p := range gs.Positions {
		if _, err := sdk.AccAddressFromBech32(p.Account); err != nil {
			return fmt.Errorf("position account: %w", err)
		}
		if _, ok := markets[p.MarketId]; !ok {
			return fmt.Errorf("position in unknown market %s", p.MarketId)
		}
		if p.Qty.IsNil() || !p.Qty.IsPositive() || p.Collateral.IsNil() || p.Collateral.IsNegative() {
			return fmt.Errorf("position of %s: size must be positive and collateral non-negative", p.Account)
		}
		if p.Side != batchtypes.SIDE_BUY && p.Side != batchtypes.SIDE_SELL {
			return fmt.Errorf("position of %s: invalid side", p.Account)
		}
		if !p.CollateralSt.IsNil() && p.CollateralSt.IsNegative() {
			return fmt.Errorf("position of %s: collateral_st must be non-negative", p.Account)
		}
	}
	for _, c := range gs.Collateral {
		if c.Amount.IsNil() || c.Amount.IsNegative() {
			return fmt.Errorf("collateral of %s must be non-negative", c.Account)
		}
	}
	for _, t := range gs.Triggers {
		if t.Id >= gs.NextTriggerId {
			return fmt.Errorf("trigger %d is not below next_trigger_id", t.Id)
		}
	}
	if gs.Ledger.Insurance.IsNil() || gs.Ledger.Insurance.IsNegative() || gs.Ledger.Revenue.IsNil() || gs.Ledger.Revenue.IsNegative() {
		return fmt.Errorf("ledger balances must be non-negative")
	}
	return nil
}

// Validate validates the governance inputs of a market.
func (m Market) Validate() error {
	if m.Id == "" {
		return fmt.Errorf("market id is required")
	}
	if m.OracleAsset == "" {
		return fmt.Errorf("market %s: oracle_asset is required", m.Id)
	}
	if m.MaxLeverage.IsNil() || !m.MaxLeverage.IsPositive() || m.MaxLeverage.GT(math.LegacyNewDec(MaxLeverageAbsolute)) {
		return fmt.Errorf("market %s: max_leverage must be within (0, %d]", m.Id, MaxLeverageAbsolute)
	}
	if m.OiCap.IsNil() || !m.OiCap.IsPositive() {
		return fmt.Errorf("market %s: oi_cap must be positive", m.Id)
	}
	if m.Alpha.IsNil() || !m.Alpha.IsPositive() || m.Alpha.GT(math.LegacyMustNewDecFromStr(MaxAlphaAbsolute)) {
		return fmt.Errorf("market %s: alpha must be within (0, %s]", m.Id, MaxAlphaAbsolute)
	}
	if m.Stress.IsNil() || m.Stress.LT(math.LegacyOneDec()) {
		return fmt.Errorf("market %s: stress must be at least 1", m.Id)
	}
	if m.ListingMinBlocks < 0 {
		return fmt.Errorf("market %s: listing_min_blocks must be non-negative", m.Id)
	}
	if m.MinQty.IsNil() || !m.MinQty.IsPositive() || m.TickSize.IsNil() || !m.TickSize.IsPositive() {
		return fmt.Errorf("market %s: min_qty and tick_size must be positive", m.Id)
	}
	return nil
}
