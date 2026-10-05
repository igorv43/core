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
	pending := map[uint64]bool{}
	for _, p := range gs.PendingPayloads {
		if pending[p.Id] || p.Id > gs.NextPendingId {
			return fmt.Errorf("pending payload %d duplicated or above next_pending_id", p.Id)
		}
		pending[p.Id] = true
		if err := p.AfterDeposit.ValidateBasic(); err != nil {
			return err
		}
	}
	for _, c := range gs.DepositCredits {
		if c.Amount.IsNil() || !c.Amount.IsPositive() {
			return fmt.Errorf("deposit credit of %s must be positive", c.Account)
		}
	}
	for _, ar := range gs.AutoReturns {
		if ar.TokenId.IsZeroAddress() {
			return fmt.Errorf("auto-return of %s has no token", ar.Account)
		}
	}
	return nil
}
