package warpledger

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	"github.com/cosmos/cosmos-sdk/version"
)

// AutoCLIOptions implements autocli.HasAutoCLIConfig. The chain enhances the
// root command with AutoCLI only for modules that declare their options, so
// the descriptors below are what expose `terrad tx|query warpledger`.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: "terra.warpledger.v1.Query",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "Params",
					Use:       "params",
					Short:     "Query the x/warpledger parameters",
				},
				{
					RpcMethod:      "WarpLedger",
					Use:            "ledger [token-id]",
					Short:          "Query the per-domain ledger and solvency figures of a warp token",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "token_id"}},
				},
				{
					RpcMethod: "WarpLedgers",
					Use:       "ledgers",
					Short:     "Query the ledgers of every tracked warp token",
				},
				{
					RpcMethod: "DepositAddress",
					Use:       "deposit-address [token-id] [domain] [recipient]",
					Short:     "Derive the migration deposit address for (token, domain, recipient)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "token_id"},
						{ProtoField: "domain"},
						{ProtoField: "recipient"},
					},
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: "terra.warpledger.v1.Msg",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "SweepMigration",
					Use:       "sweep-migration [token-id] [domain] [recipient]",
					Short:     "Forward the balance of a migration deposit address through the native warp route (permissionless)",
					Example:   version.AppName + " tx warpledger sweep-migration 0x726f757465725f61707000000000000000000001000000000000000000000000 56 0x000000000000000000000000abcdefabcdefabcdefabcdefabcdefabcdefabcd --from anyone",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "token_id"},
						{ProtoField: "domain"},
						{ProtoField: "recipient"},
					},
				},
				{
					RpcMethod: "SetDomainCap",
					Skip:      true, // governance only (authority signer)
				},
				{
					RpcMethod: "UpdateParams",
					Skip:      true, // governance only (authority signer)
				},
			},
		},
	}
}
