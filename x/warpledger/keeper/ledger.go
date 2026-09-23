package keeper

import (
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/classic-terra/core/v4/x/warpledger/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ledgerKey builds the collections key of a (token, domain) ledger.
func ledgerKey(tokenId util.HexAddress, domain uint32) collections.Pair[uint64, uint32] {
	return collections.Join(tokenId.GetInternalId(), domain)
}

// newLedger returns an empty ledger for a pair.
func newLedger(tokenId util.HexAddress, domain uint32) types.DomainLedger {
	return types.DomainLedger{
		TokenId:  tokenId,
		Domain:   domain,
		Sent:     math.ZeroInt(),
		Received: math.ZeroInt(),
		Cap:      math.ZeroInt(),
		HasCap:   false,
	}
}

// Exposure returns the net exposure of a ledger: sent - received.
func Exposure(l types.DomainLedger) math.Int {
	return l.Sent.Sub(l.Received)
}

// GetToken returns the warp token for an id.
func (k Keeper) GetToken(ctx sdk.Context, tokenId util.HexAddress) (warptypes.HypToken, error) {
	token, err := k.warpKeeper.HypTokens.Get(ctx, tokenId.GetInternalId())
	if err != nil {
		return warptypes.HypToken{}, errorsmod.Wrapf(types.ErrTokenNotFound, "%s", tokenId.String())
	}
	return token, nil
}

// GetLedger returns the ledger of a pair, or an empty ledger when none exists.
// The second return value reports whether the ledger exists in state.
func (k Keeper) GetLedger(ctx sdk.Context, tokenId util.HexAddress, domain uint32) (types.DomainLedger, bool, error) {
	ledger, err := k.Ledgers.Get(ctx, ledgerKey(tokenId, domain))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return newLedger(tokenId, domain), false, nil
		}
		return types.DomainLedger{}, false, err
	}
	return ledger, true, nil
}

// setLedger persists a ledger, enforcing the per-token domain limit when the
// pair is new.
func (k Keeper) setLedger(ctx sdk.Context, ledger types.DomainLedger, exists bool) error {
	if !exists {
		count, err := k.countDomains(ctx, ledger.TokenId)
		if err != nil {
			return err
		}
		if count >= types.MaxDomainsPerToken {
			return errorsmod.Wrapf(types.ErrTooManyDomains, "token %s already has %d domains", ledger.TokenId.String(), count)
		}
	}
	return k.Ledgers.Set(ctx, ledgerKey(ledger.TokenId, ledger.Domain), ledger)
}

// countDomains returns the number of ledgers of a token.
func (k Keeper) countDomains(ctx sdk.Context, tokenId util.HexAddress) (int, error) {
	count := 0
	err := k.Ledgers.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint32](tokenId.GetInternalId()),
		func(_ collections.Pair[uint64, uint32], _ types.DomainLedger) (bool, error) {
			count++
			return false, nil
		})
	return count, err
}

// EffectiveCap returns the cap that applies to a ledger: the explicit one when
// set by governance, otherwise the default from params.
func (k Keeper) EffectiveCap(ctx sdk.Context, ledger types.DomainLedger) (math.Int, error) {
	if ledger.HasCap {
		return ledger.Cap, nil
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return math.Int{}, err
	}
	return params.DefaultDomainCap, nil
}

// AssertOutboundAllowed rejects an outbound transfer that would push the pair
// exposure above its cap. It is called by the warp MsgServer wrapper before
// the upstream handler runs, so the cap is enforced at the edge (spec §9.3).
func (k Keeper) AssertOutboundAllowed(ctx sdk.Context, tokenId util.HexAddress, domain uint32, amount math.Int) error {
	ledger, _, err := k.GetLedger(ctx, tokenId, domain)
	if err != nil {
		return err
	}
	cap, err := k.EffectiveCap(ctx, ledger)
	if err != nil {
		return err
	}
	next := Exposure(ledger).Add(amount)
	if next.GT(cap) {
		return errorsmod.Wrapf(types.ErrDomainCapExceeded,
			"token %s domain %d: exposure %s + amount %s exceeds cap %s",
			tokenId.String(), domain, Exposure(ledger), amount, cap)
	}
	return nil
}

// RecordSent increments the sent counter of a pair after a successful
// outbound dispatch.
func (k Keeper) RecordSent(ctx sdk.Context, tokenId util.HexAddress, domain uint32, amount math.Int) error {
	ledger, exists, err := k.GetLedger(ctx, tokenId, domain)
	if err != nil {
		return err
	}
	ledger.Sent = ledger.Sent.Add(amount)
	if err := k.setLedger(ctx, ledger, exists); err != nil {
		return err
	}

	return ctx.EventManager().EmitTypedEvent(&types.EventRecordSent{
		TokenId:  tokenId.String(),
		Domain:   domain,
		Amount:   amount.String(),
		Sent:     ledger.Sent.String(),
		Received: ledger.Received.String(),
		Exposure: Exposure(ledger).String(),
	})
}

