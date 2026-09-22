package types

import "cosmossdk.io/collections"

const (
	// ModuleName is the name of the x/liquidstake module and of its module account.
	ModuleName = "liquidstake"
	// StoreKey is the store key of the module.
	StoreKey = ModuleName
	// RouterKey is the message route of the module.
	RouterKey = ModuleName

	// StDenom is the base denom of the liquid staking token (display: stLUNC).
	StDenom = "stluna"
	// StDisplayDenom is the display denom of the liquid staking token.
	StDisplayDenom = "stLUNC"
	// StExponent is the number of decimals between StDenom and StDisplayDenom.
	StExponent = 6

	// MaxValidatorsAbsolute bounds Params.MaxValidators: it caps the epoch work
	// (rewards withdrawal, delegation and undelegation loops). Not governable.
	MaxValidatorsAbsolute = 130
	// MaxRequestsPerClaim bounds the requests paid by one MsgClaim.
	MaxRequestsPerClaim = 100
)

var (
	ParamsKey        = collections.NewPrefix(0)
	EpochKey         = collections.NewPrefix(1)
	RequestsKey      = collections.NewPrefix(2)
	RequestSeqKey    = collections.NewPrefix(3)
	OwedKey          = collections.NewPrefix(4)
	ValidatorsKey    = collections.NewPrefix(5)
	RequestsByAddrIx = collections.NewPrefix(6)
)
