package types

import (
	"encoding/binary"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
)

const (
	// ModuleName is the name of the oracle module
	ModuleName = "oracle"

	// StoreKey is the string store representation
	StoreKey = ModuleName

	// RouterKey is the msg router key for the oracle module
	RouterKey = ModuleName

	// QuerierRoute is the query router key for the oracle module
	QuerierRoute = ModuleName
)

// Keys for oracle store
// Items are stored with the following key: values
//
// - 0x01<denom_Bytes>: sdk.Dec
//
// - 0x02<valAddress_Bytes>: accAddress
//
// - 0x03<valAddress_Bytes>: int64
//
// - 0x04<valAddress_Bytes>: AggregateExchangeRatePrevote
//
// - 0x05<valAddress_Bytes>: AggregateExchangeRateVote
//
// - 0x06<denom_Bytes>: sdk.Dec
//
// - 0x07<denom_Bytes>: RateSample (latest vote period, with dispersion)
//
// - 0x08<len(denom)><denom_Bytes><vote_period_BE64>: RateSample (bounded history for TWAP)
var (
	// Keys for store prefixes
	ExchangeRateKey                 = []byte{0x01} // prefix for each key to a rate
	FeederDelegationKey             = []byte{0x02} // prefix for each key to a feeder delegation
	MissCounterKey                  = []byte{0x03} // prefix for each key to a miss counter
	AggregateExchangeRatePrevoteKey = []byte{0x04} // prefix for each key to a aggregate prevote
	AggregateExchangeRateVoteKey    = []byte{0x05} // prefix for each key to a aggregate vote
	TobinTaxKey                     = []byte{0x06} // prefix for each key to a tobin tax
	DispersionKey                   = []byte{0x07} // prefix for the latest rate sample (with dispersion) of a denom
	RateHistoryKey                  = []byte{0x08} // prefix for the rate sample history of a denom
)

// MaxRateHistory bounds the number of rate samples kept per denom: one day of
// 5-block vote periods at 6 s blocks. It is a code constant (spec §12).
const MaxRateHistory = 2880

// GetExchangeRateKey - stored by *denom*
func GetExchangeRateKey(denom string) []byte {
	return append(ExchangeRateKey, []byte(denom)...)
}

// GetFeederDelegationKey - stored by *Validator* address
func GetFeederDelegationKey(v sdk.ValAddress) []byte {
	return append(FeederDelegationKey, address.MustLengthPrefix(v)...)
}

// GetMissCounterKey - stored by *Validator* address
func GetMissCounterKey(v sdk.ValAddress) []byte {
	return append(MissCounterKey, address.MustLengthPrefix(v)...)
}

// GetAggregateExchangeRatePrevoteKey - stored by *Validator* address
func GetAggregateExchangeRatePrevoteKey(v sdk.ValAddress) []byte {
	return append(AggregateExchangeRatePrevoteKey, address.MustLengthPrefix(v)...)
}

// GetAggregateExchangeRateVoteKey - stored by *Validator* address
func GetAggregateExchangeRateVoteKey(v sdk.ValAddress) []byte {
	return append(AggregateExchangeRateVoteKey, address.MustLengthPrefix(v)...)
}

// GetTobinTaxKey - stored by *denom* bytes
func GetTobinTaxKey(d string) []byte {
	return append(TobinTaxKey, []byte(d)...)
}

// ExtractDenomFromTobinTaxKey extracts the denom from the Tobin tax key by removing the first byte prefix.
func ExtractDenomFromTobinTaxKey(key []byte) (denom string) {
	denom = string(key[1:])
	return denom
}

// GetDispersionKey - stored by *denom* bytes
func GetDispersionKey(denom string) []byte {
	return append(DispersionKey, []byte(denom)...)
}

// GetRateHistoryPrefix returns the prefix of every rate sample of a denom.
func GetRateHistoryPrefix(denom string) []byte {
	return append(append(RateHistoryKey, byte(len(denom))), []byte(denom)...)
}

// GetRateHistoryKey - stored by *denom* and vote period (big-endian, so the
// store order is chronological).
func GetRateHistoryKey(denom string, votePeriod uint64) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, votePeriod)
	return append(GetRateHistoryPrefix(denom), bz...)
}
