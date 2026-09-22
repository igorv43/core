package types

import (
	"fmt"
	"strings"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"gopkg.in/yaml.v2"
)

// NewAggregateExchangeRatePrevote returns AggregateExchangeRatePrevote object
func NewAggregateExchangeRatePrevote(hash AggregateVoteHash, voter sdk.ValAddress, submitBlock uint64) AggregateExchangeRatePrevote {
	return AggregateExchangeRatePrevote{
		Hash:        hash.String(),
		Voter:       voter.String(),
		SubmitBlock: submitBlock,
	}
}

// String implement stringify
func (v AggregateExchangeRatePrevote) String() string {
	out, _ := yaml.Marshal(v)
	return string(out)
}

// NewAggregateExchangeRateVote creates a AggregateExchangeRateVote instance
func NewAggregateExchangeRateVote(exchangeRateTuples ExchangeRateTuples, voter sdk.ValAddress) AggregateExchangeRateVote {
	return AggregateExchangeRateVote{
		ExchangeRateTuples: exchangeRateTuples,
		Voter:              voter.String(),
	}
}

// String implement stringify
func (v AggregateExchangeRateVote) String() string {
	out, _ := yaml.Marshal(v)
	return string(out)
}

// NewExchangeRateTuple creates a ExchangeRateTuple instance
func NewExchangeRateTuple(denom string, exchangeRate math.LegacyDec) ExchangeRateTuple {
	return ExchangeRateTuple{
		Denom:        denom,
		ExchangeRate: exchangeRate,
	}
}

// String implement stringify
func (v ExchangeRateTuple) String() string {
	out, _ := yaml.Marshal(v)
	return string(out)
}

// ExchangeRateTuples - array of ExchangeRateTuple
type ExchangeRateTuples []ExchangeRateTuple

// String implements fmt.Stringer interface
func (tuples ExchangeRateTuples) String() string {
	out, _ := yaml.Marshal(tuples)
	return string(out)
}

// DepthSeparator separates the optional Depth2% of an asset tuple:
// "65000.0ubtc@1500000000000" (spec §21.3, §21.6 stage 2). The hash of the
// prevote covers the whole string, so the depth is committed with the price.
const DepthSeparator = "@"

// ParseExchangeRateTuples ExchangeRateTuple parser
func ParseExchangeRateTuples(tuplesStr string) (ExchangeRateTuples, error) {
	tuplesStr = strings.TrimSpace(tuplesStr)
	if len(tuplesStr) == 0 {
		return nil, nil
	}

	tupleStrs := strings.Split(tuplesStr, ",")
	tuples := make(ExchangeRateTuples, len(tupleStrs))
	duplicateCheckMap := make(map[string]bool)
	for i, tupleStr := range tupleStrs {
		coinStr, depthStr, hasDepth := strings.Cut(tupleStr, DepthSeparator)
		decCoin, err := sdk.ParseDecCoin(coinStr)
		if err != nil {
			return nil, err
		}

		tuples[i] = ExchangeRateTuple{
			Denom:        decCoin.Denom,
			ExchangeRate: decCoin.Amount,
		}
		if hasDepth {
			depth, ok := math.NewIntFromString(depthStr)
			if !ok || depth.IsNegative() {
				return nil, fmt.Errorf("invalid depth %q for %s: must be a non-negative integer", depthStr, decCoin.Denom)
			}
			tuples[i].Depth = depth
		}

		if _, ok := duplicateCheckMap[decCoin.Denom]; ok {
			return nil, fmt.Errorf("duplicated denom %s", decCoin.Denom)
		}

		duplicateCheckMap[decCoin.Denom] = true
	}

	return tuples, nil
}
