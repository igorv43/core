package keeper

import (
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	oracletypes "github.com/classic-terra/core/v4/x/oracle/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// Allocation cascade of spec §23.1 (D-22): per epoch, protocol revenue in
// the settlement denom goes first to the insurance fund until IF_target,
// then to the operational budget up to opex_cap, and the surplus is split
// between the Oracle Pool (in settlement), the Community Pool (in
// settlement) and a buyback of uluna executed by the internal spot market
// and burned. Nothing is burned or passed on while the fund is below target
// or the budget uncovered (§26.3).

// FeeSink routes protocol revenue into the cascade of spec §23.1: the spot
// fees and solver slashes of x/batch and the fee remainder of x/liquidstake
// (§24.6). Settlement joins the revenue path at once (routeFee); uluna is
// protocol revenue held in kind until the internal spot market converts it
// to settlement (see revenue.go). Any other denom has no conversion venue in
// the spec and funds the community pool.
type FeeSink struct{ k Keeper }

var _ batchtypes.FeeSink = FeeSink{}

// NewFeeSink returns the sink to register in x/batch and x/liquidstake.
func NewFeeSink(k Keeper) FeeSink { return FeeSink{k: k} }

// Deposit implements batchtypes.FeeSink.
func (s FeeSink) Deposit(ctx sdk.Context, fromModule string, coins sdk.Coins) error {
	if coins.IsZero() {
		return nil
	}
	params, err := s.k.GetParams(ctx)
	if err != nil {
		return err
	}
	var other sdk.Coins
	for _, c := range coins {
		if c.Denom == lunaDenom && c.Denom != params.SettlementDenom {
			if err := s.k.bankKeeper.SendCoinsFromModuleToModule(ctx, fromModule, types.ModuleName, sdk.NewCoins(c)); err != nil {
				return err
			}
			if err := s.k.creditRevenueInKind(ctx, fromModule, c.Amount); err != nil {
				return err
			}
			continue
		}
		if c.Denom != params.SettlementDenom {
			other = other.Add(c)
			continue
		}
		if err := s.k.bankKeeper.SendCoinsFromModuleToModule(ctx, fromModule, types.ModuleName, sdk.NewCoins(c)); err != nil {
			return err
		}
		if err := s.k.routeFee(ctx, c.Amount, false); err != nil {
			return err
		}
	}
	if !other.IsZero() {
		return s.k.distrKeeper.FundCommunityPool(ctx, other, authtypes.NewModuleAddress(fromModule))
	}
	return nil
}

// DepositInsurance implements batchtypes.FeeSink: solver slashes go 100% to
// the insurance fund (spec §16.2, §18.5). Bonds are in the settlement asset
// (x/batch Params forbid uusd); anything else falls back to Deposit.
func (s FeeSink) DepositInsurance(ctx sdk.Context, fromModule string, coins sdk.Coins) error {
	if coins.IsZero() {
		return nil
	}
	params, err := s.k.GetParams(ctx)
	if err != nil {
		return err
	}
	var rest sdk.Coins
	for _, c := range coins {
		if c.Denom != params.SettlementDenom {
			rest = rest.Add(c)
			continue
		}
		if err := s.k.bankKeeper.SendCoinsFromModuleToModule(ctx, fromModule, types.ModuleName, sdk.NewCoins(c)); err != nil {
			return err
		}
		if err := s.k.routeFee(ctx, c.Amount, true); err != nil {
			return err
		}
	}
	if !rest.IsZero() {
		return s.Deposit(ctx, fromModule, rest)
	}
	return nil
}

// runAllocation executes the cascade when the epoch elapsed.
func (k Keeper) runAllocation(ctx sdk.Context, params types.Params) error {
	l, err := k.GetLedger(ctx)
	if err != nil {
		return err
	}
	if l.EpochStartHeight == 0 {
		l.EpochStartHeight = ctx.BlockHeight()
		return k.Ledger.Set(ctx, l)
	}
	if ctx.BlockHeight()-l.EpochStartHeight < params.AllocEpochBlocks {
		return nil
	}
	revenue := l.Revenue
	rec := types.AllocationRecord{
		Epoch: l.Epoch, Height: ctx.BlockHeight(), Revenue: revenue, ToInsurance: math.ZeroInt(),
		ToOpex: math.ZeroInt(), ToOraclePool: math.ZeroInt(), ToCommunityPool: math.ZeroInt(), ToBurnBudget: math.ZeroInt(), BurnedUluna: l.BurnedEpoch,
		LunaPrice: math.LegacyZeroDec(), RevenueUlunaReceived: l.RevenueUlunaEpoch, RevenueUlunaSold: l.RevenueSoldEpoch,
	}
	// the oracle LUNC price of the epoch, for reports in LUNC (zero when missing)
	if price, err := k.oracleKeeper.GetPrice(ctx, params.LunaPriceDenom); err == nil && price.IsPositive() {
		rec.LunaPrice = price
	}
	remaining := revenue
	denom := params.SettlementDenom

	// 1. insurance fund until the target
	target, err := k.InsuranceTarget(ctx)
	if err != nil {
		return err
	}
	if gap := target.Sub(l.Insurance); gap.IsPositive() {
		rec.ToInsurance = math.MinInt(gap, remaining)
		l.Insurance = l.Insurance.Add(rec.ToInsurance)
		remaining = remaining.Sub(rec.ToInsurance)
	}
	// 2. operational budget
	if params.OpexCap.IsPositive() && remaining.IsPositive() {
		rec.ToOpex = math.MinInt(params.OpexCap, remaining)
		remaining = remaining.Sub(rec.ToOpex)
		coins := sdk.NewCoins(sdk.NewCoin(denom, rec.ToOpex))
		if params.OpexAccount != "" {
			to := sdk.MustAccAddressFromBech32(params.OpexAccount)
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, to, coins); err != nil {
				return err
			}
		} else if err := k.distrKeeper.FundCommunityPool(ctx, coins, k.ModuleAddress()); err != nil {
			return err
		}
	}
	// 3. surplus: Oracle Pool and Community Pool in settlement, buyback budget
	if remaining.IsPositive() {
		rec.ToOraclePool = params.SplitOp.MulInt(remaining).TruncateInt()
		rec.ToCommunityPool = params.SplitCp.MulInt(remaining).TruncateInt()
		rec.ToBurnBudget = remaining.Sub(rec.ToOraclePool).Sub(rec.ToCommunityPool)
		if rec.ToOraclePool.IsPositive() {
			if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, oracletypes.ModuleName, sdk.NewCoins(sdk.NewCoin(denom, rec.ToOraclePool))); err != nil {
				return err
			}
		}
		if rec.ToCommunityPool.IsPositive() {
			if err := k.distrKeeper.FundCommunityPool(ctx, sdk.NewCoins(sdk.NewCoin(denom, rec.ToCommunityPool)), k.ModuleAddress()); err != nil {
				return err
			}
		}
		l.BurnBudget = l.BurnBudget.Add(rec.ToBurnBudget)
	}
	l.Revenue = math.ZeroInt()
	l.Epoch++
	l.EpochStartHeight = ctx.BlockHeight()
	l.BurnSpentEpoch, l.BurnedEpoch, l.TrancheSoldEpoch = math.ZeroInt(), math.ZeroInt(), math.ZeroInt()
	l.RevenueSoldEpoch, l.RevenueUlunaEpoch = math.ZeroInt(), math.ZeroInt()
	if err := k.Ledger.Set(ctx, l); err != nil {
		return err
	}
	if err := k.Allocations.Set(ctx, rec.Epoch, rec); err != nil {
		return err
	}
	if err := k.pruneAllocations(ctx); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventAllocationExecuted{
		Epoch: rec.Epoch, Revenue: revenue.String(), ToInsurance: rec.ToInsurance.String(),
		ToOpex: rec.ToOpex.String(), ToOraclePool: rec.ToOraclePool.String(), ToCommunityPool: rec.ToCommunityPool.String(), ToBurnBudget: rec.ToBurnBudget.String(),
		LunaPrice: rec.LunaPrice.String(),
	})
}

