package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func validAddr(field, addr string) error {
	if _, err := sdk.AccAddressFromBech32(addr); err != nil {
		return errorsmod.Wrapf(ErrUnauthorized, "invalid %s address: %v", field, err)
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgBondOperator) ValidateBasic() error {
	if err := validAddr("operator", m.Operator); err != nil {
		return err
	}
	if _, err := NormalizeValidator(m.Validator); err != nil {
		return err
	}
	if !m.Bond.IsValid() || !m.Bond.IsPositive() {
		return errorsmod.Wrap(ErrInsufficientBond, "bond must be a positive coin")
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgUnbondOperator) ValidateBasic() error { return validAddr("operator", m.Operator) }

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgClaimBond) ValidateBasic() error { return validAddr("operator", m.Operator) }

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgSubmitEvidence) ValidateBasic() error {
	if err := validAddr("submitter", m.Submitter); err != nil {
		return err
	}
	switch m.Kind {
	case EVIDENCE_KIND_EQUIVOCATION, EVIDENCE_KIND_INVALID_OUTBOUND:
	default:
		return errorsmod.Wrap(ErrInvalidEvidence, "unknown evidence kind")
	}
	if _, err := m.CheckpointA.Digest(); err != nil {
		return err
	}
	if m.Kind == EVIDENCE_KIND_EQUIVOCATION {
		if _, err := m.CheckpointB.Digest(); err != nil {
			return err
		}
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgDistributeRewards) ValidateBasic() error {
	if err := validAddr("sender", m.Sender); err != nil {
		return err
	}
	if !m.Amount.IsValid() || !m.Amount.IsPositive() {
		return errorsmod.Wrap(ErrInvalidParams, "amount must be a positive coin")
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
