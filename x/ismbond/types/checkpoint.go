package types

import (
	"encoding/binary"
	"fmt"
	"strings"

	errorsmod "cosmossdk.io/errors"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/ethereum/go-ethereum/crypto"
)

// NormalizeValidator validates and lowercases a 20-byte ethereum-style
// address ("0x" + 40 hex chars), the identity Hyperlane validators sign with.
func NormalizeValidator(s string) (string, error) {
	bz, err := util.DecodeEthHex(s)
	if err != nil || len(bz) != 20 {
		return "", errorsmod.Wrap(ErrInvalidValidator, "must be a 20-byte hex address")
	}
	return strings.ToLower(util.EncodeEthHex(bz)), nil
}

// Digest returns the checkpoint digest Hyperlane validators sign:
// EthSigningHash(keccak256(domainHash ‖ root ‖ index ‖ message_id)) with
// domainHash = keccak256(origin ‖ merkle_tree_hook ‖ "HYPERLANE"). It mirrors
// the verification of the upstream multisig ISMs.
func (c Checkpoint) Digest() ([32]byte, error) {
	if len(c.Root) != 32 || len(c.MessageId) != 32 {
		return [32]byte{}, errorsmod.Wrap(ErrInvalidEvidence, "root and message_id must be 32 bytes")
	}
	domain := make([]byte, 0, 46)
	domain = binary.BigEndian.AppendUint32(domain, c.Origin)
	domain = append(domain, c.MerkleTreeHook.Bytes()...)
	domain = append(domain, []byte("HYPERLANE")...)
	domainHash := crypto.Keccak256Hash(domain)

	bz := make([]byte, 0, 32+32+4+32)
	bz = append(bz, domainHash[:]...)
	bz = append(bz, c.Root...)
	bz = binary.BigEndian.AppendUint32(bz, c.Index)
	bz = append(bz, c.MessageId...)
	return util.GetEthSigningHash(crypto.Keccak256(bz)), nil
}

// Signer recovers the validator address that signed the checkpoint.
func (c Checkpoint) Signer() (string, error) {
	digest, err := c.Digest()
	if err != nil {
		return "", err
	}
	if len(c.Signature) != 65 {
		return "", errorsmod.Wrap(ErrInvalidEvidence, "signature must be 65 bytes")
	}
	sig := make([]byte, 65)
	copy(sig, c.Signature) // RecoverEthSignature mutates the recovery id
	pub, err := util.RecoverEthSignature(digest[:], sig)
	if err != nil {
		return "", errorsmod.Wrap(ErrInvalidEvidence, fmt.Sprintf("signature recovery: %v", err))
	}
	return strings.ToLower(crypto.PubkeyToAddress(*pub).Hex()), nil
}

// SameSlot reports whether two checkpoints attest the same (origin, hook, index).
func (c Checkpoint) SameSlot(o Checkpoint) bool {
	return c.Origin == o.Origin && c.MerkleTreeHook.Equal(o.MerkleTreeHook) && c.Index == o.Index
}
