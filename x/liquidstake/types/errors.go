package types

import errorsmod "cosmossdk.io/errors"

// ErrorCode enumerates the x/liquidstake error codes. Code 1 is reserved by
// the SDK. Codes are part of the public ABCI surface: append only.
type ErrorCode uint32

const (
	_ ErrorCode = iota + 1

	CodeInvalidDenom       // 2
	CodeModuleCapReached   // 3
	CodeNothingToClaim     // 4
	CodeNoEligibleVals     // 5
	CodeInsufficientBuffer // 6
	CodeZeroSupply         // 7
	CodeRequestNotFound    // 8
)

// Uint32 returns the numeric code for the SDK error registry.
func (c ErrorCode) Uint32() uint32 { return uint32(c) }

var (
	ErrInvalidDenom       = errorsmod.Register(ModuleName, CodeInvalidDenom.Uint32(), "invalid denom")
	ErrModuleCapReached   = errorsmod.Register(ModuleName, CodeModuleCapReached.Uint32(), "liquid staking module cap reached")
	ErrNothingToClaim     = errorsmod.Register(ModuleName, CodeNothingToClaim.Uint32(), "no matured unstake request to claim")
	ErrNoEligibleVals     = errorsmod.Register(ModuleName, CodeNoEligibleVals.Uint32(), "no eligible validator")
	ErrInsufficientBuffer = errorsmod.Register(ModuleName, CodeInsufficientBuffer.Uint32(), "insufficient module buffer")
	ErrZeroSupply         = errorsmod.Register(ModuleName, CodeZeroSupply.Uint32(), "stLUNC supply is zero")
	ErrRequestNotFound    = errorsmod.Register(ModuleName, CodeRequestNotFound.Uint32(), "unstake request not found")
)
