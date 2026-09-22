package keeper

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestRateSampleHistoryAndTwap(t *testing.T) {
	input := CreateTestInput(t)
	ctx := input.Ctx
	k := input.OracleKeeper

	_, err := k.GetRateSample(ctx, "uusd")
	require.Error(t, err, "no sample before the first vote period")

	// three periods 30 s apart with rates 1, 2, 3 and a rising dispersion
	base := time.Unix(1_700_000_000, 0).UTC()
	for i := 1; i <= 3; i++ {
		k.SetRateSample(ctx, types.RateSample{
			Denom:        "uusd",
			ExchangeRate: math.LegacyNewDec(int64(i)),
			Dispersion:   math.LegacyNewDecWithPrec(int64(i), 3),
			VotePeriod:   uint64(i),
			Height:       int64(i * 5),
			Time:         base.Add(time.Duration(i) * 30 * time.Second),
		})
	}

	latest, err := k.GetRateSample(ctx, "uusd")
	require.NoError(t, err)
	require.Equal(t, uint64(3), latest.VotePeriod)
	require.True(t, latest.Dispersion.Equal(math.LegacyNewDecWithPrec(3, 3)))

	history := k.RateHistory(ctx, "uusd", 10)
	require.Len(t, history, 3)
	require.Equal(t, uint64(3), history[0].VotePeriod, "newest first")
	require.Equal(t, uint64(1), history[2].VotePeriod)

	// equal 30 s intervals -> the TWAP is the plain mean
	twap, samples, err := k.Twap(ctx, "uusd", 3)
	require.NoError(t, err)
	require.Len(t, samples, 3)
	require.True(t, twap.Equal(math.LegacyNewDec(2)), twap.String())

	// window of one period -> the latest rate
	twap, _, err = k.Twap(ctx, "uusd", 1)
	require.NoError(t, err)
	require.True(t, twap.Equal(math.LegacyNewDec(3)))

	// a failed period clears the latest sample but keeps the history
	k.DeleteRateSample(ctx, "uusd")
	_, err = k.GetRateSample(ctx, "uusd")
	require.Error(t, err)
	require.Len(t, k.RateHistory(ctx, "uusd", 10), 3)
}

func TestRateHistoryIsBounded(t *testing.T) {
	input := CreateTestInput(t)
	ctx := input.Ctx
	k := input.OracleKeeper
	for i := 1; i <= types.MaxRateHistory+25; i++ {
		k.SetRateSample(ctx, types.RateSample{
			Denom: "ukrw", ExchangeRate: math.LegacyOneDec(), Dispersion: math.LegacyZeroDec(),
			VotePeriod: uint64(i), Height: int64(i), Time: time.Unix(int64(i), 0),
		})
	}
	history := k.RateHistory(ctx, "ukrw", types.MaxRateHistory+100)
	require.Len(t, history, types.MaxRateHistory)
	require.Equal(t, uint64(types.MaxRateHistory+25), history[0].VotePeriod)
	require.Equal(t, uint64(26), history[len(history)-1].VotePeriod, "the 25 oldest periods were pruned")
}
