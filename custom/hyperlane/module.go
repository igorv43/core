package hyperlane

import (
	hyperlanecore "github.com/bcp-innovations/hyperlane-cosmos/x/core"
	ismmodule "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security"
	ismkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/keeper"
	pdmodule "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch"
	pdkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/keeper"
	corekeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/keeper"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warpledgerkeeper "github.com/classic-terra/core/v4/x/warpledger/keeper"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/module"
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModule{}
)

// AppModule wraps the upstream Hyperlane core module (mailbox, ISM, hooks) so
// that Terra Classic can interpose logic on MsgProcessMessage without patching
// the dependency. Everything else is delegated to the upstream module.
type AppModule struct {
	hyperlanecore.AppModule

	keeper *corekeeper.Keeper
	ledger warpledgerkeeper.Keeper
}

// NewAppModule creates a new wrapped hyperlane core AppModule.
func NewAppModule(cdc codec.Codec, keeper *corekeeper.Keeper, ledger warpledgerkeeper.Keeper) AppModule {
	return AppModule{
		AppModule: hyperlanecore.NewAppModule(cdc, keeper),
		keeper:    keeper,
		ledger:    ledger,
	}
}

// RegisterServices mirrors the upstream registration, replacing only the core
// message server with the wrapped one. The wrapper is the interposition point
// for x/warpledger inbound accounting (received_d, spec §9.5).
func (am AppModule) RegisterServices(cfg module.Configurator) {
	upstream := corekeeper.NewMsgServerImpl(am.keeper)
	coretypes.RegisterMsgServer(cfg.MsgServer(), NewMsgServer(upstream, am.ledger))
	coretypes.RegisterQueryServer(cfg.QueryServer(), corekeeper.NewQueryServerImpl(am.keeper))

	ismmodule.RegisterMsgServer(cfg.MsgServer(), ismkeeper.NewMsgServerImpl(&am.keeper.IsmKeeper))
	ismmodule.RegisterQueryService(cfg.QueryServer(), ismkeeper.NewQueryServerImpl(&am.keeper.IsmKeeper))

	pdmodule.RegisterMsgServer(cfg.MsgServer(), pdkeeper.NewMsgServerImpl(&am.keeper.PostDispatchKeeper))
	pdmodule.RegisterQueryService(cfg.QueryServer(), pdkeeper.NewQueryServerImpl(&am.keeper.PostDispatchKeeper))
}
