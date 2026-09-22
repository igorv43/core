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

// PerpFill is one cleared quantity of a perpetual market handed to the
// margin hook: the account trades qty at price against the protocol.
type PerpFill struct {
	Batch      uint64
	Account    sdk.AccAddress
	Market     Market
	Side       Side
	Qty        math.Int
	Price      math.LegacyDec
	ReduceOnly bool
	// Frontend and BuilderFee (quote units) pay the integrator of the order
	// from the account's collateral (spec §23.2); empty when not attributed.
	Frontend   string
	BuilderFee math.Int
	// Solver marks a market-maker level (never reduce-only).
	Solver bool
}

// MarginHook is implemented by x/perp for MARKET_TYPE_PERP markets: instead of
// escrowing the full amount, an order reserves initial margin (spec §14.3).
// Spot markets never call it. Reservations are a running sum per account and
// market: x/batch calls Release with the same arguments it used to Reserve
// (for the unfilled part on close, for the filled part right before Fill).
type MarginHook interface {
	// Reserve locks initial margin for an order at its limit price.
	Reserve(ctx sdk.Context, account sdk.AccAddress, market Market, side Side, qty math.Int, limitPrice math.LegacyDec) error
	// Release frees the reservation of a cancelled, expired, excluded or filled order.
	Release(ctx sdk.Context, account sdk.AccAddress, market Market, side Side, qty math.Int, limitPrice math.LegacyDec) error
	// Fill applies a cleared quantity at the clearing price; it returns an error
	// when the account no longer covers the initial margin at that price (the
	// order is then excluded and the batch re-resolved, spec §14.3).
	Fill(ctx sdk.Context, fill PerpFill) error
}
