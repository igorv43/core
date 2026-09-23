package types

import "fmt"

// DefaultGenesisState returns the default genesis state.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{Params: DefaultParams(), Operators: []Operator{}, Roots: []RootRecord{}}
}

// Validate performs basic genesis validation.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, o := range gs.Operators {
		if _, err := NormalizeValidator(o.Validator); err != nil {
			return err
		}
		if seen[o.Validator] {
			return fmt.Errorf("duplicated validator %s", o.Validator)
		}
		seen[o.Validator] = true
	}
	return nil
}
