package types

import (
	"context"
	"time"

	"cosmossdk.io/math"
	"cosmossdk.io/x/feegrant"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	liquidstakekeeper "github.com/classic-terra/core/v4/x/liquidstake/keeper"
	warpledgertypes "github.com/classic-terra/core/v4/x/warpledger/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// BankKeeper is the subset of x/bank used by the module.
type BankKeeper interface {
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
}

// CoreKeeper is the subset of the Hyperlane core keeper used by the module.
type CoreKeeper interface {
	AppRouter() *util.Router[util.HyperlaneApp]
	GetMailbox(ctx context.Context, mailboxId util.HexAddress) (coretypes.Mailbox, error)
	DispatchMessage(ctx sdk.Context, originMailboxId util.HexAddress, sender util.HexAddress, maxFee sdk.Coins, destinationDomain uint32,
		recipient util.HexAddress, body []byte, metadata util.StandardHookMetadata, postDispatchHookId *util.HexAddress) (util.HexAddress, error)
}

// LiquidStakeKeeper gives the exchange rate for the EXCHANGE_RATE beacon.
type LiquidStakeKeeper interface {
	ExchangeRate(ctx sdk.Context) (math.LegacyDec, liquidstakekeeper.Totals, error)
}

// WarpLedgerKeeper gives the route ledger for the ROUTE_SOLVENCY beacon.
type WarpLedgerKeeper interface {
	GetToken(ctx sdk.Context, tokenId util.HexAddress) (warptypes.HypToken, error)
	GetLedger(ctx sdk.Context, tokenId util.HexAddress, domain uint32) (warpledgertypes.DomainLedger, bool, error)
	EffectiveCap(ctx sdk.Context, ledger warpledgertypes.DomainLedger) (math.Int, error)
	TotalExposure(ctx sdk.Context, tokenId util.HexAddress) (math.Int, error)
}

// RootRecorder is x/ismbond: outbound roots are recorded after every dispatch.
type RootRecorder interface {
	RecordRoot(ctx sdk.Context, mailboxId util.HexAddress) error
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

// PerpKeeper reports the free collateral of an account (paymaster
// eligibility) and the digest of its positions (POSITION_DIGEST beacon).
type PerpKeeper interface {
	FreeCollateral(ctx sdk.Context, account string) (math.Int, error)
	PositionDigest(ctx sdk.Context, account string) (digest [32]byte, free math.Int, positions int, equity math.Int, err error)
}
