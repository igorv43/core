package simulation

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/remote/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/kv"
)

// NewDecodeStore returns a decoder that renders two KVPairs of the x/remote
// store as comparable strings, by collections prefix.
func NewDecodeStore(cdc codec.Codec) func(kvA, kvB kv.Pair) string {
	return func(kvA, kvB kv.Pair) string {
		switch {
		case hasPrefix(kvA.Key, types.ParamsKey):
			var a, b types.Params
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.AppsKey):
			var a, b types.RemoteApp
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.GatewaysKey):
			var a, b types.Gateway
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.AccountsKey):
			var a, b types.RemoteAccount
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.SessionsKey):
			var a, b types.Session
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.WithdrawalsKey):
			var a, b types.Withdrawal
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.WithdrawSeqKey):
			return fmt.Sprintf("%d\n%d", decodeUint64(kvA.Value), decodeUint64(kvB.Value))
		case hasPrefix(kvA.Key, types.BeaconsKey):
			var a, b types.Beacon
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.BeaconSeqKey):
			return fmt.Sprintf("%d\n%d", decodeUint64(kvA.Value), decodeUint64(kvB.Value))
		case hasPrefix(kvA.Key, types.ReceiptsKey):
			var a, b types.ConversionReceipt
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.ExecutorsKey):
			var a, b types.Executor
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.PortsKey):
			var a, b types.Port
			cdc.MustUnmarshal(kvA.Value, &a)
			cdc.MustUnmarshal(kvB.Value, &b)
			return fmt.Sprintf("%v\n%v", a, b)
		case hasPrefix(kvA.Key, types.ReceiptSeqKey), hasPrefix(kvA.Key, types.ReceiptByMsgKey):
			return fmt.Sprintf("%d\n%d", decodeUint64(kvA.Value), decodeUint64(kvB.Value))
		default:
			panic(fmt.Sprintf("invalid remote key prefix %X", kvA.Key[:1]))
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
