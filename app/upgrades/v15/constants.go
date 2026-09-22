//nolint:revive
package v15

import (
	store "cosmossdk.io/store/types"
	circuittypes "cosmossdk.io/x/circuit/types"
	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/classic-terra/core/v4/app/upgrades"
	warpledgertypes "github.com/classic-terra/core/v4/x/warpledger/types"
)

// UpgradeName is the on-chain name of the Liquidity Fabric phase 1 upgrade:
// native Hyperlane (x/core + x/warp), the SDK circuit breaker (x/circuit) and
// the per-domain collateral ledger (x/warpledger).
const UpgradeName = "v15"

var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: CreateV15UpgradeHandler,
	StoreUpgrades: store.StoreUpgrades{
		Added: []string{
			circuittypes.StoreKey,
			hyperlanetypes.ModuleName,
			warptypes.ModuleName,
			warpledgertypes.StoreKey,
		},
		Deleted: []string{},
		Renamed: []store.StoreRename{},
	},
}
