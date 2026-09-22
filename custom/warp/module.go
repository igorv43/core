package warp

import (
	"github.com/bcp-innovations/hyperlane-cosmos/x/warp"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	warpledgerkeeper "github.com/classic-terra/core/v4/x/warpledger/keeper"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/module"
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModule{}
)

// AppModule wraps the upstream Hyperlane warp module so that Terra Classic can
// interpose x/warpledger on its message server (the same idiom used by x/tax
// for bank and market) without patching the dependency. Everything else is
// delegated to the upstream module.
type AppModule struct {
	warp.AppModule

	keeper warpkeeper.Keeper
	ledger warpledgerkeeper.Keeper
}

// NewAppModule creates a new wrapped warp AppModule.
func NewAppModule(cdc codec.Codec, keeper warpkeeper.Keeper, ledger warpledgerkeeper.Keeper) AppModule {
	return AppModule{
		AppModule: warp.NewAppModule(cdc, keeper),
		keeper:    keeper,
		ledger:    ledger,
	}
}

// RegisterServices registers the wrapped message server and the upstream query
// server.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	upstream := warpkeeper.NewMsgServerImpl(am.keeper)
	warptypes.RegisterMsgServer(cfg.MsgServer(), NewMsgServer(upstream, am.ledger))
	warptypes.RegisterQueryServer(cfg.QueryServer(), warpkeeper.NewQueryServerImpl(am.keeper))
}
