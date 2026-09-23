package types

import (
	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
)

const (
	// ModuleName is the name of the x/perp module and of its module account,
	// which holds every collateral, the insurance fund and the protocol revenue.
	ModuleName = "perp"
	// StoreKey is the store key of the module.
	StoreKey = ModuleName
	// RouterKey is the message route of the module.
	RouterKey = ModuleName

	// Limits in code (spec §12.2, §26.3), not governable.
	MaxLeverageAbsolute            = 10
	MaxAlphaAbsolute               = "0.5"
	MaxOpenPositionsAbsolute       = 50
	MaxTriggersPerAccountAbsolute  = 50
	MaxPremiumWindowAbsolute       = 1_000
	MaxAllocationRecords           = 520 // ~10 years of weekly epochs
	MaxLiquidationsPerBlockDefault = 100
	MaxTriggersPerBlockDefault     = 100
	// MaxFundingReindexPerApply bounds the positions re-indexed when funding is applied.
	MaxFundingReindexPerApply = 5_000
)

var (
	ParamsKey            = collections.NewPrefix(0)
	MarketsKey           = collections.NewPrefix(1)
	PositionsKey         = collections.NewPrefix(2)
	PositionsByMarketKey = collections.NewPrefix(3)
	CollateralKey        = collections.NewPrefix(4)
	ReservationsKey      = collections.NewPrefix(5)
	TriggersKey          = collections.NewPrefix(6)
	TriggerSeqKey        = collections.NewPrefix(7)
	TriggersByAccountKey = collections.NewPrefix(8)
	TriggersByMarketKey  = collections.NewPrefix(9)
	AutoTopUpKey         = collections.NewPrefix(10)
	LedgerKey            = collections.NewPrefix(11)
	AllocationsKey       = collections.NewPrefix(12)
	UnwindIntentsKey     = collections.NewPrefix(13)
	LiqIndexKey          = collections.NewPrefix(14)
	PremiumsKey          = collections.NewPrefix(15)
	CollateralStKey      = collections.NewPrefix(16)
	FeeInLunaKey         = collections.NewPrefix(17)

	// InsuranceFundName derives the account under which the insurance fund
	// holds its inventory positions.
	InsuranceFundName = "insurance"
)

// InsuranceFundAddress is the account of the insurance fund's positions. It
// holds no bank balance: the fund's balance is Ledger.insurance inside the
// module account.
func InsuranceFundAddress() string {
	return sdk.AccAddress(address.Module(ModuleName, []byte(InsuranceFundName))).String()
}
