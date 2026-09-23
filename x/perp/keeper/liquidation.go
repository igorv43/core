package keeper

import (
	"sort"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Liquidation (spec §18.3), auto top-up (§14.5) and ADL (§18.2) driven by
// the incremental liquidation-price index of §19.5.

// liquidationCandidates walks the index for the positions whose indexed
// liquidation price is crossed by the mark: longs with liq >= mark, shorts
// with liq <= mark. Bounded by limit.
func (k Keeper) liquidationCandidates(ctx sdk.Context, market types.Market, limit int) ([]types.Position, error) {
	var out []types.Position
	collect := func(key collections.Pair[string, []byte]) (bool, error) {
		account := string(key.K2()[32:])
		p, has, err := k.GetPosition(ctx, account, market.Id)
		if err != nil {
			return true, err
		}
		if has {
			out = append(out, p)
		}
		return len(out) >= limit, nil
	}
	longs := collections.NewPrefixedPairRange[string, []byte](types.MarketSideKey(market.Id, batchtypes.SIDE_BUY)).
		StartInclusive(types.PriceKey(market.MarkPrice))
	if err := k.LiqIndex.Walk(ctx, longs, collect); err != nil {
		return nil, err
	}
	if len(out) >= limit {
		return out, nil
	}
	upper := append(types.PriceKey(market.MarkPrice), []byte(string(rune(0xff)))...)
	shorts := collections.NewPrefixedPairRange[string, []byte](types.MarketSideKey(market.Id, batchtypes.SIDE_SELL)).
		EndInclusive(upper).Descending()
	if err := k.LiqIndex.Walk(ctx, shorts, collect); err != nil {
		return nil, err
	}
	return out, nil
}

// sweepLiquidations checks the crossed positions of a market at the current
// mark: still healthy ones are re-indexed, auto top-up rescues opted-in
// accounts, the rest are liquidated. Nothing happens in a PAUSED market.
func (k Keeper) sweepLiquidations(ctx sdk.Context, params types.Params, m *types.Market) error {
	if m.State == types.ORACLE_STATE_PAUSED || !m.MarkPrice.IsPositive() {
		return nil
	}
	candidates, err := k.liquidationCandidates(ctx, *m, int(params.MaxLiquidationsPerBlock))
	if err != nil {
		return err
	}
	fund := types.InsuranceFundAddress()
	for _, p := range candidates {
		if p.Account == fund {
			continue
		}
		equity := p.Equity(m.MarkPrice, m.FundingIndex)
		required := p.MaintenanceRequirement(m.MarkPrice)
		if equity.GTE(required) {
			if err := k.setPosition(ctx, *m, p); err != nil { // stale index entry: refresh
				return err
			}
			continue
		}
		if rescued, err := k.autoTopUp(ctx, *m, &p, equity); err != nil {
			return err
		} else if rescued {
			continue
		}
		if err := k.liquidate(ctx, params, m, p); err != nil {
			return err
		}
	}
	return nil
}

// autoTopUp moves free collateral into the position up to the initial margin
// when the account opted in (spec §14.5 layer 4, item 2).
func (k Keeper) autoTopUp(ctx sdk.Context, m types.Market, p *types.Position, equity math.Int) (bool, error) {
	if has, err := k.AutoTopUp.Has(ctx, p.Account); err != nil || !has {
		return false, err
	}
	need := types.RequiredMargin(p.Qty, m.MarkPrice, types.InitialMargin(leverageInForce(m))).Sub(equity)
	if !need.IsPositive() {
		return false, nil
	}
	avail, err := k.Available(ctx, p.Account)
	if err != nil {
		return false, err
	}
	if avail.LT(need) {
		return false, nil
	}
	if err := k.addFree(ctx, p.Account, need.Neg()); err != nil {
		return false, err
	}
	p.Collateral = p.Collateral.Add(need)
	if err := k.setPosition(ctx, m, *p); err != nil {
		return false, err
	}
	return true, ctx.EventManager().EmitTypedEvent(&types.EventAutoTopUp{Account: p.Account, MarketId: m.Id, Amount: need.String()})
}

// liquidate applies the waterfall of spec §18.1 to a position below
// maintenance: penalty on the notional to the fund, transfer of the position
// to the fund at the mark (the fund absorbs a negative equity), or ADL when
// the fund's inventory cap is hit or the fund cannot absorb the loss.
func (k Keeper) liquidate(ctx sdk.Context, params types.Params, m *types.Market, p types.Position) error {
	notional := types.Notional(p.Qty, m.MarkPrice)
	penalty := params.LiqPenalty.MulInt(notional).Ceil().TruncateInt()
	equity := p.Equity(m.MarkPrice, m.FundingIndex)

	inv, err := k.insuranceInventory(ctx, m.Id)
	if err != nil {
		return err
	}
	delta := p.Qty
	if p.Side == batchtypes.SIDE_SELL {
		delta = delta.Neg()
	}
	maxInventory := params.IfMaxInventory.MulInt(m.OiCap).TruncateInt()
	balance, err := k.insuranceBalance(ctx)
	if err != nil {
		return err
	}
	shortfall := math.ZeroInt()
	if equity.LT(penalty) {
		shortfall = penalty.Sub(equity)
	}
	toADL := inv.Add(delta).Abs().GT(maxInventory) || balance.LT(shortfall)
	if toADL {
		if err := k.autoDeleverage(ctx, m, p); err != nil {
			return err
		}
		return ctx.EventManager().EmitTypedEvent(&types.EventPositionLiquidated{Account: p.Account, MarketId: m.Id, Side: p.Side.String(),
			Qty: p.Qty.String(), MarkPrice: m.MarkPrice.String(), Penalty: "0", Shortfall: shortfall.String(), ToAdl: true})
	}

	// 1–2: the position's margin pays its loss and the penalty
	if err := k.removePosition(ctx, p); err != nil {
		return err
	}
	remaining := equity.Sub(penalty) // collateral left after loss and penalty (may be negative)
	if err := k.creditInsurance(ctx, math.MinInt(penalty, math.MaxInt(equity, math.ZeroInt()))); err != nil {
		return err
	}
	if remaining.IsNegative() {
		// 3: the fund absorbs the difference
		if _, err := k.debitInsurance(ctx, remaining.Neg()); err != nil {
			return err
		}
		remaining = math.ZeroInt()
	}
	// the position moves to the fund at the mark price with what is left as collateral
	k.adjustOI(ctx, m, p.Side, p.Qty.Neg())
	if err := k.fundTakePosition(ctx, m, p.Side, p.Qty, remaining); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventPositionLiquidated{Account: p.Account, MarketId: m.Id, Side: p.Side.String(),
		Qty: p.Qty.String(), MarkPrice: m.MarkPrice.String(), Penalty: penalty.String(), Shortfall: shortfall.String()})
}