// RecordReceived increments the received counter of a pair after a successful
// inbound process.
func (k Keeper) RecordReceived(ctx sdk.Context, tokenId util.HexAddress, domain uint32, amount math.Int) error {
	ledger, exists, err := k.GetLedger(ctx, tokenId, domain)
	if err != nil {
		return err
	}
	ledger.Received = ledger.Received.Add(amount)
	if err := k.setLedger(ctx, ledger, exists); err != nil {
		return err
	}

	return ctx.EventManager().EmitTypedEvent(&types.EventRecordReceived{
		TokenId:  tokenId.String(),
		Domain:   domain,
		Amount:   amount.String(),
		Sent:     ledger.Sent.String(),
		Received: ledger.Received.String(),
		Exposure: Exposure(ledger).String(),
	})
}

// RecordInboundMessage accounts a processed hyperlane message when its
// recipient is a warp token. Messages for other applications are ignored.
// It is called by the hyperlane core MsgServer wrapper after a successful
// upstream ProcessMessage.
func (k Keeper) RecordInboundMessage(ctx sdk.Context, message util.HyperlaneMessage) error {
	recipientType := message.Recipient.GetType()
	if recipientType != uint32(warptypes.HYP_TOKEN_TYPE_COLLATERAL) &&
		recipientType != uint32(warptypes.HYP_TOKEN_TYPE_SYNTHETIC) {
		return nil
	}
	if has, err := k.warpKeeper.HypTokens.Has(ctx, message.Recipient.GetInternalId()); err != nil || !has {
		return err
	}

	payload, err := warptypes.ParseWarpPayload(message.Body)
	if err != nil {
		// upstream already accepted the message; a payload we cannot parse is
		// not a warp transfer and must not be accounted
		return nil
	}

	return k.RecordReceived(ctx, message.Recipient, message.Origin, math.NewIntFromBigInt(payload.Amount()))
}

// SetDomainCap sets the explicit cap of a pair. The token must exist in x/warp.
func (k Keeper) SetDomainCap(ctx sdk.Context, tokenId util.HexAddress, domain uint32, cap math.Int) error {
	token, err := k.GetToken(ctx, tokenId)
	if err != nil {
		return err
	}
	if err := k.assertBondedForCap(ctx, token, cap); err != nil {
		return err
	}
	ledger, exists, err := k.GetLedger(ctx, tokenId, domain)
	if err != nil {
		return err
	}
	ledger.Cap = cap
	ledger.HasCap = true
	return k.setLedger(ctx, ledger, exists)
}

// IterateLedgers walks every ledger in key order.
func (k Keeper) IterateLedgers(ctx sdk.Context, fn func(ledger types.DomainLedger) (stop bool, err error)) error {
	return k.Ledgers.Walk(ctx, nil, func(_ collections.Pair[uint64, uint32], ledger types.DomainLedger) (bool, error) {
		return fn(ledger)
	})
}

// LedgersOfToken returns the ledgers of a token in domain order.
func (k Keeper) LedgersOfToken(ctx sdk.Context, tokenId util.HexAddress) ([]types.DomainLedger, error) {
	var ledgers []types.DomainLedger
	err := k.Ledgers.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint32](tokenId.GetInternalId()),
		func(_ collections.Pair[uint64, uint32], ledger types.DomainLedger) (bool, error) {
			ledgers = append(ledgers, ledger)
			return false, nil
		})
	return ledgers, err
}

// assertBondedForCap enforces spec §7.2: a per-domain cap above
// params.bonded_cap_threshold needs the token's ISM (or the mailbox default)
// bonded at its threshold in x/ismbond. Zero threshold disables the rule.
func (k Keeper) assertBondedForCap(ctx sdk.Context, token warptypes.HypToken, cap math.Int) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if params.BondedCapThreshold.IsNil() || !params.BondedCapThreshold.IsPositive() || cap.LTE(params.BondedCapThreshold) {
		return nil
	}
	if k.ismBond == nil {
		return errorsmod.Wrap(types.ErrIsmNotBonded, "x/ismbond not available")
	}
	bonded, err := k.ismBond.IsmBondedForToken(ctx, token.IsmId, token.OriginMailbox)
	if err != nil {
		return err
	}
	if !bonded {
		return errorsmod.Wrapf(types.ErrIsmNotBonded, "cap %s above threshold %s", cap, params.BondedCapThreshold)
	}
	return nil
}

// TotalExposure returns Σ max(0, sent − received) over the domains of a token:
// the collateral the route must hold (the EndBlock invariant compares it with
// the warp module balance).
func (k Keeper) TotalExposure(ctx sdk.Context, tokenId util.HexAddress) (math.Int, error) {
	ledgers, err := k.LedgersOfToken(ctx, tokenId)
	if err != nil {
		return math.Int{}, err
	}
	total := math.ZeroInt()
	for _, l := range ledgers {
		if net := l.Sent.Sub(l.Received); net.IsPositive() {
			total = total.Add(net)
		}
	}
	return total, nil
}
