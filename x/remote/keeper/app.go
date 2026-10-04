package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/classic-terra/core/v4/x/remote/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

var _ util.HyperlaneApp = (*Keeper)(nil)

// Exists implements util.HyperlaneApp.
func (k *Keeper) Exists(ctx context.Context, recipient util.HexAddress) (bool, error) {
	return k.Apps.Has(ctx, recipient.GetInternalId())
}

// ReceiverIsmId implements util.HyperlaneApp: the app's ISM or the mailbox default.
func (k *Keeper) ReceiverIsmId(ctx context.Context, recipient util.HexAddress) (*util.HexAddress, error) {
	app, err := k.GetApp(sdk.UnwrapSDKContext(ctx), recipient)
	if err != nil {
		return nil, err
	}
	if app.IsmId != nil {
		return app.IsmId, nil
	}
	mailbox, err := k.coreKeeper.GetMailbox(ctx, app.MailboxId)
	if err != nil {
		return nil, err
	}
	return &mailbox.DefaultIsm, nil
}

// Handle implements util.HyperlaneApp: a message verified by the ISM whose
// sender is (origin, sender) controls the derived account. The payload is
// executed atomically; a rejected payload is consumed (the message counts
// as delivered) and reported by an event, never retried by relayers.
func (k *Keeper) Handle(goCtx context.Context, mailboxId util.HexAddress, message util.HyperlaneMessage) error {
	ctx := sdk.UnwrapSDKContext(goCtx)
	app, err := k.GetApp(ctx, message.Recipient)
	if err != nil {
		return err
	}
	if !app.MailboxId.Equal(mailboxId) {
		return fmt.Errorf("invalid origin mailbox address")
	}
	controller := message.Sender
	var payload types.RemotePayload
	if err := k.cdc.Unmarshal(message.Body, &payload); err != nil {
		return k.reject(ctx, message.Origin, controller, "", errorsmod.Wrap(types.ErrInvalidPayload, err.Error()))
	}
	if len(payload.OnBehalfOf) > 0 {
		gw, ok, err := k.gateway(ctx, app.Id, message.Origin)
		if err != nil {
			return err
		}
		if !ok || !gw.Equal(message.Sender) {
			return k.reject(ctx, message.Origin, controller, "", errorsmod.Wrap(types.ErrInvalidPayload, "on_behalf_of allowed only from the enrolled gateway"))
		}
		if len(payload.OnBehalfOf) != 32 {
			return k.reject(ctx, message.Origin, controller, "", errorsmod.Wrap(types.ErrInvalidPayload, "on_behalf_of must be 32 bytes"))
		}
		copy(controller[:], payload.OnBehalfOf)
	}
	fromGateway := len(payload.OnBehalfOf) > 0
	domain := message.Origin
	if payload.PortDomain != 0 {
		// a port-of-entry deposit (spec §11.6): the vault chain's gateway
		// credits the account of the port chain's user
		port, err := k.Ports.Get(ctx, payload.PortDomain)
		if !fromGateway || err != nil || port.VaultDomain != message.Origin {
			return k.reject(ctx, message.Origin, controller, "", errorsmod.Wrapf(types.ErrInvalidPayload, "port %d not served by domain %d", payload.PortDomain, message.Origin))
		}
		domain = payload.PortDomain
	}
	derived := types.DeriveAddress(domain, controller)
	if (payload.Conversion != nil || payload.Update != nil) && !fromGateway {
		return k.reject(ctx, message.Origin, controller, derived.String(), errorsmod.Wrap(types.ErrInvalidConversion, "conversion data allowed only from the enrolled gateway"))
	}
	// a conversion receipt is evidence of the deposit converted at the edge
	// (spec §14.7); it is written before the messages so a rejected payload
	// still leaves the receipt of the value that arrived through warp
	if payload.Conversion != nil {
		r, err := depositReceipt(ctx, derived.String(), domain, message.Id(), payload.Conversion)
		if err != nil {
			return k.reject(ctx, message.Origin, controller, derived.String(), err)
		}
		if err := k.recordReceipt(ctx, r); err != nil {
			return err
		}
	}
	if payload.Update != nil {
		if err := k.applyUpdate(ctx, derived.String(), payload.Update); err != nil {
			return k.reject(ctx, message.Origin, controller, derived.String(), err)
		}
	}
	if len(payload.Msgs) == 0 && (payload.Conversion != nil || payload.Update != nil) {
		return nil
	}
	if err := k.execute(ctx, domain, controller, derived, payload.Msgs); err != nil {
		return k.reject(ctx, message.Origin, controller, derived.String(), err)
	}
	return nil
}

