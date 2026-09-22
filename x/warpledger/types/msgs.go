package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgUpdateParams{}
	_ sdk.Msg = &MsgSetDomainCap{}
	_ sdk.Msg = &MsgSweepMigration{}
)

// ValidateBasic performs stateless validation of MsgUpdateParams.
func (m MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority address: %s", err)
	}
	return m.Params.Validate()
}

// ValidateBasic performs stateless validation of MsgSetDomainCap.
func (m MsgSetDomainCap) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority address: %s", err)
	}
	if m.TokenId.IsZeroAddress() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "token id must not be the zero address")
	}
	if m.Cap.IsNil() || m.Cap.IsNegative() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "cap must be a non-negative integer")
	}
	return nil
}

// ValidateBasic performs stateless validation of MsgSweepMigration.
func (m MsgSweepMigration) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender address: %s", err)
	}
	if m.TokenId.IsZeroAddress() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "token id must not be the zero address")
	}
	if m.Recipient.IsZeroAddress() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "recipient must not be the zero address")
	}
	return nil
}
