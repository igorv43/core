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
	derived := types.DeriveAddress(message.Origin, controller)
	if err := k.execute(ctx, message.Origin, controller, derived, payload.Msgs); err != nil {
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
	// anti-spam fee per message from the remote account (spec §14.4 item 7)
	if params.MsgFee.IsPositive() {
		fee := sdk.NewCoin(params.MsgFee.Denom, params.MsgFee.Amount.MulRaw(int64(len(msgs))))
		if err := k.bankKeeper.SendCoinsFromAccountToModule(cacheCtx, derived, authtypes.FeeCollectorName, sdk.NewCoins(fee)); err != nil {
			return errorsmod.Wrap(types.ErrFeeUnpaid, err.Error())
		}
	}
	for _, msg := range msgs {
		handler := k.router.Handler(msg)
		if handler == nil {
			return errorsmod.Wrap(types.ErrMsgNotWhitelisted, sdk.MsgTypeURL(msg))
		}
		if _, err := handler(cacheCtx, msg); err != nil {
			return errorsmod.Wrapf(err, "%s", sdk.MsgTypeURL(msg))
		}
	}
	write()
	return ctx.EventManager().EmitTypedEvent(&types.EventRemoteExecuted{Account: derived.String(), Domain: domain, Controller: controller.String(), Msgs: uint32(len(msgs))})
}

func (k *Keeper) reject(ctx sdk.Context, domain uint32, controller util.HexAddress, account string, err error) error {
	k.Logger(ctx).Info("remote payload rejected", "domain", domain, "controller", controller.String(), "err", err)
	return ctx.EventManager().EmitTypedEvent(&types.EventRemoteRejected{Account: account, Domain: domain, Controller: controller.String(), Reason: err.Error()})
}
