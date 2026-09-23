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
	roots  RootRecorder
}

// NewAppModule creates a new wrapped warp AppModule; the optional recorder
// is x/ismbond (root history of outbound messages).
func NewAppModule(cdc codec.Codec, keeper warpkeeper.Keeper, ledger warpledgerkeeper.Keeper, recorders ...RootRecorder) AppModule {
	am := AppModule{
		AppModule: warp.NewAppModule(cdc, keeper),
		keeper:    keeper,
		ledger:    ledger,
	}
	if len(recorders) > 0 {
		am.roots = recorders[0]
	}
	return am
}

// RegisterServices registers the wrapped message server and the upstream query
// server.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	upstream := warpkeeper.NewMsgServerImpl(am.keeper)
	if am.roots != nil {
		warptypes.RegisterMsgServer(cfg.MsgServer(), NewMsgServer(upstream, am.ledger, am.roots))
	} else {
		warptypes.RegisterMsgServer(cfg.MsgServer(), NewMsgServer(upstream, am.ledger))
	}
	warptypes.RegisterQueryServer(cfg.QueryServer(), warpkeeper.NewQueryServerImpl(am.keeper))
}
