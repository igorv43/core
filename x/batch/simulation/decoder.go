package simulation

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/kv"
)

// NewDecodeStore returns a decoder that renders two KVPairs of the x/batch
// store as comparable strings, by collections prefix.
func NewDecodeStore(cdc codec.Codec) func(kvA, kvB kv.Pair) string {
	return func(kvA, kvB kv.Pair) string {
		switch {
		case hasPrefix(kvA.Key, types.ParamsKey):
			var a, b types.Params
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.MarketsKey):
			var a, b types.Market
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.IntentsKey):
			var a, b types.Intent
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.IntentSeqKey):
			return fmt.Sprintf("%d\n%d", decodeUint64(kvA.Value), decodeUint64(kvB.Value))
		case hasPrefix(kvA.Key, types.IntentsByAccountKey) || hasPrefix(kvA.Key, types.IntentsByMarketKey):
			// key sets: the value is empty, the key is the fact
			return fmt.Sprintf("%s\n%s", hex.EncodeToString(kvA.Key), hex.EncodeToString(kvB.Key))
		case hasPrefix(kvA.Key, types.SolversKey):
			var a, b types.Solver
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.SolverEscrowKey):
			return fmt.Sprintf("%v\n%v", decodeInt(kvA.Value), decodeInt(kvB.Value))
		case hasPrefix(kvA.Key, types.CommitsKey):
			var a, b types.Commit
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.CommitSeqKey):
			return fmt.Sprintf("%d\n%d", decodeUint64(kvA.Value), decodeUint64(kvB.Value))
		case hasPrefix(kvA.Key, types.ResultsKey):
			var a, b types.BatchResult
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.FrontendsKey):
			var a, b types.Frontend
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.ApprovalsKey):
			var a, b types.FrontendApproval
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		default:
			panic(fmt.Sprintf("invalid batch key prefix %X", kvA.Key[:1]))
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
