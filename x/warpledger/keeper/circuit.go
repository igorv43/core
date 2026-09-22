package keeper

import (
	"context"

	circuitkeeper "cosmossdk.io/x/circuit/keeper"
	"github.com/classic-terra/core/v4/x/warpledger/types"
)

// circuitAdapter adapts the SDK x/circuit keeper to types.CircuitKeeper.
// The SDK keeper exposes tripping only through its message server; the
// adapter writes the disable list directly so the invariant check can pause
// outbound transfers from EndBlock without a transaction (spec §9.3).
type circuitAdapter struct {
	k *circuitkeeper.Keeper
}

// NewCircuitAdapter wraps the SDK circuit keeper.
func NewCircuitAdapter(k *circuitkeeper.Keeper) types.CircuitKeeper {
	return circuitAdapter{k: k}
}

func (c circuitAdapter) IsAllowed(ctx context.Context, typeURL string) (bool, error) {
	return c.k.IsAllowed(ctx, typeURL)
}

func (c circuitAdapter) DisableMsg(ctx context.Context, typeURL string) error {
	return c.k.DisableList.Set(ctx, typeURL)
}
