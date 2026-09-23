package types

import (
	"math/big"

	"cosmossdk.io/math"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
)

// Margin arithmetic of spec §19: fixed-precision decimals for prices, base
// units for quantities, rounding always against the participant.

// Notional returns qty·price rounded up (against the participant in margin).
func Notional(qty math.Int, price math.LegacyDec) math.Int {
	return math.LegacyNewDecFromInt(qty).Mul(price).Ceil().TruncateInt()
}

// InitialMargin returns IM = 1 / leverage.
func InitialMargin(leverage math.LegacyDec) math.LegacyDec {
	if leverage.IsNil() || !leverage.IsPositive() {
		return math.LegacyOneDec()
	}
	return math.LegacyOneDec().Quo(leverage)
}

// MaintenanceMargin returns MM = IM / 2 (spec §19.2).
func MaintenanceMargin(leverage math.LegacyDec) math.LegacyDec {
	return InitialMargin(leverage).QuoInt64(2)
}

// RequiredMargin returns ceil(notional · fraction).
func RequiredMargin(qty math.Int, price, fraction math.LegacyDec) math.Int {
	return math.LegacyNewDecFromInt(qty).Mul(price).Mul(fraction).Ceil().TruncateInt()
}

// UnrealizedPnl returns the signed PnL of the position at price in
// settlement units, rounded against the participant: gains truncated,
// losses rounded up.
func (p Position) UnrealizedPnl(price math.LegacyDec) math.Int {
	diff := price.Sub(p.EntryPrice)
	if p.Side == batchtypes.SIDE_SELL {
		diff = diff.Neg()
	}
	pnl := diff.MulInt(p.Qty)
	if pnl.IsNegative() {
		// losses round away from zero (towards −∞)
		return pnl.Neg().Ceil().TruncateInt().Neg()
	}
	return pnl.TruncateInt()
}

// FundingOwed returns what the position owes (positive) or is owed
// (negative) since it was opened: longs pay when the index grew.
func (p Position) FundingOwed(fundingIndex math.LegacyDec) math.Int {
	delta := fundingIndex.Sub(p.FundingIndexAtOpen).MulInt(p.Qty)
	if p.Side == batchtypes.SIDE_SELL {
		delta = delta.Neg()
	}
	if delta.IsNegative() {
		return delta.TruncateInt() // owed to the position: truncated in the protocol's favour
	}
	return delta.Ceil().TruncateInt()
}

// Equity returns collateral + PnL − funding owed at price.
func (p Position) Equity(price, fundingIndex math.LegacyDec) math.Int {
	return p.Collateral.Add(p.UnrealizedPnl(price)).Sub(p.FundingOwed(fundingIndex))
}

// MaintenanceRequirement returns MM · notional at price.
func (p Position) MaintenanceRequirement(price math.LegacyDec) math.Int {
	return RequiredMargin(p.Qty, price, p.MaintenanceMargin)
}

// LiquidationPrice returns the mark price at which equity equals the
// maintenance requirement (spec §19.5): the price under which the position
// is indexed for the incremental sweep.
//
//	long:  p = (entry·size − collateral + funding) / (size·(1 − MM))
//	short: p = (entry·size + collateral − funding) / (size·(1 + MM))
func (p Position) LiquidationPrice(fundingIndex math.LegacyDec) math.LegacyDec {
	if !p.Qty.IsPositive() {
		return math.LegacyZeroDec()
	}
	size := math.LegacyNewDecFromInt(p.Qty)
	f := math.LegacyNewDecFromInt(p.FundingOwed(fundingIndex))
	c := math.LegacyNewDecFromInt(p.Collateral)
	if p.Side == batchtypes.SIDE_BUY {
		den := size.Mul(math.LegacyOneDec().Sub(p.MaintenanceMargin))
		if !den.IsPositive() {
			return math.LegacyZeroDec()
		}
		v := p.EntryPrice.Mul(size).Sub(c).Add(f).Quo(den)
		if v.IsNegative() {
			return math.LegacyZeroDec()
		}
		return v
	}
	den := size.Mul(math.LegacyOneDec().Add(p.MaintenanceMargin))
	return p.EntryPrice.Mul(size).Add(c).Sub(f).Quo(den)
}

// BankruptcyPrice returns the price at which equity is zero (ADL price, spec §18.2).
func (p Position) BankruptcyPrice(fundingIndex math.LegacyDec) math.LegacyDec {
	if !p.Qty.IsPositive() {
		return math.LegacyZeroDec()
	}
	perUnit := math.LegacyNewDecFromInt(p.Collateral.Sub(p.FundingOwed(fundingIndex))).QuoInt(p.Qty)
	if p.Side == batchtypes.SIDE_BUY {
		v := p.EntryPrice.Sub(perUnit)
		if v.IsNegative() {
			return math.LegacyZeroDec()
		}
		return v
	}
	return p.EntryPrice.Add(perUnit)
}

// ADLScore returns (unrealized PnL / collateral) × (notional / collateral)
// at price (spec §18.2); zero for positions without collateral.
func (p Position) ADLScore(price, fundingIndex math.LegacyDec) math.LegacyDec {
	if !p.Collateral.IsPositive() {
		return math.LegacyZeroDec()
	}
	c := math.LegacyNewDecFromInt(p.Collateral)
	pnl := math.LegacyNewDecFromInt(p.UnrealizedPnl(price).Sub(p.FundingOwed(fundingIndex)))
	notional := math.LegacyNewDecFromInt(p.Qty).Mul(price)
	return pnl.Quo(c).Mul(notional.Quo(c))
}

// PriceKey encodes a price as a fixed 32-byte big-endian integer (18-decimal
// scale) so that byte order equals numeric order in the liquidation index.
func PriceKey(price math.LegacyDec) []byte {
	bi := price.BigInt()
	if bi.Sign() < 0 {
		bi = big.NewInt(0)
	}
	out := make([]byte, 32)
	bi.FillBytes(out)
	return out
}

// IndexKey concatenates the price key and the account for the liquidation index.
func IndexKey(price math.LegacyDec, account string) []byte {
	return append(PriceKey(price), []byte(account)...)
}

// MarketSideKey is the first key of the liquidation index: market id and
// side ("L" long, "S" short); no NUL bytes, which the string codec forbids.
func MarketSideKey(marketID string, side batchtypes.Side) string {
	if side == batchtypes.SIDE_SELL {
		return marketID + "/S"
	}
	return marketID + "/L"
}
