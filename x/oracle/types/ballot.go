package types

import (
	"sort"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NOTE: we don't need to implement proto interface on this file
//       these are not used in store or rpc response

// VoteForTally is a convenience wrapper to reduce redundant lookup cost
type VoteForTally struct {
	Denom        string
	ExchangeRate math.LegacyDec
	Voter        sdk.ValAddress
	Power        int64
}

// NewVoteForTally returns a new VoteForTally instance
func NewVoteForTally(rate math.LegacyDec, denom string, voter sdk.ValAddress, power int64) VoteForTally {
	return VoteForTally{
		ExchangeRate: rate,
		Denom:        denom,
		Voter:        voter,
		Power:        power,
	}
}

// ExchangeRateBallot is a convenience wrapper around a ExchangeRateVote slice
type ExchangeRateBallot []VoteForTally

// ToMap return organized exchange rate map by validator
func (pb ExchangeRateBallot) ToMap() map[string]math.LegacyDec {
	exchangeRateMap := make(map[string]math.LegacyDec)
	for _, vote := range pb {
		if vote.ExchangeRate.IsPositive() {
			exchangeRateMap[string(vote.Voter)] = vote.ExchangeRate
		}
	}

	return exchangeRateMap
}

// ToCrossRate return cross_rate(base/exchange_rate) ballot
func (pb ExchangeRateBallot) ToCrossRate(bases map[string]math.LegacyDec) (cb ExchangeRateBallot) {
	for i := range pb {
		vote := pb[i]

		if exchangeRateRT, ok := bases[string(vote.Voter)]; ok && vote.ExchangeRate.IsPositive() {
			vote.ExchangeRate = exchangeRateRT.Quo(vote.ExchangeRate)
		} else {
			// If we can't get reference terra exchange rate, we just convert the vote as abstain vote
			vote.ExchangeRate = math.LegacyZeroDec()
			vote.Power = 0
		}

		cb = append(cb, vote)
	}

	return cb
}

// ToCrossRateWithSort return cross_rate(base/exchange_rate) ballot
func (pb ExchangeRateBallot) ToCrossRateWithSort(bases map[string]math.LegacyDec) (cb ExchangeRateBallot) {
	for i := range pb {
		vote := pb[i]

		if exchangeRateRT, ok := bases[string(vote.Voter)]; ok && vote.ExchangeRate.IsPositive() {
			vote.ExchangeRate = exchangeRateRT.Quo(vote.ExchangeRate)
		} else {
			// If we can't get reference terra exchange rate, we just convert the vote as abstain vote
			vote.ExchangeRate = math.LegacyZeroDec()
			vote.Power = 0
		}

		cb = append(cb, vote)
	}

	sort.Sort(cb)
	return cb
}

// Power returns the total amount of voting power in the ballot
func (pb ExchangeRateBallot) Power() int64 {
	totalPower := int64(0)
	for _, vote := range pb {
		totalPower += vote.Power
	}

	return totalPower
}

// WeightedMedian returns the median weighted by the power of the ExchangeRateVote.
// CONTRACT: ballot must be sorted
func (pb ExchangeRateBallot) WeightedMedian() math.LegacyDec {
	totalPower := pb.Power()
	if pb.Len() > 0 {
		pivot := int64(0)
		for _, v := range pb {
			votePower := v.Power

			pivot += votePower
			if pivot >= (totalPower / 2) {
				return v.ExchangeRate
			}
		}
	}
	return math.LegacyZeroDec()
}

// StandardDeviation returns the standard deviation by the power of the ExchangeRateVote.
func (pb ExchangeRateBallot) StandardDeviation(median math.LegacyDec) (standardDeviation math.LegacyDec) {
	if len(pb) == 0 {
		return math.LegacyZeroDec()
	}

	defer func() {
		if e := recover(); e != nil {
			standardDeviation = math.LegacyZeroDec()
		}
	}()

	sum := math.LegacyZeroDec()
	for _, v := range pb {
		deviation := v.ExchangeRate.Sub(median)
		sum = sum.Add(deviation.Mul(deviation))
	}

	variance := sum.QuoInt64(int64(len(pb)))

	// deterministic fixed-point square root (the previous float64 round-trip
	// was a determinism hazard on the consensus path)
	standardDeviation, err := variance.ApproxSqrt()
	if err != nil {
		return math.LegacyZeroDec()
	}

	return standardDeviation
}

// WeightedPercentile returns the exchange rate at the given percentile of the
// voting power (p in [0, 1]), ignoring abstain votes (non-positive rates).
// The ballot must be sorted by exchange rate.
func (pb ExchangeRateBallot) WeightedPercentile(p math.LegacyDec) math.LegacyDec {
	totalPower := int64(0)
	for _, v := range pb {
		if v.ExchangeRate.IsPositive() {
			totalPower += v.Power
		}
	}
	if totalPower == 0 {
		return math.LegacyZeroDec()
	}
	target := p.MulInt64(totalPower)
	cumulative := math.LegacyZeroDec()
	for _, v := range pb {
		if !v.ExchangeRate.IsPositive() {
			continue
		}
		cumulative = cumulative.Add(math.LegacyNewDec(v.Power))
		if cumulative.GTE(target) {
			return v.ExchangeRate
		}
	}
	return pb[len(pb)-1].ExchangeRate
}

// Dispersion returns (P75 - P25) / median, weighted by voting power (Liquidity
// Fabric spec §21.1). It is zero for an empty ballot or a zero median.
func (pb ExchangeRateBallot) Dispersion(median math.LegacyDec) math.LegacyDec {
	if len(pb) == 0 || !median.IsPositive() {
		return math.LegacyZeroDec()
	}
	sorted := make(ExchangeRateBallot, len(pb))
	copy(sorted, pb)
	sort.Sort(sorted)
	p25 := sorted.WeightedPercentile(math.LegacyNewDecWithPrec(25, 2))
	p75 := sorted.WeightedPercentile(math.LegacyNewDecWithPrec(75, 2))
	return p75.Sub(p25).Quo(median)
}

// Len implements sort.Interface
func (pb ExchangeRateBallot) Len() int {
	return len(pb)
}

// Less reports whether the element with
// index i should sort before the element with index j.
func (pb ExchangeRateBallot) Less(i, j int) bool {
	return pb[i].ExchangeRate.LT(pb[j].ExchangeRate)
}

// Swap implements sort.Interface.
func (pb ExchangeRateBallot) Swap(i, j int) {
	pb[i], pb[j] = pb[j], pb[i]
}

// Claim is an interface that directs its rewards to an attached bank account.
type Claim struct {
	Power     int64
	Weight    int64
	WinCount  int64
	Recipient sdk.ValAddress
}

// NewClaim generates a Claim instance.
func NewClaim(power, weight, winCount int64, recipient sdk.ValAddress) Claim {
	return Claim{
		Power:     power,
		Weight:    weight,
		WinCount:  winCount,
		Recipient: recipient,
	}
}
