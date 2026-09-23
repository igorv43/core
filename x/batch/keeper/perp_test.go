package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/classic-terra/core/v4/x/batch/keeper"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

const perpMarketID = "ubtc-perp/uusd"

// fakeHook is a margin hook for tests: a reservation ledger kept in the
// batch KV store (so that cache contexts roll it back like x/perp's state),
// the fills applied and the accounts whose fills fail (no margin at p*).
type fakeHook struct {
	key       *storetypes.KVStoreKey
	fills     []types.PerpFill
	failing   map[string]bool
	rejectAll bool
}

var fakeReservePrefix = []byte{0xF0}

func newFakeHook(key *storetypes.KVStoreKey) *fakeHook {
	return &fakeHook{key: key, failing: map[string]bool{}}
}

func (h *fakeHook) amount(qty math.Int, price math.LegacyDec) math.Int {
	return math.LegacyNewDecFromInt(qty).Mul(price).TruncateInt()
}

func (h *fakeHook) reserved(ctx sdk.Context, account string) math.Int {
	bz := ctx.KVStore(h.key).Get(append(fakeReservePrefix, []byte(account)...))
	if bz == nil {
		return math.ZeroInt()
	}
	v, _ := math.NewIntFromString(string(bz))
	return v
}

func (h *fakeHook) set(ctx sdk.Context, account string, v math.Int) {
	ctx.KVStore(h.key).Set(append(fakeReservePrefix, []byte(account)...), []byte(v.String()))
}

func (h *fakeHook) Reserve(ctx sdk.Context, account sdk.AccAddress, _ types.Market, _ types.Side, qty math.Int, price math.LegacyDec) error {
	if h.rejectAll {
		return fmt.Errorf("reservation refused")
	}
	h.set(ctx, account.String(), h.reserved(ctx, account.String()).Add(h.amount(qty, price)))
	return nil
}

func (h *fakeHook) Release(ctx sdk.Context, account sdk.AccAddress, _ types.Market, _ types.Side, qty math.Int, price math.LegacyDec) error {
	next := h.reserved(ctx, account.String()).Sub(h.amount(qty, price))
	if next.IsNegative() {
		return fmt.Errorf("release below zero for %s", account)
	}
	h.set(ctx, account.String(), next)
	return nil
}

func (h *fakeHook) Fill(_ sdk.Context, f types.PerpFill) error {
	if h.failing[f.Account.String()] {
		return fmt.Errorf("no margin")
	}
	h.fills = append(h.fills, f)
	return nil
}

func (f *fixture) perpSetup(t *testing.T) *fakeHook {
	t.Helper()
	hook := newFakeHook(f.app.GetKey(types.StoreKey))
	f.k.SetMarginHook(hook)
	require.NoError(t, f.k.CreateMarket(f.ctx, types.Market{
		Id: perpMarketID, BaseDenom: "ubtc-perp", QuoteDenom: "uusd", Type: types.MARKET_TYPE_PERP, OracleDenom: "uusd",
		Enabled: false, MinQty: math.NewInt(1_000), TickSize: math.LegacyNewDecWithPrec(1, 6),
	}))
	// the fixture allows two active markets; the spot one is already enabled
	require.NoError(t, f.k.SetMarketEnabled(f.ctx, perpMarketID, true))
	return hook
}

func (f *fixture) submitPerp(t *testing.T, sender sdk.AccAddress, side types.Side, qty int64, limit math.LegacyDec, reduceOnly bool) (uint64, uint64) {
	t.Helper()
	id, batch, err := f.k.SubmitPerpIntent(f.ctx, keeper.PerpOrder{
		Sender: sender.String(), MarketID: perpMarketID, Side: side, Qty: math.NewInt(qty), LimitPrice: limit,
		ExpiryHeight: f.ctx.BlockHeight() + 100, ReduceOnly: reduceOnly, ChargeFee: true,
	})
	require.NoError(t, err)
	return id, batch
}

func TestPerpIntentReservesAndReleases(t *testing.T) {
	f := setup(t)
	hook := f.perpSetup(t)
	pref := math.LegacyNewDecWithPrec(1, 4)

	id, _ := f.submitPerp(t, f.user, types.SIDE_BUY, 10_000, pref, false)
	require.Equal(t, "1", hook.reserved(f.ctx, f.user.String()).String()) // 10_000 × 0.0001
	in, err := f.k.GetIntent(f.ctx, id)
	require.NoError(t, err)
	require.True(t, in.AmountIn.IsZero())
	require.Equal(t, "10000", in.Remaining.String())

	// no escrow left the account
	require.Equal(t, "100000000", f.balance(f.user, "uusd").String())

	_, err = f.k.CancelIntent(f.ctx, f.user.String(), id)
	require.NoError(t, err)
	require.True(t, hook.reserved(f.ctx, f.user.String()).IsZero())

	// reduce-only reserves nothing
	id, _ = f.submitPerp(t, f.user, types.SIDE_SELL, 10_000, pref, true)
	require.True(t, hook.reserved(f.ctx, f.user.String()).IsZero())
	_, err = f.k.CancelIntent(f.ctx, f.user.String(), id)
	require.NoError(t, err)

	// reservation refused → no intent
	hook.rejectAll = true
	_, _, err = f.k.SubmitPerpIntent(f.ctx, keeper.PerpOrder{
		Sender: f.user.String(), MarketID: perpMarketID, Side: types.SIDE_BUY,
		Qty: math.NewInt(10_000), LimitPrice: pref, ExpiryHeight: f.ctx.BlockHeight() + 10, ChargeFee: true,
	})
	require.Error(t, err)
}

