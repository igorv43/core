package types

import (
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper is the subset of x/bank used by the module.
type BankKeeper interface {
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
}

// OracleKeeper is the subset of x/oracle used for the reference price.
type OracleKeeper interface {
	// GetPrice returns the reference price of a name: the USD price of an
	// asset target or the LUNC exchange rate in a whitelisted denom.
	GetPrice(ctx sdk.Context, name string) (math.LegacyDec, error)
}

// FeeSink receives protocol fees and slashes. Until the insurance fund of
// x/perp exists the default sink funds the community pool (spec §23).
type FeeSink interface {
	Deposit(ctx sdk.Context, from string, coins sdk.Coins) error
}

// MarginHook is implemented by x/perp for MARKET_TYPE_PERP markets: instead of
// escrowing the full amount, an order reserves initial margin (spec §14.3).
// Spot markets never call it.
type MarginHook interface {
	// Reserve locks initial margin for an order at its limit price.
	Reserve(ctx sdk.Context, account sdk.AccAddress, market Market, side Side, qty math.Int, limitPrice math.LegacyDec) error
	// Release frees the reservation of a cancelled, expired or excluded order.
	Release(ctx sdk.Context, account sdk.AccAddress, market Market, side Side, qty math.Int, limitPrice math.LegacyDec) error
	// Fill applies a cleared quantity at the clearing price; it returns an error
	// when the account no longer covers the initial margin at that price.
	Fill(ctx sdk.Context, account sdk.AccAddress, market Market, side Side, qty math.Int, price math.LegacyDec) error
}
