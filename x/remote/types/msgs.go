package types

import (
	errorsmod "cosmossdk.io/errors"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func validAddr(field, addr string) error {
	if _, err := sdk.AccAddressFromBech32(addr); err != nil {
		return errorsmod.Wrapf(ErrUnauthorized, "invalid %s address: %v", field, err)
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgGrantSessionKey) ValidateBasic() error {
	if err := validAddr("controller", m.Controller); err != nil {
		return err
	}
	if err := validAddr("session_key", m.SessionKey); err != nil {
		return err
	}
	if m.SessionKey == m.Controller {
		return errorsmod.Wrap(ErrInvalidSession, "the session key must differ from the account")
	}
	if m.TtlSeconds < 0 {
		return errorsmod.Wrap(ErrInvalidSession, "ttl_seconds must be non-negative")
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgRevokeSessionKey) ValidateBasic() error {
	if err := validAddr("controller", m.Controller); err != nil {
		return err
	}
	return validAddr("session_key", m.SessionKey)
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgRevokeOwnSessionKey) ValidateBasic() error {
	if err := validAddr("session_key", m.SessionKey); err != nil {
		return err
	}
	return validAddr("controller", m.Controller)
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgWithdraw) ValidateBasic() error {
	if err := validAddr("controller", m.Controller); err != nil {
		return err
	}
	if m.Amount.IsNil() || !m.Amount.IsPositive() {
		return errorsmod.Wrap(ErrInvalidWithdraw, "amount must be positive")
	}
	if m.TokenOut != "" {
		if _, err := util.DecodeHexAddress(m.TokenOut); err != nil {
			return errorsmod.Wrapf(ErrInvalidWithdraw, "token_out must be a 32-byte hex: %v", err)
		}
		if m.MinAccepted == nil || m.MinAccepted.IsNil() || m.MinAccepted.IsNegative() {
			return errorsmod.Wrap(ErrInvalidWithdraw, "min_accepted must be set with token_out")
		}
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgFundPaymaster) ValidateBasic() error {
	if err := validAddr("sender", m.Sender); err != nil {
		return err
	}
	if !m.Amount.IsValid() || !m.Amount.IsPositive() {
		return errorsmod.Wrap(ErrInvalidParams, "amount must be a positive coin")
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgCreateRemoteApp) ValidateBasic() error { return validAddr("authority", m.Authority) }

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgSetGateway) ValidateBasic() error {
	if err := validAddr("authority", m.Authority); err != nil {
		return err
	}
	if !m.ExitFactory.IsZeroAddress() && len(m.ExitInitCodeHash) != 32 {
		return errorsmod.Wrap(ErrInvalidParams, "exit_init_code_hash must be 32 bytes when exit_factory is set")
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgSetBeacon) ValidateBasic() error {
	if err := validAddr("authority", m.Authority); err != nil {
		return err
	}
	if m.IntervalBlocks < 0 {
		return errorsmod.Wrap(ErrInvalidParams, "interval_blocks must be non-negative")
	}
	if m.IntervalBlocks > 0 {
		switch m.Kind {
		case BEACON_KIND_EXCHANGE_RATE, BEACON_KIND_ROUTE_SOLVENCY:
		case BEACON_KIND_POSITION_DIGEST:
			if err := validAddr("account", m.Account); err != nil {
				return err
			}
		default:
			return errorsmod.Wrap(ErrInvalidParams, "unknown beacon kind")
		}
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgUpdateParams) ValidateBasic() error {
	if err := validAddr("authority", m.Authority); err != nil {
		return err
	}
	return m.Params.Validate()
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgSetExecutor) ValidateBasic() error {
	if err := validAddr("authority", m.Authority); err != nil {
		return err
	}
	if m.Domain == 0 {
		return errorsmod.Wrap(ErrInvalidParams, "domain must be set")
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgExecutorControl) ValidateBasic() error {
	if err := validAddr("authority", m.Authority); err != nil {
		return err
	}
	switch m.Action {
	case CONTROL_ENROLL_LEG, CONTROL_PAUSE_LEG, CONTROL_UNPAUSE_LEG:
		return nil
	default:
		return errorsmod.Wrap(ErrInvalidParams, "action must be ENROLL_LEG, PAUSE_LEG or UNPAUSE_LEG")
	}
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgSetPort) ValidateBasic() error {
	if err := validAddr("authority", m.Authority); err != nil {
		return err
	}
	if m.PortDomain == 0 {
		return errorsmod.Wrap(ErrInvalidParams, "port_domain must be set")
	}
	return nil
}
