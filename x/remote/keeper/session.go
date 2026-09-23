package keeper

import (
	"errors"
	"time"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/x/feegrant"
	feegrantkeeper "cosmossdk.io/x/feegrant/keeper"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// Session keys (spec §14.4 item 3): x/authz grants scoped to the trading
// messages, expiring after session_ttl_seconds, at most max_sessions per
// remote account; the paymaster (item 4) grants a periodic fee allowance
// to keys of accounts with enough free collateral.

// GrantSession grants a session key to a remote account.
func (k Keeper) GrantSession(ctx sdk.Context, controller, sessionKey string, ttlSeconds int64) (time.Time, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if _, err := k.GetAccount(ctx, controller); err != nil {
		return time.Time{}, err
	}
	n := 0
	if err := k.Sessions.Walk(ctx, collections.NewPrefixedPairRange[string, string](controller),
		func(_ collections.Pair[string, string], _ types.Session) (bool, error) { n++; return false, nil }); err != nil {
		return time.Time{}, err
	}
	if has, err := k.Sessions.Has(ctx, collections.Join(controller, sessionKey)); err != nil {
		return time.Time{}, err
	} else if !has && n >= int(params.MaxSessions) {
		return time.Time{}, errorsmod.Wrapf(types.ErrTooManySessions, "max %d", params.MaxSessions)
	}
	if ttlSeconds <= 0 || ttlSeconds > params.SessionTtlSeconds {
		ttlSeconds = params.SessionTtlSeconds
	}
	expiry := ctx.BlockTime().Add(time.Duration(ttlSeconds) * time.Second)
	granter, grantee := sdk.MustAccAddressFromBech32(controller), sdk.MustAccAddressFromBech32(sessionKey)
	for _, url := range types.SessionMsgTypeURLs() {
		if err := k.authzKeeper.SaveGrant(ctx, grantee, granter, authz.NewGenericAuthorization(url), &expiry); err != nil {
			return time.Time{}, err
		}
	}
	paymaster, err := k.grantAllowance(ctx, params, controller, grantee, expiry)
	if err != nil {
		return time.Time{}, err
	}
	if err := k.Sessions.Set(ctx, collections.Join(controller, sessionKey), types.Session{
		Account: controller, SessionKey: sessionKey, Expiry: expiry, GrantedHeight: ctx.BlockHeight(), Paymaster: paymaster,
	}); err != nil {
		return time.Time{}, err
	}
	return expiry, ctx.EventManager().EmitTypedEvent(&types.EventSessionGranted{Account: controller, SessionKey: sessionKey, Expiry: expiry.UTC().Format(time.RFC3339), Paymaster: paymaster})
}

// grantAllowance gives the session key a periodic allowance from the
// paymaster, restricted to the session messages, when the account holds the
// minimum free collateral and no allowance exists yet.
func (k Keeper) grantAllowance(ctx sdk.Context, params types.Params, controller string, grantee sdk.AccAddress, expiry time.Time) (bool, error) {
	if k.perpKeeper == nil || !params.PaymasterDailyCap.IsPositive() {
		return false, nil
	}
	free, err := k.perpKeeper.FreeCollateral(ctx, controller)
	if err != nil {
		return false, err
	}
	if free.LT(params.PaymasterMinCollateral) {
		return false, nil
	}
	paymaster := types.PaymasterAddress()
	if _, err := k.feegrantKeeper.GetAllowance(ctx, paymaster, grantee); err == nil {
		return true, nil
	}
	period := time.Duration(params.PaymasterPeriodSeconds) * time.Second
	limit := sdk.NewCoins(params.PaymasterDailyCap)
	periodic := &feegrant.PeriodicAllowance{
		Basic:            feegrant.BasicAllowance{Expiration: &expiry},
		Period:           period,
		PeriodSpendLimit: limit,
		PeriodCanSpend:   limit,
		PeriodReset:      ctx.BlockTime().Add(period),
	}
	// the key sends MsgExec (authz) wrapping the session messages: both layers are filtered
	allowed := append(types.SessionMsgTypeURLs(), sdk.MsgTypeURL(&authz.MsgExec{}))
	allowance, err := feegrant.NewAllowedMsgAllowance(periodic, allowed)
	if err != nil {
		return false, err
	}
	if err := k.feegrantKeeper.GrantAllowance(ctx, paymaster, grantee, allowance); err != nil {
		return false, err
	}
	return true, nil
}

// RevokeSession removes a session key: authz grants, the allowance and the record.
func (k Keeper) RevokeSession(ctx sdk.Context, controller, sessionKey string) error {
	s, err := k.Sessions.Get(ctx, collections.Join(controller, sessionKey))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.ErrSessionNotFound.Wrap(sessionKey)
		}
		return err
	}
	granter, grantee := sdk.MustAccAddressFromBech32(controller), sdk.MustAccAddressFromBech32(sessionKey)
	for _, url := range types.SessionMsgTypeURLs() {
		_ = k.authzKeeper.DeleteGrant(ctx, grantee, granter, url) // may already have expired
	}
	if s.Paymaster {
		_, _ = feegrantkeeper.NewMsgServerImpl(k.feegrantKeeper).RevokeAllowance(ctx, &feegrant.MsgRevokeAllowance{
			Granter: types.PaymasterAddress().String(), Grantee: sessionKey,
		})
	}
	if err := k.Sessions.Remove(ctx, collections.Join(controller, sessionKey)); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventSessionRevoked{Account: controller, SessionKey: sessionKey})
}

// SessionsOf lists the session keys of an account.
func (k Keeper) SessionsOf(ctx sdk.Context, controller string) ([]types.Session, error) {
	out := []types.Session{}
	err := k.Sessions.Walk(ctx, collections.NewPrefixedPairRange[string, string](controller),
		func(_ collections.Pair[string, string], s types.Session) (bool, error) {
			out = append(out, s)
			return false, nil
		})
	return out, err
}

// FundPaymaster moves coins from the sender to the paymaster account.
func (k Keeper) FundPaymaster(ctx sdk.Context, sender sdk.AccAddress, amount sdk.Coin) error {
	return k.bankKeeper.SendCoins(ctx, sender, types.PaymasterAddress(), sdk.NewCoins(amount))
}
