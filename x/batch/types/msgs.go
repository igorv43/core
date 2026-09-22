package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func validAddr(field, addr string) error {
	if _, err := sdk.AccAddressFromBech32(addr); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid %s: %s", field, err)
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgSubmitIntent) ValidateBasic() error {
	if err := validAddr("sender", m.Sender); err != nil {
		return err
	}
	if m.MarketId == "" {
		return errorsmod.Wrap(ErrInvalidIntent, "market_id is required")
	}
	if m.Side != SIDE_BUY && m.Side != SIDE_SELL {
		return errorsmod.Wrap(ErrInvalidIntent, "side must be buy or sell")
	}
	if !m.AmountIn.IsValid() || !m.AmountIn.IsPositive() {
		return errorsmod.Wrap(ErrInvalidIntent, "amount_in must be a positive coin")
	}
	if m.LimitPrice.IsNil() || !m.LimitPrice.IsPositive() {
		return errorsmod.Wrap(ErrInvalidIntent, "limit_price must be positive")
	}
	if m.MinOut.IsNil() || m.MinOut.IsNegative() {
		return errorsmod.Wrap(ErrInvalidIntent, "min_out must be a non-negative integer")
	}
	if m.ExpiryHeight <= 0 {
		return errorsmod.Wrap(ErrInvalidIntent, "expiry_height must be positive")
	}
	if m.Frontend != "" {
		if err := validAddr("frontend", m.Frontend); err != nil {
			return err
		}
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgCancelIntent) ValidateBasic() error { return validAddr("sender", m.Sender) }

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgRegisterSolver) ValidateBasic() error {
	if err := validAddr("sender", m.Sender); err != nil {
		return err
	}
	if !m.Bond.IsValid() || !m.Bond.IsPositive() {
		return errorsmod.Wrap(ErrInsufficientBond, "bond must be a positive coin")
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgUnbondSolver) ValidateBasic() error { return validAddr("sender", m.Sender) }

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgDepositSolverEscrow) ValidateBasic() error {
	if err := validAddr("sender", m.Sender); err != nil {
		return err
	}
	if !m.Amount.IsValid() || !m.Amount.IsPositive() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidCoins, "amount must be a positive coin")
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgWithdrawSolverEscrow) ValidateBasic() error {
	if err := validAddr("sender", m.Sender); err != nil {
		return err
	}
	if !m.Amount.IsValid() || !m.Amount.IsPositive() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidCoins, "amount must be a positive coin")
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgCommitBid) ValidateBasic() error {
	if err := validAddr("solver", m.Solver); err != nil {
		return err
	}
	if m.MarketId == "" {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "market_id is required")
	}
	if len(m.Commitment) != 32 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "commitment must be a 32-byte SHA256 digest")
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgRevealBid) ValidateBasic() error {
	if err := validAddr("solver", m.Solver); err != nil {
		return err
	}
	if m.Bid.MarketId == "" {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "bid.market_id is required")
	}
	if len(m.Bid.Levels) == 0 || len(m.Bid.Levels) > MaxLevelsPerBidAbsolute {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "bid must have 1..%d levels", MaxLevelsPerBidAbsolute)
	}
	for i, l := range m.Bid.Levels {
		if l.Side != SIDE_BUY && l.Side != SIDE_SELL {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "level %d: side must be buy or sell", i)
		}
		if l.Price.IsNil() || !l.Price.IsPositive() || l.Qty.IsNil() || !l.Qty.IsPositive() {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "level %d: price and qty must be positive", i)
		}
	}
	if len(m.Salt) == 0 || len(m.Salt) > 64 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "salt must be 1..64 bytes")
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgRegisterFrontend) ValidateBasic() error { return validAddr("sender", m.Sender) }

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgApproveFrontend) ValidateBasic() error {
	if err := validAddr("sender", m.Sender); err != nil {
		return err
	}
	return validAddr("frontend", m.Frontend)
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgRevokeFrontend) ValidateBasic() error {
	if err := validAddr("sender", m.Sender); err != nil {
		return err
	}
	return validAddr("frontend", m.Frontend)
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgCreateMarket) ValidateBasic() error {
	if err := validAddr("authority", m.Authority); err != nil {
		return err
	}
	return m.Market.Validate()
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgSetMarketEnabled) ValidateBasic() error {
	if err := validAddr("authority", m.Authority); err != nil {
		return err
	}
	if m.MarketId == "" {
		return errorsmod.Wrap(ErrInvalidMarket, "market_id is required")
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
