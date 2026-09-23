package types

import errorsmod "cosmossdk.io/errors"

// ErrorCode enumerates the x/warpledger error codes registered in the SDK
// error registry. Code 1 is reserved by the SDK for internal errors, so the
// enumeration starts at 2. Codes are part of the public ABCI surface (clients
// match on codespace + code) and must never be renumbered; append only.
type ErrorCode uint32

const (
	_ ErrorCode = iota + 1 // 1 is reserved by cosmossdk.io/errors

	CodeDomainCapExceeded  // 2
	CodeTokenNotFound      // 3
	CodeNotCollateralToken // 4
	CodeNothingToSweep     // 5
	CodeInsufficientForFee // 6
	CodeTransfersPaused    // 7
	CodeTooManyDomains     // 8
	CodeInvalidFeeQuote    // 9
	CodeIsmNotBonded       // 10
	CodeSourceCapExceeded  // 11
	CodeOriginPaused       // 12
	CodeNotSyntheticToken  // 13
)

// Uint32 returns the numeric code for the SDK error registry.
func (c ErrorCode) Uint32() uint32 { return uint32(c) }

var (
	ErrDomainCapExceeded  = errorsmod.Register(ModuleName, CodeDomainCapExceeded.Uint32(), "domain exposure cap exceeded")
	ErrTokenNotFound      = errorsmod.Register(ModuleName, CodeTokenNotFound.Uint32(), "warp token not found")
	ErrNotCollateralToken = errorsmod.Register(ModuleName, CodeNotCollateralToken.Uint32(), "warp token is not a collateral token")
	ErrNothingToSweep     = errorsmod.Register(ModuleName, CodeNothingToSweep.Uint32(), "deposit address has no balance to sweep")
	ErrInsufficientForFee = errorsmod.Register(ModuleName, CodeInsufficientForFee.Uint32(), "deposit balance does not cover the interchain gas fee")
	ErrTransfersPaused    = errorsmod.Register(ModuleName, CodeTransfersPaused.Uint32(), "outbound warp transfers are paused by the circuit breaker")
	ErrTooManyDomains     = errorsmod.Register(ModuleName, CodeTooManyDomains.Uint32(), "maximum number of domains per token reached")
	ErrInvalidFeeQuote    = errorsmod.Register(ModuleName, CodeInvalidFeeQuote.Uint32(), "unsupported interchain gas fee quote")
)

var ErrIsmNotBonded = errorsmod.Register(ModuleName, CodeIsmNotBonded.Uint32(), "the token ISM must be bonded in x/ismbond for a cap above bonded_cap_threshold")

// Errors of the multi-origin settlement basket (spec §11.4 D-29).
var (
	ErrSourceCapExceeded = errorsmod.Register(ModuleName, CodeSourceCapExceeded.Uint32(), "origin share of the basket would exceed its cap")
	ErrOriginPaused      = errorsmod.Register(ModuleName, CodeOriginPaused.Uint32(), "origin is paused for deposits and redemptions")
	ErrNotSyntheticToken = errorsmod.Register(ModuleName, CodeNotSyntheticToken.Uint32(), "warp token is not a synthetic token")
)