// execute charges the message fee and runs the whitelisted messages of the
// payload in one cache context.
func (k *Keeper) execute(ctx sdk.Context, domain uint32, controller util.HexAddress, derived sdk.AccAddress, anys []*codectypes.Any) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if len(anys) == 0 || len(anys) > int(params.MaxMsgsPerPayload) {
		return errorsmod.Wrapf(types.ErrInvalidPayload, "payload must carry 1 to %d messages", params.MaxMsgsPerPayload)
	}
	whitelist := types.PayloadMsgTypeURLs()
	msgs := make([]sdk.Msg, 0, len(anys))
	for _, a := range anys {
		if !whitelist[a.TypeUrl] {
			return errorsmod.Wrap(types.ErrMsgNotWhitelisted, a.TypeUrl)
		}
		var msg sdk.Msg
		if err := k.cdc.UnpackAny(a, &msg); err != nil {
			return errorsmod.Wrap(types.ErrInvalidPayload, err.Error())
		}
		signers, _, err := k.cdc.GetMsgV1Signers(msg)
		if err != nil {
			return errorsmod.Wrap(types.ErrInvalidPayload, err.Error())
		}
		for _, s := range signers {
			if !sdk.AccAddress(s).Equals(derived) {
				return errorsmod.Wrapf(types.ErrInvalidSigner, "%s signed by %s", a.TypeUrl, sdk.AccAddress(s))
			}
		}
		if v, ok := msg.(sdk.HasValidateBasic); ok {
			if err := v.ValidateBasic(); err != nil {
				return errorsmod.Wrap(types.ErrInvalidPayload, err.Error())
			}
		}
		msgs = append(msgs, msg)
	}

	cacheCtx, write := ctx.CacheContext()
	// account record
	account, err := k.GetAccount(cacheCtx, derived.String())
	if err != nil {
		account = types.RemoteAccount{Domain: domain, Controller: controller, Address: derived.String(), CreatedHeight: ctx.BlockHeight()}
	}
	account.Payloads++
	if err := k.Accounts.Set(cacheCtx, derived.String(), account); err != nil {
		return err
	}
	// anti-spam fee per message (spec §14.4 item 7), sponsored like the gas
	if params.MsgFee.IsPositive() {
		fee := sdk.NewCoin(params.MsgFee.Denom, params.MsgFee.Amount.MulRaw(int64(len(msgs))))
		if err := k.chargeMsgFee(cacheCtx, derived, fee); err != nil {
			return err
		}
	}
	for _, msg := range msgs {
		handler := k.router.Handler(msg)
		if handler == nil {
			return errorsmod.Wrap(types.ErrMsgNotWhitelisted, sdk.MsgTypeURL(msg))
		}
		res, err := handler(cacheCtx, msg)
		if err != nil {
			return errorsmod.Wrapf(err, "%s", sdk.MsgTypeURL(msg))
		}
		// the message service router runs every handler on a fresh event
		// manager and returns its events only inside the result (SDK 0.53,
		// baseapp/msg_service_router.go); re-emit them like x/authz does, or
		// the Hyperlane dispatch events of a MsgWithdraw never reach the tx
		// and the agents (which index by events) never see the message
		cacheCtx.EventManager().EmitEvents(res.GetEvents())
	}
	write()
	return ctx.EventManager().EmitTypedEvent(&types.EventRemoteExecuted{Account: derived.String(), Domain: domain, Controller: controller.String(), Msgs: uint32(len(msgs))})
}

// chargeMsgFee pays the anti-spam fee of a payload to the fee collector. The
// paymaster pays when it holds the fee (spec §14.4 item 4: the protocol
// sponsors the local costs of remote users and recovers them through the
// trading fee; a remote account funded only with the settlement asset through
// a gateway holds no local denom); otherwise the remote account pays itself.
// The spam bound is the paymaster's float plus the origin gas and IGP quote
// every remote message already costs, not the fee alone.
func (k Keeper) chargeMsgFee(ctx sdk.Context, derived sdk.AccAddress, fee sdk.Coin) error {
	coins := sdk.NewCoins(fee)
	paymaster := types.PaymasterAddress()
	if k.bankKeeper.GetBalance(ctx, paymaster, fee.Denom).IsGTE(fee) {
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, paymaster, authtypes.FeeCollectorName, coins); err != nil {
			return errorsmod.Wrap(types.ErrFeeUnpaid, err.Error())
		}
		return nil
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, derived, authtypes.FeeCollectorName, coins); err != nil {
		return errorsmod.Wrapf(types.ErrFeeUnpaid, "paymaster and remote account cannot pay %s: %s", fee, err.Error())
	}
	return nil
}

func (k *Keeper) reject(ctx sdk.Context, domain uint32, controller util.HexAddress, account string, err error) error {
	k.Logger(ctx).Info("remote payload rejected", "domain", domain, "controller", controller.String(), "err", err)
	return ctx.EventManager().EmitTypedEvent(&types.EventRemoteRejected{Account: account, Domain: domain, Controller: controller.String(), Reason: err.Error()})
}
