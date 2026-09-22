package keeper

import (
	"sort"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/classic-terra/core/v4/x/oracle/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Liquidity Fabric spec §21.6, stage 2: assets priced directly in USD by the
// same prevote/vote ballot, with the feeder-reported Depth2% (§21.3). Asset
// votes are tallied without cross rates: the weighted median of the USD
// prices is the consensus price.

//-----------------------------------
// Asset vote targets

// IsAssetTarget reports whether name is an active asset vote target.
func (k Keeper) IsAssetTarget(ctx sdk.Context, name string) bool {
	return ctx.KVStore(k.storeKey).Has(types.GetAssetTargetKey(name))
}

// SetAssetTarget activates an asset vote target.
func (k Keeper) SetAssetTarget(ctx sdk.Context, asset types.Asset) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.GetAssetTargetKey(asset.Name), k.cdc.MustMarshal(&asset))
}

// IterateAssetTargets iterates the active asset vote targets in name order.
func (k Keeper) IterateAssetTargets(ctx sdk.Context, handler func(asset types.Asset) (stop bool)) {
	store := ctx.KVStore(k.storeKey)
	iter := storetypes.KVStorePrefixIterator(store, types.AssetTargetKey)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		var asset types.Asset
		k.cdc.MustUnmarshal(iter.Value(), &asset)
		if handler(asset) {
			break
		}
	}
}

// GetAssetTargets returns the active asset vote targets in name order.
func (k Keeper) GetAssetTargets(ctx sdk.Context) (targets types.AssetList) {
	k.IterateAssetTargets(ctx, func(asset types.Asset) bool {
		targets = append(targets, asset)
		return false
	})
	return targets
}

// ClearAssetTargets removes every asset vote target.
func (k Keeper) ClearAssetTargets(ctx sdk.Context) {
	store := ctx.KVStore(k.storeKey)
	iter := storetypes.KVStorePrefixIterator(store, types.AssetTargetKey)
	defer iter.Close()
	var keys [][]byte
	for ; iter.Valid(); iter.Next() {
		keys = append(keys, iter.Key())
	}
	for _, key := range keys {
		store.Delete(key)
	}
}

// ApplyAssetWhitelist syncs the asset vote targets with params.asset_whitelist
// at the end of a vote period. Unlike ApplyWhitelist it registers no bank
// metadata: assets are prices, not bank denoms.
func (k Keeper) ApplyAssetWhitelist(ctx sdk.Context, whitelist types.AssetList) {
	current := k.GetAssetTargets(ctx)
	updateRequired := len(current) != len(whitelist)
	if !updateRequired {
		for _, a := range whitelist {
			if !k.IsAssetTarget(ctx, a.Name) {
				updateRequired = true
				break
			}
		}
	}
	if !updateRequired {
		return
	}
	k.ClearAssetTargets(ctx)
	for _, a := range whitelist {
		k.SetAssetTarget(ctx, a)
	}
}

//-----------------------------------
// Asset prices

// GetAssetPrice returns the consensus USD price of an asset and the consensus
// Depth2% of the period (zero when no feeder reported it).
func (k Keeper) GetAssetPrice(ctx sdk.Context, asset string) (price math.LegacyDec, depth math.Int, err error) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.GetAssetPriceKey(asset))
	if bz == nil {
		return math.LegacyZeroDec(), math.ZeroInt(), errorsmod.Wrap(types.ErrUnknownDenom, asset)
	}
	dp := sdk.DecProto{}
	k.cdc.MustUnmarshal(bz, &dp)
	depth = math.ZeroInt()
	if sample, err := k.GetRateSample(ctx, asset); err == nil && !sample.Depth.IsNil() {
		depth = sample.Depth
	}
	return dp.Dec, depth, nil
}

// SetAssetPrice stores the consensus USD price of an asset.
func (k Keeper) SetAssetPrice(ctx sdk.Context, asset string, price math.LegacyDec) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.GetAssetPriceKey(asset), k.cdc.MustMarshal(&sdk.DecProto{Dec: price}))
}