func (k Keeper) pruneAllocations(ctx sdk.Context) error {
	var keys []uint64
	if err := k.Allocations.Walk(ctx, nil, func(key uint64, _ types.AllocationRecord) (bool, error) {
		keys = append(keys, key)
		return false, nil
	}); err != nil {
		return err
	}
	for len(keys) > types.MaxAllocationRecords {
		if err := k.Allocations.Remove(ctx, keys[0]); err != nil {
			return err
		}
		keys = keys[1:]
	}
	return nil
}

// runBuyback keeps one buy intent of settlement for uluna open in the
// internal spot market while there is burn budget and epoch cap left, and
// sends every uluna the module holds to the burn account.
//
// Rule 1 of spec §23.1 (non-governable, §26.3): nothing is bought back or
// burned while IF < IF_target. In that state, every step of the buyback
// gives way to the fund, in the cascade's own order (the fund first): the
// burn budget and the escrow returned by a closed or withdrawn buyback go to
// the fund up to the gap, an open buyback intent is withdrawn from the
// auction, and uluna already bought stays in the module unburned until the
// fund is back at its target. EventBuybackSkipped reports each such step.
func (k Keeper) runBuyback(ctx sdk.Context, params types.Params) error {
	l, err := k.GetLedger(ctx)
	if err != nil {
		return err
	}
	target, err := k.InsuranceTarget(ctx)
	if err != nil {
		return err
	}
	routed := math.ZeroInt()
	closed, cancelled := false, uint64(0)
	// returned escrow of a closed intent (unfilled part) comes back: to the
	// fund up to its gap, the rest to the budget
	if l.BuybackIntentId != 0 {
		if _, err := k.batchKeeper.GetIntent(ctx, l.BuybackIntentId); err != nil {
			l.BuybackIntentId, closed = 0, true
		} else if l.Insurance.LT(target) {
			// the fund fell below its target while the order was open: withdraw it
			// (atomic: a failed withdrawal leaves the order and the escrow untouched)
			cacheCtx, write := ctx.CacheContext()
			if _, err := k.batchKeeper.CancelIntent(cacheCtx, k.ModuleAddress().String(), l.BuybackIntentId); err != nil {
				k.Logger(ctx).Error("buyback order withdrawal failed", "intent", l.BuybackIntentId, "err", err)
			} else {
				write()
				cancelled, l.BuybackIntentId, closed = l.BuybackIntentId, 0, true
			}
		}
		if closed {
			r, err := k.reconcileBudget(ctx, params, &l, target)
			if err != nil {
				return err
			}
			routed = routed.Add(r)
		}
	}
	// the burn budget is surplus only while the fund is at its target
	if gap := target.Sub(l.Insurance); gap.IsPositive() && l.BurnBudget.IsPositive() {
		move := math.MinInt(gap, l.BurnBudget)
		l.BurnBudget = l.BurnBudget.Sub(move)
		l.Insurance = l.Insurance.Add(move)
		routed = routed.Add(move)
	}
	below := l.Insurance.LT(target)
	// a tranche sale owns the module's uluna and settlement surplus while open
	if l.TrancheSellIntentId != 0 {
		if err := k.Ledger.Set(ctx, l); err != nil {
			return err
		}
		return k.emitBuybackSkipped(ctx, l, target, routed, math.ZeroInt(), cancelled, below)
	}
	// burn what was bought (never the tranche's claimed uluna nor the revenue
	// held in kind), only at target
	uluna := sdk.NewCoin(lunaDenom, k.untrackedUluna(ctx, l))
	held := math.ZeroInt()
	if below {
		if uluna.IsPositive() {
			held = uluna.Amount
		}
	} else if uluna.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, k.burnAccount, sdk.NewCoins(uluna)); err != nil {
			return err
		}
		l.BurnedEpoch = l.BurnedEpoch.Add(uluna.Amount)
		if l.RevenueSellIntentId != 0 {
			// the burned uluna was part of the baseline of the open revenue sale
			l.RevenueUlunaMark = math.MaxInt(math.ZeroInt(), l.RevenueUlunaMark.Sub(uluna.Amount))
		}
		if err := ctx.EventManager().EmitTypedEvent(&types.EventBuyback{IntentId: l.BuybackIntentId, Spent: "0", Burned: uluna.Amount.String()}); err != nil {
			return err
		}
	}
	if err := k.Ledger.Set(ctx, l); err != nil {
		return err
	}
	if routed.IsPositive() || cancelled != 0 || (below && closed) {
		if err := k.emitBuybackSkipped(ctx, l, target, routed, held, cancelled, below); err != nil {
			return err
		}
	}
	// one sale of the module's balance at a time: a revenue sale owns the
	// refund and the proceeds while open (see revenue.go)
	if below || l.BuybackIntentId != 0 || l.RevenueSellIntentId != 0 || params.SpotMarketId == "" || !l.BurnBudget.IsPositive() || !params.BurnBuyCap.IsPositive() {
		return nil
	}
	room := params.BurnBuyCap.Sub(l.BurnSpentEpoch)
	amount := math.MinInt(l.BurnBudget, room)
	if !amount.IsPositive() {
		return nil
	}
	spot, err := k.batchKeeper.GetMarket(ctx, params.SpotMarketId)
	if err != nil || spot.BaseDenom != "uluna" || spot.QuoteDenom != params.SettlementDenom || !spot.Enabled {
		return nil
	}
	pref, err := k.oracleKeeper.GetPrice(ctx, spot.OracleDenom)
	if err != nil || !pref.IsPositive() {
		return nil
	}
	bp, err := k.batchKeeper.GetParams(ctx)
	if err != nil {
		return err
	}
	limit := roundToTick(pref.Mul(math.LegacyOneDec().Add(bp.PriceBand)), spot.TickSize, true)
	if math.LegacyNewDecFromInt(amount).Quo(limit).TruncateInt().LT(spot.MinQty) {
		return nil
	}
	id, _, err := k.batchKeeper.SubmitIntentInternal(ctx, &batchtypes.MsgSubmitIntent{
		Sender: k.ModuleAddress().String(), MarketId: spot.Id, Side: batchtypes.SIDE_BUY,
		AmountIn: sdk.NewCoin(params.SettlementDenom, amount), LimitPrice: limit, MinOut: math.ZeroInt(),
		ExpiryHeight: ctx.BlockHeight() + bp.CommitWindow + 3,
	})
	if err != nil {
		k.Logger(ctx).Error("buyback order rejected", "err", err)
		return nil
	}
	l.BuybackIntentId = id
	l.BurnBudget = l.BurnBudget.Sub(amount)
	l.BurnSpentEpoch = l.BurnSpentEpoch.Add(amount)
	if err := k.Ledger.Set(ctx, l); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventBuyback{IntentId: id, Spent: amount.String(), Burned: "0"})
}

