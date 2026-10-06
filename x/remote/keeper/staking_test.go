package keeper_test

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/keeper"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	pdkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/keeper"
	pdtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/types"
	corekeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/keeper"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	lstypes "github.com/classic-terra/core/v4/x/liquidstake/types"
	perptypes "github.com/classic-terra/core/v4/x/perp/types"
	"github.com/classic-terra/core/v4/x/remote/keeper"
	"github.com/classic-terra/core/v4/x/remote/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

const (
	lsEpochBlocks = 45
	localStake    = int64(1_000_000_000)
)

// stakingFixture extends the x/remote fixture with x/liquidstake (one bonded
// validator, short epochs like the testenv), the gateway of the origin, the
// stLUNC collateral route and a funded paymaster. f.token is the LUNC route.
type stakingFixture struct {
	*fixture
	gateway util.HexAddress
	stToken util.HexAddress
	lunaRtr util.HexAddress
	stRtr   util.HexAddress
	valAddr sdk.ValAddress
	nonce   uint32
}

func setupStaking(t *testing.T) *stakingFixture {
	t.Helper()
	f := setup(t)
	app, ctx := f.app, f.ctx
	require.NoError(t, app.SlashingKeeper.SetParams(ctx, slashingtypes.DefaultParams()))
	fund := func(addr sdk.AccAddress, amount int64) {
		coins := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(amount)))
		require.NoError(t, app.BankKeeper.MintCoins(ctx, "mint", coins))
		require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", addr, coins))
	}
	priv := ed25519.GenPrivKey()
	valAddr := sdk.ValAddress(priv.PubKey().Address())
	fund(sdk.AccAddress(valAddr), 2_000_000_000_000)
	msg, err := stakingtypes.NewMsgCreateValidator(valAddr.String(), priv.PubKey(),
		sdk.NewCoin("uluna", math.NewInt(1_000_000_000_000)), stakingtypes.NewDescription("val", "", "", "", ""),
		stakingtypes.NewCommissionRates(math.LegacyNewDecWithPrec(5, 2), math.LegacyNewDecWithPrec(20, 2), math.LegacyNewDecWithPrec(1, 2)),
		math.OneInt())
	require.NoError(t, err)
	_, err = stakingkeeper.NewMsgServerImpl(app.StakingKeeper).CreateValidator(ctx, msg)
	require.NoError(t, err)
	_, err = app.StakingKeeper.ApplyAndReturnValidatorSetUpdates(ctx)
	require.NoError(t, err)
	sp, err := app.StakingKeeper.GetParams(ctx)
	require.NoError(t, err)
	sp.UnbondingTime, sp.MaxEntries = 300*time.Second, 7
	require.NoError(t, app.StakingKeeper.SetParams(ctx, sp))
	require.NoError(t, app.LiquidStakeKeeper.InitGenesis(ctx, lstypes.DefaultGenesisState()))
	require.NoError(t, app.LiquidStakeKeeper.EndBlocker(ctx)) // opens epoch 1
	lp := lstypes.DefaultParams()
	lp.EpochBlocks = lsEpochBlocks
	lp.ExpectedBlockTime = time.Second
	lp.ValidatorCap = math.LegacyOneDec()
	require.NoError(t, app.LiquidStakeKeeper.SetParams(ctx, lp))
	// a local staker, so the epoch delegations cover the remote requests
	_, _, err = app.LiquidStakeKeeper.Stake(ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(localStake)))
	require.NoError(t, err)

	s := &stakingFixture{fixture: f, valAddr: valAddr, nonce: 100}
	s.gateway = util.CreateMockHexAddress("gateway", 1)
	require.NoError(t, f.k.SetGateway(ctx, f.appId, originDom, s.gateway, util.NewZeroAddress(), nil))
	s.lunaRtr = util.CreateMockHexAddress("router", 1)
	// the stLUNC route: collateral of the stluna denom, synthetic on the origin
	warpSrv := warpkeeper.NewMsgServerImpl(app.WarpKeeper)
	tok, err := warpSrv.CreateCollateralToken(ctx, &warptypes.MsgCreateCollateralToken{Owner: f.owner.String(), OriginMailbox: f.mailbox, OriginDenom: lstypes.StDenom})
	require.NoError(t, err)
	s.stToken = tok.Id
	s.stRtr = util.CreateMockHexAddress("router", 2)
	_, err = warpSrv.EnrollRemoteRouter(ctx, &warptypes.MsgEnrollRemoteRouter{
		Owner: f.owner.String(), TokenId: tok.Id,
		RemoteRouter: &warptypes.RemoteRouter{ReceiverDomain: originDom, ReceiverContract: s.stRtr, Gas: math.ZeroInt()},
	})
	require.NoError(t, err)
	require.NoError(t, app.WarpLedgerKeeper.SetDomainCap(ctx, tok.Id, originDom, math.NewInt(1_000_000_000_000)))
	// LUNC bridged out earlier: the collateral the remote deposits release
	transfer := &warptypes.MsgRemoteTransfer{
		Sender: f.owner.String(), TokenId: f.token, DestinationDomain: originDom, Recipient: util.CreateMockHexAddress("bridger", 1),
		Amount: math.NewInt(100_000_000_000), GasLimit: math.ZeroInt(), MaxFee: sdk.NewCoin("uluna", math.ZeroInt()),
	}
	_, err = app.MsgServiceRouter().Handler(transfer)(ctx, transfer)
	require.NoError(t, err)
	require.NoError(t, f.k.FundPaymaster(ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(10_000_000_000))))
	return s
}