// fundTakePosition adds qty on side at the mark to the fund's inventory:
// nets against an opposite inventory first, then opens or increases.
func (k Keeper) fundTakePosition(ctx sdk.Context, m *types.Market, side batchtypes.Side, qty, collateral math.Int) error {
	fund := types.InsuranceFundAddress()
	pos, exists, err := k.GetPosition(ctx, fund, m.Id)
	if err != nil {
		return err
	}
	if exists && pos.Side != side {
		closeQty := math.MinInt(qty, pos.Qty)
		if _, _, err := k.reducePosition(ctx, m, &pos, closeQty, m.MarkPrice); err != nil {
			return err
		}
		qty = qty.Sub(closeQty)
		if pos.Qty.IsZero() {
			if err := k.removePosition(ctx, pos); err != nil {
				return err
			}
			exists = false
		} else if err := k.setPosition(ctx, *m, pos); err != nil {
			return err
		}
		if qty.IsZero() {
			return k.creditInsurance(ctx, collateral)
		}
		// the closed part's collateral share goes to the fund balance
		share := collateral.Mul(closeQty).Quo(closeQty.Add(qty))
		if err := k.creditInsurance(ctx, share); err != nil {
			return err
		}
		collateral = collateral.Sub(share)
	}
	if !exists {
		pos = types.Position{Account: fund, MarketId: m.Id, Side: side, Qty: math.ZeroInt(), EntryPrice: m.MarkPrice,
			Collateral: math.ZeroInt(), FundingIndexAtOpen: m.FundingIndex, RealizedPnl: math.ZeroInt(),
			MaintenanceMargin: types.MaintenanceMargin(m.MaxLeverage)}
	} else {
		owed := pos.FundingOwed(m.FundingIndex)
		pos.Collateral = pos.Collateral.Sub(owed)
		pos.FundingIndexAtOpen = m.FundingIndex
	}
	newQty := pos.Qty.Add(qty)
	pos.EntryPrice = pos.EntryPrice.MulInt(pos.Qty).Add(m.MarkPrice.MulInt(qty)).QuoInt(newQty)
	pos.Qty = newQty
	pos.Collateral = pos.Collateral.Add(collateral)
	k.adjustOI(ctx, m, side, qty)
	return k.setPosition(ctx, *m, pos)
}

