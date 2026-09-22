//nolint:revive
package v16

import (
	store "cosmossdk.io/store/types"
	"github.com/classic-terra/core/v4/app/upgrades"
	liquidstaketypes "github.com/classic-terra/core/v4/x/liquidstake/types"
)

// UpgradeName is the on-chain name of the Liquidity Fabric phase 6 upgrade:
// native liquid staking (x/liquidstake, stLUNC).
const UpgradeName = "v16"

var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: CreateV16UpgradeHandler,
	StoreUpgrades: store.StoreUpgrades{
		Added:   []string{liquidstaketypes.StoreKey},
		Deleted: []string{},
		Renamed: []store.StoreRename{},
	},
}
