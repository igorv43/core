package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"
)

// Simulation parameter constants.
const paramsKey = "batch_params"

// RandomizedParams draws parameters that satisfy Params.Validate, keeping the
// governance-only knobs (accounts, caps that default to zero) at their default.
func RandomizedParams(r *rand.Rand) types.Params {
	p := types.DefaultParams()
	p.CommitWindow = int64(1 + r.Intn(10))
	p.PriceBand = math.LegacyNewDecWithPrec(int64(1+r.Intn(50)), 2)
	p.IntentTtlBlocks = int64(1 + r.Intn(types.MaxIntentTTLAbsolute))
	p.IntentFee = sdk.NewCoin("uluna", math.NewInt(int64(r.Intn(10_000_000))))
	p.MaxIntentsPerBatch = uint32(1 + r.Intn(types.MaxIntentsPerBatchAbsolute))
	p.MaxSolversPerBatch = uint32(1 + r.Intn(types.MaxSolversPerBatchAbsolute))
	p.MaxLevelsPerBid = uint32(1 + r.Intn(types.MaxLevelsPerBidAbsolute))
	p.MaxActiveMarkets = uint32(1 + r.Intn(types.MaxActiveMarketsAbsolute))
	p.MaxResolutionPasses = uint32(1 + r.Intn(types.MaxResolutionPassesAbsolute))
	p.SpotFeeBps = uint32(r.Intn(100))
	p.BuilderFeeMaxBps = uint32(r.Intn(types.MaxBuilderFeeBpsAbsolute + 1))
	p.SolverBondMin = sdk.NewCoin(types.DefaultSettlementDenom, math.NewInt(int64(1+r.Intn(100_000_000_000))))
	p.SlashNoReveal = sdk.NewCoin(types.DefaultSettlementDenom, math.NewInt(int64(r.Intn(10_000_000_000))))
	p.SolverUnbondBlocks = int64(1 + r.Intn(200_000))
	p.SolverSuspensionBlocks = int64(1 + r.Intn(50_000))
	p.PruneDelayBlocks = int64(1 + r.Intn(200_000))
	p.MaxFrontendApprovals = uint32(1 + r.Intn(100))
	if err := p.Validate(); err != nil {
		panic(err)
	}
	return p
}

// RandomizedGenState generates a random GenesisState for x/batch: random
// parameters over an otherwise empty state (the module starts empty on
// mainnet as well, spec §13).
func RandomizedGenState(simState *module.SimulationState) {
	var params types.Params
	simState.AppParams.GetOrGenerate(paramsKey, &params, simState.Rand, func(r *rand.Rand) { params = RandomizedParams(r) })
	gs := types.DefaultGenesisState()
	gs.Params = params
	if err := gs.Validate(); err != nil {
		panic(err)
	}
	bz, err := json.MarshalIndent(&gs.Params, "", " ")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Selected randomly generated %s parameters:\n%s\n", types.ModuleName, bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(gs)
}

// Weight of the parameter-update proposal in the simulated governance.
const (
	opWeightMsgUpdateParams      = "op_weight_msg_update_params_batch"
	defaultWeightMsgUpdateParams = 50
)

// ProposalMsgs returns the governance messages the simulation may submit:
// a MsgUpdateParams with random but valid parameters.
func ProposalMsgs(authority string) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{
		simulation.NewWeightedProposalMsg(opWeightMsgUpdateParams, defaultWeightMsgUpdateParams,
			func(r *rand.Rand, _ sdk.Context, _ []simtypes.Account) sdk.Msg {
				return &types.MsgUpdateParams{Authority: authority, Params: RandomizedParams(r)}
			}),
	}
}

var _ = math.ZeroInt
