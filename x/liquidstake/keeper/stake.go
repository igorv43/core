package keeper

import (
	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Stake locks uluna in the module and mints stLUNC at the current exchange
// rate, rounding the minted amount down (spec §19.3: rounding against the
// participant). The uluna is delegated at the next epoch.
func (k Keeper) Stake(ctx sdk.Context, sender sdk.AccAddress, amount sdk.Coin) (sdk.Coin, math.LegacyDec, error) {
	denom, err := k.bondDenom(ctx)
	if err != nil {
		return sdk.Coin{}, math.LegacyDec{}, err
	}
	if amount.Denom != denom {
		return sdk.Coin{}, math.LegacyDec{}, errorsmod.Wrapf(types.ErrInvalidDenom, "expected %s, got %s", denom, amount.Denom)
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return sdk.Coin{}, math.LegacyDec{}, err
	}

	rate, totals, err := k.ExchangeRate(ctx)
	if err != nil {
		return sdk.Coin{}, math.LegacyDec{}, err
	}

	// module cap (spec §24.2): delegated + buffer + new deposit ≤ max_share · bonded
	bonded, err := k.stakingKeeper.TotalBondedTokens(ctx)
	if err != nil {
		return sdk.Coin{}, math.LegacyDec{}, err
	}
	cap := params.MaxShare.MulInt(bonded).TruncateInt()
	if totals.Delegated.Add(totals.Buffer()).Add(amount.Amount).GT(cap) {
		return sdk.Coin{}, math.LegacyDec{}, errorsmod.Wrapf(types.ErrModuleCapReached,
			"module holds %s, cap is %s (%s of bonded %s)", totals.Delegated.Add(totals.Buffer()), cap, params.MaxShare, bonded)
	}

	minted := math.LegacyNewDecFromInt(amount.Amount).Quo(rate).TruncateInt()
	if !minted.IsPositive() {
		return sdk.Coin{}, math.LegacyDec{}, errorsmod.Wrap(types.ErrInvalidDenom, "amount too small to mint stLUNC")
	}

	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sender, types.ModuleName, sdk.NewCoins(amount)); err != nil {
		return sdk.Coin{}, math.LegacyDec{}, err
	}
	stCoin := sdk.NewCoin(types.StDenom, minted)
	if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(stCoin)); err != nil {
		return sdk.Coin{}, math.LegacyDec{}, err
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sender, sdk.NewCoins(stCoin)); err != nil {
		return sdk.Coin{}, math.LegacyDec{}, err
	}

	if err := ctx.EventManager().EmitTypedEvent(&types.EventStaked{
		Address: sender.String(), Amount: amount.String(), Minted: stCoin.String(), ExchangeRate: rate.String(),
	}); err != nil {
		return sdk.Coin{}, math.LegacyDec{}, err
	}
	return stCoin, rate, nil
}

// UnstakeResult is the outcome of an unstake.
type UnstakeResult struct {
	Amount    sdk.Coin
	Instant   bool
	RequestID uint64
	Rate      math.LegacyDec
}

