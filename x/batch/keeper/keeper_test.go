package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	terraapp "github.com/classic-terra/core/v4/app"
	apptesting "github.com/classic-terra/core/v4/app/testing"
	"github.com/classic-terra/core/v4/x/batch/keeper"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

const (
	chainID  = "batch-test"
	marketID = "uluna/uusd"
)

type fixture struct {
	app    *terraapp.TerraApp
	ctx    sdk.Context
	k      keeper.Keeper
	user   sdk.AccAddress
	solver sdk.AccAddress
	params types.Params
}

// setup boots the app with the repository test helper, initialises x/batch
// like the v17 upgrade does (default genesis), registers one spot market
// and publishes an oracle rate so P_ref exists.
func setup(t *testing.T) *fixture {
	t.Helper()
	h := &apptesting.KeeperTestHelper{}
	h.SetT(t)
	h.Setup(t, chainID)
	app, ctx := h.App, h.Ctx
	k := app.BatchKeeper

	require.NoError(t, k.InitGenesis(ctx, types.DefaultGenesisState()))
	params, err := k.GetParams(ctx)
	require.NoError(t, err)

	// P_ref = 0.0001 uusd per uluna
	app.OracleKeeper.SetLunaExchangeRate(ctx, "uusd", math.LegacyNewDecWithPrec(1, 4))

	require.NoError(t, k.CreateMarket(ctx, types.Market{
		Id: marketID, BaseDenom: "uluna", QuoteDenom: "uusd", Type: types.MARKET_TYPE_SPOT, OracleDenom: "uusd",
		Enabled: true, MinQty: math.NewInt(1_000_000), TickSize: math.LegacyNewDecWithPrec(1, 6),
	}))

	user := sdk.AccAddress([]byte("batch-user-----------"))
	solver := sdk.AccAddress([]byte("batch-solver---------"))
	h.FundAcc(user, sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(100_000_000_000)), sdk.NewCoin("uusd", math.NewInt(100_000_000))))
	h.FundAcc(solver, sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(100_000_000_000)), sdk.NewCoin("uusd", math.NewInt(100_000_000_000))))

	f := &fixture{app: app, ctx: ctx, k: k, user: user, solver: solver, params: params}
	f.at(10)
	return f
}

func (f *fixture) at(height int64) {
	h := f.ctx.BlockHeader()
	h.Height = height
	f.ctx = f.ctx.WithBlockHeader(h).WithBlockHeight(height)
}

func (f *fixture) balance(addr sdk.AccAddress, denom string) math.Int {
	return f.app.BankKeeper.GetBalance(f.ctx, addr, denom).Amount
}

func (f *fixture) registerSolver(t *testing.T) {
	t.Helper()
	require.NoError(t, f.k.RegisterSolver(f.ctx, f.solver.String(), f.params.SolverBondMin))
	require.NoError(t, f.k.DepositSolverEscrow(f.ctx, f.solver.String(), sdk.NewCoin("uluna", math.NewInt(50_000_000_000))))
	require.NoError(t, f.k.DepositSolverEscrow(f.ctx, f.solver.String(), sdk.NewCoin("uusd", math.NewInt(10_000_000))))
}

func (f *fixture) submitBuy(t *testing.T, quote int64, limit math.LegacyDec) (uint64, uint64) {
	t.Helper()
	id, batch, err := f.k.SubmitIntent(f.ctx, &types.MsgSubmitIntent{
		Sender: f.user.String(), MarketId: marketID, Side: types.SIDE_BUY,
		AmountIn: sdk.NewCoin("uusd", math.NewInt(quote)), LimitPrice: limit, MinOut: math.ZeroInt(),
		ExpiryHeight: f.ctx.BlockHeight() + 100,
	})
	require.NoError(t, err)
	return id, batch
}

func (f *fixture) commitAndReveal(t *testing.T, batch uint64, levels []types.Level, reveal bool) {
	t.Helper()
	bid := types.Bid{MarketId: marketID, Levels: levels}
	salt := []byte("salt")
	commitment, err := types.Commitment(bid, salt, f.solver.String(), batch)
	require.NoError(t, err)

	f.at(int64(batch) + 1)
	require.NoError(t, f.k.CommitBid(f.ctx, &types.MsgCommitBid{
		Solver: f.solver.String(), BatchId: batch, MarketId: marketID, Commitment: commitment,
	}))
	// the commit window closes at b+W; the reveal block is b+W+1
	f.at(int64(batch) + f.params.CommitWindow + 1)
	if reveal {
		require.NoError(t, f.k.RevealBid(f.ctx, &types.MsgRevealBid{Solver: f.solver.String(), BatchId: batch, Bid: bid, Salt: salt}))
	}
}

