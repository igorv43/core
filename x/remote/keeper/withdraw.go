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
	// dynamic withdrawal fee of the vault (spec §11.5.4): burned, so the
	// vault keeps the collateral as an unallocated reserve
	// a token with a route to the account's domain withdraws there, port or
	// not; on a port domain the settlement asset (no route to a port) goes to
	// the port's vault (spec §11.6.2, v0.9.11). A port user who names no
	// token_out withdraws it by CCTP from the vault to their own address; that
	// exit's min_accepted is in USDC units and is set below to the net
	// settlement amount (after the withdrawal fee) less the CCTP fee allowance
	destination, viaVault, err := k.destinationOf(ctx, account, tokenId)
	if err != nil {
		return util.HexAddress{}, err
	}
	portDefault := false
	if viaVault && tokenOut == "" {
		tokenOut = types.PortSentinel(account.Domain).String()
		portDefault = true
	}
	if ex, err := k.Executors.Get(ctx, destination); err == nil && ex.WithdrawFeeBps > 0 {
		fee := withdrawFee(amount, ex.WithdrawFeeBps)
		if fee.IsPositive() {
			denom, err := k.warpToken(ctx, tokenId)
			if err != nil {
				return util.HexAddress{}, err
			}
			coins := sdk.NewCoins(sdk.NewCoin(denom, fee))
			if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sender, types.ModuleName, coins); err != nil {
				return util.HexAddress{}, err
			}
			if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
				return util.HexAddress{}, err
			}
			// the fee only reduces the settlement amount sent to the exit;
			// a caller's min_accepted is in token_out units (e.g. wei for the
			// native sentinel), so it is never compared with this USDC amount:
			// the exit contract enforces it on the origin
			amount = amount.Sub(fee)
		}
	}
	if portDefault {
		// the floor leaves room for the vault chain's CCTP minimum fee within
		// port_exit_fee_tolerance_bps (spec §11.6, v0.9.12): the exit pays the
		// net minus the issuer's fee, never less than this floor
		m := types.PortExitMinAccepted(amount, params.PortExitFeeToleranceBps)
		minAccepted = &m
	}
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
		gw, ok, err := k.exitGateway(ctx, destination)
		if err != nil {
			return util.HexAddress{}, err
		}
		if !ok {
			return util.HexAddress{}, errorsmod.Wrapf(types.ErrGatewayNotFound, "domain %d offers no withdrawal exits", destination)
		}
		exit = types.ExitAddress(gw.ExitFactory, gw.ExitInitCodeHash, account.Controller, tokenOutHex, *minAccepted, seq)
		recipient = exit
	}
	// the interchain gas is the IGP quote of the route, never more: the
	// paymaster advances exactly that amount (capped by withdraw_fee_cap)
	// so nothing is stranded on the remote account
	interchainFee, err := k.quoteInterchainFee(ctx, tokenId, destination, params.WithdrawFeeCap)
	if err != nil {
		return util.HexAddress{}, err
	}
	// max_fee is the bound the hooks may charge, never the amount moved: the
	// IGP charges exactly its quote. A zero quote keeps the cap as the bound
	// because warp drops a zero coin (sdk.NewCoins) and an IGP hook refuses an
	// empty max_fee even when it charges nothing.
	maxFee := params.WithdrawFeeCap
	if interchainFee.IsPositive() {
		maxFee = interchainFee
		pm := types.PaymasterAddress()
		if k.bankKeeper.GetBalance(ctx, pm, interchainFee.Denom).IsGTE(interchainFee) {
			if err := k.bankKeeper.SendCoins(ctx, pm, sender, sdk.NewCoins(interchainFee)); err != nil {
				return util.HexAddress{}, err
			}
		}
	}
	msg := &warptypes.MsgRemoteTransfer{
		Sender: controller, TokenId: tokenId, DestinationDomain: destination, Recipient: recipient, Amount: amount,
		GasLimit: math.ZeroInt(), MaxFee: maxFee,
	}
	handler := k.router.Handler(msg)
	if handler == nil {
		return util.HexAddress{}, errorsmod.Wrap(types.ErrInvalidWithdraw, "warp transfer handler not found")
	}
	res, err := handler(ctx, msg)
	if err != nil {
		return util.HexAddress{}, err
	}
	// the router returns the handler's events inside the result (SDK 0.53):
	// re-emit the dispatch, tree insertion and gas payment events so the
	// Hyperlane agents can index the outbound message
	ctx.EventManager().EmitEvents(res.GetEvents())
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
			Account: controller, OriginDomain: destination, MessageId: out.MessageId, Direction: types.CONVERSION_WITHDRAW,
			TokenIn: token, AmountIn: amount.String(), UsdcAmount: amount.String(), EffectiveRate: math.LegacyZeroDec(),
			DexFee: "0", RouteFee: "0", MinAccepted: minAccepted.String(), TokenOut: tokenOutHex.String(), ExitAddress: exit,
			RegisteredHeight: ctx.BlockHeight(),
		}); err != nil {
			return util.HexAddress{}, err
		}
	}
	return out.MessageId, ctx.EventManager().EmitTypedEvent(&types.EventRemoteWithdraw{
		Account: controller, TokenId: tokenId.String(), Amount: amount.String(), Domain: destination,
		Recipient: recipient.String(), MessageId: out.MessageId.String(),
	})
}

