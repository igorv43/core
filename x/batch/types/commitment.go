package types

import (
	"crypto/sha256"
	"encoding/binary"
)

// Commitment computes the sealed-bid commitment of spec §15.1:
//
//	SHA256(bid_bytes || salt || solver || batch_id)
//
// bid_bytes is the protobuf encoding of the Bid; batch_id is big-endian.
func Commitment(bid Bid, salt []byte, solver string, batchID uint64) ([]byte, error) {
	bidBytes, err := bid.Marshal()
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	h.Write(bidBytes)
	h.Write(salt)
	h.Write([]byte(solver))
	var id [8]byte
	binary.BigEndian.PutUint64(id[:], batchID)
	h.Write(id[:])
	return h.Sum(nil), nil
}
