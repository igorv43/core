package types

import (
	"encoding/binary"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
)

// ABI encoding of the FabricExecutor control messages (spec §11.5.1):
// body = abi.encode(uint8 action, bytes params, uint64 nonce).

func word(b []byte) []byte {
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

func wordUint(v uint64) []byte {
	out := make([]byte, 32)
	binary.BigEndian.PutUint64(out[24:], v)
	return out
}

func wordInt(v math.Int) []byte {
	out := make([]byte, 32)
	v.BigInt().FillBytes(out)
	return out
}

// EncodeControl returns abi.encode(uint8 action, bytes params, uint64 nonce).
func EncodeControl(action ControlAction, params []byte, nonce uint64) []byte {
	out := make([]byte, 0, 32*4+len(params)+32)
	out = append(out, wordUint(uint64(action))...)
	out = append(out, wordUint(0x60)...) // offset of the bytes
	out = append(out, wordUint(nonce)...)
	out = append(out, wordUint(uint64(len(params)))...)
	out = append(out, params...)
	if pad := (32 - len(params)%32) % 32; pad > 0 {
		out = append(out, make([]byte, pad)...)
	}
	return out
}

// RebalanceParams is abi.encode(uint32 destDomain, uint256 amount).
func RebalanceParams(destDomain uint32, amount math.Int) []byte {
	return append(wordUint(uint64(destDomain)), wordInt(amount)...)
}

// SetFeeParams is abi.encode(uint32 depositFeeBps, uint32 withdrawFeeBps).
func SetFeeParams(depositBps, withdrawBps uint32) []byte {
	return append(wordUint(uint64(depositBps)), wordUint(uint64(withdrawBps))...)
}

// SetLegLimitsParams is abi.encode(uint256 min, uint256 target).
func SetLegLimitsParams(minCollateral, target math.Int) []byte {
	return append(wordInt(minCollateral), wordInt(target)...)
}

// EnrollLegParams is abi.encode(uint32 domain, bytes32 router, address bridge).
func EnrollLegParams(domain uint32, router, bridge util.HexAddress) []byte {
	out := wordUint(uint64(domain))
	out = append(out, router.Bytes()...)
	out = append(out, word(bridge.Bytes()[12:])...)
	return out
}

// PortSentinel is the token_out that routes a withdrawal to a port-of-entry
// chain by CCTP (spec §11.6.2): bytes 0..27 zero, byte 27 = 0xcc, bytes
// 28..32 = big-endian hyperlane domain of the port.
func PortSentinel(portDomain uint32) util.HexAddress {
	var out util.HexAddress
	out[27] = 0xcc
	binary.BigEndian.PutUint32(out[28:], portDomain)
	return out
}