// emitBuybackSkipped reports a buyback step that gave way to the fund. It
// stays silent on quiet blocks (nothing routed, withdrawn or received).
func (k Keeper) emitBuybackSkipped(ctx sdk.Context, l types.Ledger, target, routed, held math.Int, cancelled uint64, below bool) error {
	if !routed.IsPositive() && cancelled == 0 && !held.IsPositive() {
		return nil
	}
	reason := "insurance fund below target: no buyback or burn (spec §23.1 rule 1)"
	if !below {
		reason = "insurance fund below target: burn budget routed to the fund up to the target"
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventBuybackSkipped{
		Reason: reason, Insurance: l.Insurance.String(), Target: target.String(),
		RoutedToInsurance: routed.String(), HeldUluna: held.String(), CancelledIntentId: cancelled,
	})
}

// reconcileBudget books unexplained settlement in the module account (the
// refund of an unfilled or withdrawn buyback, or donations): to the
// insurance fund up to IF_target first (spec §23.1: the fund has absolute
// priority), the rest to the burn budget. It returns the part sent to the
// fund.
func (k Keeper) reconcileBudget(ctx sdk.Context, params types.Params, l *types.Ledger, target math.Int) (math.Int, error) {
	expected, err := k.ledgerTotal(ctx, *l)
	if err != nil {
		return math.Int{}, err
	}
	actual := k.bankKeeper.GetBalance(ctx, k.ModuleAddress(), params.SettlementDenom).Amount
	diff := actual.Sub(expected)
	if !diff.IsPositive() {
		return math.ZeroInt(), nil
	}
	toFund := math.ZeroInt()
	if gap := target.Sub(l.Insurance); gap.IsPositive() {
		toFund = math.MinInt(gap, diff)
		l.Insurance = l.Insurance.Add(toFund)
	}
	l.BurnBudget = l.BurnBudget.Add(diff.Sub(toFund))
	return toFund, nil
}

