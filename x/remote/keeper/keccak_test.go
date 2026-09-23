package keeper_test

import "golang.org/x/crypto/sha3"

func keccakOf(bz []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(bz)
	return h.Sum(nil)
}
