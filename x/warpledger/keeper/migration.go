package keeper

import (
	"strconv"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/classic-terra/core/v4/x/warpledger/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/gogoproto/proto"
)

// SweepResult is the outcome of a migration sweep.
type SweepResult struct {
	MessageId      util.HexAddress
	Amount         math.Int
	Fee            sdk.Coin
	DepositAddress sdk.AccAddress
}

// QuoteSweepFee returns the interchain gas fee the native route charges for a
// transfer of the token to the domain, as quoted by x/warp.
func (k Keeper) QuoteSweepFee(ctx sdk.Context, token warptypes.HypToken, domain uint32) (sdk.Coin, error) {
	qs := warpkeeper.NewQueryServerImpl(*k.warpKeeper)
	quote, err := qs.QuoteRemoteTransfer(ctx, &warptypes.QueryQuoteRemoteTransferRequest{
		Id:                token.Id.String(),
		DestinationDomain: strconv.FormatUint(uint64(domain), 10),
	})
	if err != nil {
		return sdk.Coin{}, err
	}

	switch len(quote.GasPayment) {
	case 0:
		return sdk.NewCoin(token.OriginDenom, math.ZeroInt()), nil
	case 1:
		return quote.GasPayment[0], nil
	default:
		return sdk.Coin{}, errorsmod.Wrapf(types.ErrInvalidFeeQuote, "multi-coin quote %s", quote.GasPayment)
	}
}

// SweepMigration forwards the balance of the derived deposit address for
// (token, domain, recipient) through the native warp route (spec §10.2).
// The interchain gas fee is paid from the swept balance itself. The transfer
// is executed through the message router, i.e. through the wrapped warp
// MsgServer, so domain caps and ledger accounting apply exactly as for a
// user transfer. It is permissionless: the effect never depends on who
// submits it.
func (k Keeper) SweepMigration(ctx sdk.Context, tokenId util.HexAddress, domain uint32, recipient util.HexAddress) (SweepResult, error) {
	allowed, err := k.circuitKeeper.IsAllowed(ctx, RemoteTransferMsgURL)
	if err != nil {
		return SweepResult{}, err
	}
	if !allowed {
		return SweepResult{}, types.ErrTransfersPaused
	}

	token, err := k.GetToken(ctx, tokenId)
	if err != nil {
		return SweepResult{}, err
	}
	if token.TokenType != warptypes.HYP_TOKEN_TYPE_COLLATERAL {
		return SweepResult{}, errorsmod.Wrapf(types.ErrNotCollateralToken, "%s", tokenId.String())
	}

	depositAddr := types.DeriveDepositAddress(tokenId, domain, recipient)
	balance := k.bankKeeper.GetBalance(ctx, depositAddr, token.OriginDenom).Amount
	if !balance.IsPositive() {
		return SweepResult{}, errorsmod.Wrapf(types.ErrNothingToSweep, "%s has no %s", depositAddr.String(), token.OriginDenom)
	}

	fee, err := k.QuoteSweepFee(ctx, token, domain)
	if err != nil {
		return SweepResult{}, err
	}

	amount := balance
	if fee.Denom == token.OriginDenom {
		amount = balance.Sub(fee.Amount)
		if !amount.IsPositive() {
			return SweepResult{}, errorsmod.Wrapf(types.ErrInsufficientForFee, "balance %s, fee %s", balance, fee)
		}
	} else if fee.Amount.IsPositive() {
		feeBalance := k.bankKeeper.GetBalance(ctx, depositAddr, fee.Denom).Amount
		if feeBalance.LT(fee.Amount) {
			return SweepResult{}, errorsmod.Wrapf(types.ErrInsufficientForFee, "fee balance %s%s, fee %s", feeBalance, fee.Denom, fee)
		}
	}

	msg := &warptypes.MsgRemoteTransfer{
		Sender:            depositAddr.String(),
		TokenId:           tokenId,
		DestinationDomain: domain,
		Recipient:         recipient,
		Amount:            amount,
		GasLimit:          math.ZeroInt(), // use the enrolled router gas
		MaxFee:            fee,
	}

	handler := k.router.Handler(msg)
	if handler == nil {
		return SweepResult{}, errorsmod.Wrap(sdkerrors.ErrUnknownRequest, "no handler for MsgRemoteTransfer")
	}
	res, err := handler(ctx, msg)
	if err != nil {
		return SweepResult{}, err
	}
	ctx.EventManager().EmitEvents(res.GetEvents())

	var resp warptypes.MsgRemoteTransferResponse
	if len(res.MsgResponses) > 0 && res.MsgResponses[0] != nil {
		if err := proto.Unmarshal(res.MsgResponses[0].Value, &resp); err != nil {
			return SweepResult{}, err
		}
	}

	result := SweepResult{
		MessageId:      resp.MessageId,
		Amount:         amount,
		Fee:            fee,
		DepositAddress: depositAddr,
	}

	if err := ctx.EventManager().EmitTypedEvent(&types.EventMigrationSwept{
		TokenId:        tokenId.String(),
		Domain:         domain,
		Recipient:      recipient.String(),
		DepositAddress: depositAddr.String(),
		Amount:         amount.String(),
		Fee:            fee.String(),
		MessageId:      resp.MessageId.String(),
	}); err != nil {
		return SweepResult{}, err
	}

	return result, nil
}
