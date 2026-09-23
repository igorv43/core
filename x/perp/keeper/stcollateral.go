package keeper

import (
	"crypto/sha256"
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// stLUNC as collateral (spec §21.5, D-16). Positions keep their margin in
// settlement units plus stLUNC units; every reader values the units through
// StValuation so that the exchange rate, the LUNC price and the redemption
// queue are always current.

// StValuation is the value of one stLUNC unit in settlement, with its inputs.
type StValuation struct {
	Rate       math.LegacyDec // uluna per stLUNC (x/liquidstake)
	Price      math.LegacyDec // settlement per uluna (x/oracle)
	Haircut    math.LegacyDec // haircut in force (base + queue term)
	UnitValue  math.LegacyDec // Rate · Price · (1 − Haircut)
	Available  bool           // false when a price is missing: stLUNC counts as zero
	QueueRatio math.LegacyDec // owed / assets of x/liquidstake
}

// stMemo is the per-block valuation cache (see Keeper.valMemo).
type stMemo struct {
	height    int64
	val       StValuation
	set       bool
	target    math.Int
	targetSet bool
}

// insuranceTargetEndBlock memoizes IF_target for the block (fills route
// their fees against it; markets do not change inside EndBlock).
func (k Keeper) insuranceTargetEndBlock(ctx sdk.Context) (math.Int, error) {
	if k.valMemo != nil && k.valMemo.targetSet && k.valMemo.height == ctx.BlockHeight() {
		return k.valMemo.target, nil
	}
	target, err := k.InsuranceTarget(ctx)
	if err != nil {
		return math.Int{}, err
	}
	if k.valMemo != nil {
		if k.valMemo.height != ctx.BlockHeight() {
			k.valMemo.set = false
		}
		k.valMemo.height, k.valMemo.target, k.valMemo.targetSet = ctx.BlockHeight(), target, true
	}
	return target, nil
}

// stValuationEndBlock returns the valuation of the current block, computed
// once: used by fills, sweeps and re-indexing, which run in EndBlock after
// every transaction of the block has been applied (the inputs are final).
func (k Keeper) stValuationEndBlock(ctx sdk.Context, params types.Params) StValuation {
	if k.valMemo == nil {
		return k.stValuation(ctx, params)
	}
	if k.valMemo.set && k.valMemo.height == ctx.BlockHeight() {
		return k.valMemo.val
	}
	val := k.stValuation(ctx, params)
	k.valMemo.height, k.valMemo.val, k.valMemo.set = ctx.BlockHeight(), val, true
	return val
}

// stValuation reads the exchange rate, the LUNC price and the redemption
// queue. When any input is missing the valuation is unavailable and stLUNC
// is worth zero (never a stale or optimistic value).
func (k Keeper) stValuation(ctx sdk.Context, params types.Params) StValuation {
	v := StValuation{Rate: math.LegacyZeroDec(), Price: math.LegacyZeroDec(), Haircut: params.StHaircut, UnitValue: math.LegacyZeroDec(), QueueRatio: math.LegacyZeroDec()}
	if k.lsKeeper == nil {
		return v
	}
	rate, totals, err := k.lsKeeper.ExchangeRate(ctx)
	if err != nil || !rate.IsPositive() {
		return v
	}
	price, err := k.oracleKeeper.GetPrice(ctx, params.LunaPriceDenom)
	if err != nil || !price.IsPositive() {
		return v
	}
	if assets := totals.Assets(); assets.IsPositive() && !totals.Owed.IsNil() && totals.Owed.IsPositive() {
		v.QueueRatio = math.LegacyNewDecFromInt(totals.Owed).QuoInt(assets)
	}
	// rule 9: the queue lengthens the conversion, the haircut grows with it
	v.Haircut = params.StHaircut.Add(params.StHaircutQueueSlope.Mul(v.QueueRatio))
	if v.Haircut.GT(math.LegacyOneDec()) {
		v.Haircut = math.LegacyOneDec()
	}
	v.Rate, v.Price = rate, price
	v.UnitValue = rate.Mul(price).Mul(math.LegacyOneDec().Sub(v.Haircut))
	v.Available = v.UnitValue.IsPositive()
	return v
}

// Value returns the settlement value of stLUNC units, truncated (against the holder).
func (v StValuation) Value(units math.Int) math.Int {
	if !v.Available || units.IsNil() || !units.IsPositive() {
		return math.ZeroInt()
	}
	return v.UnitValue.MulInt(units).TruncateInt()
}

// Units returns the stLUNC units worth at least `value`, rounded up (against the holder).
func (v StValuation) Units(value math.Int) math.Int {
	if !v.Available || !value.IsPositive() {
		return math.ZeroInt()
	}
	return math.LegacyNewDecFromInt(value).Quo(v.UnitValue).Ceil().TruncateInt()
}

// FreeSt returns the account's free stLUNC units.
func (k Keeper) FreeSt(ctx sdk.Context, account string) (math.Int, error) {
	v, err := k.CollateralSt.Get(ctx, account)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.Int{}, err
	}
	return v, nil
}

