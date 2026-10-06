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
	require.Equal(t, "uusdc.lf", p.IntentFee.Denom, "the intent fee is in the settlement asset (spec v0.9.10 §23, Annex B)")
	require.NoError(t, p.Validate())

	p.SolverBondMin = sdk.NewCoin("uusd", math.NewInt(1))
	p.SlashNoReveal = sdk.NewCoin("uusd", math.NewInt(1))
	require.ErrorContains(t, p.Validate(), "D-18")
}

// Spec v0.9.10 §23, §23.2, §16 and Annex B: the market-calibrated defaults.
func TestDefaultFeesFollowAnnexB(t *testing.T) {
	p := types.DefaultParams()
	require.Equal(t, "1000uusdc.lf", p.IntentFee.String()) // ~US$0.001
	require.Equal(t, uint32(5), p.SpotFeeBps)
	require.Equal(t, uint32(0), p.SpotSolverFeeBps)
	require.Equal(t, uint32(10), p.BuilderFeeMaxBps)
	require.Equal(t, uint32(50), p.BuilderFeeMaxSpotBps)
	require.Equal(t, "1000000000uusdc.lf", p.SlashNoReveal.String())
	// Annex B: bond of the order of 10x the largest slash (inconsistent reveal = 2x slash_no_reveal)
	require.Equal(t, p.SlashNoReveal.Amount.MulRaw(2*10).String(), p.SolverBondMin.Amount.String())

	// solver levels never pay more than intents
	p.SpotSolverFeeBps = p.SpotFeeBps + 1
	require.ErrorContains(t, p.Validate(), "spot_solver_fee_bps")
	p = types.DefaultParams()
	p.BuilderFeeMaxSpotBps = types.MaxBuilderFeeBpsAbsolute + 1
	require.ErrorContains(t, p.Validate(), "builder_fee_max_spot_bps")
}