// insuranceInventory returns the fund's net inventory in a market (long positive).
func (k Keeper) insuranceInventory(ctx sdk.Context, marketID string) (math.Int, error) {
	pos, exists, err := k.GetPosition(ctx, types.InsuranceFundAddress(), marketID)
	if err != nil || !exists {
		return math.ZeroInt(), err
	}
	if pos.Side == batchtypes.SIDE_SELL {
		return pos.Qty.Neg(), nil
	}
	return pos.Qty, nil
}

// adlRank returns the positions of the opposite side ranked by ADL score
// (spec §18.2): highest score first, ties by account address.
func (k Keeper) adlRank(ctx sdk.Context, m types.Market, side batchtypes.Side) ([]types.ADLEntry, []types.Position, error) {
	positions, err := k.positionsOfMarket(ctx, m.Id)
	if err != nil {
		return nil, nil, err
	}
	var entries []types.ADLEntry
	var ranked []types.Position
	for _, p := range positions {
		if p.Side != side || p.Account == types.InsuranceFundAddress() {
			continue
		}
		entries = append(entries, types.ADLEntry{Account: p.Account, Side: p.Side, Qty: p.Qty, Score: p.ADLScore(m.MarkPrice, m.FundingIndex)})
		ranked = append(ranked, p)
	}
	idx := make([]int, len(entries))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ea, eb := entries[idx[a]], entries[idx[b]]
		if !ea.Score.Equal(eb.Score) {
			return ea.Score.GT(eb.Score)
		}
		return ea.Account < eb.Account
	})
	sortedEntries := make([]types.ADLEntry, len(entries))
	sortedPositions := make([]types.Position, len(entries))
	for i, j := range idx {
		sortedEntries[i], sortedPositions[i] = entries[j], ranked[j]
	}
	return sortedEntries, sortedPositions, nil
}

// autoDeleverage closes a defaulting position at its bankruptcy price
// against the highest-scored positions of the opposite side (spec §18.2).
func (k Keeper) autoDeleverage(ctx sdk.Context, m *types.Market, p types.Position) error {
	price := p.BankruptcyPrice(m.FundingIndex)
	if !price.IsPositive() {
		price = m.MarkPrice
	}
	opposite := batchtypes.SIDE_SELL
	if p.Side == batchtypes.SIDE_SELL {
		opposite = batchtypes.SIDE_BUY
	}
	_, ranked, err := k.adlRank(ctx, *m, opposite)
	if err != nil {
		return err
	}
	remaining := p.Qty
	for _, cp := range ranked {
		if !remaining.IsPositive() {
			break
		}
		q := math.MinInt(remaining, cp.Qty)
		if _, _, err := k.reducePosition(ctx, m, &cp, q, price); err != nil {
			return err
		}
		if cp.Qty.IsZero() {
			if err := k.removePosition(ctx, cp); err != nil {
				return err
			}
		} else if err := k.setPosition(ctx, *m, cp); err != nil {
			return err
		}
		if err := ctx.EventManager().EmitTypedEvent(&types.EventADLExecuted{MarketId: m.Id, Defaulter: p.Account, Counterparty: cp.Account, Qty: q.String(), Price: price.String()}); err != nil {
			return err
		}
		remaining = remaining.Sub(q)
	}
	closed := p.Qty.Sub(remaining)
	if closed.IsPositive() {
		if _, _, err := k.reducePosition(ctx, m, &p, closed, price); err != nil {
			return err
		}
	}
	if p.Qty.IsZero() {
		return k.removePosition(ctx, p)
	}
	// no counterparties left for the remainder: it stays until the next block
	return k.setPosition(ctx, *m, p)
}
