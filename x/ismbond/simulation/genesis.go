package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/ismbond/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"
)

// Simulation parameter constants.
const paramsKey = "ismbond_params"

// RandomizedParams draws parameters that satisfy Params.Validate, keeping the
// governance-only knobs (accounts, caps that default to zero) at their default.
func RandomizedParams(r *rand.Rand) types.Params {
	p := types.DefaultParams()
	p.BondMin = sdk.NewCoin("uluna", math.NewInt(int64(1+r.Intn(10_000_000_000_000))))
	p.DisputeWindowBlocks = int64(1 + r.Intn(100_000))
	p.UnbondDelayBlocks = p.DisputeWindowBlocks + int64(r.Intn(100_000))
	p.EvidenceReward = math.LegacyNewDecWithPrec(int64(r.Intn(100)), 2)
	if err := p.Validate(); err != nil {
		panic(err)
	}
	return p
}

// RandomizedGenState generates a random GenesisState for x/ismbond: random
// parameters over an otherwise empty state (the module starts empty on
// mainnet as well, spec §6.6).
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
	opWeightMsgUpdateParams      = "op_weight_msg_update_params_ismbond"
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
