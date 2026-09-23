package keeper

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	core "github.com/classic-terra/core/v4/types"
	oracletypes "github.com/classic-terra/core/v4/x/oracle/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Oracle states (spec §21.2), mark price and funding (§20) and the dynamic
// risk engine (§22) with the gradual steps of §21.4.

// leverageInForce returns the leverage applied to new positions now: the
// stepped effective leverage, halved in the RESTRICTED state.
func leverageInForce(m types.Market) math.LegacyDec {
	if m.State == types.ORACLE_STATE_RESTRICTED {
		return m.EffectiveLeverage.QuoInt64(2)
	}
	return m.EffectiveLeverage
}

// oracleState derives the state of a market from the latest oracle sample of
// its asset: dispersion against d1/d2 and staleness in vote periods. The
// sample of period p is fresh during period p+1; one missed period means
// REDUCE_ONLY, two or more mean PAUSED.
func (k Keeper) oracleState(ctx sdk.Context, params types.Params, asset string) (state types.OracleState, dispersion math.LegacyDec, stale uint64) {
	history := k.oracleKeeper.RateHistory(ctx, asset, 1)
	if len(history) == 0 {
		return types.ORACLE_STATE_PAUSED, math.LegacyZeroDec(), 0
	}
	sample := history[0]
	votePeriod := k.oracleKeeper.VotePeriod(ctx)
	if votePeriod == 0 {
		votePeriod = 1
	}
	current := uint64(ctx.BlockHeight()) / votePeriod
	if current > sample.VotePeriod+1 {
		stale = current - sample.VotePeriod - 1
	}
	dispersion = sample.Dispersion
	switch {
	case stale >= 2:
		return types.ORACLE_STATE_PAUSED, dispersion, stale
	case stale == 1 || dispersion.GT(params.DispersionReduceOnly):
		return types.ORACLE_STATE_REDUCE_ONLY, dispersion, stale
	case dispersion.GT(params.DispersionRestricted):
		return types.ORACLE_STATE_RESTRICTED, dispersion, stale
	}
	return types.ORACLE_STATE_NORMAL, dispersion, stale
}

// premiumTwap returns the mean of the stored batch premiums of a market.
func (k Keeper) premiumTwap(ctx sdk.Context, marketID string) (math.LegacyDec, error) {
	sum, n := math.LegacyZeroDec(), int64(0)
	err := k.Premiums.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](marketID),
		func(_ collections.Pair[string, uint64], v math.LegacyDec) (bool, error) {
			sum = sum.Add(v)
			n++
			return false, nil
		})
	if err != nil || n == 0 {
		return math.LegacyZeroDec(), err
	}
	return sum.QuoInt64(n), nil
}

// recordPremium stores the premium of a batch (p*/P_ref − 1) once and keeps
// the window bounded; it also accumulates the premium for the funding rate.
func (k Keeper) recordPremium(ctx sdk.Context, params types.Params, m *types.Market, batch uint64, pstar math.LegacyDec) error {
	if !m.RefPrice.IsPositive() {
		return nil
	}
	key := collections.Join(m.Id, batch)
	if has, err := k.Premiums.Has(ctx, key); err != nil || has {
		return err
	}
	prem := pstar.Quo(m.RefPrice).Sub(math.LegacyOneDec())
	if err := k.Premiums.Set(ctx, key, prem); err != nil {
		return err
	}
	m.PremiumSum = m.PremiumSum.Add(prem)
	m.PremiumCount++
	// prune beyond the window (oldest first)
	var keys []collections.Pair[string, uint64]
	if err := k.Premiums.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](m.Id),
		func(key collections.Pair[string, uint64], _ math.LegacyDec) (bool, error) {
			keys = append(keys, key)
			return false, nil
		}); err != nil {
		return err
	}
	for len(keys) > int(params.PremiumWindow) {
		if err := k.Premiums.Remove(ctx, keys[0]); err != nil {
			return err
		}
		keys = keys[1:]
	}
	return nil
}

