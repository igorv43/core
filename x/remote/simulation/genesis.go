package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"
)

// Simulation parameter constants.
const paramsKey = "remote_params"

// RandomizedParams draws parameters that satisfy Params.Validate, keeping the
// governance-only knobs (accounts, caps that default to zero) at their default.
func RandomizedParams(r *rand.Rand) types.Params {
	p := types.DefaultParams()
	p.SessionTtlSeconds = int64(1 + r.Intn(60*24*3600))
	p.MaxSessions = uint32(1 + r.Intn(10))
	p.MsgFee = sdk.NewCoin("uusd", math.NewInt(int64(r.Intn(1_000_000))))
	p.MaxMsgsPerPayload = uint32(1 + r.Intn(types.MaxMsgsPerPayloadAbsolute))
	p.PaymasterDailyCap = sdk.NewCoin("uluna", math.NewInt(int64(r.Intn(1_000_000_000))))
	p.PaymasterMinCollateral = math.NewInt(int64(r.Intn(100_000_000)))
	p.PaymasterPeriodSeconds = int64(1 + r.Intn(7*24*3600))
	p.WithdrawFeeCap = sdk.NewCoin("uluna", math.NewInt(int64(r.Intn(1_000_000_000))))
	p.BeaconFeeCap = sdk.NewCoin("uluna", math.NewInt(int64(r.Intn(1_000_000_000))))
	p.MaxBeaconsPerBlock = uint32(1 + r.Intn(50))
	if err := p.Validate(); err != nil {
		panic(err)
	}
	return p
}

// RandomizedGenState generates a random GenesisState for x/remote: random
// parameters over an otherwise empty state (the module starts empty on
// mainnet as well, spec §14).
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
	opWeightMsgUpdateParams      = "op_weight_msg_update_params_remote"
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
