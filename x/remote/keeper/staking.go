package keeper

import (
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	lstypes "github.com/classic-terra/core/v4/x/liquidstake/types"
	"github.com/classic-terra/core/v4/x/remote/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// This file implements the chain side of cross-chain liquid staking
// (docs/staking/CROSS-CHAIN-LIQUID-STAKING.md §4): result references inside
// a payload (§4.2), deposit-then-execute ordering (§4.3) and the opt-in
// auto-return of matured claims (§4.4).

// ---------------------------------------------------------------------------
// §4.2 result references

// resolveWithdraw turns a MsgWithdraw with AMOUNT_FROM_PREVIOUS_RESULT into a
// literal withdrawal of the typed result of the preceding message. skip is
// true when the result is zero (a queued unstake). The result coin must be
// the origin denom of the warp token, so a payload never withdraws anything
// but the preceding result, and never more than it.
func (k *Keeper) resolveWithdraw(ctx sdk.Context, w *types.MsgWithdraw, prevURL string, prev *sdk.Result) (*types.MsgWithdraw, bool, error) {
	coin, err := k.previousResult(prevURL, prev)
	if err != nil {
		return nil, false, err
	}
	if coin.Amount.IsNil() || !coin.Amount.IsPositive() {
		return nil, true, nil
	}
	if w.MinAmount != nil && coin.Amount.LT(*w.MinAmount) {
		return nil, false, errorsmod.Wrapf(types.ErrInvalidReference, "result %s below min_amount %s", coin, w.MinAmount)
	}
	denom, err := k.warpToken(ctx, w.TokenId)
	if err != nil {
		return nil, false, err
	}
	if denom != coin.Denom {
		return nil, false, errorsmod.Wrapf(types.ErrInvalidReference, "result %s is not the denom %s of the warp token", coin, denom)
	}
	resolved := *w
	resolved.Amount = coin.Amount
	resolved.AmountFrom = types.AMOUNT_FROM_LITERAL
	resolved.MinAmount = nil
	return &resolved, false, nil
}

// previousResult decodes the amount a reference may use from the typed
// response of the preceding message (the closed list of §4.2, checked again
// at execution).
func (k *Keeper) previousResult(prevURL string, prev *sdk.Result) (sdk.Coin, error) {
	if prevURL == "" || prev == nil || !types.ResultMsgTypeURLs()[prevURL] {
		return sdk.Coin{}, errorsmod.Wrap(types.ErrInvalidReference, "the preceding message has no referenceable result")
	}
	if len(prev.MsgResponses) == 0 {
		return sdk.Coin{}, errorsmod.Wrap(types.ErrInvalidReference, "the preceding message returned no response")
	}
	value := prev.MsgResponses[0].Value
	switch prevURL {
	case sdk.MsgTypeURL(&lstypes.MsgStake{}):
		var r lstypes.MsgStakeResponse
		if err := k.cdc.Unmarshal(value, &r); err != nil {
			return sdk.Coin{}, errorsmod.Wrap(types.ErrInvalidReference, err.Error())
		}
		return r.Minted, nil
	case sdk.MsgTypeURL(&lstypes.MsgUnstake{}):
		var r lstypes.MsgUnstakeResponse
		if err := k.cdc.Unmarshal(value, &r); err != nil {
			return sdk.Coin{}, errorsmod.Wrap(types.ErrInvalidReference, err.Error())
		}
		if !r.Instant {
			return sdk.NewCoin(r.Amount.Denom, math.ZeroInt()), nil
		}
		return r.Amount, nil
	case sdk.MsgTypeURL(&lstypes.MsgClaim{}):
		var r lstypes.MsgClaimResponse
		if err := k.cdc.Unmarshal(value, &r); err != nil {
			return sdk.Coin{}, errorsmod.Wrap(types.ErrInvalidReference, err.Error())
		}
		return r.Amount, nil
	}
	return sdk.Coin{}, errorsmod.Wrap(types.ErrInvalidReference, prevURL)
}

// receipts collects the liquid-staking receipt of the last message and
// completes it with the message id of the withdrawal that references it.
type receipts struct {
	account, controller string
	domain              uint32
	pending             interface{ setWithdrawal(string) }
	event               sdk.Msg
}

type stakeReceipt struct{ *types.EventRemoteStake }

func (r stakeReceipt) setWithdrawal(id string) { r.WithdrawalMessageId = id }

type unstakeReceipt struct{ *types.EventRemoteUnstake }

func (r unstakeReceipt) setWithdrawal(id string) { r.WithdrawalMessageId = id }

type claimReceipt struct{ *types.EventRemoteClaim }

func (r claimReceipt) setWithdrawal(id string) { r.WithdrawalMessageId = id }

// observe records the receipt of a MsgStake/MsgUnstake/MsgClaim, or the
// withdrawal message id when msg is the referencing withdrawal.
func (rc *receipts) observe(cdc codec.Codec, msg sdk.Msg, res *sdk.Result, isRef bool) error {
	if len(res.MsgResponses) == 0 {
		return nil
	}
	value := res.MsgResponses[0].Value
	switch m := msg.(type) {
	case *lstypes.MsgStake:
		var r lstypes.MsgStakeResponse
		if err := cdc.Unmarshal(value, &r); err != nil {
			return err
		}
		ev := &types.EventRemoteStake{
			Account: rc.account, Controller: rc.controller, Domain: rc.domain,
			Uluna: m.Amount.String(), Minted: r.Minted.String(), Rate: r.ExchangeRate.String(),
		}
		rc.pending, rc.event = stakeReceipt{ev}, ev
	case *lstypes.MsgUnstake:
		var r lstypes.MsgUnstakeResponse
		if err := cdc.Unmarshal(value, &r); err != nil {
			return err
		}
		ev := &types.EventRemoteUnstake{
			Account: rc.account, Controller: rc.controller, Domain: rc.domain, StAmount: m.Amount.String(),
			Amount: r.Amount.String(), Instant: r.Instant, RequestId: r.RequestId, Rate: r.ExchangeRate.String(),
		}
		rc.pending, rc.event = unstakeReceipt{ev}, ev
	case *lstypes.MsgClaim:
		var r lstypes.MsgClaimResponse
		if err := cdc.Unmarshal(value, &r); err != nil {
			return err
		}
		ev := &types.EventRemoteClaim{
			Account: rc.account, Controller: rc.controller, Domain: rc.domain, Amount: r.Amount.String(), RequestIds: r.RequestIds,
		}
		rc.pending, rc.event = claimReceipt{ev}, ev
	case *types.MsgWithdraw:
		if isRef && rc.pending != nil {
			var r types.MsgWithdrawResponse
			if err := cdc.Unmarshal(value, &r); err != nil {
				return err
			}
			rc.pending.setWithdrawal(r.MessageId.String())
		}
	}
	return nil
}

// flush emits the collected receipt, if any.
func (rc *receipts) flush(ctx sdk.Context) error {
	if rc.event == nil {
		return nil
	}
	ev := rc.event
	rc.pending, rc.event = nil, nil
	return ctx.EventManager().EmitTypedEvent(ev)
}

// ---------------------------------------------------------------------------
// §4.3 deposit-then-execute

// creditKey is the key of a deposit credit: account, token and origin domain.
func creditKey(account string, tokenId util.HexAddress, origin uint32) string {
	return fmt.Sprintf("%s/%s/%d", account, tokenId.String(), origin)
}

// handleAfterDeposit executes the payload at once when its deposit already
// arrived, and otherwise stores it as pending (bounded per account).
func (k *Keeper) handleAfterDeposit(ctx sdk.Context, message util.HyperlaneMessage, domain uint32, controller util.HexAddress,
	derived sdk.AccAddress, payload types.RemotePayload,
) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	ad := *payload.AfterDeposit
	if err := ad.ValidateBasic(); err != nil {
		return k.reject(ctx, message.Origin, controller, derived.String(), err)
	}
	// static checks on arrival: a malformed payload never waits
	if _, err := k.prepare(params, derived, payload.Msgs); err != nil {
		return k.reject(ctx, message.Origin, controller, derived.String(), err)
	}
	ok, err := k.consumeCredit(ctx, params, derived.String(), ad, message.Origin)
	if err != nil {
		return err
	}
	if ok {
		if err := k.execute(ctx, domain, controller, derived, payload.Msgs); err != nil {
			return k.reject(ctx, message.Origin, controller, derived.String(), err)
		}
		return nil
	}
	count := uint32(0)
	if err := k.PendingByAccount.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](derived.String()),
		func(_ collections.Pair[string, uint64]) (bool, error) {
			count++
			return false, nil
		}); err != nil {
		return err
	}
	if count >= params.MaxPendingPerAccount {
		return k.reject(ctx, message.Origin, controller, derived.String(),
			errorsmod.Wrapf(types.ErrPendingFull, "%d payloads already wait for their deposit", count))
	}
	id, err := k.PendingSeq.Next(ctx)
	if err != nil {
		return err
	}
	id++ // ids start at 1
	p := types.PendingPayload{
		Id: id, Account: derived.String(), Domain: domain, Origin: message.Origin, Controller: controller,
		Msgs: payload.Msgs, AfterDeposit: ad, Height: ctx.BlockHeight(), MessageId: message.Id(),
	}
	if err := k.Pending.Set(ctx, id, p); err != nil {
		return err
	}
	if err := k.PendingByAccount.Set(ctx, collections.Join(p.Account, id)); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventRemotePending{
		Account: p.Account, Domain: domain, Controller: controller.String(), PendingId: id,
		TokenId: ad.TokenId.String(), Amount: ad.Amount.String(), ExpiryHeight: p.Height + params.PendingTtlBlocks,
	})
}

