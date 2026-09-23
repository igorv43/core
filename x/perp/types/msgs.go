package types

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func validAddr(field, addr string) error {
	if _, err := sdk.AccAddressFromBech32(addr); err != nil {
		return errorsmod.Wrapf(ErrUnauthorized, "invalid %s address: %v", field, err)
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgDepositCollateral) ValidateBasic() error {
	if err := validAddr("sender", m.Sender); err != nil {
		return err
	}
	if !m.Amount.IsValid() || !m.Amount.IsPositive() {
		return errorsmod.Wrap(ErrInvalidCollateral, "amount must be a positive coin")
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgWithdrawCollateral) ValidateBasic() error {
	if err := validAddr("sender", m.Sender); err != nil {
		return err
	}
	if !m.Amount.IsValid() || !m.Amount.IsPositive() {
		return errorsmod.Wrap(ErrInvalidCollateral, "amount must be a positive coin")
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgSubmitPerpIntent) ValidateBasic() error {
	if err := validAddr("sender", m.Sender); err != nil {
		return err
	}
	if m.MarketId == "" {
		return errorsmod.Wrap(ErrInvalidOrder, "market_id is required")
	}
	if m.Side != batchtypes.SIDE_BUY && m.Side != batchtypes.SIDE_SELL {
		return errorsmod.Wrap(ErrInvalidOrder, "side must be buy or sell")
	}
	if m.Qty.IsNil() || !m.Qty.IsPositive() {
		return errorsmod.Wrap(ErrInvalidOrder, "qty must be positive")
	}
	if m.LimitPrice.IsNil() || !m.LimitPrice.IsPositive() {
		return errorsmod.Wrap(ErrInvalidOrder, "limit_price must be positive")
	}
	if m.ExpiryHeight <= 0 {
		return errorsmod.Wrap(ErrInvalidOrder, "expiry_height must be positive")
	}
	if m.Frontend != "" {
		if err := validAddr("frontend", m.Frontend); err != nil {
			return err
		}
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgSubmitTriggerOrder) ValidateBasic() error {
	if err := validAddr("sender", m.Sender); err != nil {
		return err
	}
	if m.MarketId == "" {
		return errorsmod.Wrap(ErrInvalidTrigger, "market_id is required")
	}
	if m.TriggerPrice.IsNil() || !m.TriggerPrice.IsPositive() {
		return errorsmod.Wrap(ErrInvalidTrigger, "trigger_price must be positive")
	}
	if m.Qty.IsNil() || m.Qty.IsNegative() {
		return errorsmod.Wrap(ErrInvalidTrigger, "qty must be non-negative (0 = whole position)")
	}
	if m.Slippage.IsNil() || m.Slippage.IsNegative() || m.Slippage.GT(math.LegacyOneDec()) {
		return errorsmod.Wrap(ErrInvalidTrigger, "slippage must be within [0, 1]")
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgCancelTriggerOrder) ValidateBasic() error { return validAddr("sender", m.Sender) }

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgSetAutoTopUp) ValidateBasic() error { return validAddr("sender", m.Sender) }

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgSetFeeInLuna) ValidateBasic() error { return validAddr("sender", m.Sender) }

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgFundInsurance) ValidateBasic() error {
	if err := validAddr("sender", m.Sender); err != nil {
		return err
	}
	if !m.Amount.IsValid() || !m.Amount.IsPositive() {
		return errorsmod.Wrap(ErrInvalidCollateral, "amount must be a positive coin")
	}
	return nil
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgCreateMarket) ValidateBasic() error {
	if err := validAddr("authority", m.Authority); err != nil {
		return err
	}
	return m.Market().Validate()
}

// Market builds the market of a MsgCreateMarket (derived fields zeroed).
func (m MsgCreateMarket) Market() Market {
	return Market{
		Id: m.Id, OracleAsset: m.OracleAsset, MaxLeverage: m.MaxLeverage, OiCap: m.OiCap, Alpha: m.Alpha, Stress: m.Stress,
		ListingMinBlocks: m.ListingMinBlocks, VenuesAttested: m.VenuesAttested, MinQty: m.MinQty, TickSize: m.TickSize,
		StCollateralAllowed: m.StCollateralAllowed,
	}
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgUpdateMarket) ValidateBasic() error {
	if err := validAddr("authority", m.Authority); err != nil {
		return err
	}
	if m.MarketId == "" {
		return errorsmod.Wrap(ErrInvalidMarket, "market_id is required")
	}
	probe := Market{Id: m.MarketId, OracleAsset: "probe", MaxLeverage: m.MaxLeverage, OiCap: m.OiCap, Alpha: m.Alpha, Stress: m.Stress,
		ListingMinBlocks: m.ListingMinBlocks, MinQty: math.OneInt(), TickSize: math.LegacyOneDec()}
	return probe.Validate()
}

// ValidateBasic implements sdk.HasValidateBasic.
func (m MsgEnableMarket) ValidateBasic() error {
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
