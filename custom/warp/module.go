package warp

import (
	"github.com/bcp-innovations/hyperlane-cosmos/x/warp"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	warpledgerkeeper "github.com/classic-terra/core/v4/x/warpledger/keeper"
	warpledgertypes "github.com/classic-terra/core/v4/x/warpledger/types"
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
	ports  warpledgertypes.PortRegistry
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

// WithPortRegistry returns the module with x/remote's port registry, which
// guards router enrollment of settlement basket tokens (spec §11.6 D-33).
func (am AppModule) WithPortRegistry(ports warpledgertypes.PortRegistry) AppModule {
	am.ports = ports
	return am
}

// RegisterServices registers the wrapped message server and the upstream query
// server.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	upstream := warpkeeper.NewMsgServerImpl(am.keeper)
	if am.roots != nil {
		warptypes.RegisterMsgServer(cfg.MsgServer(), NewMsgServerWithPorts(upstream, am.ledger, am.ports, am.roots))
	} else {
		warptypes.RegisterMsgServer(cfg.MsgServer(), NewMsgServerWithPorts(upstream, am.ledger, am.ports))
	}
	warptypes.RegisterQueryServer(cfg.QueryServer(), warpkeeper.NewQueryServerImpl(am.keeper))
}
