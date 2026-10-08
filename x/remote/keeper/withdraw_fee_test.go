package keeper

import (
	"testing"

	"cosmossdk.io/math"
)

// The vault withdrawal fee rounds up: the user never keeps the fractional unit (CLAUDE.md).
func TestWithdrawFeeRoundsUp(t *testing.T) {
	cases := []struct {
		amount int64
		bps    uint32
		want   int64
	}{
		{40_000_000, 100, 400_000}, // exact (port test vector)
		{40_000_001, 100, 400_001}, // 400,000.01 -> 400,001
		{99, 100, 1},               // 0.99 -> 1
		{1, 1, 1},                  // 0.0001 -> 1
		{0, 100, 0},
	}
	for _, c := range cases {
		if got := withdrawFee(math.NewInt(c.amount), c.bps); !got.Equal(math.NewInt(c.want)) {
			t.Errorf("withdrawFee(%d, %d) = %s, want %d", c.amount, c.bps, got, c.want)
		}
	}
}
