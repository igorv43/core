package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Spec §16.1 / §21.5 rule 1: solver bonds are in the settlement asset
// (uusdc.lf, §11.4 D-29); §11.3 (D-18): never in USTC.
func TestSolverBondIsUsdcNeverUstc(t *testing.T) {
	p := types.DefaultParams()
	require.Equal(t, "uusdc.lf", p.SolverBondMin.Denom)
	require.Equal(t, "uusdc.lf", p.SlashNoReveal.Denom)
	require.Equal(t, "uluna", p.IntentFee.Denom, "the intent fee goes to the chain fee distribution (§23)")
	require.NoError(t, p.Validate())

	p.SolverBondMin = sdk.NewCoin("uusd", math.NewInt(1))
	p.SlashNoReveal = sdk.NewCoin("uusd", math.NewInt(1))
	require.ErrorContains(t, p.Validate(), "D-18")
}
