//nolint:revive
package v15

import (
	"context"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/classic-terra/core/v4/app/keepers"
	"github.com/classic-terra/core/v4/app/upgrades"
	"github.com/cosmos/cosmos-sdk/types/module"
)

// CreateV15UpgradeHandler adds the x/circuit, hyperlane x/core and x/warp
// modules. The new modules are absent from the stored version map, so
// RunMigrations initialises them from their default genesis. No parameters
// are set here: mailbox, ISM, hooks, tokens and circuit permissions are
// created by governance transactions after the upgrade.
func CreateV15UpgradeHandler(
	mm *module.Manager,
	cfg module.Configurator,
	_ upgrades.BaseAppParamManager,
	_ *keepers.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return mm.RunMigrations(ctx, cfg, fromVM)
	}
}