func (k Keeper) addFreeSt(ctx sdk.Context, account string, delta math.Int) error {
	cur, err := k.FreeSt(ctx, account)
	if err != nil {
		return err
	}
	next := cur.Add(delta)
	if next.IsNegative() {
		return errorsmod.Wrapf(types.ErrInsufficientFree, "free stLUNC of %s would be %s", account, next)
	}
	if next.IsZero() {
		return k.CollateralSt.Remove(ctx, account)
	}
	return k.CollateralSt.Set(ctx, account, next)
}

// totalSt returns every stLUNC unit accepted as collateral (free and in positions).
func (k Keeper) totalSt(ctx sdk.Context) (math.Int, error) {
	total := math.ZeroInt()
	if err := k.CollateralSt.Walk(ctx, nil, func(_ string, v math.Int) (bool, error) {
		total = total.Add(v)
		return false, nil
	}); err != nil {
		return math.Int{}, err
	}
	if err := k.Positions.Walk(ctx, nil, func(_ collections.Pair[string, string], p types.Position) (bool, error) {
		if !p.CollateralSt.IsNil() {
			total = total.Add(p.CollateralSt)
		}
		return false, nil
	}); err != nil {
		return math.Int{}, err
	}
	return total, nil
}

// assertGlobalCap enforces rule 6 for `extra` more units: the governance cap
// in units and the value cap relative to the fund core.
func (k Keeper) assertGlobalCap(ctx sdk.Context, params types.Params, val StValuation, extra math.Int) error {
	if !params.StGlobalCap.IsPositive() {
		return errorsmod.Wrap(types.ErrInvalidCollateral, "stLUNC collateral is closed (st_global_cap = 0)")
	}
	total, err := k.totalSt(ctx)
	if err != nil {
		return err
	}
	next := total.Add(extra)
	if next.GT(params.StGlobalCap) {
		return errorsmod.Wrapf(types.ErrInvalidCollateral, "stLUNC global cap %s exceeded (%s)", params.StGlobalCap, next)
	}
	core, err := k.insuranceBalance(ctx)
	if err != nil {
		return err
	}
	if limit := params.StGlobalCapIfRatio.MulInt(core).TruncateInt(); val.Value(next).GT(limit) {
		return errorsmod.Wrapf(types.ErrInvalidCollateral, "stLUNC value would exceed %s× the insurance core (%s)", params.StGlobalCapIfRatio, limit)
	}
	return nil
}

// posValue returns collateral + value(collateral_st) of a position.
func posValue(p types.Position, val StValuation) math.Int {
	if p.CollateralSt.IsNil() {
		return p.Collateral
	}
	return p.Collateral.Add(val.Value(p.CollateralSt))
}

// seizeToTranche moves stLUNC units already removed from an account (or a
// position) into the fund's tranche. When advance is true the core lends
// the haircut value of the units in settlement (it is repaid by the sale of
// the tranche, spec §21.5 rule 7); the caller decides who receives it.
func (k Keeper) seizeToTranche(ctx sdk.Context, units, value math.Int, advance bool) error {
	l, err := k.GetLedger(ctx)
	if err != nil {
		return err
	}
	l.TrancheSt = l.TrancheSt.Add(units)
	if advance && value.IsPositive() {
		take := math.MinInt(value, l.Insurance)
		l.Insurance = l.Insurance.Sub(take)
		l.TrancheAdvanced = l.TrancheAdvanced.Add(take)
		if take.LT(value) {
			return errorsmod.Wrapf(types.ErrInsuranceInsolvent, "core cannot advance %s against seized stLUNC", value)
		}
	}
	return k.Ledger.Set(ctx, l)
}

