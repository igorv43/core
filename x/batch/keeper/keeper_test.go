package keeper_test

import (
	"fmt"
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
	marketID = "uluna/uusdc.lf"
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

	// P_ref = 0.0001 USD (oracle uusd rate) per uluna; USDC ~ USD
	app.OracleKeeper.SetLunaExchangeRate(ctx, "uusd", math.LegacyNewDecWithPrec(1, 4))

	require.NoError(t, k.CreateMarket(ctx, types.Market{
		Id: marketID, BaseDenom: "uluna", QuoteDenom: "uusdc.lf", Type: types.MARKET_TYPE_SPOT, OracleDenom: "uusd",
		Enabled: true, MinQty: math.NewInt(1_000_000), TickSize: math.LegacyNewDecWithPrec(1, 6),
	}))

	user := sdk.AccAddress([]byte("batch-user-----------"))
	solver := sdk.AccAddress([]byte("batch-solver---------"))
	h.FundAcc(user, sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(100_000_000_000)), sdk.NewCoin("uusdc.lf", math.NewInt(100_000_000))))
	h.FundAcc(solver, sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(100_000_000_000)), sdk.NewCoin("uusdc.lf", math.NewInt(100_000_000_000))))

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
	require.NoError(t, f.k.DepositSolverEscrow(f.ctx, f.solver.String(), sdk.NewCoin("uusdc.lf", math.NewInt(10_000_000))))
}

