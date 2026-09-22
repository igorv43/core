package ante

import (
	errorsmod "cosmossdk.io/errors"
	circuittypes "cosmossdk.io/x/circuit/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// CircuitResetGuardDecorator enforces the trip/reset asymmetry of the
// Liquidity Fabric pause model (spec §6.3, D-03): any account granted
// permissions in x/circuit may trip a breaker, but only the governance
// authority may reset one. x/circuit itself lets a super-admin reset, so the
// restriction is applied here, in the ante handler, before execution.
type CircuitResetGuardDecorator struct {
	authority string
}

// NewCircuitResetGuardDecorator returns a decorator that rejects
// MsgResetCircuitBreaker unless signed by the given authority.
func NewCircuitResetGuardDecorator(authority string) CircuitResetGuardDecorator {
	return CircuitResetGuardDecorator{authority: authority}
}

// AnteHandle implements sdk.AnteDecorator.
func (d CircuitResetGuardDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if err := d.checkMsgs(tx.GetMsgs()); err != nil {
		return ctx, err
	}
	return next(ctx, tx, simulate)
}

func (d CircuitResetGuardDecorator) checkMsgs(msgs []sdk.Msg) error {
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *circuittypes.MsgResetCircuitBreaker:
			if m.Authority != d.authority {
				return errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
					"circuit breaker reset is restricted to the governance authority %s", d.authority)
			}
		case *authz.MsgExec:
			inner, err := m.GetMessages()
			if err != nil {
				return err
			}
			if err := d.checkMsgs(inner); err != nil {
				return err
			}
		}
	}
	return nil
}
