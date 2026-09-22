//nolint:revive
package v17

import (
	store "cosmossdk.io/store/types"
	"github.com/classic-terra/core/v4/app/upgrades"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
)

// UpgradeName is the on-chain name of the Liquidity Fabric Part II upgrade
// that adds the sealed-bid batch auction module (x/batch).
const UpgradeName = "v17"

var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: CreateV17UpgradeHandler,
	StoreUpgrades: store.StoreUpgrades{
		Added:   []string{batchtypes.StoreKey},
		Deleted: []string{},
		Renamed: []store.StoreRename{},
	},
}
