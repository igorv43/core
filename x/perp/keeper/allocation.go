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

// FeeSink routes the spot fees of x/batch (spec §23): settlement fees join
// the perp revenue path; other denoms fund the community pool.
type FeeSink struct{ k Keeper }

var _ batchtypes.FeeSink = FeeSink{}

// NewFeeSink returns the sink to register in x/batch.
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
func (k Keeper) runBuyback(ctx sdk.Context, params types.Params) error {
	l, err := k.GetLedger(ctx)
	if err != nil {
		return err
	}
	// returned escrow of a closed intent (unfilled part) comes back to the budget
	if l.BuybackIntentId != 0 {
		if _, err := k.batchKeeper.GetIntent(ctx, l.BuybackIntentId); err != nil {
			l.BuybackIntentId = 0
			if err := k.reconcileBudget(ctx, params, &l); err != nil {
				return err
			}
		}
	}
	// a tranche sale owns the module's uluna and settlement surplus while open
	if l.TrancheSellIntentId != 0 {
		return k.Ledger.Set(ctx, l)
	}
	// burn what was bought (never the tranche's claimed uluna)
	uluna := k.bankKeeper.GetBalance(ctx, k.ModuleAddress(), "uluna")
	uluna.Amount = uluna.Amount.Sub(l.TrancheUluna)
	if uluna.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, k.burnAccount, sdk.NewCoins(uluna)); err != nil {
			return err
		}
		l.BurnedEpoch = l.BurnedEpoch.Add(uluna.Amount)
		if err := ctx.EventManager().EmitTypedEvent(&types.EventBuyback{IntentId: l.BuybackIntentId, Spent: "0", Burned: uluna.Amount.String()}); err != nil {
			return err
		}
	}
	if err := k.Ledger.Set(ctx, l); err != nil {
		return err
	}
	if l.BuybackIntentId != 0 || params.SpotMarketId == "" || !l.BurnBudget.IsPositive() || !params.BurnBuyCap.IsPositive() {
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

// reconcileBudget returns unexplained settlement in the module account (the
// refund of an unfilled buyback, or donations) to the burn budget.
func (k Keeper) reconcileBudget(ctx sdk.Context, params types.Params, l *types.Ledger) error {
	expected, err := k.ledgerTotal(ctx, *l)
	if err != nil {
		return err
	}
	actual := k.bankKeeper.GetBalance(ctx, k.ModuleAddress(), params.SettlementDenom).Amount
	if diff := actual.Sub(expected); diff.IsPositive() {
		l.BurnBudget = l.BurnBudget.Add(diff)
	}
	return nil
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
