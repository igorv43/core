package remote

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
)

// AutoCLIOptions exposes the query commands; transactions are hand-written
// in client/cli because this AutoCLI version cannot parse Coin/hex arguments.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: "terra.remote.v1.Query",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "Params", Use: "params", Short: "Query the x/remote parameters"},
				{RpcMethod: "RemoteApps", Use: "apps", Short: "List the remote apps and their gateways"},
				{
					RpcMethod: "DeriveAddress", Use: "derive [domain] [controller-hex]", Short: "Derive the account of (domain, controller)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "domain"}, {ProtoField: "controller"}},
				},
				{
					RpcMethod: "RemoteAccount", Use: "account [domain] [controller-hex]", Short: "Query a remote account, its balances and session keys",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "domain"}, {ProtoField: "controller"}},
				},
				{
					RpcMethod: "Sessions", Use: "sessions [address]", Short: "List the session keys of a remote account",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod: "Withdrawals", Use: "withdrawals [address]", Short: "List the withdrawals of a remote account",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{RpcMethod: "Paymaster", Use: "paymaster", Short: "Query the paymaster account and balance"},
				{RpcMethod: "Beacons", Use: "beacons", Short: "List the state beacons"},
				{
					RpcMethod: "BeaconBody", Use: "beacon-body [id]", Short: "Show the words a beacon would send now",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: "terra.remote.v1.Msg",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "GrantSessionKey", Skip: true},
				{RpcMethod: "RevokeSessionKey", Skip: true},
				{RpcMethod: "RevokeOwnSessionKey", Skip: true},
				{RpcMethod: "Withdraw", Skip: true},
				{RpcMethod: "FundPaymaster", Skip: true},
				{RpcMethod: "CreateRemoteApp", Skip: true},
				{RpcMethod: "SetGateway", Skip: true},
				{RpcMethod: "SetBeacon", Skip: true},
				{RpcMethod: "UpdateParams", Skip: true},
			},
		},
	}
}
