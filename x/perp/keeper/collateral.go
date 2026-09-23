package keeper

import (
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Collateral ledger: every settlement unit deposited lives in the module
// account; the keeper tracks free collateral per account, margin inside
// positions, reservations of open orders and the module-level ledger
// (insurance fund, revenue, burn budget).

// FreeCollateral returns the account's collateral outside positions.
func (k Keeper) FreeCollateral(ctx sdk.Context, account string) (math.Int, error) {
	v, err := k.Collateral.Get(ctx, account)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.Int{}, err
	}
	return v, nil
}

func (k Keeper) setFree(ctx sdk.Context, account string, v math.Int) error {
	if v.IsNegative() {
		return errorsmod.Wrapf(types.ErrInsufficientFree, "free collateral of %s would be %s", account, v)
	}
	if v.IsZero() {
		return k.Collateral.Remove(ctx, account)
	}
	return k.Collateral.Set(ctx, account, v)
}

// addFree changes the free collateral by delta (negative allowed down to zero).
func (k Keeper) addFree(ctx sdk.Context, account string, delta math.Int) error {
	cur, err := k.FreeCollateral(ctx, account)
	if err != nil {
		return err
	}
	return k.setFree(ctx, account, cur.Add(delta))
}

// ReservedTotal returns the margin reserved by the account's open orders across markets.
func (k Keeper) ReservedTotal(ctx sdk.Context, account string) (math.Int, error) {
	total := math.ZeroInt()
	err := k.Reservations.Walk(ctx, collections.NewPrefixedPairRange[string, string](account),
		func(_ collections.Pair[string, string], v math.Int) (bool, error) {
			total = total.Add(v)
			return false, nil
		})
	return total, err
}

// Available returns free collateral minus reservations: what new orders and
// withdrawals may use.
func (k Keeper) Available(ctx sdk.Context, account string) (math.Int, error) {
	free, err := k.FreeCollateral(ctx, account)
	if err != nil {
		return math.Int{}, err
	}
	reserved, err := k.ReservedTotal(ctx, account)
	if err != nil {
		return math.Int{}, err
	}
	return free.Sub(reserved), nil
}

// Deposit moves settlement from the account into the module as free collateral.
func (k Keeper) Deposit(ctx sdk.Context, account sdk.AccAddress, amount sdk.Coin) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if amount.Denom == params.StDenom {
		return k.depositSt(ctx, params, account, amount)
	}
	if amount.Denom != params.SettlementDenom {
		return errorsmod.Wrapf(types.ErrInvalidCollateral, "collateral must be %s or %s", params.SettlementDenom, params.StDenom)
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, account, types.ModuleName, sdk.NewCoins(amount)); err != nil {
		return err
	}
	if err := k.addFree(ctx, account.String(), amount.Amount); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventCollateralDeposited{Account: account.String(), Amount: amount.String()})
}

// depositSt accepts stLUNC as collateral (spec §21.5): only while the
// internal LUNC/settlement spot market is enabled (rule 8, the conversion
// path of seized collateral) and within the global cap (rule 6).
func (k Keeper) depositSt(ctx sdk.Context, params types.Params, account sdk.AccAddress, amount sdk.Coin) error {
	if params.SpotMarketId == "" {
		return errorsmod.Wrap(types.ErrInvalidCollateral, "stLUNC needs the internal LUNC/settlement spot market (spot_market_id unset)")
	}
	spot, err := k.batchKeeper.GetMarket(ctx, params.SpotMarketId)
	if err != nil || !spot.Enabled || spot.QuoteDenom != params.SettlementDenom {
		return errorsmod.Wrap(types.ErrInvalidCollateral, "stLUNC needs the internal LUNC/settlement spot market enabled")
	}
	val := k.stValuation(ctx, params)
	if !val.Available {
		return errorsmod.Wrap(types.ErrInvalidCollateral, "stLUNC cannot be valued now (exchange rate or LUNC price missing)")
	}
	if err := k.assertGlobalCap(ctx, params, val, amount.Amount); err != nil {
		return err
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, account, types.ModuleName, sdk.NewCoins(amount)); err != nil {
		return err
	}
	if err := k.addFreeSt(ctx, account.String(), amount.Amount); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventCollateralDeposited{Account: account.String(), Amount: amount.String()})
}

// Withdraw returns available collateral to the account.
func (k Keeper) Withdraw(ctx sdk.Context, account sdk.AccAddress, amount sdk.Coin) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if amount.Denom == params.StDenom {
		return k.withdrawSt(ctx, params, account, amount)
	}
	if amount.Denom != params.SettlementDenom {
		return errorsmod.Wrapf(types.ErrInvalidCollateral, "collateral is %s or %s", params.SettlementDenom, params.StDenom)
	}
	avail, err := k.Available(ctx, account.String())
	if err != nil {
		return err
	}
	if avail.LT(amount.Amount) {
		return errorsmod.Wrapf(types.ErrInsufficientFree, "available %s, requested %s", avail, amount.Amount)
	}
	if err := k.addFree(ctx, account.String(), amount.Amount.Neg()); err != nil {
		return err
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, account, sdk.NewCoins(amount)); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventCollateralWithdrawn{Account: account.String(), Amount: amount.String()})
}

