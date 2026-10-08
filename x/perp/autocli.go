package perp

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
)

// positional argument fields shared by several queries
const (
	argMarketId = "market_id"
	argAccount  = "account"
)

// AutoCLIOptions exposes the query commands; transactions are hand-written
// in client/cli because this AutoCLI version cannot parse Coin/Dec arguments.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: "terra.perp.v1.Query",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "Params", Use: "params", Short: "Query the x/perp parameters"},
				{RpcMethod: "Markets", Use: "markets", Short: "List perp markets with mark, funding, OI and oracle state"},
				{
					RpcMethod: "Market", Use: "market [market-id]", Short: "Query a perp market",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: argMarketId}},
				},
				{
					RpcMethod: "Position", Use: "position [account] [market-id]", Short: "Query a position with equity and liquidation price",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: argAccount}, {ProtoField: argMarketId}},
				},
				{
					RpcMethod: "Positions", Use: "positions [account]", Short: "List the positions of an account",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: argAccount}},
				},
				{
					RpcMethod: "Collateral", Use: "collateral [account]", Short: "Query the collateral of an account",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: argAccount}},
				},
				{
					RpcMethod: "Triggers", Use: "triggers [account]", Short: "List the trigger orders of an account",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: argAccount}},
				},
				{RpcMethod: "InsuranceFund", Use: "insurance-fund", Short: "Query the insurance fund balance, target and inventory"},
				{
					RpcMethod: "ADLRank", Use: "adl-rank [market-id]", Short: "Rank the positions of a market by ADL score",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: argMarketId}},
				},
				{
					RpcMethod: "ListingCheck", Use: "listing-check [market-id]", Short: "Evaluate the listing criteria of a market",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: argMarketId}},
				},
				{RpcMethod: "Allocations", Use: "allocations", Short: "List the allocation cascade records"},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: "terra.perp.v1.Msg",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "DepositCollateral", Skip: true},
				{RpcMethod: "WithdrawCollateral", Skip: true},
				{RpcMethod: "SubmitPerpIntent", Skip: true},
				{RpcMethod: "SubmitTriggerOrder", Skip: true},
				{RpcMethod: "CancelTriggerOrder", Skip: true},
				{RpcMethod: "SetAutoTopUp", Skip: true},
				{RpcMethod: "FundInsurance", Skip: true},
				{RpcMethod: "SetFeeInLuna", Skip: true},
				{RpcMethod: "CreateMarket", Skip: true},
				{RpcMethod: "UpdateMarket", Skip: true},
				{RpcMethod: "EnableMarket", Skip: true},
				{RpcMethod: "UpdateParams", Skip: true},
			},
		},
	}
}
