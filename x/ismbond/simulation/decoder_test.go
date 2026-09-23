package simulation_test

import (
	"fmt"
	"testing"

	"github.com/classic-terra/core/v4/x/ismbond/simulation"
	"github.com/classic-terra/core/v4/x/ismbond/types"
	"github.com/cosmos/cosmos-sdk/types/kv"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/stretchr/testify/require"
)

func TestDecodeStore(t *testing.T) {
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	dec := simulation.NewDecodeStore(cdc)

	params := types.DefaultParams()
	kvPairs := kv.Pairs{Pairs: []kv.Pair{
		{Key: types.ParamsKey.Bytes(), Value: cdc.MustMarshal(&params)},
		{Key: types.ByValidatorKey.Bytes(), Value: []byte("0xabc")},
		{Key: []byte{0xff}, Value: []byte{}},
	}}
	tests := []struct {
		name        string
		expectedLog string
	}{
		{"params", fmt.Sprintf("%v\n%v", params, params)},
		{"by_validator", "0xabc\n0xabc"},
		{"other", ""},
	}
	for i, tt := range tests {
		i, tt := i, tt
		t.Run(tt.name, func(t *testing.T) {
			switch i {
			case len(tests) - 1:
				require.Panics(t, func() { dec(kvPairs.Pairs[i], kvPairs.Pairs[i]) }, tt.name)
			default:
				require.Equal(t, tt.expectedLog, dec(kvPairs.Pairs[i], kvPairs.Pairs[i]), tt.name)
			}
		})
	}
}
