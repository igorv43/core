package types

import "fmt"

// DefaultGenesisState returns the default genesis state.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:  DefaultParams(),
		Ledgers: []DomainLedger{},
	}
}

// Validate performs basic genesis state validation.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(gs.Ledgers))
	perToken := make(map[string]int)
	for _, l := range gs.Ledgers {
		if err := l.Validate(); err != nil {
			return err
		}
		key := fmt.Sprintf("%s/%d", l.TokenId.String(), l.Domain)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicated ledger for token %s and domain %d", l.TokenId.String(), l.Domain)
		}
		seen[key] = struct{}{}
		perToken[l.TokenId.String()]++
		if perToken[l.TokenId.String()] > MaxDomainsPerToken {
			return fmt.Errorf("token %s has more than %d domains", l.TokenId.String(), MaxDomainsPerToken)
		}
	}
	return nil
}

// Validate validates a single ledger record.
func (l DomainLedger) Validate() error {
	if l.TokenId.IsZeroAddress() {
		return fmt.Errorf("ledger token id must not be the zero address")
	}
	if l.Sent.IsNil() || l.Sent.IsNegative() {
		return fmt.Errorf("ledger sent must be a non-negative integer")
	}
	if l.Received.IsNil() || l.Received.IsNegative() {
		return fmt.Errorf("ledger received must be a non-negative integer")
	}
	if l.HasCap && (l.Cap.IsNil() || l.Cap.IsNegative()) {
		return fmt.Errorf("ledger cap must be a non-negative integer")
	}
	return nil
}
