package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"
)

// Simulation parameter constants.
const paramsKey = "liquidstake_params"

// RandomizedParams draws parameters that satisfy Params.Validate, keeping the
// governance-only knobs (accounts, caps that default to zero) at their default.
func RandomizedParams(r *rand.Rand) types.Params {
	p := types.DefaultParams()
	p.EpochBlocks = int64(1 + r.Intn(10_000))
	p.FeeRate = math.LegacyNewDecWithPrec(int64(r.Intn(30)), 2)
	p.BurnShare = math.LegacyNewDecWithPrec(int64(r.Intn(100)), 2)
	p.MaxShare = math.LegacyNewDecWithPrec(int64(5+r.Intn(50)), 2)
	p.BufferRatio = math.LegacyNewDecWithPrec(int64(r.Intn(10)), 2)
	p.MaxCommission = math.LegacyNewDecWithPrec(int64(5+r.Intn(50)), 2)
	p.MinUptime = math.LegacyNewDecWithPrec(int64(50+r.Intn(50)), 2)
	// validator_cap ≥ 1/max_validators: pick the cap first, then a compatible count
	cap := int64(5 + r.Intn(46)) // 5%..50%
	p.ValidatorCap = math.LegacyNewDecWithPrec(cap, 2)
	minVals := int(math.LegacyOneDec().Quo(p.ValidatorCap).Ceil().TruncateInt64())
	p.MaxValidators = uint32(minVals + r.Intn(types.MaxValidatorsAbsolute-minVals+1))
	if err := p.Validate(); err != nil {
		panic(err)
	}
	return p
}

// RandomizedGenState generates a random GenesisState for x/liquidstake: random
// parameters over an otherwise empty state (the module starts empty on
// mainnet as well, spec §6).
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
	opWeightMsgUpdateParams      = "op_weight_msg_update_params_liquidstake"
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