// SetAssetPriceWithEvent stores the price and emits the ABCI event.
func (k Keeper) SetAssetPriceWithEvent(ctx sdk.Context, asset string, price math.LegacyDec, depth math.Int) {
	k.SetAssetPrice(ctx, asset, price)
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeAssetPriceUpdate,
			sdk.NewAttribute(types.AttributeKeyAsset, asset),
			sdk.NewAttribute(types.AttributeKeyPrice, price.String()),
			sdk.NewAttribute(types.AttributeKeyDepth, depth.String()),
		),
	)
}

// DeleteAssetPrice removes the consensus price of an asset.
func (k Keeper) DeleteAssetPrice(ctx sdk.Context, asset string) {
	ctx.KVStore(k.storeKey).Delete(types.GetAssetPriceKey(asset))
}

// IterateAssetPrices iterates the consensus asset prices in name order.
func (k Keeper) IterateAssetPrices(ctx sdk.Context, handler func(asset string, price math.LegacyDec) (stop bool)) {
	store := ctx.KVStore(k.storeKey)
	iter := storetypes.KVStorePrefixIterator(store, types.AssetPriceKey)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		asset := string(iter.Key()[len(types.AssetPriceKey):])
		dp := sdk.DecProto{}
		k.cdc.MustUnmarshal(iter.Value(), &dp)
		if handler(asset, dp.Dec) {
			break
		}
	}
}

// GetPrice returns the reference price of a name: the USD price when it is an
// asset target, otherwise the LUNC exchange rate in that denom. Names never
// collide (Params.Validate). Used by x/batch and x/perp for P_ref.
func (k Keeper) GetPrice(ctx sdk.Context, name string) (math.LegacyDec, error) {
	if k.IsAssetTarget(ctx, name) {
		price, _, err := k.GetAssetPrice(ctx, name)
		return price, err
	}
	return k.GetLunaExchangeRate(ctx, name)
}

//-----------------------------------
// Ballots

// OrganizeAssetBallots collects the asset votes of the period from the
// aggregate votes of the active validators: one price ballot per asset and
// one depth ballot with the votes that reported a positive Depth2%. Tuples
// that are not asset targets are ignored here (they belong to the denom
// ballots). Abstain votes (non-positive price) get zero power.
func (k Keeper) OrganizeAssetBallots(ctx sdk.Context, validatorClaimMap map[string]types.Claim, targets types.AssetList) (prices, depths map[string]types.ExchangeRateBallot) {
	prices = map[string]types.ExchangeRateBallot{}
	depths = map[string]types.ExchangeRateBallot{}
	k.IterateAggregateExchangeRateVotes(ctx, func(voterAddr sdk.ValAddress, vote types.AggregateExchangeRateVote) (stop bool) {
		claim, ok := validatorClaimMap[vote.Voter]
		if !ok {
			return false
		}
		for _, tuple := range vote.ExchangeRateTuples {
			if !targets.Contains(tuple.Denom) {
				continue
			}
			power := claim.Power
			if !tuple.ExchangeRate.IsPositive() {
				power = 0
			}
			prices[tuple.Denom] = append(prices[tuple.Denom], types.NewVoteForTally(tuple.ExchangeRate, tuple.Denom, voterAddr, power))
			if !tuple.Depth.IsNil() && tuple.Depth.IsPositive() {
				depths[tuple.Denom] = append(depths[tuple.Denom], types.NewVoteForTally(math.LegacyNewDecFromInt(tuple.Depth), tuple.Denom, voterAddr, power))
			}
		}
		return false
	})
	for asset, ballot := range prices {
		sort.Sort(ballot)
		prices[asset] = ballot
	}
	for asset, ballot := range depths {
		sort.Sort(ballot)
		depths[asset] = ballot
	}
	return prices, depths
}
