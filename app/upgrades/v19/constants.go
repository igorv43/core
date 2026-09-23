//nolint:revive
package v19

import (
	store "cosmossdk.io/store/types"
	"github.com/classic-terra/core/v4/app/upgrades"
	ismbondtypes "github.com/classic-terra/core/v4/x/ismbond/types"
	remotetypes "github.com/classic-terra/core/v4/x/remote/types"
)

// UpgradeName is the on-chain name of the Liquidity Fabric upgrade that adds
// remote accounts (x/remote, D-19/D-27) and ISM operator bonds (x/ismbond, D-17).
const UpgradeName = "v19"

var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: CreateV19UpgradeHandler,
	StoreUpgrades: store.StoreUpgrades{
		Added:   []string{remotetypes.StoreKey, ismbondtypes.StoreKey},
		Deleted: []string{},
		Renamed: []store.StoreRename{},
	},
}
