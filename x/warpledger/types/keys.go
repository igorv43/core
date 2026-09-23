package types

import "cosmossdk.io/collections"

const (
	// ModuleName is the name of the x/warpledger module.
	ModuleName = "warpledger"
	// StoreKey is the store key of the module.
	StoreKey = ModuleName
	// RouterKey is the message route of the module.
	RouterKey = ModuleName

	// MigrationDerivationPrefix is the first derivation key of migration deposit addresses.
	MigrationDerivationPrefix = "migrate"

	// MaxDomainsPerToken is the absolute maximum number of remote domains a
	// single warp token may have ledgers for. It bounds the EndBlock work of
	// the invariant check (spec §3.2, §12) and is not governable.
	MaxDomainsPerToken = 64
)

var (
	// ParamsKey is the collections prefix of the module parameters.
	ParamsKey = collections.NewPrefix(0)
	// LedgersKey is the collections prefix of the (token, domain) ledgers.
	LedgersKey = collections.NewPrefix(1)
	// BasketsKey is the collections prefix of the basket token set (D-29).
	BasketsKey = collections.NewPrefix(2)
)
