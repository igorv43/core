package types

import (
	"encoding/binary"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"golang.org/x/crypto/sha3"
)

// keccak256 hashes with the EVM's Keccak-256.
func keccak256(parts ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

// ExitSalt is keccak256(abi.encode(bytes32 controller, bytes32 tokenOut,
// uint256 minOut, uint64 nonce)): four 32-byte words.
func ExitSalt(controller, tokenOut util.HexAddress, minOut math.Int, nonce uint64) []byte {
	min32 := make([]byte, 32)
	minOut.BigInt().FillBytes(min32)
	nonce32 := make([]byte, 32)
	binary.BigEndian.PutUint64(nonce32[24:], nonce)
	return keccak256(controller.Bytes(), tokenOut.Bytes(), min32, nonce32)
}

// ExitAddress derives the CREATE2 address of the withdrawal exit contract of
// (controller, tokenOut, minOut, nonce) deployed by factory with a constant
// init code hash (spec §14.7.2): keccak256(0xff ‖ factory ‖ salt ‖ initCodeHash)[12:],
// returned as a 32-byte hyperlane address (left-padded).
func ExitAddress(factory util.HexAddress, initCodeHash []byte, controller, tokenOut util.HexAddress, minOut math.Int, nonce uint64) util.HexAddress {
	salt := ExitSalt(controller, tokenOut, minOut, nonce)
	factory20 := factory.Bytes()[12:]
	sum := keccak256([]byte{0xff}, factory20, salt, initCodeHash)
	var out util.HexAddress
	copy(out[12:], sum[12:])
	return out
}
