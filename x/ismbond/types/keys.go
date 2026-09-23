package types

import "cosmossdk.io/collections"

const (
	// ModuleName is the name of the x/ismbond module and of the account that holds the bonds.
	ModuleName = "ismbond"
	// StoreKey is the store key of the module.
	StoreKey = ModuleName
	// RouterKey is the message route of the module.
	RouterKey = ModuleName

	// MaxRootsPerMailbox bounds the root history kept per mailbox regardless of the window.
	MaxRootsPerMailbox = 100_000
)

var (
	ParamsKey      = collections.NewPrefix(0)
	OperatorsKey   = collections.NewPrefix(1)
	ByValidatorKey = collections.NewPrefix(2)
	RootsKey       = collections.NewPrefix(3)
)
