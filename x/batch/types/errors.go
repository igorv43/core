package types

import errorsmod "cosmossdk.io/errors"

// ErrorCode enumerates the x/batch error codes. Code 1 is reserved by the SDK; append only.
type ErrorCode uint32

const (
	_ ErrorCode = iota + 1

	CodeMarketNotFound      // 2
	CodeMarketDisabled      // 3
	CodeMarketExists        // 4
	CodeInvalidMarket       // 5
	CodeInvalidIntent       // 6
	CodeIntentNotFound      // 7
	CodeTooManyIntents      // 8
	CodeSolverNotFound      // 9
	CodeSolverExists        // 10
	CodeSolverSuspended     // 11
	CodeSolverUnbonding     // 12
	CodeInsufficientBond    // 13
	CodeInsufficientEscrow  // 14
	CodeCommitWindowClosed  // 15
	CodeCommitExists        // 16
	CodeCommitNotFound      // 17
	CodeRevealMismatch      // 18
	CodeRevealWindowClosed  // 19
	CodeFrontendNotFound    // 20
	CodeFrontendFeeTooHigh  // 21
	CodeTooManyApprovals    // 22
	CodeReferencePriceUnset // 23
	CodeUnauthorized        // 24
)

// Uint32 returns the numeric code for the SDK error registry.
func (c ErrorCode) Uint32() uint32 { return uint32(c) }

var (
	ErrMarketNotFound      = errorsmod.Register(ModuleName, CodeMarketNotFound.Uint32(), "market not found")
	ErrMarketDisabled      = errorsmod.Register(ModuleName, CodeMarketDisabled.Uint32(), "market disabled")
	ErrMarketExists        = errorsmod.Register(ModuleName, CodeMarketExists.Uint32(), "market already exists")
	ErrInvalidMarket       = errorsmod.Register(ModuleName, CodeInvalidMarket.Uint32(), "invalid market")
	ErrInvalidIntent       = errorsmod.Register(ModuleName, CodeInvalidIntent.Uint32(), "invalid intent")
	ErrIntentNotFound      = errorsmod.Register(ModuleName, CodeIntentNotFound.Uint32(), "intent not found")
	ErrTooManyIntents      = errorsmod.Register(ModuleName, CodeTooManyIntents.Uint32(), "too many open intents")
	ErrSolverNotFound      = errorsmod.Register(ModuleName, CodeSolverNotFound.Uint32(), "solver not registered")
	ErrSolverExists        = errorsmod.Register(ModuleName, CodeSolverExists.Uint32(), "solver already registered")
	ErrSolverSuspended     = errorsmod.Register(ModuleName, CodeSolverSuspended.Uint32(), "solver suspended")
	ErrSolverUnbonding     = errorsmod.Register(ModuleName, CodeSolverUnbonding.Uint32(), "solver is unbonding")
	ErrInsufficientBond    = errorsmod.Register(ModuleName, CodeInsufficientBond.Uint32(), "insufficient solver bond")
	ErrInsufficientEscrow  = errorsmod.Register(ModuleName, CodeInsufficientEscrow.Uint32(), "insufficient solver escrow")
	ErrCommitWindowClosed  = errorsmod.Register(ModuleName, CodeCommitWindowClosed.Uint32(), "commit window closed for this batch")
	ErrCommitExists        = errorsmod.Register(ModuleName, CodeCommitExists.Uint32(), "commit already submitted")
	ErrCommitNotFound      = errorsmod.Register(ModuleName, CodeCommitNotFound.Uint32(), "commit not found")
	ErrRevealMismatch      = errorsmod.Register(ModuleName, CodeRevealMismatch.Uint32(), "reveal does not match the commitment")
	ErrRevealWindowClosed  = errorsmod.Register(ModuleName, CodeRevealWindowClosed.Uint32(), "reveal window closed for this batch")
	ErrFrontendNotFound    = errorsmod.Register(ModuleName, CodeFrontendNotFound.Uint32(), "frontend not registered")
	ErrFrontendFeeTooHigh  = errorsmod.Register(ModuleName, CodeFrontendFeeTooHigh.Uint32(), "frontend fee above the cap")
	ErrTooManyApprovals    = errorsmod.Register(ModuleName, CodeTooManyApprovals.Uint32(), "too many frontend approvals")
	ErrReferencePriceUnset = errorsmod.Register(ModuleName, CodeReferencePriceUnset.Uint32(), "reference price unavailable")
	ErrUnauthorized        = errorsmod.Register(ModuleName, CodeUnauthorized.Uint32(), "unauthorized")
)
