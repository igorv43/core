package batch

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
)

// AutoCLIOptions exposes the query commands; transactions are hand-written
// in client/cli because this AutoCLI version cannot parse Coin arguments.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: "terra.batch.v1.Query",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "Params", Use: "params", Short: "Query the x/batch parameters"},
				{RpcMethod: "Markets", Use: "markets", Short: "List markets"},
				{
					RpcMethod: "Market", Use: "market [market-id]", Short: "Query a market, its reference price and last result",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "market_id"}},
				},
				{
					RpcMethod: "Intent", Use: "intent [intent-id]", Short: "Query an intent",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "intent_id"}},
				},
				{
					RpcMethod: "IntentsByAccount", Use: "intents [address]", Short: "List the open intents of an account",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod: "Batch", Use: "batch [batch-id]", Short: "Query the results and commits of a batch",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "batch_id"}},
				},
				{
					RpcMethod: "Solver", Use: "solver [address]", Short: "Query a solver and its escrow",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{RpcMethod: "Solvers", Use: "solvers", Short: "List solvers"},
				{
					RpcMethod: "Frontend", Use: "frontend [address]", Short: "Query a registered integrator",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod: "Approvals", Use: "approvals [address]", Short: "List the integrator approvals of an account",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{RpcMethod: "Pipeline", Use: "pipeline", Short: "Show the batch schedule at the current height"},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: "terra.batch.v1.Msg",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "SubmitIntent", Skip: true},
				{RpcMethod: "CancelIntent", Skip: true},
				{RpcMethod: "RegisterSolver", Skip: true},
				{RpcMethod: "UnbondSolver", Skip: true},
				{RpcMethod: "DepositSolverEscrow", Skip: true},
				{RpcMethod: "WithdrawSolverEscrow", Skip: true},
				{RpcMethod: "CommitBid", Skip: true},
				{RpcMethod: "RevealBid", Skip: true},
				{RpcMethod: "RegisterFrontend", Skip: true},
				{RpcMethod: "ApproveFrontend", Skip: true},
				{RpcMethod: "RevokeFrontend", Skip: true},
				{RpcMethod: "CreateMarket", Skip: true},
				{RpcMethod: "SetMarketEnabled", Skip: true},
				{RpcMethod: "UpdateParams", Skip: true},
			},
		},
	}
}
