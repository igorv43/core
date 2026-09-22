package types_test

import (
	"testing"

	"github.com/classic-terra/core/v4/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestParseExchangeRateTuples(t *testing.T) {
	valid := "123.0uluna,123.123ukrw"
	_, err := types.ParseExchangeRateTuples(valid)
	require.NoError(t, err)

	duplicatedDenom := "100.0uluna,123.123ukrw,121233.123ukrw"
	_, err = types.ParseExchangeRateTuples(duplicatedDenom)
	require.Error(t, err)

	invalidCoins := "123.123"
	_, err = types.ParseExchangeRateTuples(invalidCoins)
	require.Error(t, err)

	invalidCoinsWithValid := "123.0uluna,123.1"
	_, err = types.ParseExchangeRateTuples(invalidCoinsWithValid)
	require.Error(t, err)

	abstainCoinsWithValid := "0.0uluna,123.1ukrw"
	_, err = types.ParseExchangeRateTuples(abstainCoinsWithValid)
	require.NoError(t, err)
}

func TestParseExchangeRateTuplesWithDepth(t *testing.T) {
	// spec §21.6 stage 2: "<price><asset>@<depth>" commits the Depth2% with the price
	tuples, err := types.ParseExchangeRateTuples("65000.5ubtc@1500000000000,0.0001uusd")
	require.NoError(t, err)
	require.Len(t, tuples, 2)
	require.Equal(t, "ubtc", tuples[0].Denom)
	require.Equal(t, "65000.500000000000000000", tuples[0].ExchangeRate.String())
	require.Equal(t, "1500000000000", tuples[0].Depth.String())
	require.Equal(t, "uusd", tuples[1].Denom)
	require.True(t, tuples[1].Depth.IsNil())

	_, err = types.ParseExchangeRateTuples("65000.5ubtc@-1")
	require.Error(t, err)
	_, err = types.ParseExchangeRateTuples("65000.5ubtc@abc")
	require.Error(t, err)
	_, err = types.ParseExchangeRateTuples("65000.5ubtc@1@2")
	require.Error(t, err)
}
