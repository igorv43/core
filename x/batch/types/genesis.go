package types

import "fmt"

// DefaultGenesisState returns the default genesis state: no markets.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:            DefaultParams(),
		Markets:           []Market{},
		Intents:           []Intent{},
		NextIntentId:      1,
		Solvers:           []Solver{},
		SolverEscrows:     []SolverEscrowEntry{},
		Frontends:         []Frontend{},
		FrontendApprovals: []FrontendApproval{},
	}
}

// Validate performs basic genesis validation.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	markets := make(map[string]struct{}, len(gs.Markets))
	for _, m := range gs.Markets {
		if err := m.Validate(); err != nil {
			return err
		}
		if _, dup := markets[m.Id]; dup {
			return fmt.Errorf("duplicated market %s", m.Id)
		}
		markets[m.Id] = struct{}{}
	}
	ids := make(map[uint64]struct{}, len(gs.Intents))
	for _, i := range gs.Intents {
		if _, dup := ids[i.Id]; dup {
			return fmt.Errorf("duplicated intent %d", i.Id)
		}
		if i.Id >= gs.NextIntentId {
			return fmt.Errorf("intent %d is not below next_intent_id %d", i.Id, gs.NextIntentId)
		}
		if _, ok := markets[i.MarketId]; !ok {
			return fmt.Errorf("intent %d references unknown market %s", i.Id, i.MarketId)
		}
		ids[i.Id] = struct{}{}
	}
	return nil
}

// Validate validates a market definition.
func (m Market) Validate() error {
	if m.Id == "" || m.Id != m.BaseDenom+"/"+m.QuoteDenom {
		return fmt.Errorf("market id must be base/quote, got %q", m.Id)
	}
	if m.BaseDenom == "" || m.QuoteDenom == "" || m.BaseDenom == m.QuoteDenom {
		return fmt.Errorf("market %s: base and quote must be distinct non-empty denoms", m.Id)
	}
	if m.Type != MARKET_TYPE_SPOT && m.Type != MARKET_TYPE_PERP {
		return fmt.Errorf("market %s: type must be spot or perp", m.Id)
	}
	if m.OracleDenom == "" {
		return fmt.Errorf("market %s: oracle_denom is required", m.Id)
	}
	if m.MinQty.IsNil() || !m.MinQty.IsPositive() {
		return fmt.Errorf("market %s: min_qty must be positive", m.Id)
	}
	if m.TickSize.IsNil() || !m.TickSize.IsPositive() {
		return fmt.Errorf("market %s: tick_size must be positive", m.Id)
	}
	return nil
}