// consumeCredit takes the deposit a pending payload waits for out of the
// account's credit for (token, origin); false when it has not arrived (or
// the credit expired).
func (k Keeper) consumeCredit(ctx sdk.Context, params types.Params, account string, ad types.AfterDeposit, origin uint32) (bool, error) {
	key := creditKey(account, ad.TokenId, origin)
	c, err := k.Credits.Get(ctx, key)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	if ctx.BlockHeight() > c.Height+params.PendingTtlBlocks || c.Amount.LT(ad.Amount) {
		return false, nil
	}
	c.Amount = c.Amount.Sub(ad.Amount)
	if c.Amount.IsZero() {
		if err := k.CreditExpiry.Remove(ctx, collections.Join(c.Height, key)); err != nil {
			return false, err
		}
		return true, k.Credits.Remove(ctx, key)
	}
	return true, k.Credits.Set(ctx, key, c)
}

// OnWarpDelivered is called by the hyperlane core MsgServer wrapper
// (custom/hyperlane) after a message was processed. A warp deposit from an
// origin with an enrolled gateway is recorded as a deposit credit of the
// recipient, matched by origin domain + recipient + amount, and the pending
// payloads of the recipient that wait for that (token, origin) are marked
// ready for the EndBlock. Other messages are ignored; only store errors fail.
func (k Keeper) OnWarpDelivered(ctx sdk.Context, message util.HyperlaneMessage) error {
	t := message.Recipient.GetType()
	if t != uint32(warptypes.HYP_TOKEN_TYPE_COLLATERAL) && t != uint32(warptypes.HYP_TOKEN_TYPE_SYNTHETIC) {
		return nil
	}
	if k.ledgerKeeper == nil {
		return nil
	}
	if _, err := k.ledgerKeeper.GetToken(ctx, message.Recipient); err != nil {
		return nil
	}
	gated := false
	if err := k.Gateways.Walk(ctx, nil, func(key collections.Pair[uint64, uint32], _ types.Gateway) (bool, error) {
		gated = key.K2() == message.Origin
		return gated, nil
	}); err != nil {
		return err
	}
	if !gated {
		return nil
	}
	payload, err := warptypes.ParseWarpPayload(message.Body)
	if err != nil {
		return nil
	}
	amount := math.NewIntFromBigInt(payload.Amount())
	if !amount.IsPositive() {
		return nil
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	account := payload.GetCosmosAccount().String()
	key := creditKey(account, message.Recipient, message.Origin)
	c, err := k.Credits.Get(ctx, key)
	switch {
	case err == nil:
		if err := k.CreditExpiry.Remove(ctx, collections.Join(c.Height, key)); err != nil {
			return err
		}
		if ctx.BlockHeight() > c.Height+params.PendingTtlBlocks {
			c.Amount = math.ZeroInt()
		}
	case errors.Is(err, collections.ErrNotFound):
		c = types.DepositCredit{Account: account, TokenId: message.Recipient, Origin: message.Origin, Amount: math.ZeroInt()}
	default:
		return err
	}
	c.Amount = c.Amount.Add(amount)
	c.Height = ctx.BlockHeight()
	if err := k.Credits.Set(ctx, key, c); err != nil {
		return err
	}
	if err := k.CreditExpiry.Set(ctx, collections.Join(c.Height, key)); err != nil {
		return err
	}
	// mark the pending payloads of the account that wait for this deposit
	// (at most max_pending_per_account entries)
	var ready []uint64
	if err := k.PendingByAccount.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](account),
		func(pk collections.Pair[string, uint64]) (bool, error) {
			p, err := k.Pending.Get(ctx, pk.K2())
			if err != nil {
				return true, err
			}
			if p.Origin == message.Origin && p.AfterDeposit.TokenId.Equal(message.Recipient) {
				ready = append(ready, p.Id)
			}
			return false, nil
		}); err != nil {
		return err
	}
	for _, id := range ready {
		if err := k.PendingReady.Set(ctx, id); err != nil {
			return err
		}
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventDepositCredited{
		Account: account, TokenId: message.Recipient.String(), Origin: message.Origin, Amount: amount.String(), Credit: c.Amount.String(),
	})
}

