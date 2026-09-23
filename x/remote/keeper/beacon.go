package keeper

import (
	"crypto/sha256"
	"errors"
	"math/big"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// State beacons (spec §14.6 item 3, D-27): facts attested by the chain
// transmitted through Hyperlane to remote contracts on a fixed period. The
// body is a sequence of 32-byte big-endian words so EVM contracts decode it
// with abi.decode(body, (uint256[])):
//
//	word 0  kind            word 1  height          word 2  unix time
//	EXCHANGE_RATE:   3 rate·1e18   4 stLUNC supply   5 uluna assets
//	ROUTE_SOLVENCY:  3 token id    4 domain          5 sent   6 received   7 cap   8 collateral balance   9 solvent (1/0)
//	POSITION_DIGEST: 3 sha256 of the positions   4 free collateral   5 positions   6 Σ equity
//
// The interchain gas is paid by the paymaster up to beacon_fee_cap.

func word(v *big.Int) []byte {
	out := make([]byte, 32)
	if v == nil || v.Sign() < 0 {
		return out
	}
	v.FillBytes(out)
	return out
}

func wordInt(i math.Int) []byte {
	if i.IsNil() {
		return make([]byte, 32)
	}
	return word(i.BigInt())
}

func wordDec(d math.LegacyDec) []byte {
	if d.IsNil() {
		return make([]byte, 32)
	}
	return word(d.BigInt()) // 18-decimal scale
}

// BeaconBody builds the body a beacon would send now.
func (k Keeper) BeaconBody(ctx sdk.Context, b types.Beacon) ([]byte, error) {
	body := make([]byte, 0, 32*10)
	body = append(body, word(big.NewInt(int64(b.Kind)))...)
	body = append(body, word(big.NewInt(ctx.BlockHeight()))...)
	body = append(body, word(big.NewInt(ctx.BlockTime().Unix()))...)
	switch b.Kind {
	case types.BEACON_KIND_EXCHANGE_RATE:
		if k.lsKeeper == nil {
			return nil, errorsmod.Wrap(types.ErrInvalidParams, "liquid staking not available")
		}
		rate, totals, err := k.lsKeeper.ExchangeRate(ctx)
		if err != nil {
			return nil, err
		}
		body = append(body, wordDec(rate)...)
		body = append(body, wordInt(totals.StSupply)...)
		body = append(body, wordInt(totals.Assets())...)
	case types.BEACON_KIND_ROUTE_SOLVENCY:
		if k.ledgerKeeper == nil {
			return nil, errorsmod.Wrap(types.ErrInvalidParams, "warp ledger not available")
		}
		token, err := k.ledgerKeeper.GetToken(ctx, b.TokenId)
		if err != nil {
			return nil, err
		}
		ledger, _, err := k.ledgerKeeper.GetLedger(ctx, b.TokenId, b.Domain)
		if err != nil {
			return nil, err
		}
		cap, err := k.ledgerKeeper.EffectiveCap(ctx, ledger)
		if err != nil {
			return nil, err
		}
		balance := k.bankKeeper.GetBalance(ctx, k.warpAddress, token.OriginDenom).Amount
		exposure, err := k.ledgerKeeper.TotalExposure(ctx, b.TokenId)
		if err != nil {
			return nil, err
		}
		solvent := big.NewInt(0)
		if balance.GTE(exposure) {
			solvent = big.NewInt(1)
		}
		body = append(body, b.TokenId.Bytes()...)
		body = append(body, word(big.NewInt(int64(b.Domain)))...)
		body = append(body, wordInt(ledger.Sent)...)
		body = append(body, wordInt(ledger.Received)...)
		body = append(body, wordInt(cap)...)
		body = append(body, wordInt(balance)...)
		body = append(body, word(solvent)...)
	case types.BEACON_KIND_POSITION_DIGEST:
		if k.perpKeeper == nil {
			return nil, errorsmod.Wrap(types.ErrInvalidParams, "perp not available")
		}
		digest, free, n, equity, err := k.perpKeeper.PositionDigest(ctx, b.Account)
		if err != nil {
			return nil, err
		}
		body = append(body, digest[:]...)
		body = append(body, wordInt(free)...)
		body = append(body, word(big.NewInt(int64(n)))...)
		body = append(body, wordInt(equity)...)
	default:
		return nil, errorsmod.Wrap(types.ErrInvalidParams, "unknown beacon kind")
	}
	return body, nil
}

// SetBeacon creates or updates a beacon (governance); interval zero removes it.
func (k Keeper) SetBeacon(ctx sdk.Context, msg *types.MsgSetBeacon) (uint64, error) {
	if msg.IntervalBlocks == 0 {
		if msg.Id == 0 {
			return 0, errorsmod.Wrap(types.ErrInvalidParams, "interval_blocks = 0 removes a beacon: id required")
		}
		return msg.Id, k.Beacons.Remove(ctx, msg.Id)
	}
	if _, err := k.GetApp(ctx, msg.AppId); err != nil {
		return 0, err
	}
	b := types.Beacon{Id: msg.Id, AppId: msg.AppId, Domain: msg.Domain, Recipient: msg.Recipient, Kind: msg.Kind,
		IntervalBlocks: msg.IntervalBlocks, TokenId: msg.TokenId, Account: msg.Account}
	if _, err := k.BeaconBody(ctx, b); err != nil {
		return 0, errorsmod.Wrap(types.ErrInvalidParams, err.Error())
	}
	if b.Id == 0 {
		id, err := k.BeaconSeq.Next(ctx)
		if err != nil {
			return 0, err
		}
		b.Id = id + 1
	} else if old, err := k.Beacons.Get(ctx, b.Id); err == nil {
		b.LastHeight = old.LastHeight
	}
	return b.Id, k.Beacons.Set(ctx, b.Id, b)
}

// EndBlocker dispatches the beacons that are due, bounded per block; a
// dispatch the paymaster cannot fund is skipped and retried next block.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	var due []types.Beacon
	if err := k.Beacons.Walk(ctx, nil, func(_ uint64, b types.Beacon) (bool, error) {
		if ctx.BlockHeight()-b.LastHeight >= b.IntervalBlocks {
			due = append(due, b)
		}
		return uint32(len(due)) >= params.MaxBeaconsPerBlock, nil
	}); err != nil {
		return err
	}
	for _, b := range due {
		if err := k.sendBeacon(ctx, params, b); err != nil {
			k.Logger(ctx).Error("beacon not sent", "id", b.Id, "err", err)
			continue
		}
	}
	return nil
}