func (f *fixture) submitBuy(t *testing.T, quote int64, limit math.LegacyDec) (uint64, uint64) {
	t.Helper()
	id, batch, err := f.k.SubmitIntent(f.ctx, &types.MsgSubmitIntent{
		Sender: f.user.String(), MarketId: marketID, Side: types.SIDE_BUY,
		AmountIn: sdk.NewCoin("uusdc.lf", math.NewInt(quote)), LimitPrice: limit, MinOut: math.ZeroInt(),
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
	userUsdBefore := f.balance(f.user, "uusdc.lf")
	id, batch := f.submitBuy(t, 1_000_000, pref)
	require.Equal(t, uint64(10), batch)
	// the escrow and the anti-spam intent fee (settlement asset) left the account
	require.Equal(t, "uusdc.lf", f.params.IntentFee.Denom)
	require.Equal(t, userUsdBefore.SubRaw(1_000_000).Sub(f.params.IntentFee.Amount).String(), f.balance(f.user, "uusdc.lf").String())
	require.Equal(t, userLunaBefore.String(), f.balance(f.user, "uluna").String())

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

	// user: 10,000 LUNC minus the 5 bps protocol fee; intent closed
	got := f.balance(f.user, "uluna").Sub(userLunaBefore)
	require.Equal(t, "9995000000", got.String())
	_, err = f.k.GetIntent(f.ctx, id)
	require.ErrorIs(t, err, types.ErrIntentNotFound)

	// solver escrow: base delivered, quote received minus spot_solver_fee_bps (0 by default)
	esc, err := f.k.SolverEscrowBalances(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Equal(t, "40000000000", esc.AmountOf("uluna").String())
	require.Equal(t, "11000000", esc.AmountOf("uusdc.lf").String())

	// the module holds exactly bond + escrow; nothing leaked
	moduleLuna := f.balance(f.k.ModuleAddress(), "uluna")
	moduleUsd := f.balance(f.k.ModuleAddress(), "uusdc.lf")
	require.Equal(t, "40000000000", moduleLuna.String())
	require.Equal(t, f.params.SolverBondMin.Amount.AddRaw(11_000_000).String(), moduleUsd.String())

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

// deliverTx runs a message handler the way baseapp does: on a branched
// context whose writes (and events) are kept only when the handler succeeds.
func (f *fixture) deliverTx(handler func(ctx sdk.Context) error) (sdk.Events, error) {
	cacheCtx, write := f.ctx.CacheContext()
	cacheCtx = cacheCtx.WithEventManager(sdk.NewEventManager())
	if err := handler(cacheCtx); err != nil {
		return nil, err
	}
	write()
	return cacheCtx.EventManager().Events(), nil
}

func hasSlashEvent(events sdk.Events, reason string) bool {
	for _, e := range events {
		if e.Type != "terra.batch.v1.EventSolverSlashed" {
			continue
		}
		for _, a := range e.Attributes {
			if a.Key == "reason" && a.Value == fmt.Sprintf("%q", reason) {
				return true
			}
		}
	}
	return false
}

// TestRevealMismatchIsSlashedTwice covers spec §16.2 at tx level: the
// inconsistent reveal must not fail, otherwise the tx revert undoes the
// 2 x slash_no_reveal penalty (SPEC-CONFORMANCE-2026-10-04 row A-02).
func TestRevealMismatchIsSlashedTwice(t *testing.T) {
	f := setup(t)
	f.registerSolver(t)
	ms := keeper.NewMsgServerImpl(f.k)
	pref := math.LegacyNewDecWithPrec(1, 4)
	_, batch := f.submitBuy(t, 1_000_000, pref)

	bid := types.Bid{MarketId: marketID, Levels: []types.Level{{Side: types.SIDE_SELL, Price: pref, Qty: math.NewInt(10_000_000_000)}}}
	commitment, err := types.Commitment(bid, []byte("salt"), f.solver.String(), batch)
	require.NoError(t, err)
	f.at(int64(batch) + 1)
	_, err = f.deliverTx(func(ctx sdk.Context) error {
		_, err := ms.CommitBid(ctx, &types.MsgCommitBid{Solver: f.solver.String(), BatchId: batch, MarketId: marketID, Commitment: commitment})
		return err
	})
	require.NoError(t, err)
	moduleBefore := f.balance(f.k.ModuleAddress(), f.params.SlashNoReveal.Denom)

	f.at(int64(batch) + f.params.CommitWindow + 1)
	events, err := f.deliverTx(func(ctx sdk.Context) error {
		_, err := ms.RevealBid(ctx, &types.MsgRevealBid{Solver: f.solver.String(), BatchId: batch, Bid: bid, Salt: []byte("other")})
		return err
	})
	require.NoError(t, err, "an inconsistent reveal must succeed so the slash is committed")
	require.True(t, hasSlashEvent(events, keeper.ReasonRevealMismatch), "EventSolverSlashed missing from the reveal tx: %v", events)

	twice := f.params.SlashNoReveal.Amount.MulRaw(2)
	s, err := f.k.GetSolver(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Equal(t, f.params.SolverBondMin.Amount.Sub(twice).String(), s.Bond.Amount.String())
	require.Equal(t, uint64(0), s.Reveals, "an inconsistent reveal does not count for the reveal rate")
	// the penalty left the module (bond custody) for the fee sink
	require.Equal(t, moduleBefore.Sub(twice).String(), f.balance(f.k.ModuleAddress(), f.params.SlashNoReveal.Denom).String())

	// the commitment is consumed: the correct opening is now rejected
	_, err = f.deliverTx(func(ctx sdk.Context) error {
		_, err := ms.RevealBid(ctx, &types.MsgRevealBid{Solver: f.solver.String(), BatchId: batch, Bid: bid, Salt: []byte("salt")})
		return err
	})
	require.ErrorIs(t, err, types.ErrCommitExists)
	c, err := f.k.Commits.Get(f.ctx, collectionsJoin3(batch, marketID, f.solver.String()))
	require.NoError(t, err)
	require.True(t, c.Revealed)
	require.Nil(t, c.Bid)

	// EndBlock does not add the no-reveal slash on top, and the consumed
	// commitment contributes no level: the intent stays open
	require.NoError(t, f.k.EndBlocker(f.ctx))
	s, err = f.k.GetSolver(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Equal(t, f.params.SolverBondMin.Amount.Sub(twice).String(), s.Bond.Amount.String())
	res, err := f.k.Results.Get(f.ctx, collectionsJoin(batch, marketID))
	require.NoError(t, err)
	require.False(t, res.Executed)
	esc, err := f.k.SolverEscrowBalances(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Equal(t, "50000000000", esc.AmountOf("uluna").String())
}

// TestRevealRateBelowThresholdSuspendsSolver covers spec §16.2: a reveal
// rate below 90% over RevealRateWindowBlocks suspends the solver for
// solver_suspension_blocks, and a suspended solver cannot commit.
func TestRevealRateBelowThresholdSuspendsSolver(t *testing.T) {
	f := setup(t)
	f.registerSolver(t) // window starts at height 10
	start := f.ctx.BlockHeight()

	// three commits, none revealed (each batch is resolved and slashed at b+W+1)
	for _, b := range []uint64{20, 40, 60} {
		f.commitAndReveal(t, b, []types.Level{{Side: types.SIDE_SELL, Price: math.LegacyNewDecWithPrec(1, 4), Qty: math.NewInt(1_000_000)}}, false)
		require.NoError(t, f.k.EndBlocker(f.ctx))
	}
	s, err := f.k.GetSolver(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Equal(t, uint64(3), s.Commits)
	require.Equal(t, uint64(0), s.Reveals)
	require.Zero(t, s.SuspendedUntil)

	// one block before the window elapses: nothing happens
	f.at(start + types.RevealRateWindowBlocks - 1)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	s, err = f.k.GetSolver(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Zero(t, s.SuspendedUntil)

	end := start + types.RevealRateWindowBlocks
	f.at(end)
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.k.EndBlocker(f.ctx))
	suspended := false
	for _, e := range f.ctx.EventManager().Events() {
		if e.Type == "terra.batch.v1.EventSolverSuspended" {
			suspended = true
		}
	}
	require.True(t, suspended, "EventSolverSuspended not emitted")
	s, err = f.k.GetSolver(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Equal(t, end+f.params.SolverSuspensionBlocks, s.SuspendedUntil)
	require.Equal(t, uint64(0), s.Commits)
	require.Equal(t, uint64(0), s.Reveals)
	require.Equal(t, end, s.WindowStart)

	// a suspended solver cannot commit until the suspension ends
	batch := uint64(end)
	f.at(end + 1)
	err = f.k.CommitBid(f.ctx, &types.MsgCommitBid{Solver: f.solver.String(), BatchId: batch, MarketId: marketID, Commitment: make([]byte, 32)})
	require.ErrorIs(t, err, types.ErrSolverSuspended)
	batch = uint64(s.SuspendedUntil)
	f.at(s.SuspendedUntil + 1)
	require.NoError(t, f.k.CommitBid(f.ctx, &types.MsgCommitBid{Solver: f.solver.String(), BatchId: batch, MarketId: marketID, Commitment: make([]byte, 32)}))
}

// TestRevealRateAtThresholdKeepsSolverActive: a solver revealing at least
// 90% of its commits in the window is not suspended.
func TestRevealRateAtThresholdKeepsSolverActive(t *testing.T) {
	f := setup(t)
	f.registerSolver(t)
	start := f.ctx.BlockHeight()
	pref := math.LegacyNewDecWithPrec(1, 4)
	for _, b := range []uint64{20, 40, 60} {
		f.commitAndReveal(t, b, []types.Level{{Side: types.SIDE_SELL, Price: pref, Qty: math.NewInt(1_000_000)}}, true)
		require.NoError(t, f.k.EndBlocker(f.ctx))
	}
	s, err := f.k.GetSolver(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Equal(t, uint64(3), s.Commits)
	require.Equal(t, uint64(3), s.Reveals)

	end := start + types.RevealRateWindowBlocks
	f.at(end)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	s, err = f.k.GetSolver(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Zero(t, s.SuspendedUntil)
	require.Equal(t, end, s.WindowStart)
	require.Equal(t, uint64(0), s.Commits)
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
	before := f.balance(f.user, "uusdc.lf")
	id, _ := f.submitBuy(t, 1_000_000, math.LegacyNewDecWithPrec(1, 4))
	require.Equal(t, before.SubRaw(1_000_000).Sub(f.params.IntentFee.Amount).String(), f.balance(f.user, "uusdc.lf").String())

	f.at(110)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	// the escrow comes back; the anti-spam intent fee is not refundable (§14.2 rule 5)
	require.Equal(t, before.Sub(f.params.IntentFee.Amount).String(), f.balance(f.user, "uusdc.lf").String())
	_, err := f.k.GetIntent(f.ctx, id)
	require.ErrorIs(t, err, types.ErrIntentNotFound)
}

func TestCancelRefundsAndOnlyOwner(t *testing.T) {
	f := setup(t)
	before := f.balance(f.user, "uusdc.lf")
	id, _ := f.submitBuy(t, 1_000_000, math.LegacyNewDecWithPrec(1, 4))
	_, err := f.k.CancelIntent(f.ctx, f.solver.String(), id)
	require.ErrorIs(t, err, types.ErrUnauthorized)
	refund, err := f.k.CancelIntent(f.ctx, f.user.String(), id)
	require.NoError(t, err)
	require.Equal(t, "1000000uusdc.lf", refund.String())
	require.Equal(t, before.Sub(f.params.IntentFee.Amount).String(), f.balance(f.user, "uusdc.lf").String())
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
	require.NoError(t, f.k.WithdrawSolverEscrow(f.ctx, f.solver.String(), sdk.NewCoin("uusdc.lf", math.NewInt(1_000_000))))
}

func TestSolverUnbondRefundsBondAndEscrow(t *testing.T) {
	f := setup(t)
	f.registerSolver(t)
	lunaBefore := f.balance(f.solver, "uluna")
	usdBefore := f.balance(f.solver, "uusdc.lf")

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
	require.Equal(t, usdBefore.AddRaw(10_000_000).Add(f.params.SolverBondMin.Amount).String(), f.balance(f.solver, "uusdc.lf").String())
}

func TestFrontendFeeRequiresApproval(t *testing.T) {
	f := setup(t)
	f.registerSolver(t)
	frontend := sdk.AccAddress([]byte("batch-frontend-------"))
	require.NoError(t, f.k.RegisterFrontend(f.ctx, frontend.String(), 5))
	require.ErrorIs(t, f.k.RegisterFrontend(f.ctx, frontend.String(), max(f.params.BuilderFeeMaxBps, f.params.BuilderFeeMaxSpotBps)+1), types.ErrFrontendFeeTooHigh)
	pref := math.LegacyNewDecWithPrec(1, 4)

	// without approval the attribution is dropped
	id, _, err := f.k.SubmitIntent(f.ctx, &types.MsgSubmitIntent{
		Sender: f.user.String(), MarketId: marketID, Side: types.SIDE_BUY, AmountIn: sdk.NewCoin("uusdc.lf", math.NewInt(1_000_000)),
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
	_, batch, err := f.k.SubmitIntent(f.ctx, &types.MsgSubmitIntent{
		Sender: f.user.String(), MarketId: marketID, Side: types.SIDE_BUY, AmountIn: sdk.NewCoin("uusdc.lf", math.NewInt(1_000_000)),
		LimitPrice: pref, MinOut: math.ZeroInt(), ExpiryHeight: f.ctx.BlockHeight() + 100, Frontend: frontend.String(),
	})
	require.NoError(t, err)
	f.commitAndReveal(t, batch, []types.Level{{Side: types.SIDE_SELL, Price: pref, Qty: math.NewInt(10_000_000_000)}}, true)
	require.NoError(t, f.k.EndBlocker(f.ctx))

	// 10,000 LUNC minus the 5 bps protocol fee and the 5 bps builder fee
	got := f.balance(f.user, "uluna").Sub(userLunaBefore)
	require.Equal(t, "9990000000", got.String())
	require.Equal(t, "5000000", f.balance(frontend, "uluna").String())
}

// Spec §23.2 (v0.9.10): a frontend registers one fee, applied up to the cap
// of the market type: builder_fee_max_spot_bps on spot fills.
func TestBuilderFeeCappedBySpotCap(t *testing.T) {
	f := setup(t)
	f.registerSolver(t)
	frontend := sdk.AccAddress([]byte("batch-frontend-------"))
	require.NoError(t, f.k.RegisterFrontend(f.ctx, frontend.String(), 30))
	require.NoError(t, f.k.ApproveFrontend(f.ctx, f.user.String(), frontend.String(), 30))
	// governance lowers the spot cap below the registered fee
	p := f.params
	p.BuilderFeeMaxSpotBps = 20
	require.NoError(t, f.k.SetParams(f.ctx, p))
	pref := math.LegacyNewDecWithPrec(1, 4)
	before := f.balance(f.user, "uluna")
	_, batch, err := f.k.SubmitIntent(f.ctx, &types.MsgSubmitIntent{
		Sender: f.user.String(), MarketId: marketID, Side: types.SIDE_BUY, AmountIn: sdk.NewCoin("uusdc.lf", math.NewInt(1_000_000)),
		LimitPrice: pref, MinOut: math.ZeroInt(), ExpiryHeight: f.ctx.BlockHeight() + 100, Frontend: frontend.String(),
	})
	require.NoError(t, err)
	f.commitAndReveal(t, batch, []types.Level{{Side: types.SIDE_SELL, Price: pref, Qty: math.NewInt(10_000_000_000)}}, true)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	// builder fee at the 20 bps cap, not the registered 30
	require.Equal(t, "20000000", f.balance(frontend, "uluna").String())
	require.Equal(t, "9975000000", f.balance(f.user, "uluna").Sub(before).String())
}

// Spec §23 (v0.9.10): solver levels pay spot_solver_fee_bps on what they
// receive, user intents spot_fee_bps.
func TestSpotSolverFeeRate(t *testing.T) {
	f := setup(t)
	p := f.params
	p.SpotSolverFeeBps = 1
	require.NoError(t, f.k.SetParams(f.ctx, p))
	f.params = p
	f.registerSolver(t)
	pref := math.LegacyNewDecWithPrec(1, 4)
	before := f.balance(f.user, "uluna")
	_, batch := f.submitBuy(t, 1_000_000, pref)
	f.commitAndReveal(t, batch, []types.Level{{Side: types.SIDE_SELL, Price: pref, Qty: math.NewInt(10_000_000_000)}}, true)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	require.Equal(t, "9995000000", f.balance(f.user, "uluna").Sub(before).String()) // intent: 5 bps
	esc, err := f.k.SolverEscrowBalances(f.ctx, f.solver.String())
	require.NoError(t, err)
	require.Equal(t, "10999900", esc.AmountOf("uusdc.lf").String()) // level: 1 bps of 1 USDC received
}

func TestPerpMarketNeedsMarginHook(t *testing.T) {
	f := setup(t)
	err := f.k.CreateMarket(f.ctx, types.Market{
		Id: "uluna-perp/uusdc.lf", BaseDenom: "uluna", QuoteDenom: "uusdc.lf", Type: types.MARKET_TYPE_PERP, OracleDenom: "uusd",
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
