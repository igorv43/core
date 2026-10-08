package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/remote/types"
	"github.com/stretchr/testify/require"
)

// TestPortExitFeeToleranceParam: default 10 bps, valid within [0, 100]
// (MaxPortExitFeeToleranceBpsAbsolute), refused above.
func TestPortExitFeeToleranceParam(t *testing.T) {
	p := types.DefaultParams()
	require.Equal(t, uint32(10), p.PortExitFeeToleranceBps)
	require.NoError(t, p.Validate())
	for _, bps := range []uint32{0, 1, 99, 100} {
		p.PortExitFeeToleranceBps = bps
		require.NoError(t, p.Validate(), bps)
	}
	for _, bps := range []uint32{101, 10_000, ^uint32(0)} {
		p.PortExitFeeToleranceBps = bps
		require.ErrorContains(t, p.Validate(), "port_exit_fee_tolerance_bps must be at most 100", bps)
	}
}

// TestPortExitMinAccepted: floor = net - ceil(net x bps / 10,000), exact
// integers, the allowance rounded up (the floor down).
func TestPortExitMinAccepted(t *testing.T) {
	for _, c := range []struct {
		net  int64
		bps  uint32
		want string
	}{
		{123_456_789, 10, "123333332"}, // ceil(123,456.789) = 123,457
		{40_000_000, 10, "39960000"},   // exact: 40,000
		{39_600_000, 10, "39560400"},   // exact: 39,600
		{7_000_001, 25, "6982500"},     // ceil(17,500.0025) = 17,501
		{999, 10, "998"},               // ceil(0.999) = 1
		{1, 10, "0"},                   // ceil(0.001) = 1
		{1, 100, "0"},                  // ceil(0.01) = 1
		{10_000, 100, "9900"},          // exact: 100
		{10_001, 100, "9900"},          // ceil(100.01) = 101
		{149_469_951, 10, "149320481"}, // ceil(149,469.951) = 149,470
		{123_456_789, 0, "123456789"},  // tolerance off: the full net amount
		{0, 10, "0"},                   // nothing to protect
	} {
		got := types.PortExitMinAccepted(math.NewInt(c.net), c.bps)
		require.Equal(t, c.want, got.String(), "net %d bps %d", c.net, c.bps)
		// the floor is never above the net, and never more than one unit below
		// the exact real-valued floor net x (1 - bps/10,000)
		require.True(t, got.LTE(math.NewInt(c.net)))
		exact := math.LegacyNewDec(c.net).Mul(math.LegacyOneDec().Sub(math.LegacyNewDec(int64(c.bps)).QuoInt64(10_000)))
		require.True(t, math.LegacyNewDecFromInt(got).LTE(exact))
		require.True(t, exact.Sub(math.LegacyNewDecFromInt(got)).LT(math.LegacyOneDec()))
	}
}