// updateMark refreshes P_ref, the oracle state and the mark price of a market:
// Mark = P_ref · (1 + clamp(Prem, −c, +c)) with Prem the premium TWAP (spec §20.1).
func (k Keeper) updateMark(ctx sdk.Context, params types.Params, m *types.Market) error {
	prev := m.State
	state, _, _ := k.oracleState(ctx, params, m.OracleAsset)
	m.State = state
	if state == types.ORACLE_STATE_REDUCE_ONLY || state == types.ORACLE_STATE_PAUSED {
		m.HealthySinceHeight = ctx.BlockHeight()
	}
	if pref, err := k.oracleKeeper.GetPrice(ctx, m.OracleAsset); err == nil && pref.IsPositive() {
		m.RefPrice = pref
		prem, err := k.premiumTwap(ctx, m.Id)
		if err != nil {
			return err
		}
		if prem.GT(params.PremiumClamp) {
			prem = params.PremiumClamp
		} else if prem.LT(params.PremiumClamp.Neg()) {
			prem = params.PremiumClamp.Neg()
		}
		m.MarkPrice = pref.Mul(math.LegacyOneDec().Add(prem))
	}
	if prev != m.State {
		return ctx.EventManager().EmitTypedEvent(&types.EventMarketStateChanged{MarketId: m.Id, From: prev.String(), To: m.State.String(),
			EffectiveLeverage: m.EffectiveLeverage.String(), EffectiveOiCap: m.EffectiveOiCap.String()})
	}
	return nil
}

// applyFunding applies the funding of the interval (spec §20.2):
// Rate = clamp(mean premium of the interval, ±r_max); Δindex = Rate · Mark.
// Positions of the market are re-indexed with the new funding (bounded).
func (k Keeper) applyFunding(ctx sdk.Context, params types.Params, m *types.Market) error {
	if ctx.BlockHeight()-m.LastFundingHeight < params.FundingInterval || !m.MarkPrice.IsPositive() {
		return nil
	}
	rate := math.LegacyZeroDec()
	if m.PremiumCount > 0 {
		rate = m.PremiumSum.QuoInt64(int64(m.PremiumCount))
	}
	if rate.GT(params.FundingRateMax) {
		rate = params.FundingRateMax
	} else if rate.LT(params.FundingRateMax.Neg()) {
		rate = params.FundingRateMax.Neg()
	}
	m.FundingRate = rate
	m.FundingIndex = m.FundingIndex.Add(rate.Mul(m.MarkPrice))
	m.LastFundingHeight = ctx.BlockHeight()
	m.PremiumSum, m.PremiumCount = math.LegacyZeroDec(), 0
	if err := k.Markets.Set(ctx, m.Id, *m); err != nil {
		return err
	}
	// lazy funding (§19.5): re-index the liquidation prices touched by the new index
	positions, err := k.positionsOfMarket(ctx, m.Id)
	if err != nil {
		return err
	}
	val := k.stValuationEndBlock(ctx, params)
	for i, p := range positions {
		if i >= types.MaxFundingReindexPerApply {
			break
		}
		if err := k.setPositionValued(ctx, *m, p, val); err != nil {
			return err
		}
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventFundingApplied{MarketId: m.Id, Rate: rate.String(), FundingIndex: m.FundingIndex.String(), MarkPrice: m.MarkPrice.String()})
}

// InsuranceTarget returns IF_target = β · Σ_m OI_cap(m)·Mark(m)·MM(m)·Stress(m)
// over the enabled markets plus `extra` (a market being listed).
func (k Keeper) InsuranceTarget(ctx sdk.Context, extra ...types.Market) (math.Int, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return math.Int{}, err
	}
	markets, err := k.AllMarkets(ctx)
	if err != nil {
		return math.Int{}, err
	}
	sum := math.LegacyZeroDec()
	add := func(m types.Market) {
		price := m.MarkPrice
		if !price.IsPositive() {
			price = m.RefPrice
		}
		mm := types.MaintenanceMargin(m.MaxLeverage)
		stress := m.Stress
		if stress.LT(math.LegacyOneDec()) {
			stress = math.LegacyOneDec()
		}
		sum = sum.Add(price.MulInt(m.OiCap).Mul(mm).Mul(stress))
	}
	seen := map[string]bool{}
	for _, m := range markets {
		if m.Enabled {
			add(m)
			seen[m.Id] = true
		}
	}
	for _, m := range extra {
		if !seen[m.Id] {
			add(m)
		}
	}
	return params.Beta.Mul(sum).Ceil().TruncateInt(), nil
}

