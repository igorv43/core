package simulation

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/perp/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/kv"
)

// NewDecodeStore returns a decoder that renders two KVPairs of the x/perp
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
		case hasPrefix(kvA.Key, types.PositionsKey):
			var a, b types.Position
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.PositionsByMarketKey) || hasPrefix(kvA.Key, types.TriggersByAccountKey) || hasPrefix(kvA.Key, types.TriggersByMarketKey) || hasPrefix(kvA.Key, types.AutoTopUpKey) || hasPrefix(kvA.Key, types.FeeInLunaKey) || hasPrefix(kvA.Key, types.LiqIndexKey):
			// key sets: the value is empty, the key is the fact
			return fmt.Sprintf("%s\n%s", hex.EncodeToString(kvA.Key), hex.EncodeToString(kvB.Key))
		case hasPrefix(kvA.Key, types.CollateralKey):
			return fmt.Sprintf("%v\n%v", decodeInt(kvA.Value), decodeInt(kvB.Value))
		case hasPrefix(kvA.Key, types.CollateralStKey):
			return fmt.Sprintf("%v\n%v", decodeInt(kvA.Value), decodeInt(kvB.Value))
		case hasPrefix(kvA.Key, types.ReservationsKey):
			return fmt.Sprintf("%v\n%v", decodeInt(kvA.Value), decodeInt(kvB.Value))
		case hasPrefix(kvA.Key, types.TriggersKey):
			var a, b types.TriggerOrder
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.TriggerSeqKey):
			return fmt.Sprintf("%d\n%d", decodeUint64(kvA.Value), decodeUint64(kvB.Value))
		case hasPrefix(kvA.Key, types.LedgerKey):
			var a, b types.Ledger
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.AllocationsKey):
			var a, b types.AllocationRecord
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.UnwindIntentsKey):
			return fmt.Sprintf("%d\n%d", decodeUint64(kvA.Value), decodeUint64(kvB.Value))
		case hasPrefix(kvA.Key, types.PremiumsKey):
			return fmt.Sprintf("%v\n%v", decodeInt(kvA.Value), decodeInt(kvB.Value))
		default:
			panic(fmt.Sprintf("invalid perp key prefix %X", kvA.Key[:1]))
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
