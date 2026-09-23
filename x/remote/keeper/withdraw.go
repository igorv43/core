package keeper

import (
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Withdraw sends tokens of the remote account back through warp to its
// controller (spec §14.4 item 2: the destination is locked to the
// (domain, controller) pair; no parameter can relax it). The interchain
// gas is covered by the paymaster up to withdraw_fee_cap.
//
// With token_out set (spec §14.7.2, D-30) the transfer goes to the
// deterministic exit contract of (controller, token_out, min_accepted, seq)
// on the origin, whose only beneficiary is the controller, so the custody
// rule holds; a conversion receipt records the request.
func (k Keeper) Withdraw(ctx sdk.Context, controller string, tokenId util.HexAddress, amount math.Int, tokenOut string, minAccepted *math.Int) (util.HexAddress, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return util.HexAddress{}, err
	}
	account, err := k.GetAccount(ctx, controller)
	if err != nil {
		return util.HexAddress{}, err
	}
	sender := sdk.MustAccAddressFromBech32(controller)
	seq, err := k.WithdrawSeq.Get(ctx, controller)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return util.HexAddress{}, err
	}
	seq++
	recipient := account.Controller
	var exit util.HexAddress
	var tokenOutHex util.HexAddress
	if tokenOut != "" {
		if minAccepted == nil || minAccepted.IsNil() || minAccepted.IsNegative() {
			return util.HexAddress{}, errorsmod.Wrap(types.ErrInvalidWithdraw, "min_accepted must be set with token_out")
		}
		tokenOutHex, err = util.DecodeHexAddress(tokenOut)
		if err != nil {
			return util.HexAddress{}, errorsmod.Wrap(types.ErrInvalidWithdraw, err.Error())
		}
		gw, ok, err := k.exitGateway(ctx, account.Domain)
		if err != nil {
			return util.HexAddress{}, err
		}
		if !ok {
			return util.HexAddress{}, errorsmod.Wrapf(types.ErrGatewayNotFound, "domain %d offers no withdrawal exits", account.Domain)
		}
		exit = types.ExitAddress(gw.ExitFactory, gw.ExitInitCodeHash, account.Controller, tokenOutHex, *minAccepted, seq)
		recipient = exit
	}
	// gas for the interchain message from the paymaster, when it has it
	if params.WithdrawFeeCap.IsPositive() {
		pm := types.PaymasterAddress()
		if k.bankKeeper.GetBalance(ctx, pm, params.WithdrawFeeCap.Denom).IsGTE(params.WithdrawFeeCap) {
			if err := k.bankKeeper.SendCoins(ctx, pm, sender, sdk.NewCoins(params.WithdrawFeeCap)); err != nil {
				return util.HexAddress{}, err
			}
		}
	}
	msg := &warptypes.MsgRemoteTransfer{
		Sender: controller, TokenId: tokenId, DestinationDomain: account.Domain, Recipient: recipient, Amount: amount,
		GasLimit: math.ZeroInt(), MaxFee: params.WithdrawFeeCap,
	}
	handler := k.router.Handler(msg)
	if handler == nil {
		return util.HexAddress{}, errorsmod.Wrap(types.ErrInvalidWithdraw, "warp transfer handler not found")
	}
	res, err := handler(ctx, msg)
	if err != nil {
		return util.HexAddress{}, err
	}
	var out warptypes.MsgRemoteTransferResponse
	if len(res.MsgResponses) == 0 || k.cdc.Unmarshal(res.MsgResponses[0].Value, &out) != nil {
		return util.HexAddress{}, errorsmod.Wrap(types.ErrInvalidWithdraw, "warp transfer returned no message id")
	}
	if err := k.WithdrawSeq.Set(ctx, controller, seq); err != nil {
		return util.HexAddress{}, err
	}
	if err := k.Withdrawals.Set(ctx, collections.Join(controller, seq), types.Withdrawal{
		Account: controller, Seq: seq, MessageId: out.MessageId, TokenId: tokenId, Amount: amount, Height: ctx.BlockHeight(),
	}); err != nil {
		return util.HexAddress{}, err
	}
	if seq > types.MaxWithdrawalsKept {
		_ = k.Withdrawals.Remove(ctx, collections.Join(controller, seq-types.MaxWithdrawalsKept))
	}
	if tokenOut != "" {
		token, err := k.warpToken(ctx, tokenId)
		if err != nil {
			return util.HexAddress{}, err
		}
		if err := k.recordReceipt(ctx, types.ConversionReceipt{
			Account: controller, OriginDomain: account.Domain, MessageId: out.MessageId, Direction: types.CONVERSION_WITHDRAW,
			TokenIn: token, AmountIn: amount.String(), UsdcAmount: amount.String(), EffectiveRate: math.LegacyZeroDec(),
			DexFee: "0", RouteFee: "0", MinAccepted: minAccepted.String(), TokenOut: tokenOutHex.String(), ExitAddress: exit,
			RegisteredHeight: ctx.BlockHeight(),
		}); err != nil {
			return util.HexAddress{}, err
		}
	}
	return out.MessageId, ctx.EventManager().EmitTypedEvent(&types.EventRemoteWithdraw{
		Account: controller, TokenId: tokenId.String(), Amount: amount.String(), Domain: account.Domain,
		Recipient: recipient.String(), MessageId: out.MessageId.String(),
	})
}

// warpToken returns the origin denom of a warp token, for receipts.
func (k Keeper) warpToken(ctx sdk.Context, tokenId util.HexAddress) (string, error) {
	if k.ledgerKeeper == nil {
		return tokenId.String(), nil
	}
	token, err := k.ledgerKeeper.GetToken(ctx, tokenId)
	if err != nil {
		return "", errorsmod.Wrap(types.ErrInvalidWithdraw, "warp token not found")
	}
	return token.OriginDenom, nil
}

// WithdrawalsOf lists the recorded withdrawals of an account, newest first.
func (k Keeper) WithdrawalsOf(ctx sdk.Context, controller string) ([]types.Withdrawal, error) {
	out := []types.Withdrawal{}
	err := k.Withdrawals.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](controller).Descending(),
		func(_ collections.Pair[string, uint64], w types.Withdrawal) (bool, error) {
			out = append(out, w)
			return false, nil
		})
	return out, err
}