// Unstake burns stLUNC at the current exchange rate. The uluna is paid
// immediately from the buffer when it covers the amount; otherwise the
// request is queued and undelegated in a single batch at the end of the
// epoch (spec §24.4).
func (k Keeper) Unstake(ctx sdk.Context, sender sdk.AccAddress, stAmount sdk.Coin) (UnstakeResult, error) {
	if stAmount.Denom != types.StDenom {
		return UnstakeResult{}, errorsmod.Wrapf(types.ErrInvalidDenom, "expected %s, got %s", types.StDenom, stAmount.Denom)
	}
	denom, err := k.bondDenom(ctx)
	if err != nil {
		return UnstakeResult{}, err
	}
	rate, totals, err := k.ExchangeRate(ctx)
	if err != nil {
		return UnstakeResult{}, err
	}
	if totals.StSupply.IsZero() {
		return UnstakeResult{}, types.ErrZeroSupply
	}

	amount := math.LegacyNewDecFromInt(stAmount.Amount).Mul(rate).TruncateInt()
	if !amount.IsPositive() {
		return UnstakeResult{}, errorsmod.Wrap(types.ErrInvalidDenom, "amount too small to redeem")
	}

	// burn the receipt first: the sender's claim is now the uluna amount
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sender, types.ModuleName, sdk.NewCoins(stAmount)); err != nil {
		return UnstakeResult{}, err
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(stAmount)); err != nil {
		return UnstakeResult{}, err
	}

	coin := sdk.NewCoin(denom, amount)
	if amount.LTE(totals.Buffer()) {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sender, sdk.NewCoins(coin)); err != nil {
			return UnstakeResult{}, err
		}
		if err := ctx.EventManager().EmitTypedEvent(&types.EventUnstaked{
			Address: sender.String(), StAmount: stAmount.String(), Amount: coin.String(), ExchangeRate: rate.String(), Instant: true,
		}); err != nil {
			return UnstakeResult{}, err
		}
		return UnstakeResult{Amount: coin, Instant: true, Rate: rate}, nil
	}

	epoch, err := k.GetEpoch(ctx)
	if err != nil {
		return UnstakeResult{}, err
	}
	id, err := k.RequestSeq.Next(ctx)
	if err != nil {
		return UnstakeResult{}, err
	}
	req := types.UnstakeRequest{
		Id:       id,
		Address:  sender.String(),
		StAmount: stAmount.Amount,
		Amount:   amount,
		Epoch:    epoch.Number,
	}
	if err := k.Requests.Set(ctx, id, req); err != nil {
		return UnstakeResult{}, err
	}
	if err := k.RequestsByAddr.Set(ctx, collections.Join(sender.String(), id)); err != nil {
		return UnstakeResult{}, err
	}
	if err := k.addOwed(ctx, amount); err != nil {
		return UnstakeResult{}, err
	}

	if err := ctx.EventManager().EmitTypedEvent(&types.EventUnstaked{
		Address: sender.String(), StAmount: stAmount.String(), Amount: coin.String(), ExchangeRate: rate.String(), Instant: false, RequestId: id,
	}); err != nil {
		return UnstakeResult{}, err
	}
	return UnstakeResult{Amount: coin, Instant: false, RequestID: id, Rate: rate}, nil
}

// Claim pays every matured request of the sender (bounded by MaxRequestsPerClaim).
func (k Keeper) Claim(ctx sdk.Context, sender sdk.AccAddress) (sdk.Coin, []uint64, error) {
	denom, err := k.bondDenom(ctx)
	if err != nil {
		return sdk.Coin{}, nil, err
	}
	now := ctx.BlockTime()
	total := math.ZeroInt()
	var ids []uint64

	rng := collections.NewPrefixedPairRange[string, uint64](sender.String())
	if err := k.RequestsByAddr.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
		if len(ids) >= types.MaxRequestsPerClaim {
			return true, nil
		}
		req, err := k.Requests.Get(ctx, key.K2())
		if err != nil {
			return true, err
		}
		if !req.Undelegated || req.CompletionTime.After(now) {
			return false, nil
		}
		total = total.Add(req.Amount)
		ids = append(ids, req.Id)
		return false, nil
	}); err != nil {
		return sdk.Coin{}, nil, err
	}
	if len(ids) == 0 {
		return sdk.Coin{}, nil, types.ErrNothingToClaim
	}

	balance := k.bankKeeper.GetBalance(ctx, k.ModuleAddress(), denom).Amount
	if balance.LT(total) {
		// matured unbonding not yet credited (or slashed in transit): pay what
		// is possible next block rather than failing forever
		return sdk.Coin{}, nil, errorsmod.Wrapf(types.ErrInsufficientBuffer, "module balance %s below matured claims %s", balance, total)
	}

	coin := sdk.NewCoin(denom, total)
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sender, sdk.NewCoins(coin)); err != nil {
		return sdk.Coin{}, nil, err
	}
	for _, id := range ids {
		if err := k.Requests.Remove(ctx, id); err != nil {
			return sdk.Coin{}, nil, err
		}
		if err := k.RequestsByAddr.Remove(ctx, collections.Join(sender.String(), id)); err != nil {
			return sdk.Coin{}, nil, err
		}
	}
	if err := k.addOwed(ctx, total.Neg()); err != nil {
		return sdk.Coin{}, nil, err
	}

	if err := ctx.EventManager().EmitTypedEvent(&types.EventClaimed{
		Address: sender.String(), Amount: coin.String(), RequestIds: ids,
	}); err != nil {
		return sdk.Coin{}, nil, err
	}
	return coin, ids, nil
}