func TestPerpBatchFillsThroughHook(t *testing.T) {
	f := setup(t)
	hook := f.perpSetup(t)
	f.registerSolver(t)
	pref := math.LegacyNewDecWithPrec(1, 4)

	f.at(10)
	id, batch := f.submitPerp(t, f.user, types.SIDE_BUY, 10_000, pref, false)
	// solver level: reserved at reveal through the hook, not escrow
	bid := types.Bid{MarketId: perpMarketID, Levels: []types.Level{{Side: types.SIDE_SELL, Price: pref, Qty: math.NewInt(10_000)}}}
	salt := []byte("s")
	commitment, err := types.Commitment(bid, salt, f.solver.String(), batch)
	require.NoError(t, err)
	f.at(int64(batch) + 1)
	require.NoError(t, f.k.CommitBid(f.ctx, &types.MsgCommitBid{Solver: f.solver.String(), BatchId: batch, MarketId: perpMarketID, Commitment: commitment}))
	f.at(int64(batch) + f.params.CommitWindow + 1)
	require.NoError(t, f.k.RevealBid(f.ctx, &types.MsgRevealBid{Solver: f.solver.String(), BatchId: batch, Bid: bid, Salt: salt}))
	require.Equal(t, "1", hook.reserved(f.ctx, f.solver.String()).String())

	require.NoError(t, f.k.EndBlocker(f.ctx))

	res, err := f.k.Results.Get(f.ctx, collectionsJoin(batch, perpMarketID))
	require.NoError(t, err)
	require.True(t, res.Executed)
	require.Equal(t, "10000", res.Volume.String())
	require.Len(t, hook.fills, 2)
	require.Equal(t, batch, hook.fills[0].Batch)
	// every reservation released: the intent completed, the level settled
	require.True(t, hook.reserved(f.ctx, f.user.String()).IsZero())
	require.True(t, hook.reserved(f.ctx, f.solver.String()).IsZero())
	_, err = f.k.GetIntent(f.ctx, id)
	require.ErrorIs(t, err, types.ErrIntentNotFound)
	// solver escrow untouched: perps do not use it
	esc, err := f.k.SolverEscrowBalances(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Equal(t, "50000000000", esc.AmountOf("uluna").String())
}

func TestPerpFillFailureExcludesAndReresolves(t *testing.T) {
	f := setup(t)
	hook := f.perpSetup(t)
	pref := math.LegacyNewDecWithPrec(1, 4)
	other := sdk.AccAddress([]byte("batch-other----------"))
	third := sdk.AccAddress([]byte("batch-third----------"))
	for _, a := range []sdk.AccAddress{other, third} {
		require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(10_000_000)))))
		require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", a, sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(10_000_000)))))
	}

	f.at(10)
	// two buyers, one seller; the first buyer has no margin at p* → excluded, the second fills
	failID, batch := f.submitPerp(t, f.user, types.SIDE_BUY, 10_000, pref, false)
	okID, _ := f.submitPerp(t, other, types.SIDE_BUY, 10_000, pref, false)
	_, _ = f.submitPerp(t, third, types.SIDE_SELL, 10_000, pref, false)
	hook.failing[f.user.String()] = true

	f.at(int64(batch) + f.params.CommitWindow + 1)
	require.NoError(t, f.k.EndBlocker(f.ctx))

	res, err := f.k.Results.Get(f.ctx, collectionsJoin(batch, perpMarketID))
	require.NoError(t, err)
	require.True(t, res.Executed)
	require.Equal(t, "10000", res.Volume.String())
	for _, fl := range hook.fills {
		require.NotEqual(t, f.user.String(), fl.Account.String())
	}
	// the failing intent stays open with its reservation; the filled ones closed
	in, err := f.k.GetIntent(f.ctx, failID)
	require.NoError(t, err)
	require.Equal(t, "10000", in.Remaining.String())
	require.Equal(t, "1", hook.reserved(f.ctx, f.user.String()).String())
	_, err = f.k.GetIntent(f.ctx, okID)
	require.ErrorIs(t, err, types.ErrIntentNotFound)
	require.True(t, hook.reserved(f.ctx, other.String()).IsZero())
	require.True(t, hook.reserved(f.ctx, third.String()).IsZero())
}