// quoteInterchainFee quotes the interchain gas of a warp transfer of the token
// to the destination (the mailbox hooks with the enrolled router's gas, as
// `warp QuoteRemoteTransfer` does). The result is a coin in the cap's denom:
// zero when the hooks charge nothing, an error when the quote is in another
// denom or above the cap (the paymaster never advances more than the cap).
func (k Keeper) quoteInterchainFee(ctx sdk.Context, tokenId util.HexAddress, destination uint32, cap sdk.Coin) (sdk.Coin, error) {
	zero := sdk.NewCoin(cap.Denom, math.ZeroInt())
	if k.ledgerKeeper == nil {
		return zero, errorsmod.Wrap(types.ErrInvalidWithdraw, "warp ledger not available to quote the interchain fee")
	}
	token, err := k.ledgerKeeper.GetToken(ctx, tokenId)
	if err != nil {
		return zero, err
	}
	router, err := k.ledgerKeeper.GetRemoteRouter(ctx, tokenId, destination)
	if err != nil {
		return zero, errorsmod.Wrapf(types.ErrInvalidWithdraw, "no remote router for domain %d: %s", destination, err.Error())
	}
	quote, err := k.coreKeeper.QuoteDispatch(ctx, token.OriginMailbox, util.NewZeroAddress(),
		util.StandardHookMetadata{GasLimit: router.Gas, Address: sdk.AccAddress{}}, util.HyperlaneMessage{Destination: destination})
	if err != nil {
		return zero, errorsmod.Wrap(types.ErrInvalidWithdraw, err.Error())
	}
	if quote.IsZero() {
		return zero, nil
	}
	if len(quote) != 1 || quote[0].Denom != cap.Denom {
		return zero, errorsmod.Wrapf(types.ErrInvalidWithdraw, "interchain fee %s is not payable in %s", quote, cap.Denom)
	}
	if quote[0].Amount.GT(cap.Amount) {
		return zero, errorsmod.Wrapf(types.ErrInvalidWithdraw, "interchain fee %s exceeds withdraw_fee_cap %s", quote[0], cap)
	}
	return quote[0], nil
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

// withdrawFee is the vault withdrawal fee amount x bps / 10,000 rounded up: a fee
// is never rounded in the user's favour. bps comes from the dynamic fee band
// (< 10,000), so the fee never exceeds amount.
func withdrawFee(amount math.Int, bps uint32) math.Int {
	return amount.MulRaw(int64(bps)).AddRaw(9_999).QuoRaw(10_000)
}
