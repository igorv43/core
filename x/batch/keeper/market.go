package keeper

import (
	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CreateMarket adds a market (governance). Perp markets require the margin
// hook to be registered by x/perp.
func (k Keeper) CreateMarket(ctx sdk.Context, m types.Market) error {
	if err := m.Validate(); err != nil {
		return errorsmod.Wrap(types.ErrInvalidMarket, err.Error())
	}
	if has, err := k.Markets.Has(ctx, m.Id); err != nil {
		return err
	} else if has {
		return types.ErrMarketExists.Wrap(m.Id)
	}
	if m.Type == types.MARKET_TYPE_PERP && k.marginHook == nil {
		return errorsmod.Wrap(types.ErrInvalidMarket, "perpetual markets need x/perp (margin hook not registered)")
	}
	if m.Enabled {
		if err := k.assertMarketCapacity(ctx); err != nil {
			return err
		}
	}
	return k.Markets.Set(ctx, m.Id, m)
}

// SetMarketEnabled toggles a market (governance). Disabling stops new
// intents and resolution; open intents are refunded at the next EndBlock.
func (k Keeper) SetMarketEnabled(ctx sdk.Context, id string, enabled bool) error {
	m, err := k.GetMarket(ctx, id)
	if err != nil {
		return err
	}
	if enabled && !m.Enabled {
		if err := k.assertMarketCapacity(ctx); err != nil {
			return err
		}
	}
	m.Enabled = enabled
	return k.Markets.Set(ctx, id, m)
}

// assertMarketCapacity enforces params.max_active_markets (spec §12.2).
func (k Keeper) assertMarketCapacity(ctx sdk.Context) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	active := uint32(0)
	if err := k.Markets.Walk(ctx, nil, func(_ string, m types.Market) (bool, error) {
		if m.Enabled {
			active++
		}
		return false, nil
	}); err != nil {
		return err
	}
	if active >= params.MaxActiveMarkets {
		return errorsmod.Wrapf(types.ErrInvalidMarket, "max_active_markets (%d) reached", params.MaxActiveMarkets)
	}
	return nil
}

// EnabledMarkets returns enabled markets in id order.
func (k Keeper) EnabledMarkets(ctx sdk.Context) ([]types.Market, error) {
	var out []types.Market
	err := k.Markets.Walk(ctx, nil, func(_ string, m types.Market) (bool, error) {
		if m.Enabled {
			out = append(out, m)
		}
		return false, nil
	})
	return out, err
}

// ActiveIntentCount counts the open intents of a market.
func (k Keeper) ActiveIntentCount(ctx sdk.Context, marketID string) (uint64, error) {
	n := uint64(0)
	err := k.IntentsByMarket.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](marketID),
		func(_ collections.Pair[string, uint64]) (bool, error) { n++; return false, nil })
	return n, err
}