// withdrawSt returns free stLUNC as long as the reservations of the account
// stay covered by what remains (settlement plus stLUNC capacity).
func (k Keeper) withdrawSt(ctx sdk.Context, params types.Params, account sdk.AccAddress, amount sdk.Coin) error {
	freeSt, err := k.FreeSt(ctx, account.String())
	if err != nil {
		return err
	}
	if freeSt.LT(amount.Amount) {
		return errorsmod.Wrapf(types.ErrInsufficientFree, "free stLUNC %s, requested %s", freeSt, amount.Amount)
	}
	free, err := k.FreeCollateral(ctx, account.String())
	if err != nil {
		return err
	}
	reserved, err := k.ReservedTotal(ctx, account.String())
	if err != nil {
		return err
	}
	val := k.stValuation(ctx, params)
	if reserved.GT(free.Add(stCapacity(params, free, val.Value(freeSt.Sub(amount.Amount))))) {
		return errorsmod.Wrap(types.ErrInsufficientFree, "open orders reserve this stLUNC")
	}
	if err := k.addFreeSt(ctx, account.String(), amount.Amount.Neg()); err != nil {
		return err
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, account, sdk.NewCoins(amount)); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventCollateralWithdrawn{Account: account.String(), Amount: amount.String()})
}

// FundInsurance moves settlement from the account into the insurance fund
// (permissionless donation; the fund can never be withdrawn, spec §26.3).
func (k Keeper) FundInsurance(ctx sdk.Context, account sdk.AccAddress, amount sdk.Coin) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if amount.Denom != params.SettlementDenom {
		return errorsmod.Wrapf(types.ErrInvalidCollateral, "the insurance fund holds %s", params.SettlementDenom)
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, account, types.ModuleName, sdk.NewCoins(amount)); err != nil {
		return err
	}
	return k.creditInsurance(ctx, amount.Amount)
}

// reservation returns the margin reserved by the account in a market.
func (k Keeper) reservation(ctx sdk.Context, account, marketID string) (math.Int, error) {
	v, err := k.Reservations.Get(ctx, collections.Join(account, marketID))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.Int{}, err
	}
	return v, nil
}

func (k Keeper) setReservation(ctx sdk.Context, account, marketID string, v math.Int) error {
	if !v.IsPositive() {
		return k.Reservations.Remove(ctx, collections.Join(account, marketID))
	}
	return k.Reservations.Set(ctx, collections.Join(account, marketID), v)
}

//-----------------------------------
// Module ledger

// insuranceBalance returns the insurance fund balance.
func (k Keeper) insuranceBalance(ctx sdk.Context) (math.Int, error) {
	l, err := k.GetLedger(ctx)
	if err != nil {
		return math.Int{}, err
	}
	return l.Insurance, nil
}

// creditInsurance adds settlement to the insurance fund.
func (k Keeper) creditInsurance(ctx sdk.Context, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}
	l, err := k.GetLedger(ctx)
	if err != nil {
		return err
	}
	l.Insurance = l.Insurance.Add(amount)
	return k.Ledger.Set(ctx, l)
}

// debitInsurance takes settlement from the insurance fund; it returns the
// part it could not cover.
func (k Keeper) debitInsurance(ctx sdk.Context, amount math.Int) (uncovered math.Int, err error) {
	if !amount.IsPositive() {
		return math.ZeroInt(), nil
	}
	l, err := k.GetLedger(ctx)
	if err != nil {
		return math.Int{}, err
	}
	take := math.MinInt(amount, l.Insurance)
	l.Insurance = l.Insurance.Sub(take)
	return amount.Sub(take), k.Ledger.Set(ctx, l)
}

// creditRevenue adds settlement to the protocol revenue of the epoch.
func (k Keeper) creditRevenue(ctx sdk.Context, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}
	l, err := k.GetLedger(ctx)
	if err != nil {
		return err
	}
	l.Revenue = l.Revenue.Add(amount)
	return k.Ledger.Set(ctx, l)
}

// routeFee splits a protocol fee or penalty: if_fee_share to the insurance
// fund while IF < IF_target, the rest to the revenue of the allocation
// cascade (spec §23). Penalties go 100% to the fund (allToInsurance).
func (k Keeper) routeFee(ctx sdk.Context, amount math.Int, allToInsurance bool) error {
	if !amount.IsPositive() {
		return nil
	}
	if allToInsurance {
		return k.creditInsurance(ctx, amount)
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	balance, err := k.insuranceBalance(ctx)
	if err != nil {
		return err
	}
	target, err := k.insuranceTargetEndBlock(ctx)
	if err != nil {
		return err
	}
	toInsurance := math.ZeroInt()
	if balance.LT(target) {
		toInsurance = math.MinInt(params.IfFeeShare.MulInt(amount).Ceil().TruncateInt(), target.Sub(balance))
	}
	if err := k.creditInsurance(ctx, toInsurance); err != nil {
		return err
	}
	return k.creditRevenue(ctx, amount.Sub(toInsurance))
}
