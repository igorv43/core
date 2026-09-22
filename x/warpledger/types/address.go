package types

import (
	"encoding/binary"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
)

// DeriveDepositAddress returns the migration deposit address for a
// (token, domain, recipient) triple (spec §10.2):
//
//	address.Module("warpledger", "migrate" || token_id || domain(be32) || recipient)
//
// The token id is part of the derivation so that a route is unambiguous even
// when more than one collateral token exists for the same denom. The address
// has no private key; only MsgSweepMigration moves its balance.
func DeriveDepositAddress(tokenId util.HexAddress, domain uint32, recipient util.HexAddress) sdk.AccAddress {
	domainBz := make([]byte, 4)
	binary.BigEndian.PutUint32(domainBz, domain)

	return address.Module(
		ModuleName,
		[]byte(MigrationDerivationPrefix),
		tokenId.Bytes(),
		domainBz,
		recipient.Bytes(),
	)
}