func (k Keeper) sendBeacon(ctx sdk.Context, params types.Params, b types.Beacon) error {
	app, err := k.GetApp(ctx, b.AppId)
	if err != nil {
		return err
	}
	body, err := k.BeaconBody(ctx, b)
	if err != nil {
		return err
	}
	paymaster := types.PaymasterAddress()
	maxFee := sdk.NewCoins()
	if params.BeaconFeeCap.IsPositive() {
		if !k.bankKeeper.GetBalance(ctx, paymaster, params.BeaconFeeCap.Denom).IsGTE(params.BeaconFeeCap) {
			return errors.New("paymaster cannot fund the interchain gas")
		}
		maxFee = sdk.NewCoins(params.BeaconFeeCap)
	}
	cacheCtx, write := ctx.CacheContext()
	id, err := k.coreKeeper.DispatchMessage(cacheCtx, app.MailboxId, app.Id, maxFee, b.Domain, b.Recipient, body,
		util.StandardHookMetadata{Address: paymaster, GasLimit: math.ZeroInt()}, nil)
	if err != nil {
		return err
	}
	if k.roots != nil {
		if err := k.roots.RecordRoot(cacheCtx, app.MailboxId); err != nil {
			return err
		}
	}
	write()
	b.LastHeight = ctx.BlockHeight()
	if err := k.Beacons.Set(ctx, b.Id, b); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventBeaconSent{BeaconId: b.Id, Domain: b.Domain, Recipient: b.Recipient.String(), Kind: b.Kind.String(), MessageId: id.String()})
}

// AllBeacons lists the beacons.
func (k Keeper) AllBeacons(ctx sdk.Context) ([]types.Beacon, error) {
	out := []types.Beacon{}
	err := k.Beacons.Walk(ctx, nil, func(_ uint64, b types.Beacon) (bool, error) {
		out = append(out, b)
		return false, nil
	})
	return out, err
}

// PositionDigestOf is exported for tests: sha256 over the canonical bytes.
func PositionDigestOf(parts [][]byte) [32]byte {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
