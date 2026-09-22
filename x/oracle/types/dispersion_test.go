package types

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestBallotDispersion(t *testing.T) {
	voter := func(i byte) sdk.ValAddress {
		return sdk.ValAddress([]byte{i, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20})
	}

	// four voters with equal power at 90, 100, 110, 120: median 100 (weighted median
	// of an even count picks the first crossing), P25 = 90, P75 = 110
	ballot := ExchangeRateBallot{
		NewVoteForTally(math.LegacyNewDec(90), "uusd", voter(1), 10),
		NewVoteForTally(math.LegacyNewDec(100), "uusd", voter(2), 10),
		NewVoteForTally(math.LegacyNewDec(110), "uusd", voter(3), 10),
		NewVoteForTally(math.LegacyNewDec(120), "uusd", voter(4), 10),
	}
	median := ballot.WeightedMedian()
	require.True(t, median.Equal(math.LegacyNewDec(100)), median.String())
	disp := ballot.Dispersion(median)
	require.True(t, disp.Equal(math.LegacyNewDecWithPrec(2, 1)), "expected 0.2, got %s", disp) // (110-90)/100

	// power weighting: one heavy voter dominates both percentiles -> zero dispersion
	heavy := ExchangeRateBallot{
		NewVoteForTally(math.LegacyNewDec(90), "uusd", voter(1), 1),
		NewVoteForTally(math.LegacyNewDec(100), "uusd", voter(2), 100),
		NewVoteForTally(math.LegacyNewDec(150), "uusd", voter(3), 1),
	}
	require.True(t, heavy.Dispersion(heavy.WeightedMedian()).IsZero())

	// abstain votes (non-positive rate) are ignored
	withAbstain := append(ExchangeRateBallot{NewVoteForTally(math.LegacyZeroDec(), "uusd", voter(9), 1000)}, ballot...)
	require.True(t, withAbstain.Dispersion(math.LegacyNewDec(100)).Equal(disp))

	// empty ballot / zero median
	require.True(t, ExchangeRateBallot{}.Dispersion(math.LegacyOneDec()).IsZero())
	require.True(t, ballot.Dispersion(math.LegacyZeroDec()).IsZero())
}

func TestStandardDeviationIsDeterministicFixedPoint(t *testing.T) {
	voter := sdk.ValAddress([]byte("validator-address----"))
	ballot := ExchangeRateBallot{
		NewVoteForTally(math.LegacyNewDec(1), "uusd", voter, 1),
		NewVoteForTally(math.LegacyNewDec(3), "uusd", voter, 1),
	}
	// median 1 (first crossing of half power): deviations 0 and 2 -> variance 2 -> sqrt(2)
	sd := ballot.StandardDeviation(math.LegacyNewDec(1))
	sqrt2, _ := math.LegacyNewDec(2).ApproxSqrt()
	require.True(t, sd.Equal(sqrt2), sd.String())
}
