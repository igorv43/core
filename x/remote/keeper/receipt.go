package keeper

import (
	"errors"
	"math/big"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// storeReceipt writes a receipt under the next seq of its account and
// indexes it by message id.
func (k Keeper) storeReceipt(ctx sdk.Context, r types.ConversionReceipt) error {
	seq, err := k.ReceiptSeq.Get(ctx, r.Account)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	seq++
	if err := k.ReceiptSeq.Set(ctx, r.Account, seq); err != nil {
		return err
	}
	if err := k.Receipts.Set(ctx, collections.Join(r.Account, seq), r); err != nil {
		return err
	}
	return k.ReceiptByMsg.Set(ctx, collections.Join(r.Account, r.MessageId.Bytes()), seq)
}

// recordReceipt stores a receipt and emits its event (spec §14.7.5).
func (k Keeper) recordReceipt(ctx sdk.Context, r types.ConversionReceipt) error {
	if err := k.storeReceipt(ctx, r); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventConversionReceiptCreated{
		Account: r.Account, MessageId: r.MessageId.String(), Direction: r.Direction.String(), TokenIn: r.TokenIn,
		AmountIn: r.AmountIn, UsdcAmount: r.UsdcAmount, EffectiveRate: r.EffectiveRate.String(), TokenOut: r.TokenOut,
		MinAccepted: r.MinAccepted, ExitAddress: r.ExitAddress.String(),
	})
}

// beUint reads a big-endian unsigned integer of up to 32 bytes.
func beUint(bz []byte) (math.Int, error) {
	if len(bz) > 32 {
		return math.Int{}, errorsmod.Wrap(types.ErrInvalidConversion, "amount longer than 32 bytes")
	}
	return math.NewIntFromBigInt(new(big.Int).SetBytes(bz)), nil
}

// depositReceipt builds the receipt of a converted deposit from the data the
// gateway measured (spec §14.7.3 item 2). effective_rate = usdc / amount_in,
// both in display units (USDC has 6 decimals).
func depositReceipt(ctx sdk.Context, account string, domain uint32, messageId util.HexAddress, c *types.ConversionData) (types.ConversionReceipt, error) {
	if c.TokenIn == "" || c.DecimalsIn > 36 {
		return types.ConversionReceipt{}, errorsmod.Wrap(types.ErrInvalidConversion, "token_in and decimals_in required")
	}
	amountIn, err := beUint(c.AmountIn)
	if err != nil {
		return types.ConversionReceipt{}, err
	}
	usdc, err := beUint(c.UsdcAmount)
	if err != nil {
		return types.ConversionReceipt{}, err
	}
	dexFee, err := beUint(c.DexFee)
	if err != nil {
		return types.ConversionReceipt{}, err
	}
	routeFee, err := beUint(c.RouteFee)
	if err != nil {
		return types.ConversionReceipt{}, err
	}
	minAccepted, err := beUint(c.MinAccepted)
	if err != nil {
		return types.ConversionReceipt{}, err
	}
	rate := math.LegacyZeroDec()
	if amountIn.IsPositive() {
		// usdc/1e6 ÷ amount_in/10^decimals = usdc · 10^decimals / (amount_in · 1e6)
		num := math.LegacyNewDecFromInt(usdc).MulInt(math.NewIntFromBigInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(c.DecimalsIn)), nil)))
		rate = num.QuoInt(amountIn).QuoInt64(1_000_000)
	}
	return types.ConversionReceipt{
		Account: account, OriginDomain: domain, OriginBlock: c.OriginBlock, MessageId: messageId, Direction: types.CONVERSION_DEPOSIT,
		TokenIn: c.TokenIn, AmountIn: amountIn.String(), UsdcAmount: usdc.String(), EffectiveRate: rate,
		DexFee: dexFee.String(), RouteFee: routeFee.String(), MinAccepted: minAccepted.String(), RegisteredHeight: ctx.BlockHeight(),
	}, nil
}

// applyUpdate confirms the outcome of a withdrawal exit reported by the
// enrolled gateway (spec §14.7.3 item 6). Unknown or already confirmed
// receipts are rejected; the update never moves value.
func (k Keeper) applyUpdate(ctx sdk.Context, account string, u *types.ConversionUpdate) error {
	if len(u.WithdrawMessageId) != 32 {
		return errorsmod.Wrap(types.ErrInvalidConversion, "withdraw_message_id must be 32 bytes")
	}
	seq, err := k.ReceiptByMsg.Get(ctx, collections.Join(account, u.WithdrawMessageId))
	if err != nil {
		return errorsmod.Wrap(types.ErrInvalidConversion, "no receipt for the withdrawal")
	}
	key := collections.Join(account, seq)
	r, err := k.Receipts.Get(ctx, key)
	if err != nil {
		return err
	}
	if r.Direction != types.CONVERSION_WITHDRAW || r.Confirmed {
		return errorsmod.Wrap(types.ErrInvalidConversion, "receipt is not an unconfirmed withdrawal")
	}
	amountOut, err := beUint(u.AmountOut)
	if err != nil {
		return err
	}
	r.AmountOut, r.FallbackToUsdc, r.Confirmed = amountOut.String(), u.FallbackToUsdc, true
	if u.OriginBlock > 0 {
		r.OriginBlock = u.OriginBlock
	}
	if err := k.Receipts.Set(ctx, key, r); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventConversionReceiptUpdated{
		Account: account, MessageId: r.MessageId.String(), AmountOut: r.AmountOut, FallbackToUsdc: r.FallbackToUsdc,
	})
}

// ReceiptsOf lists the receipts of an account, newest first (tests, export).
func (k Keeper) ReceiptsOf(ctx sdk.Context, account string) ([]types.ConversionReceipt, error) {
	out := []types.ConversionReceipt{}
	err := k.Receipts.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](account).Descending(),
		func(_ collections.Pair[string, uint64], r types.ConversionReceipt) (bool, error) {
			out = append(out, r)
			return false, nil
		})
	return out, err
}
