package keeper

import (
	"strings"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CreateMarket registers a perp market (governance), disabled, together with
// its MARKET_TYPE_PERP market in x/batch. It is enabled later by
// MsgEnableMarket once the listing criteria hold (spec §20.3, §21.3).
func (k Keeper) CreateMarket(ctx sdk.Context, m types.Market) error {
	if err := m.Validate(); err != nil {
		return errorsmod.Wrap(types.ErrInvalidMarket, err.Error())
	}
	if has, err := k.Markets.Has(ctx, m.Id); err != nil {
		return err
	} else if has {
		return types.ErrMarketExists.Wrap(m.Id)
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	base, quote, ok := strings.Cut(m.Id, "/")
	if !ok || quote != params.SettlementDenom || base == "" {
		return errorsmod.Wrapf(types.ErrInvalidMarket, "id must be <base>/%s", params.SettlementDenom)
	}
	if err := k.batchKeeper.CreateMarket(ctx, batchtypes.Market{
		Id: m.Id, BaseDenom: base, QuoteDenom: quote, Type: batchtypes.MARKET_TYPE_PERP, OracleDenom: m.OracleAsset,
		Enabled: false, MinQty: m.MinQty, TickSize: m.TickSize,
	}); err != nil {
		return err
	}
	m.Enabled = false
	m.State = types.ORACLE_STATE_PAUSED
	m.MarkPrice, m.RefPrice = math.LegacyZeroDec(), math.LegacyZeroDec()
	m.FundingIndex, m.FundingRate = math.LegacyZeroDec(), math.LegacyZeroDec()
	m.OiLong, m.OiShort = math.ZeroInt(), math.ZeroInt()
	m.EffectiveLeverage, m.EffectiveOiCap = m.MaxLeverage, m.OiCap
	m.CreatedHeight, m.HealthySinceHeight, m.LastFundingHeight = ctx.BlockHeight(), ctx.BlockHeight(), ctx.BlockHeight()
	m.PremiumSum, m.PremiumCount = math.LegacyZeroDec(), 0
	if err := k.updateMark(ctx, params, &m); err != nil {
		return err
	}
	return k.Markets.Set(ctx, m.Id, m)
}

// UpdateMarket changes the governance inputs of a market.
func (k Keeper) UpdateMarket(ctx sdk.Context, msg *types.MsgUpdateMarket) error {
	m, err := k.GetMarket(ctx, msg.MarketId)
	if err != nil {
		return err
	}
	m.MaxLeverage, m.OiCap, m.Alpha, m.Stress = msg.MaxLeverage, msg.OiCap, msg.Alpha, msg.Stress
	m.ListingMinBlocks, m.VenuesAttested = msg.ListingMinBlocks, msg.VenuesAttested
	if err := m.Validate(); err != nil {
		return errorsmod.Wrap(types.ErrInvalidMarket, err.Error())
	}
	// a lower governance cap applies immediately to new positions; a higher one climbs by steps
	if m.EffectiveLeverage.GT(m.MaxLeverage) {
		m.EffectiveLeverage = m.MaxLeverage
	}
	if m.EffectiveOiCap.GT(m.OiCap) {
		m.EffectiveOiCap = m.OiCap
	}
	return k.Markets.Set(ctx, m.Id, m)
}

// ListingCheck evaluates the criteria of spec §21.3 for a market.
func (k Keeper) ListingCheck(ctx sdk.Context, m types.Market) (types.QueryListingCheckResponse, error) {
	res := types.QueryListingCheckResponse{VenuesAttested: m.VenuesAttested}
	depth, n := k.depthMedian(ctx, m.OracleAsset)
	res.Depth_2Pct, res.DepthSamples = depth, math.NewInt(int64(n)).String()
	price := m.MarkPrice
	if !price.IsPositive() {
		price = m.RefPrice
	}
	if n > 0 && price.IsPositive() {
		res.OiCapWithinDepth = price.MulInt(m.OiCap).LTE(m.Alpha.MulInt(depth))
	}
	target, err := k.InsuranceTarget(ctx, m)
	if err != nil {
		return res, err
	}
	balance, err := k.insuranceBalance(ctx)
	if err != nil {
		return res, err
	}
	res.InsuranceTarget = target
	res.InsuranceCoversTarget = balance.GTE(target)
	res.HealthyBlocks = ctx.BlockHeight() - m.HealthySinceHeight
	healthyNow := m.State == types.ORACLE_STATE_NORMAL || m.State == types.ORACLE_STATE_RESTRICTED
	res.HealthyLongEnough = healthyNow && res.HealthyBlocks >= m.ListingMinBlocks
	res.Listable = res.OiCapWithinDepth && res.InsuranceCoversTarget && res.VenuesAttested && res.HealthyLongEnough
	return res, nil
}

// EnableMarket enables (after the listing check) or disables a market and
// its x/batch counterpart. Disabling stops new orders; positions remain
// managed (funding, liquidation) and can be reduced once re-enabled.
func (k Keeper) EnableMarket(ctx sdk.Context, id string, enabled bool) error {
	m, err := k.GetMarket(ctx, id)
	if err != nil {
		return err
	}
	if enabled && !m.Enabled {
		params, err := k.GetParams(ctx)
		if err != nil {
			return err
		}
		if err := k.updateMark(ctx, params, &m); err != nil {
			return err
		}
		check, err := k.ListingCheck(ctx, m)
		if err != nil {
			return err
		}
		if !check.Listable {
			return errorsmod.Wrapf(types.ErrListingCriteria, "depth=%t insurance=%t venues=%t healthy=%t",
				check.OiCapWithinDepth, check.InsuranceCoversTarget, check.VenuesAttested, check.HealthyLongEnough)
		}
	}
	m.Enabled = enabled
	if err := k.Markets.Set(ctx, m.Id, m); err != nil {
		return err
	}
	return k.batchKeeper.SetMarketEnabled(ctx, m.Id, enabled)
}

// MarketView returns a market with its derived data.
func (k Keeper) MarketView(ctx sdk.Context, params types.Params, m types.Market) (types.MarketView, error) {
	inv, err := k.insuranceInventory(ctx, m.Id)
	if err != nil {
		return types.MarketView{}, err
	}
	_, disp, stale := k.oracleState(ctx, params, m.OracleAsset)
	lev := leverageInForce(m)
	return types.MarketView{Market: m, InsuranceInventory: inv, Dispersion: disp, StalePeriods: stale,
		InitialMargin: types.InitialMargin(lev), MaintenanceMargin: types.MaintenanceMargin(lev)}, nil
}
