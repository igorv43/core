package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgStake{}
	_ sdk.Msg = &MsgUnstake{}
	_ sdk.Msg = &MsgClaim{}
	_ sdk.Msg = &MsgUpdateParams{}
)

// ValidateBasic performs stateless validation of MsgStake.
func (m MsgStake) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender: %s", err)
	}
	if !m.Amount.IsValid() || !m.Amount.IsPositive() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidCoins, "amount must be a positive coin")
	}
	return nil
}

// ValidateBasic performs stateless validation of MsgUnstake.
func (m MsgUnstake) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender: %s", err)
	}
	if !m.Amount.IsValid() || !m.Amount.IsPositive() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidCoins, "amount must be a positive coin")
	}
	if m.Amount.Denom != StDenom {
		return errorsmod.Wrapf(ErrInvalidDenom, "expected %s, got %s", StDenom, m.Amount.Denom)
	}
	return nil
}

// ValidateBasic performs stateless validation of MsgClaim.
func (m MsgClaim) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender: %s", err)
	}
	return nil
}

// ValidateBasic performs stateless validation of MsgUpdateParams.
func (m MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority: %s", err)
	}
	return m.Params.Validate()
}