// ledgerTotal sums every settlement balance the ledger accounts for: free
// collateral, position margins, insurance, revenue and burn budget.
func (k Keeper) ledgerTotal(ctx sdk.Context, l types.Ledger) (math.Int, error) {
	total := l.Insurance.Add(l.Revenue).Add(l.BurnBudget)
	if err := k.Collateral.Walk(ctx, nil, func(_ string, v math.Int) (bool, error) {
		total = total.Add(v)
		return false, nil
	}); err != nil {
		return math.Int{}, err
	}
	if err := k.Positions.Walk(ctx, nil, func(_ collections.Pair[string, string], p types.Position) (bool, error) {
		total = total.Add(p.Collateral)
		return false, nil
	}); err != nil {
		return math.Int{}, err
	}
	return total, nil
}

// AllocationRecords returns the newest records, up to limit.
func (k Keeper) AllocationRecords(ctx sdk.Context, limit int) ([]types.AllocationRecord, error) {
	var out []types.AllocationRecord
	err := k.Allocations.Walk(ctx, new(collections.Range[uint64]).Descending(), func(_ uint64, r types.AllocationRecord) (bool, error) {
		out = append(out, r)
		return limit > 0 && len(out) >= limit, nil
	})
	if errors.Is(err, collections.ErrInvalidIterator) {
		return out, nil
	}
	return out, err
}
