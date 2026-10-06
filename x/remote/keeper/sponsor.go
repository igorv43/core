package keeper

import (
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

var _ batchtypes.IntentFeeSponsor = Keeper{}

// SponsorIntentFee pays the x/batch intent fee of a remote account from the
// paymaster (spec §14.4 item 4: the protocol sponsors the local costs of
// remote users and recovers them through the trading fee). A remote account
// funded only with the settlement asset through a gateway holds no uluna, so
// without this it could not submit spot or perp intents, directly or through
// a session key.
//
// Only accounts with an x/remote record are sponsored; local users pay
// themselves. Per remote account the sponsored intent fees are bounded by
// paymaster_daily_cap per paymaster period (the same bound as the session-key
// fee allowance), and by the paymaster's float. Out of budget or float the
// account pays itself (false, nil), like chargeMsgFee.
func (k Keeper) SponsorIntentFee(ctx sdk.Context, sender sdk.AccAddress, fee sdk.Coin) (bool, error) {
	if !fee.IsPositive() {
		return false, nil
	}
	has, err := k.Accounts.Has(ctx, sender.String())
	if err != nil || !has {
		return false, err
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return false, err
	}
	capCoin := params.PaymasterDailyCap
	if !capCoin.IsPositive() || fee.Denom != capCoin.Denom {
		return false, nil
	}
	now := ctx.BlockTime().Unix()
	b, err := k.IntentFeeBudgets.Get(ctx, sender.String())
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return false, err
	}
	if err != nil || now-b.PeriodStart >= params.PaymasterPeriodSeconds || b.Spent.Denom != capCoin.Denom {
		b = types.AutoReturnBudget{PeriodStart: now, Spent: sdk.NewCoin(capCoin.Denom, math.ZeroInt())}
	}
	if b.Spent.Amount.Add(fee.Amount).GT(capCoin.Amount) {
		return false, nil
	}
	paymaster := types.PaymasterAddress()
	if !k.bankKeeper.GetBalance(ctx, paymaster, fee.Denom).IsGTE(fee) {
		return false, nil
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, paymaster, authtypes.FeeCollectorName, sdk.NewCoins(fee)); err != nil {
		return false, err
	}
	b.Spent = b.Spent.Add(fee)
	if err := k.IntentFeeBudgets.Set(ctx, sender.String(), b); err != nil {
		return false, err
	}
	return true, nil
}
