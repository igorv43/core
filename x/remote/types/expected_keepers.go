package types

import (
	"context"
	"time"

	"cosmossdk.io/math"
	"cosmossdk.io/x/feegrant"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// BankKeeper is the subset of x/bank used by the module.
type BankKeeper interface {
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
}

// CoreKeeper is the subset of the Hyperlane core keeper used by the module.
type CoreKeeper interface {
	AppRouter() *util.Router[util.HyperlaneApp]
	GetMailbox(ctx context.Context, mailboxId util.HexAddress) (coretypes.Mailbox, error)
}

// AuthzKeeper grants and revokes the session-key authorizations.
type AuthzKeeper interface {
	SaveGrant(ctx context.Context, grantee, granter sdk.AccAddress, authorization authz.Authorization, expiration *time.Time) error
	DeleteGrant(ctx context.Context, grantee, granter sdk.AccAddress, msgType string) error
}

// FeeGrantKeeper grants the paymaster allowances.
type FeeGrantKeeper interface {
	GrantAllowance(ctx context.Context, granter, grantee sdk.AccAddress, feeAllowance feegrant.FeeAllowanceI) error
	GetAllowance(ctx context.Context, granter, grantee sdk.AccAddress) (feegrant.FeeAllowanceI, error)
}

// PerpKeeper reports the free collateral of an account (paymaster eligibility).
type PerpKeeper interface {
	FreeCollateral(ctx sdk.Context, account string) (math.Int, error)
}
