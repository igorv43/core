package types

import errorsmod "cosmossdk.io/errors"

// ErrorCode enumerates the x/perp error codes. Code 1 is reserved by the SDK; append only.
type ErrorCode uint32

const (
	_ ErrorCode = iota + 1

	CodeMarketNotFound      // 2
	CodeMarketExists        // 3
	CodeMarketDisabled      // 4
	CodeInvalidMarket       // 5
	CodeInsufficientMargin  // 6
	CodeInsufficientFree    // 7
	CodePositionNotFound    // 8
	CodeTooManyPositions    // 9
	CodeReduceOnly          // 10
	CodeMarketPaused        // 11
	CodeOICapExceeded       // 12
	CodeTriggerNotFound     // 13
	CodeTooManyTriggers     // 14
	CodeInvalidTrigger      // 15
	CodeUnauthorized        // 16
	CodeInvalidCollateral   // 17
	CodeListingCriteria     // 18
	CodeInvalidParams       // 19
	CodeInvalidOrder        // 20
	CodeReferencePriceUnset // 21
	CodeInsuranceInsolvent  // 22
	CodeInvalidReservation  // 23
)

// Uint32 returns the numeric code for the SDK error registry.
func (c ErrorCode) Uint32() uint32 { return uint32(c) }

var (
	ErrMarketNotFound      = errorsmod.Register(ModuleName, CodeMarketNotFound.Uint32(), "perp market not found")
	ErrMarketExists        = errorsmod.Register(ModuleName, CodeMarketExists.Uint32(), "perp market already exists")
	ErrMarketDisabled      = errorsmod.Register(ModuleName, CodeMarketDisabled.Uint32(), "perp market disabled")
	ErrInvalidMarket       = errorsmod.Register(ModuleName, CodeInvalidMarket.Uint32(), "invalid perp market")
	ErrInsufficientMargin  = errorsmod.Register(ModuleName, CodeInsufficientMargin.Uint32(), "insufficient margin")
	ErrInsufficientFree    = errorsmod.Register(ModuleName, CodeInsufficientFree.Uint32(), "insufficient free collateral")
	ErrPositionNotFound    = errorsmod.Register(ModuleName, CodePositionNotFound.Uint32(), "position not found")
	ErrTooManyPositions    = errorsmod.Register(ModuleName, CodeTooManyPositions.Uint32(), "too many open positions")
	ErrReduceOnly          = errorsmod.Register(ModuleName, CodeReduceOnly.Uint32(), "order may only reduce a position")
	ErrMarketPaused        = errorsmod.Register(ModuleName, CodeMarketPaused.Uint32(), "market paused by the oracle state")
	ErrOICapExceeded       = errorsmod.Register(ModuleName, CodeOICapExceeded.Uint32(), "open interest cap exceeded")
	ErrTriggerNotFound     = errorsmod.Register(ModuleName, CodeTriggerNotFound.Uint32(), "trigger order not found")
	ErrTooManyTriggers     = errorsmod.Register(ModuleName, CodeTooManyTriggers.Uint32(), "too many trigger orders")
	ErrInvalidTrigger      = errorsmod.Register(ModuleName, CodeInvalidTrigger.Uint32(), "invalid trigger order")
	ErrUnauthorized        = errorsmod.Register(ModuleName, CodeUnauthorized.Uint32(), "unauthorized")
	ErrInvalidCollateral   = errorsmod.Register(ModuleName, CodeInvalidCollateral.Uint32(), "invalid collateral")
	ErrListingCriteria     = errorsmod.Register(ModuleName, CodeListingCriteria.Uint32(), "listing criteria not met")
	ErrInvalidParams       = errorsmod.Register(ModuleName, CodeInvalidParams.Uint32(), "invalid params")
	ErrInvalidOrder        = errorsmod.Register(ModuleName, CodeInvalidOrder.Uint32(), "invalid order")
	ErrReferencePriceUnset = errorsmod.Register(ModuleName, CodeReferencePriceUnset.Uint32(), "reference price unavailable")
	ErrInsuranceInsolvent  = errorsmod.Register(ModuleName, CodeInsuranceInsolvent.Uint32(), "insurance fund cannot cover the loss")
	ErrInvalidReservation  = errorsmod.Register(ModuleName, CodeInvalidReservation.Uint32(), "invalid margin reservation")
)
