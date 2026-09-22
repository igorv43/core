package liquidstake

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
)

// AutoCLIOptions implements autocli.HasAutoCLIConfig (this chain only
// exposes AutoCLI commands for modules that declare their options).
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: "terra.liquidstake.v1.Query",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "Params", Use: "params", Short: "Query the x/liquidstake parameters"},
				{RpcMethod: "ExchangeRate", Use: "exchange-rate", Short: "Query the stLUNC exchange rate and its components"},
				{RpcMethod: "Delegations", Use: "delegations", Short: "Query the module delegations per validator and the eligibility set"},
				{
					RpcMethod: "UnstakeQueue", Use: "unstake-queue [address]", Short: "Query queued and matured redemptions (optionally of one address)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address", Optional: true}},
				},
				{RpcMethod: "Epoch", Use: "epoch", Short: "Query the current epoch"},
			},
		},
		// Tx commands are hand-written in client/cli (Coin positional args are
		// not supported by this AutoCLI version); AutoCLI only serves queries.
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: "terra.liquidstake.v1.Msg",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "Stake", Skip: true},
				{RpcMethod: "Unstake", Skip: true},
				{RpcMethod: "Claim", Skip: true},
				{RpcMethod: "UpdateParams", Skip: true},
			},
		},
	}
}
