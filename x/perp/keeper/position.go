package keeper

import (
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetPosition returns the position of an account in a market.
func (k Keeper) GetPosition(ctx sdk.Context, account, marketID string) (types.Position, bool, error) {
	p, err := k.Positions.Get(ctx, collections.Join(account, marketID))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Position{}, false, nil
		}
		return types.Position{}, false, err
	}
	return p, true, nil
}

// setPosition stores a position and (re)indexes it by liquidation price.
func (k Keeper) setPosition(ctx sdk.Context, market types.Market, p types.Position) error {
	old, had, err := k.GetPosition(ctx, p.Account, p.MarketId)
	if err != nil {
		return err
	}
	if had {
		if err := k.LiqIndex.Remove(ctx, collections.Join(types.MarketSideKey(old.MarketId, old.Side), types.IndexKey(old.LiqPriceIndexed, old.Account))); err != nil {
			return err
		}
	}
	p.LiqPriceIndexed = p.LiquidationPrice(market.FundingIndex)
	p.LastUpdateHeight = ctx.BlockHeight()
	if err := k.Positions.Set(ctx, collections.Join(p.Account, p.MarketId), p); err != nil {
		return err
	}
	if err := k.PositionsByMarket.Set(ctx, collections.Join(p.MarketId, p.Account)); err != nil {
		return err
	}
	return k.LiqIndex.Set(ctx, collections.Join(types.MarketSideKey(p.MarketId, p.Side), types.IndexKey(p.LiqPriceIndexed, p.Account)))
}

// removePosition deletes a position and its index entries.
func (k Keeper) removePosition(ctx sdk.Context, p types.Position) error {
	if err := k.Positions.Remove(ctx, collections.Join(p.Account, p.MarketId)); err != nil {
		return err
	}
	if err := k.PositionsByMarket.Remove(ctx, collections.Join(p.MarketId, p.Account)); err != nil {
		return err
	}
	return k.LiqIndex.Remove(ctx, collections.Join(types.MarketSideKey(p.MarketId, p.Side), types.IndexKey(p.LiqPriceIndexed, p.Account)))
}

// positionsOfMarket returns every position of a market in account order.
func (k Keeper) positionsOfMarket(ctx sdk.Context, marketID string) ([]types.Position, error) {
	var out []types.Position
	err := k.PositionsByMarket.Walk(ctx, collections.NewPrefixedPairRange[string, string](marketID),
		func(key collections.Pair[string, string]) (bool, error) {
			p, err := k.Positions.Get(ctx, collections.Join(key.K2(), key.K1()))
			if err != nil {
				return true, err
			}
			out = append(out, p)
			return false, nil
		})
	return out, err
}

// PositionsOfAccount returns every position of an account in market order.
func (k Keeper) PositionsOfAccount(ctx sdk.Context, account string) ([]types.Position, error) {
	var out []types.Position
	err := k.Positions.Walk(ctx, collections.NewPrefixedPairRange[string, string](account),
		func(_ collections.Pair[string, string], p types.Position) (bool, error) {
			out = append(out, p)
			return false, nil
		})
	return out, err
}

func (k Keeper) openPositionCount(ctx sdk.Context, account string) (uint32, error) {
	n := uint32(0)
	err := k.Positions.Walk(ctx, collections.NewPrefixedPairRange[string, string](account),
		func(_ collections.Pair[string, string], _ types.Position) (bool, error) { n++; return false, nil })
	return n, err
}

func (k Keeper) adjustOI(ctx sdk.Context, market *types.Market, side batchtypes.Side, delta math.Int) {
	if side == batchtypes.SIDE_BUY {
		market.OiLong = market.OiLong.Add(delta)
	} else {
		market.OiShort = market.OiShort.Add(delta)
	}
}