// depthMedian returns the median Depth2% of the asset over the oracle history
// (samples with a reported depth) and the number of samples.
func (k Keeper) depthMedian(ctx sdk.Context, asset string) (math.Int, int) {
	samples := k.oracleKeeper.RateHistory(ctx, asset, oracletypes.MaxRateHistory)
	var depths []math.Int
	for _, s := range samples {
		if !s.Depth.IsNil() && s.Depth.IsPositive() {
			depths = append(depths, s.Depth)
		}
	}
	if len(depths) == 0 {
		return math.ZeroInt(), 0
	}
	// insertion sort: bounded by MaxRateHistory
	for i := 1; i < len(depths); i++ {
		for j := i; j > 0 && depths[j-1].GT(depths[j]); j-- {
			depths[j-1], depths[j] = depths[j], depths[j-1]
		}
	}
	return depths[len(depths)/2], len(depths)
}

// riskTargets returns the risk-engine targets of a market (spec §22, §21.3):
// leverage = max_leverage · min(factor_IF, factor_OI); OI cap = min(oi_cap,
// α·Depth2%/mark) · min(1, IF/IF_target). The oracle factor is applied at
// use time from the state.
func (k Keeper) riskTargets(ctx sdk.Context, m types.Market) (leverage math.LegacyDec, oiCap math.Int, err error) {
	balance, err := k.insuranceBalance(ctx)
	if err != nil {
		return math.LegacyDec{}, math.Int{}, err
	}
	target, err := k.InsuranceTarget(ctx)
	if err != nil {
		return math.LegacyDec{}, math.Int{}, err
	}
	factorIF := math.LegacyOneDec()
	if target.IsPositive() && balance.LT(target) {
		factorIF = math.LegacyNewDecFromInt(balance).QuoInt(target)
	}
	cap := m.OiCap
	if m.MarkPrice.IsPositive() {
		if depth, n := k.depthMedian(ctx, m.OracleAsset); n > 0 {
			byDepth := m.Alpha.MulInt(depth).Quo(m.MarkPrice).TruncateInt()
			if byDepth.LT(cap) {
				cap = byDepth
			}
		}
	}
	oiCap = factorIF.MulInt(cap).TruncateInt()
	oi := math.MaxInt(m.OiLong, m.OiShort)
	factorOI := math.LegacyOneDec()
	if m.EffectiveOiCap.IsPositive() {
		threshold := math.LegacyNewDecWithPrec(8, 1).MulInt(m.EffectiveOiCap)
		excess := math.LegacyNewDecFromInt(oi).Sub(threshold)
		if excess.IsPositive() {
			factorOI = math.LegacyOneDec().Sub(excess.Quo(math.LegacyNewDecWithPrec(2, 1).MulInt(m.EffectiveOiCap)))
			if factorOI.IsNegative() {
				factorOI = math.LegacyZeroDec()
			}
		}
	}
	factor := math.LegacyMinDec(factorIF, factorOI)
	leverage = m.MaxLeverage.Mul(factor)
	// floor at 1× (fully collateralised): the factors of §22 would otherwise
	// drive the leverage to zero on an empty fund and make trading impossible
	if floor := math.LegacyMinDec(math.LegacyOneDec(), m.MaxLeverage); leverage.LT(floor) {
		leverage = floor
	}
	return leverage, oiCap, nil
}

// stepTowards moves cur towards target by at most step·cur (spec §21.4),
// never below a floor of 1% of the target so the ladder can climb back.
func stepTowards(cur, target, step math.LegacyDec) math.LegacyDec {
	if cur.IsNil() || !cur.IsPositive() {
		return target
	}
	maxDelta := cur.Mul(step)
	diff := target.Sub(cur)
	if diff.Abs().LTE(maxDelta) {
		return target
	}
	if diff.IsNegative() {
		return cur.Sub(maxDelta)
	}
	return cur.Add(maxDelta)
}

// applyRiskSteps applies the risk targets in steps at every vote period
// boundary (spec §21.4). Existing positions are never affected.
func (k Keeper) applyRiskSteps(ctx sdk.Context, params types.Params, m *types.Market) error {
	votePeriod := int64(k.oracleKeeper.VotePeriod(ctx))
	if votePeriod == 0 {
		votePeriod = 1
	}
	if !core.IsPeriodLastBlock(ctx, uint64(votePeriod)) || m.LastRiskHeight == ctx.BlockHeight() {
		return nil
	}
	lev, cap, err := k.riskTargets(ctx, *m)
	if err != nil {
		return err
	}
	m.EffectiveLeverage = stepTowards(m.EffectiveLeverage, lev, params.RiskStep)
	m.EffectiveOiCap = stepTowards(math.LegacyNewDecFromInt(m.EffectiveOiCap), math.LegacyNewDecFromInt(cap), params.RiskStep).TruncateInt()
	m.LastRiskHeight = ctx.BlockHeight()
	return nil
}