// removePending deletes a pending payload and its indexes.
func (k Keeper) removePending(ctx sdk.Context, p types.PendingPayload) error {
	if err := k.PendingReady.Remove(ctx, p.Id); err != nil {
		return err
	}
	if err := k.PendingByAccount.Remove(ctx, collections.Join(p.Account, p.Id)); err != nil {
		return err
	}
	return k.Pending.Remove(ctx, p.Id)
}

// processPending runs in EndBlock, each step bounded by
// max_pending_execs_per_block (≤ MaxPendingExecsPerBlockAbsolute): it executes
// the ready payloads whose deposit arrived, expires the payloads older than
// pending_ttl_blocks with EventRemoteRejected "deposit not received", and
// prunes the expired deposit credits.
func (k *Keeper) processPending(ctx sdk.Context, params types.Params) error {
	bound := int(params.MaxPendingExecsPerBlock)
	// 1. ready payloads
	var ready []uint64
	if err := k.PendingReady.Walk(ctx, nil, func(id uint64) (bool, error) {
		ready = append(ready, id)
		return len(ready) >= bound, nil
	}); err != nil {
		return err
	}
	for _, id := range ready {
		if err := k.PendingReady.Remove(ctx, id); err != nil {
			return err
		}
		p, err := k.Pending.Get(ctx, id)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return err
		}
		if ctx.BlockHeight() > p.Height+params.PendingTtlBlocks {
			continue // expired: step 2 reports it
		}
		ok, err := k.consumeCredit(ctx, params, p.Account, p.AfterDeposit, p.Origin)
		if err != nil {
			return err
		}
		if !ok {
			continue // another payload took the credit: ready again on the next deposit
		}
		if err := k.removePending(ctx, p); err != nil {
			return err
		}
		derived := sdk.MustAccAddressFromBech32(p.Account)
		if err := k.execute(ctx, p.Domain, p.Controller, derived, p.Msgs); err != nil {
			if err := k.reject(ctx, p.Origin, p.Controller, p.Account, err); err != nil {
				return err
			}
		}
	}
	// 2. expiry: ids grow with the height, so the oldest come first
	var expired []types.PendingPayload
	if err := k.Pending.Walk(ctx, nil, func(_ uint64, p types.PendingPayload) (bool, error) {
		if ctx.BlockHeight() <= p.Height+params.PendingTtlBlocks {
			return true, nil
		}
		expired = append(expired, p)
		return len(expired) >= bound, nil
	}); err != nil {
		return err
	}
	for _, p := range expired {
		if err := k.removePending(ctx, p); err != nil {
			return err
		}
		if err := k.reject(ctx, p.Origin, p.Controller, p.Account,
			errorsmod.Wrapf(types.ErrInvalidPayload, "deposit not received (pending %d)", p.Id)); err != nil {
			return err
		}
	}
	// 3. expired credits
	var stale []collections.Pair[int64, string]
	if err := k.CreditExpiry.Walk(ctx, nil, func(key collections.Pair[int64, string]) (bool, error) {
		if ctx.BlockHeight() <= key.K1()+params.PendingTtlBlocks {
			return true, nil
		}
		stale = append(stale, key)
		return len(stale) >= bound, nil
	}); err != nil {
		return err
	}
	for _, key := range stale {
		if err := k.CreditExpiry.Remove(ctx, key); err != nil {
			return err
		}
		if c, err := k.Credits.Get(ctx, key.K2()); err == nil && c.Height == key.K1() {
			if err := k.Credits.Remove(ctx, key.K2()); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// §4.4 auto-return of matured claims

// destinationOf is the domain withdrawals of an account go to (the vault
// domain for a port user).
func (k Keeper) destinationOf(ctx sdk.Context, account types.RemoteAccount) uint32 {
	if port, err := k.Ports.Get(ctx, account.Domain); err == nil {
		return port.VaultDomain
	}
	return account.Domain
}

// SetAutoReturn opts a remote account in (through a warp token routed to its
// domain) or out of the auto-return.
func (k Keeper) SetAutoReturn(ctx sdk.Context, controller string, enabled bool, tokenId util.HexAddress) error {
	account, err := k.GetAccount(ctx, controller)
	if err != nil {
		return err
	}
	if !enabled {
		if err := k.AutoReturns.Remove(ctx, controller); err != nil {
			return err
		}
		return ctx.EventManager().EmitTypedEvent(&types.EventAutoReturnSet{Account: controller, Enabled: false})
	}
	if k.ledgerKeeper == nil {
		return errorsmod.Wrap(types.ErrInvalidParams, "warp ledger not available")
	}
	if _, err := k.ledgerKeeper.GetRemoteRouter(ctx, tokenId, k.destinationOf(ctx, account)); err != nil {
		return errorsmod.Wrapf(types.ErrInvalidParams, "token %s has no router to domain %d", tokenId, k.destinationOf(ctx, account))
	}
	if err := k.AutoReturns.Set(ctx, controller, types.AutoReturn{Account: controller, TokenId: tokenId, SetHeight: ctx.BlockHeight()}); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventAutoReturnSet{Account: controller, Enabled: true, TokenId: tokenId.String()})
}

// autoReturns runs once per liquid-staking epoch (x/liquidstake ends its
// epoch earlier in the same EndBlock): the matured requests of opted-in
// remote accounts are claimed and withdrawn to their controllers, at most
// max_auto_returns_per_epoch returns and AutoReturnScanFactor times as many
// accounts examined, round-robin from a cursor. The paymaster advances the
// interchain gas within paymaster_daily_cap per paymaster period; once it
// cannot, the rest waits for the next epoch (nothing is claimed, so nothing
// is lost and the user can always claim manually).
func (k *Keeper) autoReturns(ctx sdk.Context, params types.Params) error {
	if k.lsKeeper == nil {
		return nil
	}
	epoch, err := k.lsKeeper.GetEpoch(ctx)
	if err != nil {
		return err
	}
	last, err := k.AutoReturnEpoch.Get(ctx)
	if err == nil && last == epoch.Number {
		return nil
	}
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	if err := k.AutoReturnEpoch.Set(ctx, epoch.Number); err != nil {
		return err
	}
	accounts, err := k.autoReturnBatch(ctx, int(params.MaxAutoReturnsPerEpoch)*types.AutoReturnScanFactor)
	if err != nil || len(accounts) == 0 {
		return err
	}
	budget, err := k.autoReturnBudget(ctx, params)
	if err != nil {
		return err
	}
	returned := uint32(0)
	scanned := ""
	for _, ar := range accounts {
		if returned >= params.MaxAutoReturnsPerEpoch {
			break
		}
		done, stop, err := k.autoReturnOne(ctx, params, ar, &budget)
		if err != nil {
			return err
		}
		if stop {
			break // paymaster cap or balance: the rest waits for the next epoch
		}
		scanned = ar.Account
		if done {
			returned++
		}
	}
	if err := k.AutoReturnBudget.Set(ctx, budget); err != nil {
		return err
	}
	if scanned != "" {
		return k.AutoReturnCursor.Set(ctx, scanned)
	}
	return nil
}

// autoReturnBatch returns up to limit opted-in accounts after the cursor,
// wrapping around once.
func (k Keeper) autoReturnBatch(ctx sdk.Context, limit int) ([]types.AutoReturn, error) {
	cursor, err := k.AutoReturnCursor.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}
	var out []types.AutoReturn
	collect := func(_ string, ar types.AutoReturn) (bool, error) {
		out = append(out, ar)
		return len(out) >= limit, nil
	}
	if err := k.AutoReturns.Walk(ctx, new(collections.Range[string]).StartExclusive(cursor), collect); err != nil {
		return nil, err
	}
	if len(out) < limit && cursor != "" {
		if err := k.AutoReturns.Walk(ctx, new(collections.Range[string]).EndInclusive(cursor), collect); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// autoReturnBudget returns the paymaster budget of the current period.
func (k Keeper) autoReturnBudget(ctx sdk.Context, params types.Params) (types.AutoReturnBudget, error) {
	now := ctx.BlockTime().Unix()
	b, err := k.AutoReturnBudget.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return b, err
	}
	if err != nil || now-b.PeriodStart >= params.PaymasterPeriodSeconds || b.Spent.Denom != params.PaymasterDailyCap.Denom {
		b = types.AutoReturnBudget{PeriodStart: now, Spent: sdk.NewCoin(params.PaymasterDailyCap.Denom, math.ZeroInt())}
	}
	return b, nil
}

// autoReturnOne claims and withdraws the matured requests of one account in a
// cache context. done reports a return; stop that the paymaster cannot fund
// more returns this epoch.
func (k *Keeper) autoReturnOne(ctx sdk.Context, params types.Params, ar types.AutoReturn, budget *types.AutoReturnBudget) (done, stop bool, err error) {
	account, err := k.GetAccount(ctx, ar.Account)
	if err != nil {
		return false, false, nil
	}
	destination := k.destinationOf(ctx, account)
	fee, err := k.quoteInterchainFee(ctx, ar.TokenId, destination, params.WithdrawFeeCap)
	if err != nil {
		k.Logger(ctx).Info("auto-return not quoted", "account", ar.Account, "err", err)
		return false, false, nil
	}
	if fee.IsPositive() {
		if fee.Denom != budget.Spent.Denom || budget.Spent.Add(fee).Amount.GT(params.PaymasterDailyCap.Amount) ||
			!k.bankKeeper.GetBalance(ctx, types.PaymasterAddress(), fee.Denom).IsGTE(fee) {
			return false, true, nil
		}
	}
	cacheCtx, write := ctx.CacheContext()
	addr := sdk.MustAccAddressFromBech32(ar.Account)
	claimed, ids, err := k.lsKeeper.Claim(cacheCtx, addr)
	if err != nil {
		// nothing matured, or the matured unbonding is not credited yet
		return false, false, nil
	}
	denom, err := k.warpToken(cacheCtx, ar.TokenId)
	if err != nil || denom != claimed.Denom {
		k.Logger(ctx).Info("auto-return token does not carry the claim", "account", ar.Account, "token", ar.TokenId.String())
		return false, false, nil
	}
	id, err := k.Withdraw(cacheCtx, ar.Account, ar.TokenId, claimed.Amount, "", nil)
	if err != nil {
		k.Logger(ctx).Info("auto-return withdrawal failed", "account", ar.Account, "err", err)
		return false, false, nil
	}
	if err := cacheCtx.EventManager().EmitTypedEvent(&types.EventRemoteClaim{
		Account: ar.Account, Controller: account.Controller.String(), Domain: account.Domain, Amount: claimed.String(),
		RequestIds: ids, WithdrawalMessageId: id.String(), Auto: true,
	}); err != nil {
		return false, false, err
	}
	write()
	if fee.IsPositive() {
		budget.Spent = budget.Spent.Add(fee)
	}
	return true, false, nil
}

// RemoteStaking returns the pending payloads, deposit credits and auto-return
// of an account (query).
func (k Keeper) RemoteStaking(ctx sdk.Context, address string) (*types.QueryRemoteStakingResponse, error) {
	out := &types.QueryRemoteStakingResponse{Pending: []types.PendingPayload{}, Credits: []types.DepositCredit{}}
	if err := k.PendingByAccount.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](address),
		func(pk collections.Pair[string, uint64]) (bool, error) {
			p, err := k.Pending.Get(ctx, pk.K2())
			if err != nil {
				return true, err
			}
			out.Pending = append(out.Pending, p)
			return false, nil
		}); err != nil {
		return nil, err
	}
	prefix := address + "/"
	if err := k.Credits.Walk(ctx, new(collections.Range[string]).Prefix(prefix), func(_ string, c types.DepositCredit) (bool, error) {
		out.Credits = append(out.Credits, c)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if ar, err := k.AutoReturns.Get(ctx, address); err == nil {
		out.AutoReturn = &ar
	}
	return out, nil
}
