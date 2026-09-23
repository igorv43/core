//nolint:revive
package v19

import (
	"context"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/classic-terra/core/v4/app/keepers"
	"github.com/classic-terra/core/v4/app/upgrades"
	"github.com/cosmos/cosmos-sdk/types/module"
)

// CreateV19UpgradeHandler adds x/remote and x/ismbond from their default
// genesis. Governance registers the remote app for the mailbox
// (MsgCreateRemoteApp), enrols gateways, funds the paymaster, and sets the
// x/warpledger bonded_cap_threshold once operators have bonded. The
// warpledger parameter set gains bonded_cap_threshold = 0 (rule disabled)
// without migration: the stored proto decodes the new field as zero.
func CreateV19UpgradeHandler(
	mm *module.Manager,
	cfg module.Configurator,
	_ upgrades.BaseAppParamManager,
	_ *keepers.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return mm.RunMigrations(ctx, cfg, fromVM)
	}
}