func TestPipelineClearsIntentAgainstSolverLevel(t *testing.T) {
	f := setup(t)
	f.registerSolver(t)
	pref := math.LegacyNewDecWithPrec(1, 4)

	// height 10: 1 USD buys LUNC at up to P_ref → 10,000 LUNC of demand
	userLunaBefore := f.balance(f.user, "uluna")
	userUsdBefore := f.balance(f.user, "uusd")
	id, batch := f.submitBuy(t, 1_000_000, pref)
	require.Equal(t, uint64(10), batch)
	require.Equal(t, userUsdBefore.SubRaw(1_000_000).String(), f.balance(f.user, "uusd").String())
	// the anti-spam intent fee left the account
	require.Equal(t, userLunaBefore.Sub(f.params.IntentFee.Amount).String(), f.balance(f.user, "uluna").String())

	// the solver offers exactly the demand at P_ref
	f.commitAndReveal(t, batch, []types.Level{{Side: types.SIDE_SELL, Price: pref, Qty: math.NewInt(10_000_000_000)}}, true)

	// EndBlock of b+W+1 resolves batch b
	require.NoError(t, f.k.EndBlocker(f.ctx))

	res, err := f.k.Results.Get(f.ctx, collectionsJoin(batch, marketID))
	require.NoError(t, err)
	require.True(t, res.Executed)
	require.Equal(t, pref.String(), res.ClearingPrice.String())
	require.Equal(t, "10000000000", res.Volume.String())
	require.Equal(t, uint32(2), res.Fills)

	// user: 10,000 LUNC minus the 10 bps protocol fee; intent closed
	got := f.balance(f.user, "uluna").Sub(userLunaBefore.Sub(f.params.IntentFee.Amount))
	require.Equal(t, "9990000000", got.String())
	_, err = f.k.GetIntent(f.ctx, id)
	require.ErrorIs(t, err, types.ErrIntentNotFound)

	// solver escrow: base delivered, quote received minus fee
	esc, err := f.k.SolverEscrowBalances(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Equal(t, "40000000000", esc.AmountOf("uluna").String())
	require.Equal(t, "10999000", esc.AmountOf("uusd").String())

	// the module holds exactly bond + escrow; nothing leaked
	moduleLuna := f.balance(f.k.ModuleAddress(), "uluna")
	moduleUsd := f.balance(f.k.ModuleAddress(), "uusd")
	require.Equal(t, "40000000000", moduleLuna.String())
	require.Equal(t, f.params.SolverBondMin.Amount.AddRaw(10_999_000).String(), moduleUsd.String())

	// commits of the batch were pruned
	_, err = f.k.Commits.Get(f.ctx, collectionsJoin3(batch, marketID, f.solver.String()))
	require.Error(t, err)
}

func TestUnrevealedCommitIsSlashed(t *testing.T) {
	f := setup(t)
	f.registerSolver(t)
	pref := math.LegacyNewDecWithPrec(1, 4)
	_, batch := f.submitBuy(t, 1_000_000, pref)

	f.commitAndReveal(t, batch, []types.Level{{Side: types.SIDE_SELL, Price: pref, Qty: math.NewInt(10_000_000_000)}}, false)
	require.NoError(t, f.k.EndBlocker(f.ctx))

	s, err := f.k.GetSolver(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Equal(t, f.params.SolverBondMin.Amount.Sub(f.params.SlashNoReveal.Amount).String(), s.Bond.Amount.String())

	// nothing to match: the intent stays open for the next batch
	res, err := f.k.Results.Get(f.ctx, collectionsJoin(batch, marketID))
	require.NoError(t, err)
	require.False(t, res.Executed)
}

func TestRevealMismatchIsSlashedTwice(t *testing.T) {
	f := setup(t)
	f.registerSolver(t)
	pref := math.LegacyNewDecWithPrec(1, 4)
	_, batch := f.submitBuy(t, 1_000_000, pref)

	bid := types.Bid{MarketId: marketID, Levels: []types.Level{{Side: types.SIDE_SELL, Price: pref, Qty: math.NewInt(10_000_000_000)}}}
	commitment, err := types.Commitment(bid, []byte("salt"), f.solver.String(), batch)
	require.NoError(t, err)
	f.at(int64(batch) + 1)
	require.NoError(t, f.k.CommitBid(f.ctx, &types.MsgCommitBid{Solver: f.solver.String(), BatchId: batch, MarketId: marketID, Commitment: commitment}))
	f.at(int64(batch) + f.params.CommitWindow + 1)
	err = f.k.RevealBid(f.ctx, &types.MsgRevealBid{Solver: f.solver.String(), BatchId: batch, Bid: bid, Salt: []byte("other")})
	require.ErrorIs(t, err, types.ErrRevealMismatch)

	s, err := f.k.GetSolver(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Equal(t, f.params.SolverBondMin.Amount.Sub(f.params.SlashNoReveal.Amount.MulRaw(2)).String(), s.Bond.Amount.String())
}

func TestCommitOutsideWindowRejected(t *testing.T) {
	f := setup(t)
	f.registerSolver(t)
	commitment := make([]byte, 32)
	// batch 10, window (10, 12]
	f.at(10)
	err := f.k.CommitBid(f.ctx, &types.MsgCommitBid{Solver: f.solver.String(), BatchId: 10, MarketId: marketID, Commitment: commitment})
	require.ErrorIs(t, err, types.ErrCommitWindowClosed)
	f.at(13)
	err = f.k.CommitBid(f.ctx, &types.MsgCommitBid{Solver: f.solver.String(), BatchId: 10, MarketId: marketID, Commitment: commitment})
	require.ErrorIs(t, err, types.ErrCommitWindowClosed)
	f.at(12)
	require.NoError(t, f.k.CommitBid(f.ctx, &types.MsgCommitBid{Solver: f.solver.String(), BatchId: 10, MarketId: marketID, Commitment: commitment}))
}

func TestIntentOutsideBandDoesNotExecute(t *testing.T) {
	f := setup(t)
	f.registerSolver(t)
	// limit 5% below P_ref: no candidate price inside the 2% band
	limit := math.LegacyNewDecWithPrec(95, 6)
	_, batch := f.submitBuy(t, 1_000_000, limit)
	f.commitAndReveal(t, batch, []types.Level{{Side: types.SIDE_SELL, Price: limit, Qty: math.NewInt(10_000_000_000)}}, true)
	require.NoError(t, f.k.EndBlocker(f.ctx))

	res, err := f.k.Results.Get(f.ctx, collectionsJoin(batch, marketID))
	require.NoError(t, err)
	require.False(t, res.Executed)
	// the escrow is untouched
	esc, err := f.k.SolverEscrowBalances(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Equal(t, "50000000000", esc.AmountOf("uluna").String())
}

func TestExpiredIntentIsRefunded(t *testing.T) {
	f := setup(t)
	before := f.balance(f.user, "uusd")
	id, _ := f.submitBuy(t, 1_000_000, math.LegacyNewDecWithPrec(1, 4))
	require.Equal(t, before.SubRaw(1_000_000).String(), f.balance(f.user, "uusd").String())

	f.at(110)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	require.Equal(t, before.String(), f.balance(f.user, "uusd").String())
	_, err := f.k.GetIntent(f.ctx, id)
	require.ErrorIs(t, err, types.ErrIntentNotFound)
}

func TestCancelRefundsAndOnlyOwner(t *testing.T) {
	f := setup(t)
	before := f.balance(f.user, "uusd")
	id, _ := f.submitBuy(t, 1_000_000, math.LegacyNewDecWithPrec(1, 4))
	_, err := f.k.CancelIntent(f.ctx, f.solver.String(), id)
	require.ErrorIs(t, err, types.ErrUnauthorized)
	refund, err := f.k.CancelIntent(f.ctx, f.user.String(), id)
	require.NoError(t, err)
	require.Equal(t, "1000000uusd", refund.String())
	require.Equal(t, before.String(), f.balance(f.user, "uusd").String())
}

func TestEscrowWithdrawRespectsRevealedReservation(t *testing.T) {
	f := setup(t)
	f.registerSolver(t)
	pref := math.LegacyNewDecWithPrec(1, 4)
	_, batch := f.submitBuy(t, 1_000_000, pref)
	f.commitAndReveal(t, batch, []types.Level{{Side: types.SIDE_SELL, Price: pref, Qty: math.NewInt(50_000_000_000)}}, true)

	// the whole base escrow is reserved by the revealed level
	err := f.k.WithdrawSolverEscrow(f.ctx, f.solver.String(), sdk.NewCoin("uluna", math.NewInt(1)))
	require.ErrorIs(t, err, types.ErrInsufficientEscrow)
	// quote is free
	require.NoError(t, f.k.WithdrawSolverEscrow(f.ctx, f.solver.String(), sdk.NewCoin("uusd", math.NewInt(1_000_000))))
}

func TestSolverUnbondRefundsBondAndEscrow(t *testing.T) {
	f := setup(t)
	f.registerSolver(t)
	lunaBefore := f.balance(f.solver, "uluna")
	usdBefore := f.balance(f.solver, "uusd")

	until, err := f.k.UnbondSolver(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Equal(t, f.ctx.BlockHeight()+f.params.SolverUnbondBlocks, until)
	// an unbonding solver cannot commit
	err = f.k.CommitBid(f.ctx, &types.MsgCommitBid{Solver: f.solver.String(), BatchId: uint64(f.ctx.BlockHeight() - 1), MarketId: marketID, Commitment: make([]byte, 32)})
	require.ErrorIs(t, err, types.ErrSolverUnbonding)

	f.at(until)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	_, err = f.k.GetSolver(f.ctx, f.solver.String())
	require.ErrorIs(t, err, types.ErrSolverNotFound)
	require.Equal(t, lunaBefore.AddRaw(50_000_000_000).String(), f.balance(f.solver, "uluna").String())
	require.Equal(t, usdBefore.AddRaw(10_000_000).Add(f.params.SolverBondMin.Amount).String(), f.balance(f.solver, "uusd").String())
}

func TestFrontendFeeRequiresApproval(t *testing.T) {
	f := setup(t)
	f.registerSolver(t)
	frontend := sdk.AccAddress([]byte("batch-frontend-------"))
	require.NoError(t, f.k.RegisterFrontend(f.ctx, frontend.String(), 5))
	require.ErrorIs(t, f.k.RegisterFrontend(f.ctx, frontend.String(), f.params.BuilderFeeMaxBps+1), types.ErrFrontendFeeTooHigh)
	pref := math.LegacyNewDecWithPrec(1, 4)

	// without approval the attribution is dropped
	id, batch, err := f.k.SubmitIntent(f.ctx, &types.MsgSubmitIntent{
		Sender: f.user.String(), MarketId: marketID, Side: types.SIDE_BUY, AmountIn: sdk.NewCoin("uusd", math.NewInt(1_000_000)),
		LimitPrice: pref, MinOut: math.ZeroInt(), ExpiryHeight: f.ctx.BlockHeight() + 100, Frontend: frontend.String(),
	})
	require.NoError(t, err)
	in, err := f.k.GetIntent(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, "", in.Frontend)
	_, err = f.k.CancelIntent(f.ctx, f.user.String(), id)
	require.NoError(t, err)

	// with approval the builder fee (5 bps) is paid on top of the protocol fee
	require.NoError(t, f.k.ApproveFrontend(f.ctx, f.user.String(), frontend.String(), 5))
	userLunaBefore := f.balance(f.user, "uluna")
	_, batch, err = f.k.SubmitIntent(f.ctx, &types.MsgSubmitIntent{
		Sender: f.user.String(), MarketId: marketID, Side: types.SIDE_BUY, AmountIn: sdk.NewCoin("uusd", math.NewInt(1_000_000)),
		LimitPrice: pref, MinOut: math.ZeroInt(), ExpiryHeight: f.ctx.BlockHeight() + 100, Frontend: frontend.String(),
	})
	require.NoError(t, err)
	f.commitAndReveal(t, batch, []types.Level{{Side: types.SIDE_SELL, Price: pref, Qty: math.NewInt(10_000_000_000)}}, true)
	require.NoError(t, f.k.EndBlocker(f.ctx))

	got := f.balance(f.user, "uluna").Sub(userLunaBefore.Sub(f.params.IntentFee.Amount))
	require.Equal(t, "9985000000", got.String())
	require.Equal(t, "5000000", f.balance(frontend, "uluna").String())
}

func TestPerpMarketNeedsMarginHook(t *testing.T) {
	f := setup(t)
	err := f.k.CreateMarket(f.ctx, types.Market{
		Id: "uluna-perp/uusd", BaseDenom: "uluna", QuoteDenom: "uusd", Type: types.MARKET_TYPE_PERP, OracleDenom: "uusd",
		Enabled: false, MinQty: math.NewInt(1_000_000), TickSize: math.LegacyNewDecWithPrec(1, 6),
	})
	require.ErrorIs(t, err, types.ErrInvalidMarket)
}

func TestGenesisRoundTrip(t *testing.T) {
	f := setup(t)
	f.registerSolver(t)
	f.submitBuy(t, 1_000_000, math.LegacyNewDecWithPrec(1, 4))
	gs, err := f.k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, gs.Markets, 1)
	require.Len(t, gs.Intents, 1)
	require.Len(t, gs.Solvers, 1)
	require.Len(t, gs.SolverEscrows, 2)
	require.Equal(t, uint64(2), gs.NextIntentId)
	require.NoError(t, gs.Validate())
}