// adPayload builds a gateway payload acting for the controller, optionally
// waiting for a deposit.
func (s *stakingFixture) adPayload(t *testing.T, ad *types.AfterDeposit, msgs ...sdk.Msg) []byte {
	t.Helper()
	var anys []*codectypes.Any
	for _, m := range msgs {
		a, err := codectypes.NewAnyWithValue(m)
		require.NoError(t, err)
		anys = append(anys, a)
	}
	bz, err := s.app.AppCodec().Marshal(&types.RemotePayload{OnBehalfOf: s.controller.Bytes(), Msgs: anys, AfterDeposit: ad})
	require.NoError(t, err)
	return bz
}

// fromGateway delivers a control message from the enrolled gateway.
func (s *stakingFixture) fromGateway(t *testing.T, body []byte) {
	t.Helper()
	s.nonce++
	require.NoError(t, s.k.Handle(s.ctx, s.mailbox, util.HyperlaneMessage{
		Version: 3, Nonce: s.nonce, Origin: originDom, Sender: s.gateway, Destination: localDomain, Recipient: s.appId, Body: body,
	}))
}

// warpIn delivers a real warp transfer through the app's message router: the
// wrapped hyperlane core server processes it (mailbox, ISM, warp collateral
// release, x/warpledger accounting) and x/remote observes the deposit.
func (s *stakingFixture) warpIn(t *testing.T, token, router util.HexAddress, recipient sdk.AccAddress, amount int64) {
	t.Helper()
	s.nonce++
	body, err := warptypes.NewWarpPayload(recipient.Bytes(), *big.NewInt(amount))
	require.NoError(t, err)
	msg := util.HyperlaneMessage{
		Version: 3, Nonce: s.nonce, Origin: originDom, Sender: router, Destination: localDomain, Recipient: token, Body: body.Bytes(),
	}
	process := &coretypes.MsgProcessMessage{MailboxId: s.mailbox, Relayer: s.owner.String(), Metadata: "0x00", Message: msg.String()}
	res, err := s.app.MsgServiceRouter().Handler(process)(s.ctx, process)
	require.NoError(t, err)
	// the router returns the handler's events inside the result, as in a tx
	s.ctx.EventManager().EmitEvents(res.GetEvents())
}

func (s *stakingFixture) fresh() { s.ctx = s.ctx.WithEventManager(sdk.NewEventManager()) }

