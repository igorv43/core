//nolint:revive
package v18

import (
	store "cosmossdk.io/store/types"
	"github.com/classic-terra/core/v4/app/upgrades"
	perptypes "github.com/classic-terra/core/v4/x/perp/types"
)

// UpgradeName is the on-chain name of the Liquidity Fabric Part II upgrade
// that adds the perpetuals module (x/perp). Release gate: G-06 (spec §30.1).
const UpgradeName = "v18"

var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: CreateV18UpgradeHandler,
	StoreUpgrades: store.StoreUpgrades{
		Added:   []string{perptypes.StoreKey},
		Deleted: []string{},
		Renamed: []store.StoreRename{},
	},
}
