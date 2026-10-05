package types_test

import (
	"testing"

	"github.com/classic-terra/core/v4/x/perp/types"
	"github.com/stretchr/testify/require"
)

// Spec §11.4 (D-29): the settlement asset is the USDC basket uusdc.lf; §11.3
// (D-18): USTC never denominates the insurance fund, which is always in the
// settlement denom, so the settlement denom can never be uusd.
func TestSettlementDenomIsUsdcNeverUstc(t *testing.T) {
	p := types.DefaultParams()
	require.Equal(t, "uusdc.lf", p.SettlementDenom)
	require.Equal(t, "uusd", p.LunaPriceDenom, "oracle USD price reference of LUNC only")
	require.NoError(t, p.Validate())

	p.SettlementDenom = "uusd"
	require.ErrorContains(t, p.Validate(), "D-18")

	p = types.DefaultParams()
	p.RevenueSellCap = p.RevenueSellCap.Neg()
	require.ErrorContains(t, p.Validate(), "revenue_sell_cap")
}