// reducePosition closes qty of a position at price: realizes PnL and funding
// on the closed part, returns the proportional collateral plus the realized
// result to the account's free collateral. A negative result beyond the
// collateral share is covered by the insurance fund (shortfall).
func (k Keeper) reducePosition(ctx sdk.Context, market *types.Market, p *types.Position, qty math.Int, price math.LegacyDec) (realized, shortfall math.Int, err error) {
	if qty.GT(p.Qty) {
		qty = p.Qty
	}
	part := types.Position{Side: p.Side, Qty: qty, EntryPrice: p.EntryPrice, FundingIndexAtOpen: p.FundingIndexAtOpen, Collateral: math.ZeroInt(), MaintenanceMargin: p.MaintenanceMargin}
	realized = part.UnrealizedPnl(price).Sub(part.FundingOwed(market.FundingIndex))
	// collateral share, rounded down for the participant
	share := p.Collateral.Mul(qty).Quo(p.Qty)
	credit := share.Add(realized)
	shortfall = math.ZeroInt()
	if credit.IsNegative() {
		shortfall = credit.Neg()
		credit = math.ZeroInt()
	}
	p.Qty = p.Qty.Sub(qty)
	p.Collateral = p.Collateral.Sub(share)
	p.RealizedPnl = p.RealizedPnl.Add(realized)
	k.adjustOI(ctx, market, p.Side, qty.Neg())
	if p.Account == types.InsuranceFundAddress() {
		// the fund's inventory settles into the fund balance
		if err := k.creditInsurance(ctx, credit); err != nil {
			return realized, shortfall, err
		}
	} else if err := k.addFree(ctx, p.Account, credit); err != nil {
		return realized, shortfall, err
	}
	if shortfall.IsPositive() {
		uncovered, err := k.debitInsurance(ctx, shortfall)
		if err != nil {
			return realized, shortfall, err
		}
		if uncovered.IsPositive() {
			// spec §18.1 step 4: the fund could not cover; ADL of the counterparties
			k.Logger(ctx).Error("insurance fund could not cover a shortfall", "market", p.MarketId, "account", p.Account, "uncovered", uncovered)
		}
	}
	return realized, shortfall, nil
}

// increasePosition opens or adds qty at price, moving IM·notional from free
// collateral into the position. Pending funding of the existing size is
// settled into the collateral first so the funding index can be reset.
func (k Keeper) increasePosition(ctx sdk.Context, params types.Params, market *types.Market, account string, side batchtypes.Side, qty math.Int, price math.LegacyDec, p *types.Position, exists bool) error {
	im := types.InitialMargin(market.EffectiveLeverage)
	required := types.RequiredMargin(qty, price, im)
	if !exists {
		n, err := k.openPositionCount(ctx, account)
		if err != nil {
			return err
		}
		if n >= params.MaxOpenPositionsPerAccount {
			return errorsmod.Wrapf(types.ErrTooManyPositions, "max %d", params.MaxOpenPositionsPerAccount)
		}
		*p = types.Position{Account: account, MarketId: market.Id, Side: side, Qty: math.ZeroInt(), EntryPrice: price,
			Collateral: math.ZeroInt(), FundingIndexAtOpen: market.FundingIndex, RealizedPnl: math.ZeroInt()}
	} else {
		owed := p.FundingOwed(market.FundingIndex)
		p.Collateral = p.Collateral.Sub(owed)
		p.RealizedPnl = p.RealizedPnl.Sub(owed)
		p.FundingIndexAtOpen = market.FundingIndex
		if p.Collateral.IsNegative() {
			return errorsmod.Wrap(types.ErrInsufficientMargin, "funding owed exceeds the position collateral")
		}
	}
	if account == types.InsuranceFundAddress() {
		return errorsmod.Wrap(types.ErrReduceOnly, "the insurance fund only reduces inventory")
	}
	if err := k.addFree(ctx, account, required.Neg()); err != nil {
		return errorsmod.Wrapf(types.ErrInsufficientMargin, "initial margin %s: %v", required, err)
	}
	// weighted entry price
	oldNotional := p.EntryPrice.MulInt(p.Qty)
	newQty := p.Qty.Add(qty)
	p.EntryPrice = oldNotional.Add(price.MulInt(qty)).QuoInt(newQty)
	p.Qty = newQty
	p.Collateral = p.Collateral.Add(required)
	p.MaintenanceMargin = im.QuoInt64(2)
	k.adjustOI(ctx, market, side, qty)
	return nil
}

// View builds the derived view of a position at the market's mark price.
func (k Keeper) View(ctx sdk.Context, market types.Market, p types.Position) types.PositionView {
	mark := market.MarkPrice
	return types.PositionView{
		Position:          p,
		MarkPrice:         mark,
		Notional:          types.Notional(p.Qty, mark),
		UnrealizedPnl:     p.UnrealizedPnl(mark),
		FundingOwed:       p.FundingOwed(market.FundingIndex),
		Equity:            p.Equity(mark, market.FundingIndex),
		MaintenanceMargin: p.MaintenanceRequirement(mark),
		LiquidationPrice:  p.LiquidationPrice(market.FundingIndex),
		AdlScore:          p.ADLScore(mark, market.FundingIndex),
	}
}
