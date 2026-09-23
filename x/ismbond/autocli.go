package ismbond

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
)

// AutoCLIOptions exposes the query commands; transactions are hand-written
// in client/cli because this AutoCLI version cannot parse Coin/bytes arguments.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: "terra.ismbond.v1.Query",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "Params", Use: "params", Short: "Query the x/ismbond parameters"},
				{RpcMethod: "Operator", Use: "operator [address]", Short: "Query a bonded operator",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}}},
				{RpcMethod: "Operators", Use: "operators", Short: "List the bonded operators"},
				{RpcMethod: "IsmBonded", Use: "ism-bonded [ism-id]", Short: "Report whether a multisig ISM is bonded at its threshold",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "ism_id"}}},
				{RpcMethod: "Root", Use: "root [mailbox-id] [index]", Short: "Query the recorded outbound root at an index",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "mailbox_id"}, {ProtoField: "index"}}},
				{RpcMethod: "Roots", Use: "roots [mailbox-id]", Short: "List the recorded outbound roots, newest first",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "mailbox_id"}}},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: "terra.ismbond.v1.Msg",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "BondOperator", Skip: true}, {RpcMethod: "UnbondOperator", Skip: true}, {RpcMethod: "ClaimBond", Skip: true},
				{RpcMethod: "SubmitEvidence", Skip: true}, {RpcMethod: "DistributeRewards", Skip: true}, {RpcMethod: "UpdateParams", Skip: true},
			},
		},
	}
}
