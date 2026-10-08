package types

import errorsmod "cosmossdk.io/errors"

// ErrorCode enumerates the x/remote error codes. Code 1 is reserved by the SDK; append only.
type ErrorCode uint32

const (
	_ ErrorCode = iota + 1

	CodeAppNotFound       // 2
	CodeInvalidPayload    // 3
	CodeMsgNotWhitelisted // 4
	CodeInvalidSigner     // 5
	CodeAccountNotFound   // 6
	CodeTooManySessions   // 7
	CodeSessionNotFound   // 8
	CodeUnauthorized      // 9
	CodeInvalidParams     // 10
	CodeInvalidWithdraw   // 11
	CodeFeeUnpaid         // 12
	CodeGatewayNotFound   // 13
	CodeInvalidSession    // 14
	CodeInvalidConversion // 15
	CodeInvalidReference  // 16
	CodePendingFull       // 17
	CodePortConflict      // 18
)

// Uint32 returns the numeric code for the SDK error registry.
func (c ErrorCode) Uint32() uint32 { return uint32(c) }

var (
	ErrAppNotFound       = errorsmod.Register(ModuleName, CodeAppNotFound.Uint32(), "remote app not found")
	ErrInvalidPayload    = errorsmod.Register(ModuleName, CodeInvalidPayload.Uint32(), "invalid remote payload")
	ErrMsgNotWhitelisted = errorsmod.Register(ModuleName, CodeMsgNotWhitelisted.Uint32(), "message type not allowed in a remote payload")
	ErrInvalidSigner     = errorsmod.Register(ModuleName, CodeInvalidSigner.Uint32(), "payload message must be signed by the remote account")
	ErrAccountNotFound   = errorsmod.Register(ModuleName, CodeAccountNotFound.Uint32(), "remote account not found")
	ErrTooManySessions   = errorsmod.Register(ModuleName, CodeTooManySessions.Uint32(), "too many session keys")
	ErrSessionNotFound   = errorsmod.Register(ModuleName, CodeSessionNotFound.Uint32(), "session key not found")
	ErrUnauthorized      = errorsmod.Register(ModuleName, CodeUnauthorized.Uint32(), "unauthorized")
	ErrInvalidParams     = errorsmod.Register(ModuleName, CodeInvalidParams.Uint32(), "invalid params")
	ErrInvalidWithdraw   = errorsmod.Register(ModuleName, CodeInvalidWithdraw.Uint32(), "invalid withdrawal")
	ErrFeeUnpaid         = errorsmod.Register(ModuleName, CodeFeeUnpaid.Uint32(), "remote message fee not paid")
	ErrGatewayNotFound   = errorsmod.Register(ModuleName, CodeGatewayNotFound.Uint32(), "gateway not found")
	ErrInvalidSession    = errorsmod.Register(ModuleName, CodeInvalidSession.Uint32(), "invalid session key")
)

// ErrInvalidConversion covers conversion data and receipts (spec §14.7).
var ErrInvalidConversion = errorsmod.Register(ModuleName, CodeInvalidConversion.Uint32(), "invalid conversion data")

// ErrInvalidReference covers result references inside a payload (cross-chain
// liquid staking §4.2); ErrPendingFull is returned when an account already
// has max_pending_per_account payloads waiting for their deposit (§4.3).
var (
	ErrInvalidReference = errorsmod.Register(ModuleName, CodeInvalidReference.Uint32(), "invalid result reference")
	ErrPendingFull      = errorsmod.Register(ModuleName, CodePendingFull.Uint32(), "too many pending payloads")
)

// ErrPortConflict is returned when a port-of-entry registration conflicts with
// a direct route of the settlement asset (spec §11.6 D-33, v0.9.11), or when a
// port account withdraws a token that neither has a route to its domain nor
// can leave through the vault's CCTP exit.
var ErrPortConflict = errorsmod.Register(ModuleName, CodePortConflict.Uint32(), "port-of-entry conflict")
