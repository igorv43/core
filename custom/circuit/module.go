package circuit

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	circuitv1 "cosmossdk.io/api/cosmos/circuit/v1"
	"cosmossdk.io/x/circuit"
	circuitkeeper "cosmossdk.io/x/circuit/keeper"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/module"
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
)

// AppModuleBasic wraps the upstream x/circuit basic module.
type AppModuleBasic struct {
	circuit.AppModuleBasic
}

// AppModule wraps the upstream x/circuit module. The only override is
// AutoCLIOptions: upstream declares nested positional arguments
// (`permissions.level`) that the AutoCLI shipped with this SDK line cannot
// resolve and that make `terrad` panic at start-up. The tx commands keep the
// same names; the permissions of `authorize` are given as flags instead.
type AppModule struct {
	circuit.AppModule
}

// NewAppModule creates a new wrapped x/circuit AppModule.
func NewAppModule(cdc codec.Codec, keeper circuitkeeper.Keeper) AppModule {
	return AppModule{AppModule: circuit.NewAppModule(cdc, keeper)}
}

// AutoCLIOptions implements autocli.HasAutoCLIConfig without nested
// positional arguments.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: circuitv1.Query_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod:      "Account",
					Use:            "account [address]",
					Short:          "Query a specific account's permissions",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod: "Accounts",
					Use:       "accounts",
					Short:     "Query all account permissions",
				},
				{
					RpcMethod: "DisabledList",
					Use:       "disabled-list",
					Short:     "Query a list of all disabled message types",
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: circuitv1.Msg_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "AuthorizeCircuitBreaker",
					Use:       "authorize [grantee] --permissions <json> --from [granter]",
					Short:     "Authorize an account to trip the circuit breaker (governance).",
					Long: `Authorize an account to trip the circuit breaker. Permissions are a JSON
object: {"level":"LEVEL_SOME_MSGS","limit_type_urls":["/hyperlane.warp.v1.MsgRemoteTransfer"]}.
Levels: LEVEL_SOME_MSGS, LEVEL_ALL_MSGS, LEVEL_SUPER_ADMIN. Reset is always
restricted to the governance authority on Terra Classic.`,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "grantee"},
					},
				},
				{
					RpcMethod: "TripCircuitBreaker",
					Use:       "disable [msg_type_urls]",
					Short:     "Disable a message type from being executed",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "msg_type_urls", Varargs: true},
					},
				},
				{
					RpcMethod: "ResetCircuitBreaker",
					Use:       "reset [msg_type_urls]",
					Short:     "Re-enable a message type (governance authority only)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "msg_type_urls", Varargs: true},
					},
				},
			},
		},
	}
}
