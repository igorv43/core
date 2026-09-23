package simulation

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/ismbond/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/kv"
)

// NewDecodeStore returns a decoder that renders two KVPairs of the x/ismbond
// store as comparable strings, by collections prefix.
func NewDecodeStore(cdc codec.Codec) func(kvA, kvB kv.Pair) string {
	return func(kvA, kvB kv.Pair) string {
		switch {
		case hasPrefix(kvA.Key, types.ParamsKey):
			var a, b types.Params
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.OperatorsKey):
			var a, b types.Operator
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.ByValidatorKey):
			return fmt.Sprintf("%s\n%s", kvA.Value, kvB.Value)
		case hasPrefix(kvA.Key, types.RootsKey):
			var a, b types.RootRecord
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		default:
			panic(fmt.Sprintf("invalid ismbond key prefix %X", kvA.Key[:1]))
		}
	}
}

func hasPrefix(key []byte, p collections.Prefix) bool { return bytes.HasPrefix(key, p.Bytes()) }

func decodeInt(bz []byte) math.Int {
	var i math.Int
	if err := i.Unmarshal(bz); err != nil {
		panic(err)
	}
	return i
}

func decodeUint64(bz []byte) uint64 {
	v, err := collections.Uint64Value.Decode(bz)
	if err != nil {
		panic(err)
	}
	return v
}

var (
	_ = hex.EncodeToString
	_ = decodeInt
	_ = decodeUint64
)
