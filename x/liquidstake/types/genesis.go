package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// DefaultGenesisState returns the default genesis state.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:          DefaultParams(),
		Epoch:           Epoch{Number: 0, StartHeight: 0},
		UnstakeRequests: []UnstakeRequest{},
		NextRequestId:   1,
		Owed:            math.ZeroInt(),
		Validators:      []ValidatorState{},
	}
}

// Validate performs basic genesis state validation.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	if gs.Owed.IsNil() || gs.Owed.IsNegative() {
		return fmt.Errorf("owed must be a non-negative integer")
	}
	seen := make(map[uint64]struct{}, len(gs.UnstakeRequests))
	owed := math.ZeroInt()
	for _, r := range gs.UnstakeRequests {
		if _, dup := seen[r.Id]; dup {
			return fmt.Errorf("duplicated unstake request id %d", r.Id)
		}
		seen[r.Id] = struct{}{}
		if r.Id >= gs.NextRequestId {
			return fmt.Errorf("unstake request id %d is not below next_request_id %d", r.Id, gs.NextRequestId)
		}
		if r.Amount.IsNil() || !r.Amount.IsPositive() || r.StAmount.IsNil() || !r.StAmount.IsPositive() {
			return fmt.Errorf("unstake request %d must have positive amounts", r.Id)
		}
		// every unfilled request (queued or undelegated) is owed
		owed = owed.Add(r.Amount)
	}
	if !owed.Equal(gs.Owed) {
		return fmt.Errorf("owed %s does not match the sum of unfilled requests %s", gs.Owed, owed)
	}
	return nil
}
