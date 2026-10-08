package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// RefloorPortExit lowers the minimum of a stuck port exit at the request of
// the account that withdrew (spec v0.9.13 §11.6.4).
//
// A default port exit carries min_accepted = net - ceil(net x
// port_exit_fee_tolerance_bps / 10,000) in its CREATE2 salt. If the vault
// chain's CCTP minimum fee rises above that allowance, FabricExit.execute
// reverts BelowMinimum and the USDC waits in the exit (a non-EVM controller
// has no fallback on the vault chain). Only the owner can accept less:
//
//   - the signer is the receipt's account (keyed by (account, message_id),
//     so nobody can name another account's withdrawal); a port account signs
//     it inside its gateway payload;
//   - the receipt is an unconfirmed withdrawal whose token_out is the
//     sentinel of the account's own port domain and whose origin is that
//     port's vault;
//   - new_min_accepted is strictly lower than the current minimum and at
//     least net - ceil(net x MaxPortExitFeeToleranceBpsAbsolute / 10,000):
//     the same 1 % bound governance can never exceed for the tolerance itself,
//     so a consent can never accept less than the protocol could impose;
//   - at most MaxPortExitRefloors consents per receipt (each is a control
//     message whose gas the paymaster advances).
//
// Nothing about where the funds go changes: the REFLOOR_EXIT order names the
// exit by its CREATE2 parameters (controller, port sentinel, salt minimum,
// withdrawal seq) and address, and the exit keeps paying the same controller.
// The receipt shows the new min_accepted; its confirmation path is unchanged.
func (k Keeper) RefloorPortExit(ctx sdk.Context, account string, messageId util.HexAddress, newMin math.Int) (util.HexAddress, uint64, error) {
	if newMin.IsNil() || newMin.IsNegative() {
		return util.HexAddress{}, 0, errorsmod.Wrap(types.ErrInvalidConversion, "new_min_accepted must be non-negative")
	}
	acc, err := k.GetAccount(ctx, account)
	if err != nil {
		return util.HexAddress{}, 0, err
	}
	port, err := k.Ports.Get(ctx, acc.Domain)
	if err != nil {
		return util.HexAddress{}, 0, errorsmod.Wrapf(types.ErrInvalidConversion, "account domain %d is not a port: no port exit to re-floor", acc.Domain)
	}
	seq, err := k.ReceiptByMsg.Get(ctx, collections.Join(account, messageId.Bytes()))
	if err != nil {
		return util.HexAddress{}, 0, errorsmod.Wrapf(types.ErrInvalidConversion, "no receipt of %s for withdrawal %s", account, messageId)
	}
	key := collections.Join(account, seq)
	r, err := k.Receipts.Get(ctx, key)
	if err != nil {
		return util.HexAddress{}, 0, err
	}
	sentinel := types.PortSentinel(acc.Domain)
	if r.Direction != types.CONVERSION_WITHDRAW || r.Confirmed {
		return util.HexAddress{}, 0, errorsmod.Wrap(types.ErrInvalidConversion, "receipt is not an unconfirmed withdrawal")
	}
	if r.TokenOut != sentinel.String() || r.OriginDomain != port.VaultDomain {
		return util.HexAddress{}, 0, errorsmod.Wrapf(types.ErrInvalidConversion, "receipt is not a port exit of domain %d through vault %d", acc.Domain, port.VaultDomain)
	}
	if r.Refloors >= types.MaxPortExitRefloors {
		return util.HexAddress{}, 0, errorsmod.Wrapf(types.ErrInvalidConversion, "the exit was already re-floored %d times (maximum)", r.Refloors)
	}
	net, ok := math.NewIntFromString(r.UsdcAmount)
	if !ok {
		return util.HexAddress{}, 0, errorsmod.Wrapf(types.ErrInvalidConversion, "receipt usdc_amount %q", r.UsdcAmount)
	}
	current, ok := math.NewIntFromString(r.MinAccepted)
	if !ok {
		return util.HexAddress{}, 0, errorsmod.Wrapf(types.ErrInvalidConversion, "receipt min_accepted %q", r.MinAccepted)
	}
	// the salt minimum and nonce identify the exit; receipts written before
	// v0.9.13 recorded neither, but they were never re-floored (min_accepted is
	// the salt minimum) and their seq is the withdrawal's
	saltMin := current
	if r.ExitMinAccepted != "" {
		if saltMin, ok = math.NewIntFromString(r.ExitMinAccepted); !ok {
			return util.HexAddress{}, 0, errorsmod.Wrapf(types.ErrInvalidConversion, "receipt exit_min_accepted %q", r.ExitMinAccepted)
		}
	} else if r.Refloors > 0 {
		return util.HexAddress{}, 0, errorsmod.Wrap(types.ErrInvalidConversion, "receipt has no exit_min_accepted")
	}
	nonce := r.ExitNonce
	if nonce == 0 {
		if nonce, err = k.withdrawalSeqOf(ctx, account, messageId); err != nil {
			return util.HexAddress{}, 0, err
		}
	}
	lowest := types.PortExitMinAccepted(net, types.MaxPortExitFeeToleranceBpsAbsolute)
	if !newMin.LT(current) {
		return util.HexAddress{}, 0, errorsmod.Wrapf(types.ErrInvalidConversion, "new_min_accepted %s must be lower than the current minimum %s", newMin, current)
	}
	if newMin.LT(lowest) {
		return util.HexAddress{}, 0, errorsmod.Wrapf(types.ErrInvalidConversion, "new_min_accepted %s is below net %s - ceil(net x %d / 10000) = %s",
			newMin, net, types.MaxPortExitFeeToleranceBpsAbsolute, lowest)
	}
	// the exit the order names must be the one the withdrawal paid
	gw, found, err := k.exitGateway(ctx, r.OriginDomain)
	if err != nil {
		return util.HexAddress{}, 0, err
	}
	if !found {
		return util.HexAddress{}, 0, errorsmod.Wrapf(types.ErrGatewayNotFound, "domain %d offers no withdrawal exits", r.OriginDomain)
	}
	exit := types.ExitAddress(gw.ExitFactory, gw.ExitInitCodeHash, acc.Controller, sentinel, saltMin, nonce)
	if !exit.Equal(r.ExitAddress) {
		return util.HexAddress{}, 0, errorsmod.Wrapf(types.ErrInvalidConversion, "exit %s of the registered factory differs from the receipt's %s", exit, r.ExitAddress)
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return util.HexAddress{}, 0, err
	}
	id, err := k.dispatchControl(ctx, params, r.OriginDomain, types.CONTROL_REFLOOR_EXIT,
		types.RefloorExitParams(acc.Controller, sentinel, saltMin, nonce, newMin, exit),
		fmt.Sprintf("exit=%s min=%s->%s", exit, current, newMin))
	if err != nil {
		return util.HexAddress{}, 0, err
	}
	ex, err := k.Executors.Get(ctx, r.OriginDomain)
	if err != nil {
		return util.HexAddress{}, 0, err
	}
	r.MinAccepted = newMin.String()
	r.ExitMinAccepted = saltMin.String()
	r.ExitNonce = nonce
	r.Refloors++
	if err := k.Receipts.Set(ctx, key, r); err != nil {
		return util.HexAddress{}, 0, err
	}
	return id, ex.Nonce, ctx.EventManager().EmitTypedEvent(&types.EventPortExitRefloored{
		Account: account, MessageId: r.MessageId.String(), ExitAddress: exit.String(), VaultDomain: r.OriginDomain,
		UsdcAmount: net.String(), PreviousMinAccepted: current.String(), NewMinAccepted: newMin.String(),
		LowestAllowed: lowest.String(), Refloors: r.Refloors, ControlMessageId: id.String(), Nonce: ex.Nonce,
	})
}

// withdrawalSeqOf finds the withdrawal sequence of a message id among the
// recorded withdrawals of an account (receipts written before v0.9.13).
func (k Keeper) withdrawalSeqOf(ctx sdk.Context, account string, messageId util.HexAddress) (uint64, error) {
	var seq uint64
	err := k.Withdrawals.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](account),
		func(_ collections.Pair[string, uint64], w types.Withdrawal) (bool, error) {
			if w.MessageId.Equal(messageId) {
				seq = w.Seq
				return true, nil
			}
			return false, nil
		})
	if err != nil {
		return 0, err
	}
	if seq == 0 {
		return 0, errorsmod.Wrapf(types.ErrInvalidConversion, "withdrawal %s is no longer recorded", messageId)
	}
	return seq, nil
}
