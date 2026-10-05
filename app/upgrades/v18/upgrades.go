//nolint:revive
package v18

import (
	"context"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/classic-terra/core/v4/app/keepers"
	"github.com/classic-terra/core/v4/app/upgrades"
	"github.com/cosmos/cosmos-sdk/types/module"
)

// CreateV18UpgradeHandler adds the x/perp module. RunMigrations initialises
// it from its default genesis: Annex B parameters, no market, empty
// insurance fund. Governance registers markets with MsgCreateMarket and
// enables them with MsgEnableMarket once the listing criteria of spec §21.3
// hold; the buyback spot market is set with MsgUpdateParams. The settlement
// denom defaults to the USDC basket `uusdc.lf` (spec §11.4 D-29) and can never
// be USTC (§11.3 D-18, enforced by Params.Validate).
func CreateV18UpgradeHandler(
	mm *module.Manager,
	cfg module.Configurator,
	_ upgrades.BaseAppParamManager,
	_ *keepers.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return mm.RunMigrations(ctx, cfg, fromVM)
	}
}
