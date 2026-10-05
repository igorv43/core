// Package types holds the Liquidity Fabric policy applied on top of the
// upstream x/circuit module: the message types that no circuit breaker may
// ever disable (spec §24.5, §6.3). It is a leaf package so that both the
// circuit MsgServer wrapper (custom/circuit) and the ante handler
// (custom/auth/ante) share one list.
package types

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	circuitante "cosmossdk.io/x/circuit/ante"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Type URLs of the liquid staking user exit. They are written as constants
// (not derived from x/liquidstake) to keep this package a leaf; a unit test
// asserts they match sdk.MsgTypeURL of the real messages.
const (
	MsgUnstakeTypeURL = "/terra.liquidstake.v1.MsgUnstake"
	MsgClaimTypeURL   = "/terra.liquidstake.v1.MsgClaim"
)

// protectedMsgTypeURLs is the single, non-governable list (spec §26.3) of
// messages that the circuit breaker can never disable: the exit of liquid
// staking must not be pausable by the same group that pauses the rest
// (spec §24.5). Kept unexported so it cannot be mutated at runtime.
var protectedMsgTypeURLs = [...]string{
	MsgUnstakeTypeURL,
	MsgClaimTypeURL,
}

// ProtectedMsgTypeURLs returns a copy of the protected type URL list.
func ProtectedMsgTypeURLs() []string {
	out := make([]string, len(protectedMsgTypeURLs))
	copy(out, protectedMsgTypeURLs[:])
	return out
}

// IsProtected reports whether typeURL can never be disabled by the circuit
// breaker.
func IsProtected(typeURL string) bool {
	for _, u := range protectedMsgTypeURLs {
		if u == typeURL {
			return true
		}
	}
	return false
}

// ErrProtected returns the error used whenever a circuit message names a
// protected type URL. It wraps sdkerrors.ErrUnauthorized (ABCI code 4).
func ErrProtected(typeURL string) error {
	return errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "%s can never be disabled by the circuit breaker", typeURL)
}

// CheckNoProtected returns ErrProtected for the first protected URL in urls.
func CheckNoProtected(urls []string) error {
	for _, u := range urls {
		if IsProtected(u) {
			return ErrProtected(u)
		}
	}
	return nil
}

// ProtectedCircuitBreaker decorates a circuit breaker so that protected type
// URLs are always allowed, even if an entry for them is present in the
// x/circuit disabled list (defence for entries stored before the MsgServer
// guard existed, or written by a keeper path that bypassed it).
type ProtectedCircuitBreaker struct {
	inner circuitante.CircuitBreaker
}

var _ circuitante.CircuitBreaker = ProtectedCircuitBreaker{}

// NewProtectedCircuitBreaker wraps inner.
func NewProtectedCircuitBreaker(inner circuitante.CircuitBreaker) ProtectedCircuitBreaker {
	return ProtectedCircuitBreaker{inner: inner}
}

// IsAllowed implements circuitante.CircuitBreaker.
func (b ProtectedCircuitBreaker) IsAllowed(ctx context.Context, typeURL string) (bool, error) {
	if IsProtected(typeURL) {
		return true, nil
	}
	return b.inner.IsAllowed(ctx, typeURL)
}
