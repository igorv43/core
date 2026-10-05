//nolint:revive
package v17

import (
	"context"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/classic-terra/core/v4/app/keepers"
	"github.com/classic-terra/core/v4/app/upgrades"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

// CreateV17UpgradeHandler adds the x/batch module and the oracle stage 2 parameter. RunMigrations initialises
// it from its default genesis: Annex B parameters and no markets, so no
// auction runs until governance registers a market with MsgSetMarket (spec
// §12). The solver bond and its slashes default to the settlement basket
// `uusdc.lf` (spec §11.4 D-29, §16.1); USTC is refused by Params.Validate
// (§11.3 D-18).
func CreateV17UpgradeHandler(
	mm *module.Manager,
	cfg module.Configurator,
	_ upgrades.BaseAppParamManager,
	keepers *keepers.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		// x/oracle stage 2 adds the asset_whitelist parameter: write it empty
		// so the legacy param set reads without panicking; governance fills it
		// per asset later (spec §21.6).
		keepers.OracleKeeper.EnsureAssetWhitelistParam(sdk.UnwrapSDKContext(ctx))
		return mm.RunMigrations(ctx, cfg, fromVM)
	}
}
