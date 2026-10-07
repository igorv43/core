package keeper

import (
	"testing"

	"cosmossdk.io/math"
)

// Spot fees round up: the user never keeps the fractional unit (CLAUDE.md, same rule as x/perp).
func TestBpsCeil(t *testing.T) {
	cases := []struct {
		amount int64
		bps    uint32
		want   int64
	}{
		{0, 5, 0},
		{1, 5, 1},              // 0.0005 -> 1
		{19_999, 5, 10},        // 9.9995 -> 10
		{20_000, 5, 10},        // exact
		{20_001, 5, 11},        // 10.0005 -> 11
		{1_000_000, 0, 0},      // solver levels at 0 bps pay nothing
		{1_000_000, 50, 5_000}, // builder cap, exact
		{7, 10_000, 7},         // never above the amount
	}
	for _, c := range cases {
		if got := bpsCeil(math.NewInt(c.amount), c.bps); !got.Equal(math.NewInt(c.want)) {
			t.Errorf("bpsCeil(%d, %d) = %s, want %d", c.amount, c.bps, got, c.want)
		}
	}
}
