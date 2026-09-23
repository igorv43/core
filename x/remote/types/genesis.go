package types

import "fmt"

// DefaultGenesisState returns the default genesis state: no app.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(), Apps: []RemoteApp{}, Gateways: []Gateway{}, Accounts: []RemoteAccount{},
		Sessions: []Session{}, Withdrawals: []Withdrawal{}, Beacons: []Beacon{},
	}
}

// Validate performs basic genesis validation.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	apps := map[uint64]bool{}
	for _, a := range gs.Apps {
		if apps[a.Id.GetInternalId()] {
			return fmt.Errorf("duplicated remote app %s", a.Id)
		}
		apps[a.Id.GetInternalId()] = true
	}
	for _, g := range gs.Gateways {
		if !apps[g.AppId.GetInternalId()] {
			return fmt.Errorf("gateway for unknown app %s", g.AppId)
		}
	}
	for _, a := range gs.Accounts {
		if a.Address != DeriveAddress(a.Domain, a.Controller).String() {
			return fmt.Errorf("remote account %s does not match its controller", a.Address)
		}
	}
	return nil
}
