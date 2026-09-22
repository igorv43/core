package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper is the subset of x/bank used by x/warpledger.
type BankKeeper interface {
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
}

// CircuitKeeper is the subset of x/circuit used by x/warpledger.
type CircuitKeeper interface {
	// IsAllowed reports whether a message type URL is currently enabled.
	IsAllowed(ctx context.Context, typeURL string) (bool, error)
	// DisableMsg trips the breaker for a message type URL. Reset is only
	// possible through governance (custom ante handler guard).
	DisableMsg(ctx context.Context, typeURL string) error
}