func (s *stakingFixture) count(typ string) int {
	n := 0
	for _, ev := range s.ctx.EventManager().Events() {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

func (s *stakingFixture) attr(typ, key string) string {
	for _, ev := range s.ctx.EventManager().Events() {
		if ev.Type == typ {
			for _, a := range ev.Attributes {
				if a.Key == key {
					return strings.Trim(a.Value, `"`)
				}
			}
		}
	}
	return ""
}

func (s *stakingFixture) bal(addr sdk.AccAddress, denom string) math.Int {
	return s.app.BankKeeper.GetBalance(s.ctx, addr, denom).Amount
}

func (s *stakingFixture) advance(blocks int64, d time.Duration) {
	h := s.ctx.BlockHeader()
	h.Height += blocks
	h.Time = h.Time.Add(d)
	s.ctx = s.ctx.WithBlockHeader(h).WithBlockHeight(h.Height).WithBlockTime(h.Time)
}

// epoch advances to the next liquid-staking epoch and runs both EndBlockers
// in the module order (x/liquidstake before x/remote).
func (s *stakingFixture) epoch(t *testing.T) {
	t.Helper()
	s.advance(lsEpochBlocks, time.Minute)
	require.NoError(t, s.app.LiquidStakeKeeper.EndBlocker(s.ctx))
	require.NoError(t, s.k.EndBlocker(s.ctx))
}

func (s *stakingFixture) pending(t *testing.T, account string) []types.PendingPayload {
	t.Helper()
	r, err := s.k.RemoteStaking(s.ctx, account)
	require.NoError(t, err)
	return r.Pending
}

func amount(v int64) *math.Int {
	i := math.NewInt(v)
	return &i
}

func luna(v int64) sdk.Coin { return sdk.NewCoin("uluna", math.NewInt(v)) }

func st(v int64) sdk.Coin { return sdk.NewCoin(lstypes.StDenom, math.NewInt(v)) }

func (s *stakingFixture) refWithdraw(token util.HexAddress, min *math.Int) *types.MsgWithdraw {
	return &types.MsgWithdraw{Controller: s.derived.String(), TokenId: token, AmountFrom: types.AMOUNT_FROM_PREVIOUS_RESULT, MinAmount: min}
}

// §4.1: the liquid-staking messages are payload messages, never session-key scope.
func TestLiquidStakeMessagesWhitelisted(t *testing.T) {
	urls := types.PayloadMsgTypeURLs()
	session := map[string]bool{}
	for _, u := range types.SessionMsgTypeURLs() {
		session[u] = true
	}
	for _, m := range []sdk.Msg{&lstypes.MsgStake{}, &lstypes.MsgUnstake{}, &lstypes.MsgClaim{}, &types.MsgSetAutoReturn{}} {
		require.True(t, urls[sdk.MsgTypeURL(m)], sdk.MsgTypeURL(m))
		require.False(t, session[sdk.MsgTypeURL(m)], "a session key never moves the principal: %s", sdk.MsgTypeURL(m))
	}
	require.False(t, urls[sdk.MsgTypeURL(&lstypes.MsgUpdateParams{})])
}

// §4.2: MINTED, PAID (instant), the zero skip of a queued unstake, CLAIMED.
func TestResultReferences(t *testing.T) {
	s := setupStaking(t)
	acc := s.derived
	// MINTED: stake 100 LUNC and return the stLUNC at once
	s.deliver(t, s.controller, s.payload(t, nil,
		&lstypes.MsgStake{Sender: acc.String(), Amount: luna(100_000_000)},
		s.refWithdraw(s.stToken, amount(100_000_000)),
	))
	require.Empty(t, s.rejected(t))
	require.True(t, s.bal(acc, lstypes.StDenom).IsZero(), "every minted stLUNC left through warp")
	ledger, _, err := s.app.WarpLedgerKeeper.GetLedger(s.ctx, s.stToken, originDom)
	require.NoError(t, err)
	require.Equal(t, "100000000", ledger.Sent.String())
	ws, err := s.k.WithdrawalsOf(s.ctx, acc.String())
	require.NoError(t, err)
	require.Len(t, ws, 1)
	require.Equal(t, "100000000", ws[0].Amount.String())
	require.Equal(t, ws[0].MessageId.String(), s.attr("terra.remote.v1.EventRemoteStake", "withdrawal_message_id"), "the receipt names the return")
	require.Equal(t, "100000000"+lstypes.StDenom, s.attr("terra.remote.v1.EventRemoteStake", "minted"))

	// PAID (instant): stake 100 more without returning, then unstake 40 and return the uluna
	s.fresh()
	s.deliver(t, s.controller, s.payload(t, nil, &lstypes.MsgStake{Sender: acc.String(), Amount: luna(100_000_000)}))
	require.Empty(t, s.rejected(t))
	before := s.bal(acc, "uluna")
	s.deliver(t, s.controller, s.payload(t, nil,
		&lstypes.MsgUnstake{Sender: acc.String(), Amount: st(40_000_000)},
		s.refWithdraw(s.token, nil),
	))
	require.Empty(t, s.rejected(t))
	require.Equal(t, before.String(), s.bal(acc, "uluna").String(), "the instant redemption went straight back")
	ws, _ = s.k.WithdrawalsOf(s.ctx, acc.String())
	require.Equal(t, "40000000", ws[0].Amount.String())
	require.Equal(t, "true", s.attr("terra.remote.v1.EventRemoteUnstake", "instant"))

	// zero skip: the epoch delegates the module (2 % buffer); a large unstake is queued
	s.epoch(t)
	s.fresh()
	s.deliver(t, s.controller, s.payload(t, nil,
		&lstypes.MsgUnstake{Sender: acc.String(), Amount: st(50_000_000)},
		s.refWithdraw(s.token, amount(1)),
	))
	require.Empty(t, s.rejected(t))
	require.Equal(t, 1, s.count("terra.remote.v1.EventRemoteSkipped"), "PAID = 0 skips the withdrawal, min_amount does not apply")
	require.Equal(t, 1, s.count("terra.remote.v1.EventRemoteExecuted"))
	ws, _ = s.k.WithdrawalsOf(s.ctx, acc.String())
	require.Len(t, ws, 2)
	reqs := 0
	require.NoError(t, s.app.LiquidStakeKeeper.Requests.Walk(s.ctx, nil, func(_ uint64, r lstypes.UnstakeRequest) (bool, error) {
		require.Equal(t, acc.String(), r.Address)
		reqs++
		return false, nil
	}))
	require.Equal(t, 1, reqs)

	// CLAIMED: undelegation batch, unbonding, then claim and return
	s.epoch(t)
	ubd, err := s.app.StakingKeeper.UnbondingTime(s.ctx)
	require.NoError(t, err)
	s.advance(1, ubd+time.Second)
	_, err = s.app.StakingKeeper.CompleteUnbonding(s.ctx, s.app.LiquidStakeKeeper.ModuleAddress(), s.valAddr)
	require.NoError(t, err)
	s.fresh()
	before = s.bal(acc, "uluna")
	s.deliver(t, s.controller, s.payload(t, nil,
		&lstypes.MsgClaim{Sender: acc.String()},
		s.refWithdraw(s.token, amount(50_000_000)),
	))
	require.Empty(t, s.rejected(t))
	require.Equal(t, before.String(), s.bal(acc, "uluna").String())
	ws, _ = s.k.WithdrawalsOf(s.ctx, acc.String())
	require.Len(t, ws, 3)
	require.Equal(t, "50000000", ws[0].Amount.String())
	require.Equal(t, ws[0].MessageId.String(), s.attr("terra.remote.v1.EventRemoteClaim", "withdrawal_message_id"))
}

// §4.2: the closed list — only a MsgWithdraw, only after MsgStake/MsgUnstake/
// MsgClaim, never above min/through another denom, never unresolved.
func TestResultReferencesForbidden(t *testing.T) {
	s := setupStaking(t)
	acc := s.derived
	supply := func() math.Int { return s.app.BankKeeper.GetSupply(s.ctx, lstypes.StDenom).Amount }
	cases := []struct {
		name   string
		msgs   []sdk.Msg
		reason string
	}{
		{"first message", []sdk.Msg{s.refWithdraw(s.token, nil)}, "must follow"},
		{"after a perp message", []sdk.Msg{
			&perptypes.MsgDepositCollateral{Sender: acc.String(), Amount: sdk.NewCoin(settle, math.NewInt(1_000_000))},
			s.refWithdraw(s.token, nil),
		}, "must follow"},
		{"not adjacent", []sdk.Msg{
			&lstypes.MsgStake{Sender: acc.String(), Amount: luna(10_000_000)},
			&perptypes.MsgSetAutoTopUp{Sender: acc.String(), Enabled: true},
			s.refWithdraw(s.stToken, nil),
		}, "must follow"},
		{"below min_amount", []sdk.Msg{
			&lstypes.MsgStake{Sender: acc.String(), Amount: luna(10_000_000)},
			s.refWithdraw(s.stToken, amount(10_000_001)),
		}, "below min_amount"},
		{"another denom", []sdk.Msg{
			&lstypes.MsgStake{Sender: acc.String(), Amount: luna(10_000_000)},
			s.refWithdraw(s.token, nil),
		}, "is not the denom"},
		{"amount and reference", []sdk.Msg{
			&lstypes.MsgStake{Sender: acc.String(), Amount: luna(10_000_000)},
			&types.MsgWithdraw{Controller: acc.String(), TokenId: s.stToken, Amount: math.NewInt(5), AmountFrom: types.AMOUNT_FROM_PREVIOUS_RESULT},
		}, "amount must be empty"},
		{"min without reference", []sdk.Msg{
			&types.MsgWithdraw{Controller: acc.String(), TokenId: s.token, Amount: math.NewInt(5), MinAmount: amount(1)},
		}, "only with amount_from"},
	}
	for _, c := range cases {
		s.fresh()
		s.deliver(t, s.controller, s.payload(t, nil, c.msgs...))
		require.Contains(t, s.rejected(t), c.reason, c.name)
		require.Equal(t, localStake, supply().Int64(), "%s: the whole payload reverted", c.name)
	}
	// an unresolved reference never reaches the handler
	_, err := keeper.NewMsgServerImpl(s.k).Withdraw(s.ctx, s.refWithdraw(s.token, nil))
	require.ErrorIs(t, err, types.ErrInvalidReference)
}

// §4.3: payload before deposit → pending → executed in the EndBlock that sees
// the deposit; deposit first → executed at once; partial deposits add up.
func TestDepositThenExecute(t *testing.T) {
	s := setupStaking(t)
	acc := s.derived.String()
	stake := func(v int64) []byte {
		return s.adPayload(t, &types.AfterDeposit{TokenId: s.token, Amount: math.NewInt(v)},
			&lstypes.MsgStake{Sender: acc, Amount: luna(v)}, s.refWithdraw(s.stToken, amount(v)))
	}
	// payload first
	s.fromGateway(t, stake(100_000_000))
	require.Empty(t, s.rejected(t))
	require.Equal(t, 1, s.count("terra.remote.v1.EventRemotePending"))
	require.Len(t, s.pending(t, acc), 1)
	require.Equal(t, localStake, s.app.BankKeeper.GetSupply(s.ctx, lstypes.StDenom).Amount.Int64(), "nothing executes before its deposit")
	require.NoError(t, s.k.EndBlocker(s.ctx))
	require.Len(t, s.pending(t, acc), 1, "still waiting")
	// a deposit of another amount does not match
	s.warpIn(t, s.token, s.lunaRtr, s.derived, 60_000_000)
	require.Equal(t, 1, s.count("terra.remote.v1.EventDepositCredited"))
	require.NoError(t, s.k.EndBlocker(s.ctx))
	require.Len(t, s.pending(t, acc), 1)
	// the rest arrives: 60 + 40 = 100
	s.fresh()
	s.warpIn(t, s.token, s.lunaRtr, s.derived, 40_000_000)
	require.Len(t, s.pending(t, acc), 1, "executed in EndBlock, not inside the relayer's message")
	require.NoError(t, s.k.EndBlocker(s.ctx))
	require.Empty(t, s.rejected(t))
	require.Empty(t, s.pending(t, acc))
	require.Equal(t, 1, s.count("terra.remote.v1.EventRemoteStake"))
	r, err := s.k.RemoteStaking(s.ctx, acc)
	require.NoError(t, err)
	require.Empty(t, r.Credits, "the credit was consumed")

	// deposit first: the payload executes on arrival
	s.fresh()
	s.warpIn(t, s.token, s.lunaRtr, s.derived, 50_000_000)
	s.fromGateway(t, stake(50_000_000))
	require.Empty(t, s.rejected(t))
	require.Equal(t, 0, s.count("terra.remote.v1.EventRemotePending"))
	require.Equal(t, 1, s.count("terra.remote.v1.EventRemoteStake"))
	ledger, _, err := s.app.WarpLedgerKeeper.GetLedger(s.ctx, s.stToken, originDom)
	require.NoError(t, err)
	require.Equal(t, "150000000", ledger.Sent.String())

	// only the enrolled gateway may ask for the ordering
	s.fresh()
	body, err := s.app.AppCodec().Marshal(&types.RemotePayload{AfterDeposit: &types.AfterDeposit{TokenId: s.token, Amount: math.NewInt(1)}})
	require.NoError(t, err)
	s.deliver(t, s.controller, body)
	require.Contains(t, s.rejected(t), "only from the enrolled gateway")
	// a malformed payload is rejected on arrival, it never waits
	s.fresh()
	s.fromGateway(t, s.adPayload(t, &types.AfterDeposit{TokenId: s.token, Amount: math.NewInt(1)}, s.refWithdraw(s.token, nil)))
	require.Contains(t, s.rejected(t), "must follow")
	require.Empty(t, s.pending(t, acc))
}

// §4.3: expiry, the per-account bound and the per-block bound.
func TestPendingBoundsAndExpiry(t *testing.T) {
	s := setupStaking(t)
	acc := s.derived.String()
	p, err := s.k.GetParams(s.ctx)
	require.NoError(t, err)
	stake := func(v int64) []byte {
		return s.adPayload(t, &types.AfterDeposit{TokenId: s.token, Amount: math.NewInt(v)}, &lstypes.MsgStake{Sender: acc, Amount: luna(v)})
	}
	// per account: max_pending_per_account (3) then refused
	for i := 0; i < int(p.MaxPendingPerAccount); i++ {
		s.fromGateway(t, stake(int64(10_000_000+i)))
	}
	require.Empty(t, s.rejected(t))
	s.fromGateway(t, stake(20_000_000))
	require.Contains(t, s.rejected(t), "too many pending payloads")
	require.Len(t, s.pending(t, acc), int(p.MaxPendingPerAccount))

	// per block: one execution per EndBlock
	p.MaxPendingExecsPerBlock = 1
	require.NoError(t, s.k.SetParams(s.ctx, p))
	s.warpIn(t, s.token, s.lunaRtr, s.derived, 10_000_000)
	s.warpIn(t, s.token, s.lunaRtr, s.derived, 10_000_001)
	s.fresh()
	require.NoError(t, s.k.EndBlocker(s.ctx))
	require.Equal(t, 1, s.count("terra.remote.v1.EventRemoteExecuted"))
	require.Len(t, s.pending(t, acc), 2)
	s.fresh()
	require.NoError(t, s.k.EndBlocker(s.ctx))
	require.Equal(t, 1, s.count("terra.remote.v1.EventRemoteExecuted"))
	require.Len(t, s.pending(t, acc), 1)

	// expiry: the last one never gets its deposit
	s.advance(p.PendingTtlBlocks+1, time.Hour)
	s.fresh()
	require.NoError(t, s.k.EndBlocker(s.ctx))
	require.Contains(t, s.rejected(t), "deposit not received")
	require.Empty(t, s.pending(t, acc))
	// a deposit after the expiry never revives it
	s.warpIn(t, s.token, s.lunaRtr, s.derived, 10_000_002)
	s.fresh()
	require.NoError(t, s.k.EndBlocker(s.ctx))
	require.Equal(t, 0, s.count("terra.remote.v1.EventRemoteExecuted"))
	// credits expire too
	s.advance(p.PendingTtlBlocks+1, time.Hour)
	require.NoError(t, s.k.EndBlocker(s.ctx))
	r, err := s.k.RemoteStaking(s.ctx, acc)
	require.NoError(t, err)
	require.Empty(t, r.Credits)

	// no credit is recorded for an origin without a gateway
	require.NoError(t, s.k.SetGateway(s.ctx, s.appId, originDom, util.NewZeroAddress(), util.NewZeroAddress(), nil))
	s.fresh()
	s.warpIn(t, s.token, s.lunaRtr, s.derived, 1_000_000)
	require.Equal(t, 0, s.count("terra.remote.v1.EventDepositCredited"))

	// absolute maxima in code
	p.MaxPendingPerAccount = types.MaxPendingPerAccountAbsolute + 1
	require.Error(t, p.Validate())
	p = types.DefaultParams()
	p.PendingTtlBlocks = types.MaxPendingTTLBlocksAbsolute + 1
	require.Error(t, p.Validate())
	p = types.DefaultParams()
	p.MaxPendingExecsPerBlock = types.MaxPendingExecsPerBlockAbsolute + 1
	require.Error(t, p.Validate())
	p = types.DefaultParams()
	p.MaxAutoReturnsPerEpoch = types.MaxAutoReturnsPerEpochAbsolute + 1
	require.Error(t, p.Validate())
}

// Full flow through real warp routes (spec §2.1/§2.2): stake → stLUNC back on
// the origin, then instant unstake → LUNC back on the origin, both with the
// payload arriving before its deposit.
func TestRemoteStakeAndInstantUnstakeFullFlow(t *testing.T) {
	s := setupStaking(t)
	acc := s.derived
	lunaBefore := s.bal(acc, "uluna")
	const amt = 250_000_000
	// 1. GW.stake: payload first, then the LUNC deposit
	s.fromGateway(t, s.adPayload(t, &types.AfterDeposit{TokenId: s.token, Amount: math.NewInt(amt)},
		&lstypes.MsgStake{Sender: acc.String(), Amount: luna(amt)}, s.refWithdraw(s.stToken, amount(amt))))
	s.warpIn(t, s.token, s.lunaRtr, acc, amt)
	require.Equal(t, lunaBefore.AddRaw(amt).String(), s.bal(acc, "uluna").String(), "the collateral was released to the account")
	s.fresh()
	require.NoError(t, s.k.EndBlocker(s.ctx))
	require.Empty(t, s.rejected(t))
	require.Equal(t, lunaBefore.String(), s.bal(acc, "uluna").String())
	require.True(t, s.bal(acc, lstypes.StDenom).IsZero())
	stLedger, _, err := s.app.WarpLedgerKeeper.GetLedger(s.ctx, s.stToken, originDom)
	require.NoError(t, err)
	require.Equal(t, "250000000", stLedger.Sent.String(), "stLUNC in flight to the origin = minted")
	require.True(t, s.hasEvent("hyperlane.warp.v1.EventSendRemoteTransfer"))
	require.NotEmpty(t, s.attr("terra.remote.v1.EventRemoteStake", "withdrawal_message_id"))

	// 2. GW.unstake: payload first, then the stLUNC deposit (instant: the buffer holds it)
	lunaLedger, _, err := s.app.WarpLedgerKeeper.GetLedger(s.ctx, s.token, originDom)
	require.NoError(t, err)
	sentBefore := lunaLedger.Sent
	s.fresh()
	s.fromGateway(t, s.adPayload(t, &types.AfterDeposit{TokenId: s.stToken, Amount: math.NewInt(amt)},
		&lstypes.MsgUnstake{Sender: acc.String(), Amount: st(amt)}, s.refWithdraw(s.token, amount(amt))))
	require.Equal(t, 1, s.count("terra.remote.v1.EventRemotePending"))
	s.warpIn(t, s.stToken, s.stRtr, acc, amt)
	require.Equal(t, "250000000", s.bal(acc, lstypes.StDenom).String())
	require.NoError(t, s.k.EndBlocker(s.ctx))
	require.Empty(t, s.rejected(t))
	require.True(t, s.bal(acc, lstypes.StDenom).IsZero())
	require.Equal(t, lunaBefore.String(), s.bal(acc, "uluna").String(), "nothing stranded on the account")
	lunaLedger, _, _ = s.app.WarpLedgerKeeper.GetLedger(s.ctx, s.token, originDom)
	require.Equal(t, sentBefore.AddRaw(amt).String(), lunaLedger.Sent.String(), "LUNC back to the controller's domain")
	require.Equal(t, "true", s.attr("terra.remote.v1.EventRemoteUnstake", "instant"))
	stLedger, _, _ = s.app.WarpLedgerKeeper.GetLedger(s.ctx, s.stToken, originDom)
	require.True(t, stLedger.Sent.Sub(stLedger.Received).IsZero(), "the stLUNC route is back to zero exposure")
	require.Equal(t, localStake, s.app.BankKeeper.GetSupply(s.ctx, lstypes.StDenom).Amount.Int64(), "only the local staker's stLUNC remains")
}

// queuedRequest stakes and queues an unstake for a remote account of the
// origin, through plain payloads, and opts it into the auto-return.
func (s *stakingFixture) queuedRequest(t *testing.T, controller util.HexAddress, returnToken util.HexAddress) sdk.AccAddress {
	t.Helper()
	acc := types.DeriveAddress(originDom, controller)
	require.NoError(t, s.app.BankKeeper.SendCoins(s.ctx, s.owner, acc, sdk.NewCoins(luna(200_000_000))))
	s.deliver(t, controller, s.payload(t, nil,
		&lstypes.MsgStake{Sender: acc.String(), Amount: luna(100_000_000)},
		&types.MsgSetAutoReturn{Controller: acc.String(), Enabled: true, TokenId: returnToken},
	))
	require.Empty(t, s.rejected(t))
	return acc
}

func (s *stakingFixture) unstakeAll(t *testing.T, controller util.HexAddress) {
	t.Helper()
	acc := types.DeriveAddress(originDom, controller)
	s.deliver(t, controller, s.payload(t, nil, &lstypes.MsgUnstake{Sender: acc.String(), Amount: st(100_000_000)}))
	require.Empty(t, s.rejected(t))
}

// mature runs the undelegation batch and completes the unbonding.
func (s *stakingFixture) mature(t *testing.T) {
	t.Helper()
	s.epoch(t)
	ubd, err := s.app.StakingKeeper.UnbondingTime(s.ctx)
	require.NoError(t, err)
	s.advance(1, ubd+time.Second)
	_, err = s.app.StakingKeeper.CompleteUnbonding(s.ctx, s.app.LiquidStakeKeeper.ModuleAddress(), s.valAddr)
	require.NoError(t, err)
}

// §4.4: matured requests of opted-in accounts return to the controller at the
// epoch, bounded by max_auto_returns_per_epoch; opted-out accounts are left alone.
func TestAutoReturnBoundedPerEpoch(t *testing.T) {
	s := setupStaking(t)
	ctrls := []util.HexAddress{util.CreateMockHexAddress("evm-user", 11), util.CreateMockHexAddress("evm-user", 12), util.CreateMockHexAddress("evm-user", 13)}
	accs := make([]sdk.AccAddress, len(ctrls))
	for i, c := range ctrls {
		accs[i] = s.queuedRequest(t, c, s.token)
	}
	// the third opts out again
	s.deliver(t, ctrls[2], s.payload(t, nil, &types.MsgSetAutoReturn{Controller: accs[2].String(), Enabled: false}))
	require.Empty(t, s.rejected(t))
	s.epoch(t) // delegates: the unstakes below are queued
	for _, c := range ctrls {
		s.unstakeAll(t, c)
	}
	p, err := s.k.GetParams(s.ctx)
	require.NoError(t, err)
	p.MaxAutoReturnsPerEpoch = 1
	require.NoError(t, s.k.SetParams(s.ctx, p))
	s.mature(t)

	returned := func() int {
		n := 0
		for _, a := range accs {
			ws, err := s.k.WithdrawalsOf(s.ctx, a.String())
			require.NoError(t, err)
			n += len(ws)
		}
		return n
	}
	require.Equal(t, 0, returned())
	s.fresh()
	s.epoch(t)
	require.Equal(t, 1, returned(), "one return per epoch")
	require.Equal(t, "true", s.attr("terra.remote.v1.EventRemoteClaim", "auto"))
	// not again within the same epoch
	require.NoError(t, s.k.EndBlocker(s.ctx))
	require.Equal(t, 1, returned())
	s.epoch(t)
	require.Equal(t, 2, returned(), "the next opted-in account at the next epoch")
	s.epoch(t)
	require.Equal(t, 2, returned(), "the opted-out account keeps its claim for a manual MsgClaim")
	for _, a := range accs[:2] {
		ws, _ := s.k.WithdrawalsOf(s.ctx, a.String())
		require.Len(t, ws, 1)
		require.Equal(t, "100000000", ws[0].Amount.String())
	}
	n := 0
	require.NoError(t, s.app.LiquidStakeKeeper.Requests.Walk(s.ctx, nil, func(_ uint64, r lstypes.UnstakeRequest) (bool, error) {
		require.Equal(t, accs[2].String(), r.Address)
		n++
		return false, nil
	}))
	require.Equal(t, 1, n)
	// an opt-in needs a route to the account's domain
	s.fresh()
	s.deliver(t, ctrls[2], s.payload(t, nil, &types.MsgSetAutoReturn{Controller: accs[2].String(), Enabled: true, TokenId: util.CreateMockHexAddress("x", 1)}))
	require.Contains(t, s.rejected(t), "no router")
}

// §4.4: the paymaster advances the interchain gas of auto-returns only within
// paymaster_daily_cap per period; the rest waits for a later epoch.
func TestAutoReturnPaymasterCap(t *testing.T) {
	s := setupStaking(t)
	owner := s.owner.String()
	// a LUNC route on a mailbox whose IGP quotes 2,500,000 uluna per transfer
	pdSrv := pdkeeper.NewMsgServerImpl(&s.app.HyperlaneKeeper.PostDispatchKeeper)
	igp, err := pdSrv.CreateIgp(s.ctx, &pdtypes.MsgCreateIgp{Owner: owner, Denom: "uluna"})
	require.NoError(t, err)
	_, err = pdSrv.SetDestinationGasConfig(s.ctx, &pdtypes.MsgSetDestinationGasConfig{
		Owner: owner, IgpId: igp.Id,
		DestinationGasConfig: &pdtypes.DestinationGasConfig{
			RemoteDomain: originDom,
			GasOracle:    &pdtypes.GasOracle{TokenExchangeRate: pdtypes.TokenExchangeRateScale, GasPrice: math.NewInt(10)},
			GasOverhead:  math.NewInt(50_000),
		},
	})
	require.NoError(t, err)
	noop, err := pdSrv.CreateNoopHook(s.ctx, &pdtypes.MsgCreateNoopHook{Owner: owner})
	require.NoError(t, err)
	ism, err := ismkeeper.NewMsgServerImpl(&s.app.HyperlaneKeeper.IsmKeeper).CreateNoopIsm(s.ctx, &ismtypes.MsgCreateNoopIsm{Creator: owner})
	require.NoError(t, err)
	mb, err := corekeeper.NewMsgServerImpl(s.app.HyperlaneKeeper).CreateMailbox(s.ctx, &coretypes.MsgCreateMailbox{
		Owner: owner, LocalDomain: localDomain, DefaultIsm: ism.Id, DefaultHook: &noop.Id, RequiredHook: &igp.Id,
	})
	require.NoError(t, err)
	warpSrv := warpkeeper.NewMsgServerImpl(s.app.WarpKeeper)
	tok, err := warpSrv.CreateCollateralToken(s.ctx, &warptypes.MsgCreateCollateralToken{Owner: owner, OriginMailbox: mb.Id, OriginDenom: "uluna"})
	require.NoError(t, err)
	_, err = warpSrv.EnrollRemoteRouter(s.ctx, &warptypes.MsgEnrollRemoteRouter{
		Owner: owner, TokenId: tok.Id,
		RemoteRouter: &warptypes.RemoteRouter{ReceiverDomain: originDom, ReceiverContract: util.CreateMockHexAddress("router", 3), Gas: math.NewInt(200_000)},
	})
	require.NoError(t, err)
	require.NoError(t, s.app.WarpLedgerKeeper.SetDomainCap(s.ctx, tok.Id, originDom, math.NewInt(1_000_000_000_000)))
	const quote = int64(2_500_000)

	ctrls := []util.HexAddress{util.CreateMockHexAddress("evm-user", 21), util.CreateMockHexAddress("evm-user", 22)}
	accs := make([]sdk.AccAddress, len(ctrls))
	for i, c := range ctrls {
		accs[i] = s.queuedRequest(t, c, tok.Id)
	}
	s.epoch(t)
	for _, c := range ctrls {
		s.unstakeAll(t, c)
	}
	p, err := s.k.GetParams(s.ctx)
	require.NoError(t, err)
	p.PaymasterDailyCap = luna(quote) // one return per period
	require.NoError(t, s.k.SetParams(s.ctx, p))
	s.mature(t)

	returned := func() int {
		n := 0
		for _, a := range accs {
			ws, _ := s.k.WithdrawalsOf(s.ctx, a.String())
			n += len(ws)
		}
		return n
	}
	pm := types.PaymasterAddress()
	pmBefore := s.bal(pm, "uluna")
	s.epoch(t)
	require.Equal(t, 1, returned(), "the cap funds one return")
	require.Equal(t, pmBefore.SubRaw(quote).String(), s.bal(pm, "uluna").String(), "the paymaster advanced exactly the quote")
	s.epoch(t)
	require.Equal(t, 1, returned(), "same period: the second waits, unclaimed")
	// next period
	s.advance(0, 24*time.Hour)
	s.epoch(t)
	require.Equal(t, 2, returned())
	for _, a := range accs {
		require.Equal(t, "100000000", s.bal(a, "uluna").String(), "the claim left through warp, the gas came from the paymaster")
	}

	// an empty paymaster funds nothing: the request stays claimable
	acc3 := s.queuedRequest(t, util.CreateMockHexAddress("evm-user", 23), tok.Id)
	s.epoch(t)
	s.unstakeAll(t, util.CreateMockHexAddress("evm-user", 23))
	s.mature(t)
	require.NoError(t, s.app.BankKeeper.SendCoins(s.ctx, pm, s.owner, s.app.BankKeeper.GetAllBalances(s.ctx, pm)))
	s.advance(0, 24*time.Hour)
	s.epoch(t)
	ws, _ := s.k.WithdrawalsOf(s.ctx, acc3.String())
	require.Empty(t, ws)
	_, _, err = s.app.LiquidStakeKeeper.Claim(s.ctx, acc3)
	require.NoError(t, err, "never lost: the user can still claim manually")
}

// Regression (testenv, 2026-10-05): NewKeeper registers &k with the Hyperlane
// app router and returns a copy; SetBeaconSources runs later on the app's copy.
// A gateway payload whose deposit was already credited executes on arrival in
// Handle of the ROUTER's copy, which must see x/liquidstake and x/warpledger
// too (before the fix: "result ...stluna is not the denom 0x... of the warp
// token"). The payload goes through MsgProcessMessage, not s.k.Handle.
func TestStakeOnArrivalThroughTheMailboxRouter(t *testing.T) {
	s := setupStaking(t)
	acc := s.derived
	s.warpIn(t, s.token, s.lunaRtr, acc, 100_000_000) // deposit first: credited
	s.fresh()
	body := s.adPayload(t, &types.AfterDeposit{TokenId: s.token, Amount: math.NewInt(100_000_000)},
		&lstypes.MsgStake{Sender: acc.String(), Amount: luna(100_000_000)},
		s.refWithdraw(s.stToken, amount(1)),
	)
	s.nonce++
	msg := util.HyperlaneMessage{
		Version: 3, Nonce: s.nonce, Origin: originDom, Sender: s.gateway, Destination: localDomain, Recipient: s.appId, Body: body,
	}
	process := &coretypes.MsgProcessMessage{MailboxId: s.mailbox, Relayer: s.owner.String(), Metadata: "0x00", Message: msg.String()}
	res, err := s.app.MsgServiceRouter().Handler(process)(s.ctx, process)
	require.NoError(t, err)
	s.ctx.EventManager().EmitEvents(res.GetEvents())
	require.Empty(t, s.rejected(t))
	require.Equal(t, 0, s.count("terra.remote.v1.EventRemotePending"), "the deposit was credited: executed on arrival")
	require.Equal(t, "100000000uluna", s.attr("terra.remote.v1.EventRemoteStake", "uluna"))
	require.True(t, s.bal(acc, lstypes.StDenom).IsZero(), "the minted stLUNC left through warp")
	ledger, _, err := s.app.WarpLedgerKeeper.GetLedger(s.ctx, s.stToken, originDom)
	require.NoError(t, err)
	require.Equal(t, s.attr("terra.remote.v1.EventRemoteStake", "minted"), ledger.Sent.String()+lstypes.StDenom)
}