// chargeSettlement takes `amount` of settlement from an account: free
// settlement first, then the haircut value of its free stLUNC, seized into
// the tranche with the core advancing the settlement. Returns what came from
// each source.
func (k Keeper) chargeSettlement(ctx sdk.Context, val StValuation, account string, amount math.Int) (fromSettlement, fromSt math.Int, err error) {
	free, err := k.FreeCollateral(ctx, account)
	if err != nil {
		return math.Int{}, math.Int{}, err
	}
	fromSettlement = math.MinInt(free, amount)
	rest := amount.Sub(fromSettlement)
	fromSt = math.ZeroInt()
	if rest.IsPositive() {
		freeSt, err := k.FreeSt(ctx, account)
		if err != nil {
			return math.Int{}, math.Int{}, err
		}
		units := val.Units(rest)
		if !val.Available || freeSt.LT(units) {
			return math.Int{}, math.Int{}, errorsmod.Wrapf(types.ErrInsufficientFree, "%s needed, free settlement %s", amount, free)
		}
		if err := k.addFreeSt(ctx, account, units.Neg()); err != nil {
			return math.Int{}, math.Int{}, err
		}
		if err := k.seizeToTranche(ctx, units, rest, true); err != nil {
			return math.Int{}, math.Int{}, err
		}
		fromSt = rest
	}
	if fromSettlement.IsPositive() {
		if err := k.addFree(ctx, account, fromSettlement.Neg()); err != nil {
			return math.Int{}, math.Int{}, err
		}
	}
	return fromSettlement, fromSt, nil
}

// stCapacity is the settlement value of free stLUNC usable as margin next to
// `settlement`: at most st_share_cap / (1 − st_share_cap) of it (rule 5).
func stCapacity(params types.Params, settlement, stValue math.Int) math.Int {
	if !settlement.IsPositive() || !stValue.IsPositive() {
		return math.ZeroInt()
	}
	limit := params.StShareCap.Quo(math.LegacyOneDec().Sub(params.StShareCap)).MulInt(settlement).TruncateInt()
	return math.MinInt(stValue, limit)
}

// AvailableFor returns the margin an account may reserve in a market:
// settlement free (+ stLUNC capacity when the market allows it) − reservations.
func (k Keeper) AvailableFor(ctx sdk.Context, params types.Params, account string, market types.Market, val StValuation) (math.Int, error) {
	free, err := k.FreeCollateral(ctx, account)
	if err != nil {
		return math.Int{}, err
	}
	reserved, err := k.ReservedTotal(ctx, account)
	if err != nil {
		return math.Int{}, err
	}
	avail := free.Sub(reserved)
	if market.StCollateralAllowed && val.Available {
		freeSt, err := k.FreeSt(ctx, account)
		if err != nil {
			return math.Int{}, err
		}
		avail = avail.Add(stCapacity(params, free, val.Value(freeSt)))
	}
	return avail, nil
}

// Valuation returns the current stLUNC valuation (queries and tests).
func (k Keeper) Valuation(ctx sdk.Context) StValuation {
	params, err := k.GetParams(ctx)
	if err != nil {
		return StValuation{}
	}
	return k.stValuation(ctx, params)
}

// PositionDigest returns a sha256 over the account's positions (market, side,
// qty, entry, collateral, collateral_st, funding index) in market order plus
// the free collateral, the number of positions and the sum of equities at
// the current marks (POSITION_DIGEST beacon, spec §14.6 item 3).
func (k Keeper) PositionDigest(ctx sdk.Context, account string) ([32]byte, math.Int, int, math.Int, error) {
	var digest [32]byte
	positions, err := k.PositionsOfAccount(ctx, account)
	if err != nil {
		return digest, math.Int{}, 0, math.Int{}, err
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return digest, math.Int{}, 0, math.Int{}, err
	}
	val := k.stValuation(ctx, params)
	h := sha256.New()
	equity := math.ZeroInt()
	for _, p := range positions {
		m, err := k.GetMarket(ctx, p.MarketId)
		if err != nil {
			return digest, math.Int{}, 0, math.Int{}, err
		}
		st := p.CollateralSt
		if st.IsNil() {
			st = math.ZeroInt()
		}
		h.Write([]byte(p.MarketId))
		h.Write([]byte{byte(p.Side)})
		h.Write([]byte(p.Qty.String() + "|" + p.EntryPrice.String() + "|" + p.Collateral.String() + "|" + st.String() + "|" + p.FundingIndexAtOpen.String() + "\n"))
		equity = equity.Add(valued(p, val).Equity(m.MarkPrice, m.FundingIndex))
	}
	free, err := k.FreeCollateral(ctx, account)
	if err != nil {
		return digest, math.Int{}, 0, math.Int{}, err
	}
	h.Write([]byte("free|" + free.String()))
	copy(digest[:], h.Sum(nil))
	return digest, free, len(positions), equity, nil
}
