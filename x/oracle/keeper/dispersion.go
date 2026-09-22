package keeper

import (
	"cosmossdk.io/math"
	"cosmossdk.io/store/prefix"
	storetypes "cosmossdk.io/store/types"
	"github.com/classic-terra/core/v4/x/oracle/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Liquidity Fabric spec §21.6, stage 1: persist the ballot dispersion of every
// vote period and keep a bounded history of consensus rates so that the
// price-band and oracle-state logic of the batch auction can read them. No
// change to how the exchange rate itself is formed.

// SetRateSample stores the latest sample of a denom and appends it to the
// bounded history, pruning the oldest entries beyond MaxRateHistory.
func (k Keeper) SetRateSample(ctx sdk.Context, sample types.RateSample) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&sample)
	store.Set(types.GetDispersionKey(sample.Denom), bz)
	store.Set(types.GetRateHistoryKey(sample.Denom, sample.VotePeriod), bz)
	k.pruneRateHistory(ctx, sample.Denom, types.MaxRateHistory)
}

// GetRateSample returns the latest sample (rate + dispersion) of a denom.
func (k Keeper) GetRateSample(ctx sdk.Context, denom string) (types.RateSample, error) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.GetDispersionKey(denom))
	if bz == nil {
		return types.RateSample{}, types.ErrUnknownDenom.Wrap(denom)
	}
	var sample types.RateSample
	k.cdc.MustUnmarshal(bz, &sample)
	return sample, nil
}

// DeleteRateSample removes the latest sample of a denom (the history is kept).
func (k Keeper) DeleteRateSample(ctx sdk.Context, denom string) {
	ctx.KVStore(k.storeKey).Delete(types.GetDispersionKey(denom))
}

// IterateRateSamples iterates the latest samples of every denom.
func (k Keeper) IterateRateSamples(ctx sdk.Context, handler func(sample types.RateSample) (stop bool)) {
	store := ctx.KVStore(k.storeKey)
	iter := storetypes.KVStorePrefixIterator(store, types.DispersionKey)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		var sample types.RateSample
		k.cdc.MustUnmarshal(iter.Value(), &sample)
		if handler(sample) {
			break
		}
	}
}

// RateHistory returns up to `periods` samples of a denom, newest first.
func (k Keeper) RateHistory(ctx sdk.Context, denom string, periods uint64) []types.RateSample {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.GetRateHistoryPrefix(denom))
	iter := store.ReverseIterator(nil, nil)
	defer iter.Close()
	var out []types.RateSample
	for ; iter.Valid() && uint64(len(out)) < periods; iter.Next() {
		var sample types.RateSample
		k.cdc.MustUnmarshal(iter.Value(), &sample)
		out = append(out, sample)
	}
	return out
}

// pruneRateHistory deletes the oldest samples of a denom beyond `keep`.
func (k Keeper) pruneRateHistory(ctx sdk.Context, denom string, keep int) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.GetRateHistoryPrefix(denom))
	iter := store.ReverseIterator(nil, nil)
	defer iter.Close()
	var toDelete [][]byte
	count := 0
	for ; iter.Valid(); iter.Next() {
		count++
		if count > keep {
			toDelete = append(toDelete, append([]byte{}, iter.Key()...))
		}
	}
	for _, key := range toDelete {
		store.Delete(key)
	}
}

// Twap returns the time-weighted average of the last `periods` samples of a
// denom. Samples are weighted by the time until the next sample; the newest
// sample gets the median interval of the window so that it counts once. With
// a single sample the TWAP is that rate.
func (k Keeper) Twap(ctx sdk.Context, denom string, periods uint64) (math.LegacyDec, []types.RateSample, error) {
	if periods == 0 {
		periods = 1
	}
	if periods > types.MaxRateHistory {
		periods = types.MaxRateHistory
	}
	samples := k.RateHistory(ctx, denom, periods)
	if len(samples) == 0 {
		return math.LegacyZeroDec(), nil, types.ErrUnknownDenom.Wrap(denom)
	}
	// samples are newest first; walk oldest -> newest
	n := len(samples)
	if n == 1 {
		return samples[0].ExchangeRate, samples, nil
	}
	weighted := math.LegacyZeroDec()
	totalWeight := math.LegacyZeroDec()
	var intervals []int64
	for i := n - 1; i > 0; i-- {
		older, newer := samples[i], samples[i-1]
		dt := newer.Time.Unix() - older.Time.Unix()
		if dt <= 0 {
			dt = 1
		}
		intervals = append(intervals, dt)
		w := math.LegacyNewDec(dt)
		weighted = weighted.Add(older.ExchangeRate.Mul(w))
		totalWeight = totalWeight.Add(w)
	}
	// newest sample: weight = median interval of the window (deterministic)
	sortInt64(intervals)
	last := math.LegacyNewDec(intervals[len(intervals)/2])
	weighted = weighted.Add(samples[0].ExchangeRate.Mul(last))
	totalWeight = totalWeight.Add(last)
	return weighted.Quo(totalWeight), samples, nil
}

func sortInt64(a []int64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}
