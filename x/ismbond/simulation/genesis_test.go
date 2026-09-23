package simulation_test

import (
	"encoding/json"
	"math/rand"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/ismbond/simulation"
	"github.com/classic-terra/core/v4/x/ismbond/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"
)

// TestRandomizedGenState checks that every seed yields a genesis that
// validates and that the parameters are actually randomised.
func TestRandomizedGenState(t *testing.T) {
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	seen := map[string]bool{}
	for seed := int64(1); seed <= 20; seed++ {
		r := rand.New(rand.NewSource(seed))
		simState := module.SimulationState{
			AppParams: make(simtypes.AppParams), Cdc: cdc, Rand: r, NumBonded: 3, BondDenom: sdk.DefaultBondDenom,
			Accounts: simtypes.RandomAccounts(r, 3), InitialStake: sdkmath.NewInt(1000), GenState: make(map[string]json.RawMessage),
		}
		simulation.RandomizedGenState(&simState)
		var gs types.GenesisState
		cdc.MustUnmarshalJSON(simState.GenState[types.ModuleName], &gs)
		require.NoError(t, gs.Validate())
		seen[gs.Params.String()] = true
	}
	require.Greater(t, len(seen), 1, "parameters must vary with the seed")
}

func TestProposalMsgs(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	msgs := simulation.ProposalMsgs("authority")
	require.Len(t, msgs, 1)
	msg, ok := msgs[0].MsgSimulatorFn()(r, sdk.Context{}, nil).(*types.MsgUpdateParams)
	require.True(t, ok)
	require.Equal(t, "authority", msg.Authority)
	require.NoError(t, msg.Params.Validate())
}
