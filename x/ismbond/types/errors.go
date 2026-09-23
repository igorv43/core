package types

import errorsmod "cosmossdk.io/errors"

// ErrorCode enumerates the x/ismbond error codes. Code 1 is reserved by the SDK; append only.
type ErrorCode uint32

const (
	_ ErrorCode = iota + 1

	CodeOperatorNotFound   // 2
	CodeOperatorExists     // 3
	CodeInsufficientBond   // 4
	CodeInvalidValidator   // 5
	CodeUnbonding          // 6
	CodeNotClaimable       // 7
	CodeInvalidEvidence    // 8
	CodeRootNotRecorded    // 9
	CodeInvalidParams      // 10
	CodeUnauthorized       // 11
	CodeValidatorNotBonded // 12
)

// Uint32 returns the numeric code for the SDK error registry.
func (c ErrorCode) Uint32() uint32 { return uint32(c) }

var (
	ErrOperatorNotFound   = errorsmod.Register(ModuleName, CodeOperatorNotFound.Uint32(), "operator not found")
	ErrOperatorExists     = errorsmod.Register(ModuleName, CodeOperatorExists.Uint32(), "operator already bonded")
	ErrInsufficientBond   = errorsmod.Register(ModuleName, CodeInsufficientBond.Uint32(), "bond below the minimum")
	ErrInvalidValidator   = errorsmod.Register(ModuleName, CodeInvalidValidator.Uint32(), "invalid validator address")
	ErrUnbonding          = errorsmod.Register(ModuleName, CodeUnbonding.Uint32(), "operator is unbonding")
	ErrNotClaimable       = errorsmod.Register(ModuleName, CodeNotClaimable.Uint32(), "bond not claimable yet")
	ErrInvalidEvidence    = errorsmod.Register(ModuleName, CodeInvalidEvidence.Uint32(), "invalid evidence")
	ErrRootNotRecorded    = errorsmod.Register(ModuleName, CodeRootNotRecorded.Uint32(), "no local root recorded for that index")
	ErrInvalidParams      = errorsmod.Register(ModuleName, CodeInvalidParams.Uint32(), "invalid params")
	ErrUnauthorized       = errorsmod.Register(ModuleName, CodeUnauthorized.Uint32(), "unauthorized")
	ErrValidatorNotBonded = errorsmod.Register(ModuleName, CodeValidatorNotBonded.Uint32(), "validator has no active bond")
)
