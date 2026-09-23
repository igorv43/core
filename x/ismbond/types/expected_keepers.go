package types

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	pdtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/types"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper is the subset of x/bank used by the module.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
}

// CoreKeeper is the subset of the Hyperlane core used to read the local
// mailbox state that outbound checkpoints attest.
type CoreKeeper interface {
	GetMailbox(ctx context.Context, mailboxId util.HexAddress) (coretypes.Mailbox, error)
	MerkleTreeHookQuery(ctx context.Context, req *pdtypes.QueryMerkleTreeHookRequest) (*pdtypes.QueryMerkleTreeHookResponse, error)
	IsmQuery(ctx context.Context, req *ismtypes.QueryIsmRequest) (*ismtypes.QueryIsmResponse, error)
}

// DistributionKeeper receives the slashed remainder.
type DistributionKeeper interface {
	FundCommunityPool(ctx context.Context, amount sdk.Coins, sender sdk.AccAddress) error
}
