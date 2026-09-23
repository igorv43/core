package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"
)

// Simulation parameter constants.
const paramsKey = "perp_params"

// RandomizedParams draws parameters that satisfy Params.Validate, keeping the
// governance-only knobs (accounts, caps that default to zero) at their default.
func RandomizedParams(r *rand.Rand) types.Params {
	p := types.DefaultParams()
	p.LiqPenalty = math.LegacyNewDecWithPrec(int64(r.Intn(10)), 2)
	p.IfUnwindPerBatch = math.LegacyNewDecWithPrec(int64(1+r.Intn(50)), 2)
	p.IfMaxInventory = math.LegacyNewDecWithPrec(int64(1+r.Intn(50)), 2)
	p.IfFeeShare = math.LegacyNewDecWithPrec(int64(r.Intn(100)), 2)
	p.Beta = math.LegacyNewDec(int64(1 + r.Intn(10)))
	p.PremiumWindow = uint32(1 + r.Intn(types.MaxPremiumWindowAbsolute))
	p.PremiumClamp = math.LegacyNewDecWithPrec(int64(1+r.Intn(50)), 3)
	p.FundingInterval = int64(1 + r.Intn(10_000))
	p.FundingRateMax = math.LegacyNewDecWithPrec(int64(1+r.Intn(100)), 4)
	restricted := int64(1 + r.Intn(20))
	p.DispersionRestricted = math.LegacyNewDecWithPrec(restricted, 3)
	p.DispersionReduceOnly = math.LegacyNewDecWithPrec(restricted+int64(r.Intn(20)), 3)
	p.PerpFeeBps = uint32(r.Intn(100))
	p.MaxOpenPositionsPerAccount = uint32(1 + r.Intn(types.MaxOpenPositionsAbsolute))
	p.MaxTriggersPerAccount = uint32(1 + r.Intn(types.MaxTriggersPerAccountAbsolute))
	p.TriggerSlippageDefault = math.LegacyNewDecWithPrec(int64(1+r.Intn(10)), 2)
	p.RiskStep = math.LegacyNewDecWithPrec(int64(1+r.Intn(50)), 2)
	p.AllocEpochBlocks = int64(1 + r.Intn(200_000))
	op := int64(r.Intn(101))
	cp := int64(r.Intn(int(101 - op)))
	p.SplitOp, p.SplitCp, p.SplitBurn = math.LegacyNewDecWithPrec(op, 2), math.LegacyNewDecWithPrec(cp, 2), math.LegacyNewDecWithPrec(100-op-cp, 2)
	p.MaxLiquidationsPerBlock = uint32(1 + r.Intn(500))
	p.MaxTriggersPerBlock = uint32(1 + r.Intn(500))
	p.StHaircut = math.LegacyNewDecWithPrec(int64(10+r.Intn(80)), 2)
	p.StShareCap = math.LegacyNewDecWithPrec(int64(1+r.Intn(99)), 2)
	p.StGlobalCapIfRatio = math.LegacyNewDec(int64(r.Intn(5)))
	p.StHaircutQueueSlope = math.LegacyNewDecWithPrec(int64(r.Intn(100)), 2)
	p.LunaFeeDiscount = math.LegacyNewDecWithPrec(int64(r.Intn(50)), 2)
	if err := p.Validate(); err != nil {
		panic(err)
	}
	return p
}

// RandomizedGenState generates a random GenesisState for x/perp: random
// parameters over an otherwise empty state (the module starts empty on
// mainnet as well, spec §17).
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
	opWeightMsgUpdateParams      = "op_weight_msg_update_params_perp"
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
