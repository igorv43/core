package types

import "cosmossdk.io/collections"

const (
	// ModuleName is the name of the x/batch module and of its escrow account.
	ModuleName = "batch"
	// StoreKey is the store key of the module.
	StoreKey = ModuleName
	// RouterKey is the message route of the module.
	RouterKey = ModuleName

	// Absolute maxima of spec §12.2, enforced in code and not governable.
	MaxIntentsPerBatchAbsolute  = 5_000
	MaxSolversPerBatchAbsolute  = 50
	MaxLevelsPerBidAbsolute     = 20
	MaxActiveMarketsAbsolute    = 16
	MaxIntentTTLAbsolute        = 14_400
	MaxResolutionPassesAbsolute = 10
	// MaxIntentsPerAccount bounds the intents one account may keep open.
	MaxIntentsPerAccount = 50
	// MaxExpiredPerBlock bounds the expiry sweep of EndBlock.
	MaxExpiredPerBlock = 500
	// RevealRateWindowBlocks and MinRevealRate implement spec §16.2 (rate < 90% in 1,000 blocks).
	RevealRateWindowBlocks = 1_000
	MinRevealRateBps       = 9_000
	// MaxBuilderFeeBpsAbsolute caps params.builder_fee_max_bps.
	MaxBuilderFeeBpsAbsolute = 100
)

var (
	ParamsKey           = collections.NewPrefix(0)
	MarketsKey          = collections.NewPrefix(1)
	IntentsKey          = collections.NewPrefix(2)
	IntentSeqKey        = collections.NewPrefix(3)
	IntentsByAccountKey = collections.NewPrefix(4)
	IntentsByMarketKey  = collections.NewPrefix(5)
	SolversKey          = collections.NewPrefix(6)
	SolverEscrowKey     = collections.NewPrefix(7)
	CommitsKey          = collections.NewPrefix(8)
	CommitSeqKey        = collections.NewPrefix(9)
	ResultsKey          = collections.NewPrefix(10)
	FrontendsKey        = collections.NewPrefix(11)
	ApprovalsKey        = collections.NewPrefix(12)
)
