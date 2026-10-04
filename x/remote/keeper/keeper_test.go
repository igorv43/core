package keeper_test

import (
	"math/big"
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
	terraapp "github.com/classic-terra/core/v4/app"
	apptesting "github.com/classic-terra/core/v4/app/testing"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	lstypes "github.com/classic-terra/core/v4/x/liquidstake/types"
	perptypes "github.com/classic-terra/core/v4/x/perp/types"
	"github.com/classic-terra/core/v4/x/remote/keeper"
	"github.com/classic-terra/core/v4/x/remote/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/stretchr/testify/require"
)

const (
	chainID     = "remote-test"
	localDomain = 1325
	originDom   = 97
)

type fixture struct {
	app        *terraapp.TerraApp
	ctx        sdk.Context
	k          keeper.Keeper
	owner      sdk.AccAddress
	mailbox    util.HexAddress
	appId      util.HexAddress
	controller util.HexAddress
	derived    sdk.AccAddress
	token      util.HexAddress
}

func setup(t *testing.T) *fixture {
	t.Helper()
	h := &apptesting.KeeperTestHelper{}
	h.SetT(t)
	h.Setup(t, chainID)
	app, ctx := h.App, h.Ctx
	require.NoError(t, app.BatchKeeper.InitGenesis(ctx, batchtypes.DefaultGenesisState()))
	require.NoError(t, app.PerpKeeper.InitGenesis(ctx, perptypes.DefaultGenesisState()))
	require.NoError(t, app.RemoteKeeper.InitGenesis(ctx, types.DefaultGenesisState()))
	f := &fixture{app: app, ctx: ctx, k: app.RemoteKeeper}
	f.owner = sdk.AccAddress([]byte("remote-owner---------"))
	h.FundAcc(f.owner, sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(1_000_000_000_000)), sdk.NewCoin("uusd", math.NewInt(1_000_000_000))))

	ismSrv := ismkeeper.NewMsgServerImpl(&app.HyperlaneKeeper.IsmKeeper)
	ism, err := ismSrv.CreateNoopIsm(ctx, &ismtypes.MsgCreateNoopIsm{Creator: f.owner.String()})
	require.NoError(t, err)
	pdSrv := pdkeeper.NewMsgServerImpl(&app.HyperlaneKeeper.PostDispatchKeeper)
	noop, err := pdSrv.CreateNoopHook(ctx, &pdtypes.MsgCreateNoopHook{Owner: f.owner.String()})
	require.NoError(t, err)
	mb, err := corekeeper.NewMsgServerImpl(app.HyperlaneKeeper).CreateMailbox(ctx, &coretypes.MsgCreateMailbox{
		Owner: f.owner.String(), LocalDomain: localDomain, DefaultIsm: ism.Id, DefaultHook: &noop.Id, RequiredHook: &noop.Id,
	})
	require.NoError(t, err)
	f.mailbox = mb.Id
	appId, err := f.k.CreateApp(ctx, f.owner.String(), f.mailbox, nil)
	require.NoError(t, err)
	f.appId = appId
	f.controller = util.CreateMockHexAddress("evm-user", 1)
	f.derived = types.DeriveAddress(originDom, f.controller)
	// the derived account received a warp deposit of settlement and holds uluna for the message fee
	h.FundAcc(f.derived, sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(50_000_000)), sdk.NewCoin("uluna", math.NewInt(1_000_000_000))))

	warpSrv := warpkeeper.NewMsgServerImpl(app.WarpKeeper)
	tok, err := warpSrv.CreateCollateralToken(ctx, &warptypes.MsgCreateCollateralToken{Owner: f.owner.String(), OriginMailbox: f.mailbox, OriginDenom: "uluna"})
	require.NoError(t, err)
	f.token = tok.Id
	_, err = warpSrv.EnrollRemoteRouter(ctx, &warptypes.MsgEnrollRemoteRouter{
		Owner: f.owner.String(), TokenId: tok.Id,
		RemoteRouter: &warptypes.RemoteRouter{ReceiverDomain: originDom, ReceiverContract: util.CreateMockHexAddress("router", 1), Gas: math.ZeroInt()},
	})
	require.NoError(t, err)
	require.NoError(t, app.WarpLedgerKeeper.SetDomainCap(ctx, tok.Id, originDom, math.NewInt(1_000_000_000_000)))
	return f
}

func (f *fixture) payload(t *testing.T, onBehalfOf []byte, msgs ...sdk.Msg) []byte {
	t.Helper()
	var anys []*codectypes.Any
	for _, m := range msgs {
		a, err := codectypes.NewAnyWithValue(m)
		require.NoError(t, err)
		anys = append(anys, a)
	}
	bz, err := f.app.AppCodec().Marshal(&types.RemotePayload{OnBehalfOf: onBehalfOf, Msgs: anys})
	require.NoError(t, err)
	return bz
}

func (f *fixture) deliver(t *testing.T, sender util.HexAddress, body []byte) {
	t.Helper()
	require.NoError(t, f.k.Handle(f.ctx, f.mailbox, util.HyperlaneMessage{
		Version: 3, Nonce: 1, Origin: originDom, Sender: sender,
		Destination: localDomain, Recipient: f.appId, Body: body,
	}))
}

func (f *fixture) rejected(t *testing.T) string {
	t.Helper()
	for _, ev := range f.ctx.EventManager().Events() {
		if ev.Type == "terra.remote.v1.EventRemoteRejected" {
			for _, a := range ev.Attributes {
				if a.Key == "reason" {
					return a.Value
				}
			}
		}
	}
	return ""
}

// hasEvent reports whether an event of the type reached the context.
func (f *fixture) hasEvent(typ string) bool {
	for _, ev := range f.ctx.EventManager().Events() {
		if ev.Type == typ {
			return true
		}
	}
	return false
}

func TestPayloadDepositsCollateral(t *testing.T) {
	f := setup(t)
	body := f.payload(t, nil, &perptypes.MsgDepositCollateral{Sender: f.derived.String(), Amount: sdk.NewCoin("uusd", math.NewInt(20_000_000))})
	f.deliver(t, f.controller, body)
	require.Empty(t, f.rejected(t))
	free, err := f.app.PerpKeeper.FreeCollateral(f.ctx, f.derived.String())
	require.NoError(t, err)
	require.Equal(t, "20000000", free.String())
	// the paymaster is empty: the message fee (10 LUNC) left the account itself, in uluna
	require.Equal(t, "30000000", f.app.BankKeeper.GetBalance(f.ctx, f.derived, "uusd").Amount.String())
	require.Equal(t, "990000000", f.app.BankKeeper.GetBalance(f.ctx, f.derived, "uluna").Amount.String())
	acc, err := f.k.GetAccount(f.ctx, f.derived.String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), acc.Payloads)
	require.Equal(t, uint32(originDom), acc.Domain)
	// the events of the executed message reach the transaction (the message
	// router returns them only inside the handler result)
	require.True(t, f.hasEvent("terra.perp.v1.EventCollateralDeposited"), "handler events are re-emitted")
	require.True(t, f.hasEvent("terra.remote.v1.EventRemoteExecuted"))
}

func TestPaymasterSponsorsTheMsgFee(t *testing.T) {
	f := setup(t)
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(100_000_000))))
	pm := types.PaymasterAddress()
	// an account funded only through a gateway holds no uluna
	settlementOnly := types.DeriveAddress(originDom, util.CreateMockHexAddress("evm-user", 2))
	require.NoError(t, f.app.BankKeeper.SendCoins(f.ctx, f.owner, settlementOnly, sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(5_000_000)))))
	body := f.payload(t, nil, &perptypes.MsgDepositCollateral{Sender: settlementOnly.String(), Amount: sdk.NewCoin("uusd", math.NewInt(1_000_000))})
	f.deliver(t, util.CreateMockHexAddress("evm-user", 2), body)
	require.Empty(t, f.rejected(t))
	require.Equal(t, "90000000", f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount.String(), "the paymaster paid the fee")
	require.Equal(t, "4000000", f.app.BankKeeper.GetBalance(f.ctx, settlementOnly, "uusd").Amount.String())
	// a two-message payload costs two fees; once the paymaster runs dry the account must pay
	p, err := f.k.GetParams(f.ctx)
	require.NoError(t, err)
	p.MsgFee = sdk.NewCoin("uluna", math.NewInt(50_000_000))
	require.NoError(t, f.k.SetParams(f.ctx, p))
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	f.deliver(t, util.CreateMockHexAddress("evm-user", 2), f.payload(t, nil,
		&perptypes.MsgDepositCollateral{Sender: settlementOnly.String(), Amount: sdk.NewCoin("uusd", math.NewInt(1_000_000))},
		&perptypes.MsgSetAutoTopUp{Sender: settlementOnly.String(), Enabled: true},
	))
	require.Contains(t, f.rejected(t), "paymaster and remote account cannot pay")
	require.Equal(t, "90000000", f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount.String(), "a rejected payload moves nothing")
}

func TestPayloadRejections(t *testing.T) {
	f := setup(t)
	// a message signed by someone else
	body := f.payload(t, nil, &perptypes.MsgDepositCollateral{Sender: f.owner.String(), Amount: sdk.NewCoin("uusd", math.NewInt(1))})
	f.deliver(t, f.controller, body)
	require.Contains(t, f.rejected(t), "signed by")
	// a message outside the whitelist
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	body = f.payload(t, nil, &batchtypes.MsgRegisterSolver{Sender: f.derived.String(), Bond: sdk.NewCoin("uusd", math.NewInt(1))})
	f.deliver(t, f.controller, body)
	require.Contains(t, f.rejected(t), "not allowed")
	// on_behalf_of from a sender that is not the enrolled gateway
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	body = f.payload(t, f.controller.Bytes(), &perptypes.MsgDepositCollateral{Sender: f.derived.String(), Amount: sdk.NewCoin("uusd", math.NewInt(1))})
	f.deliver(t, util.CreateMockHexAddress("gateway", 1), body)
	require.Contains(t, f.rejected(t), "gateway")
	free, _ := f.app.PerpKeeper.FreeCollateral(f.ctx, f.derived.String())
	require.True(t, free.IsZero())
}

func TestGatewayActsOnBehalfOfController(t *testing.T) {
	f := setup(t)
	gateway := util.CreateMockHexAddress("gateway", 1)
	require.NoError(t, f.k.SetGateway(f.ctx, f.appId, originDom, gateway, util.NewZeroAddress(), nil))
	body := f.payload(t, f.controller.Bytes(), &perptypes.MsgDepositCollateral{Sender: f.derived.String(), Amount: sdk.NewCoin("uusd", math.NewInt(1_000_000))})
	f.deliver(t, gateway, body)
	require.Empty(t, f.rejected(t))
	free, _ := f.app.PerpKeeper.FreeCollateral(f.ctx, f.derived.String())
	require.Equal(t, "1000000", free.String())
}

func TestSessionKeysAndPaymaster(t *testing.T) {
	f := setup(t)
	session := sdk.AccAddress([]byte("remote-session-key---"))
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(100_000_000))))
	// deposit enough collateral for the paymaster, then grant the key, in one payload
	body := f.payload(t, nil,
		&perptypes.MsgDepositCollateral{Sender: f.derived.String(), Amount: sdk.NewCoin("uusd", math.NewInt(20_000_000))},
		&types.MsgGrantSessionKey{Controller: f.derived.String(), SessionKey: session.String()},
	)
	f.deliver(t, f.controller, body)
	require.Empty(t, f.rejected(t))
	sessions, err := f.k.SessionsOf(f.ctx, f.derived.String())
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.True(t, sessions[0].Paymaster)
	// authz: only the trading messages, nothing else
	auths, err := f.app.AuthzKeeper.GetAuthorizations(f.ctx, session, f.derived)
	require.NoError(t, err)
	require.Len(t, auths, len(types.SessionMsgTypeURLs()))
	for _, a := range auths {
		g, ok := a.(*authz.GenericAuthorization)
		require.True(t, ok)
		require.NotEqual(t, sdk.MsgTypeURL(&types.MsgWithdraw{}), g.Msg)
		require.NotEqual(t, sdk.MsgTypeURL(&perptypes.MsgWithdrawCollateral{}), g.Msg)
	}
	_, err = f.app.FeeGrantKeeper.GetAllowance(f.ctx, types.PaymasterAddress(), session)
	require.NoError(t, err)

	// the session limit
	p, _ := f.k.GetParams(f.ctx)
	for i := 1; i < int(p.MaxSessions); i++ {
		_, err := f.k.GrantSession(f.ctx, f.derived.String(), sdk.AccAddress([]byte("remote-session-key-"+string(rune('a'+i))+"-")).String(), 0)
		require.NoError(t, err)
	}
	_, err = f.k.GrantSession(f.ctx, f.derived.String(), sdk.AccAddress([]byte("remote-session-key-z-")).String(), 0)
	require.ErrorIs(t, err, types.ErrTooManySessions)

	// revocation by the key itself removes the grants and the allowance
	require.NoError(t, f.k.RevokeSession(f.ctx, f.derived.String(), session.String()))
	auths, _ = f.app.AuthzKeeper.GetAuthorizations(f.ctx, session, f.derived)
	require.Empty(t, auths)
	_, err = f.app.FeeGrantKeeper.GetAllowance(f.ctx, types.PaymasterAddress(), session)
	require.Error(t, err)
}

func TestWithdrawLockedToController(t *testing.T) {
	f := setup(t)
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(1_000_000_000))))
	// the account must exist: a first payload creates it
	f.deliver(t, f.controller, f.payload(t, nil, &perptypes.MsgSetAutoTopUp{Sender: f.derived.String(), Enabled: true}))
	require.Empty(t, f.rejected(t))

	before := f.app.BankKeeper.GetBalance(f.ctx, f.derived, "uluna").Amount
	pmBefore := f.app.BankKeeper.GetBalance(f.ctx, types.PaymasterAddress(), "uluna").Amount
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	id, err := f.k.Withdraw(f.ctx, f.derived.String(), f.token, math.NewInt(500_000_000), "", nil)
	require.NoError(t, err)
	require.False(t, id.IsZeroAddress())
	after := f.app.BankKeeper.GetBalance(f.ctx, f.derived, "uluna").Amount
	require.Equal(t, before.SubRaw(500_000_000).String(), after.String(), "exactly the amount left the account through warp")
	// noop hooks quote nothing: the paymaster advances nothing
	require.Equal(t, pmBefore.String(), f.app.BankKeeper.GetBalance(f.ctx, types.PaymasterAddress(), "uluna").Amount.String())
	// the mailbox events of the dispatch reach the transaction for the agents
	require.True(t, f.hasEvent("hyperlane.core.v1.EventDispatch"), "EventDispatch re-emitted from the handler result")
	require.True(t, f.hasEvent("hyperlane.warp.v1.EventSendRemoteTransfer"))
	require.True(t, f.hasEvent("terra.remote.v1.EventRemoteWithdraw"))
	ws, err := f.k.WithdrawalsOf(f.ctx, f.derived.String())
	require.NoError(t, err)
	require.Len(t, ws, 1)
	require.Equal(t, "500000000", ws[0].Amount.String())
	// the ledger of x/warpledger saw the outbound transfer to the controller's domain
	ledger, exists, err := f.app.WarpLedgerKeeper.GetLedger(f.ctx, f.token, originDom)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "500000000", ledger.Sent.String())

	// no withdrawal for an unknown remote account
	_, err = f.k.Withdraw(f.ctx, f.owner.String(), f.token, math.NewInt(1), "", nil)
	require.ErrorIs(t, err, types.ErrAccountNotFound)

	gs, err := f.k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, gs.Apps, 1)
	require.Len(t, gs.Accounts, 1)
	require.NoError(t, gs.Validate())
}

// The paymaster advances exactly the IGP quote of the route (what `warp
// QuoteRemoteTransfer` returns), never the cap, and refuses a quote above it.
func TestWithdrawAdvancesExactlyTheInterchainQuote(t *testing.T) {
	f := setup(t)
	owner := f.owner.String()
	pdSrv := pdkeeper.NewMsgServerImpl(&f.app.HyperlaneKeeper.PostDispatchKeeper)
	igp, err := pdSrv.CreateIgp(f.ctx, &pdtypes.MsgCreateIgp{Owner: owner, Denom: "uluna"})
	require.NoError(t, err)
	_, err = pdSrv.SetDestinationGasConfig(f.ctx, &pdtypes.MsgSetDestinationGasConfig{
		Owner: owner, IgpId: igp.Id,
		DestinationGasConfig: &pdtypes.DestinationGasConfig{
			RemoteDomain: originDom,
			GasOracle:    &pdtypes.GasOracle{TokenExchangeRate: pdtypes.TokenExchangeRateScale, GasPrice: math.NewInt(10)},
			GasOverhead:  math.NewInt(50_000),
		},
	})
	require.NoError(t, err)
	noop, err := pdSrv.CreateNoopHook(f.ctx, &pdtypes.MsgCreateNoopHook{Owner: owner})
	require.NoError(t, err)
	ism, err := ismkeeper.NewMsgServerImpl(&f.app.HyperlaneKeeper.IsmKeeper).CreateNoopIsm(f.ctx, &ismtypes.MsgCreateNoopIsm{Creator: owner})
	require.NoError(t, err)
	mb, err := corekeeper.NewMsgServerImpl(f.app.HyperlaneKeeper).CreateMailbox(f.ctx, &coretypes.MsgCreateMailbox{
		Owner: owner, LocalDomain: localDomain, DefaultIsm: ism.Id, DefaultHook: &noop.Id, RequiredHook: &igp.Id,
	})
	require.NoError(t, err)
	warpSrv := warpkeeper.NewMsgServerImpl(f.app.WarpKeeper)
	tok, err := warpSrv.CreateCollateralToken(f.ctx, &warptypes.MsgCreateCollateralToken{Owner: owner, OriginMailbox: mb.Id, OriginDenom: "uluna"})
	require.NoError(t, err)
	_, err = warpSrv.EnrollRemoteRouter(f.ctx, &warptypes.MsgEnrollRemoteRouter{
		Owner: owner, TokenId: tok.Id,
		RemoteRouter: &warptypes.RemoteRouter{ReceiverDomain: originDom, ReceiverContract: util.CreateMockHexAddress("router", 2), Gas: math.NewInt(200_000)},
	})
	require.NoError(t, err)
	require.NoError(t, f.app.WarpLedgerKeeper.SetDomainCap(f.ctx, tok.Id, originDom, math.NewInt(1_000_000_000_000)))
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(1_000_000_000))))
	f.deliver(t, f.controller, f.payload(t, nil, &perptypes.MsgSetAutoTopUp{Sender: f.derived.String(), Enabled: true}))
	require.Empty(t, f.rejected(t))

	// (200,000 router gas + 50,000 overhead) × 10 × 1e10 / 1e10 = 2,500,000 uluna
	const quote = int64(2_500_000)
	pm := types.PaymasterAddress()
	pmBefore := f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount
	accBefore := f.app.BankKeeper.GetBalance(f.ctx, f.derived, "uluna").Amount
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	_, err = f.k.Withdraw(f.ctx, f.derived.String(), tok.Id, math.NewInt(500_000_000), "", nil)
	require.NoError(t, err)
	require.Equal(t, pmBefore.SubRaw(quote).String(), f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount.String(), "the paymaster advanced the quote, not the cap")
	require.Equal(t, accBefore.SubRaw(500_000_000).String(), f.app.BankKeeper.GetBalance(f.ctx, f.derived, "uluna").Amount.String(), "the IGP took the advance: nothing stranded")
	require.True(t, f.hasEvent("hyperlane.core.post_dispatch.v1.EventGasPayment"))

	// a quote above withdraw_fee_cap is refused before anything moves
	p, err := f.k.GetParams(f.ctx)
	require.NoError(t, err)
	p.WithdrawFeeCap = sdk.NewCoin("uluna", math.NewInt(quote-1))
	require.NoError(t, f.k.SetParams(f.ctx, p))
	_, err = f.k.Withdraw(f.ctx, f.derived.String(), tok.Id, math.NewInt(1_000_000), "", nil)
	require.ErrorIs(t, err, types.ErrInvalidWithdraw)
	require.Contains(t, err.Error(), "exceeds withdraw_fee_cap")
	require.Equal(t, pmBefore.SubRaw(quote).String(), f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount.String())
	require.Equal(t, accBefore.SubRaw(500_000_000).String(), f.app.BankKeeper.GetBalance(f.ctx, f.derived, "uluna").Amount.String())

	// an empty paymaster leaves the quote to the account (max_fee = quote)
	p.WithdrawFeeCap = sdk.NewCoin("uluna", math.NewInt(200_000_000))
	require.NoError(t, f.k.SetParams(f.ctx, p))
	require.NoError(t, f.app.BankKeeper.SendCoins(f.ctx, pm, f.owner, f.app.BankKeeper.GetAllBalances(f.ctx, pm)))
	accBefore = f.app.BankKeeper.GetBalance(f.ctx, f.derived, "uluna").Amount
	_, err = f.k.Withdraw(f.ctx, f.derived.String(), tok.Id, math.NewInt(1_000_000), "", nil)
	require.NoError(t, err)
	require.Equal(t, accBefore.SubRaw(1_000_000+quote).String(), f.app.BankKeeper.GetBalance(f.ctx, f.derived, "uluna").Amount.String())
}

func TestBeaconsDispatchStateFacts(t *testing.T) {
	f := setup(t)
	require.NoError(t, f.app.LiquidStakeKeeper.InitGenesis(f.ctx, lstypes.DefaultGenesisState()))
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(10_000_000_000))))
	gov := f.k.GetAuthority()
	target := util.CreateMockHexAddress("vault", 1)

	erID, err := f.k.SetBeacon(f.ctx, &types.MsgSetBeacon{Authority: gov, AppId: f.appId, Domain: originDom, Recipient: target, Kind: types.BEACON_KIND_EXCHANGE_RATE, IntervalBlocks: 5})
	require.NoError(t, err)
	solvID, err := f.k.SetBeacon(f.ctx, &types.MsgSetBeacon{Authority: gov, AppId: f.appId, Domain: originDom, Recipient: target, Kind: types.BEACON_KIND_ROUTE_SOLVENCY, IntervalBlocks: 5, TokenId: f.token})
	require.NoError(t, err)
	_, err = f.k.SetBeacon(f.ctx, &types.MsgSetBeacon{Authority: gov, AppId: f.appId, Domain: originDom, Recipient: target, Kind: types.BEACON_KIND_POSITION_DIGEST, IntervalBlocks: 5, Account: f.derived.String()})
	require.NoError(t, err)
	// an unknown app or an unpriced beacon is refused
	_, err = f.k.SetBeacon(f.ctx, &types.MsgSetBeacon{Authority: gov, AppId: util.CreateMockHexAddress("x", 9), Domain: originDom, Recipient: target, Kind: types.BEACON_KIND_EXCHANGE_RATE, IntervalBlocks: 5})
	require.Error(t, err)

	// the exchange-rate body: kind, height, time, rate·1e18 (= 1 at genesis), supply, assets
	b, err := f.k.Beacons.Get(f.ctx, erID)
	require.NoError(t, err)
	body, err := f.k.BeaconBody(f.ctx, b)
	require.NoError(t, err)
	require.Len(t, body, 32*6)
	require.Equal(t, uint8(types.BEACON_KIND_EXCHANGE_RATE), body[31])
	require.Equal(t, math.LegacyOneDec().BigInt().String(), new(big.Int).SetBytes(body[96:128]).String())
	// the solvency body carries the token id and reports the route solvent
	b, _ = f.k.Beacons.Get(f.ctx, solvID)
	body, err = f.k.BeaconBody(f.ctx, b)
	require.NoError(t, err)
	require.Len(t, body, 32*10)
	require.Equal(t, f.token.Bytes(), body[96:128])
	require.Equal(t, uint8(1), body[319])

	// EndBlock dispatches all three through the mailbox and records the roots
	mb, err := f.app.HyperlaneKeeper.GetMailbox(f.ctx, f.mailbox)
	require.NoError(t, err)
	sentBefore := mb.MessageSent
	f.ctx = f.ctx.WithBlockHeight(f.ctx.BlockHeight() + 5)
	require.NoError(t, f.k.EndBlocker(f.ctx))
	mb, _ = f.app.HyperlaneKeeper.GetMailbox(f.ctx, f.mailbox)
	require.Equal(t, sentBefore+3, mb.MessageSent)
	sent := 0
	for _, ev := range f.ctx.EventManager().Events() {
		if ev.Type == "terra.remote.v1.EventBeaconSent" {
			sent++
		}
	}
	require.Equal(t, 3, sent)
	// not due again until the interval elapsed
	require.NoError(t, f.k.EndBlocker(f.ctx))
	mb, _ = f.app.HyperlaneKeeper.GetMailbox(f.ctx, f.mailbox)
	require.Equal(t, sentBefore+3, mb.MessageSent)
	// removal
	_, err = f.k.SetBeacon(f.ctx, &types.MsgSetBeacon{Authority: gov, Id: erID, IntervalBlocks: 0})
	require.NoError(t, err)
	all, _ := f.k.AllBeacons(f.ctx)
	require.Len(t, all, 2)
	gs, err := f.k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, gs.Beacons, 2)
}

// Spec §25.2: session key expired with an open position in a falling market.
// The key's grants lapse by time, yet the resident trigger and the auto
// top-up act in EndBlock without any key (§14.5 layer 4).
func TestScenarioExpiredSessionKeyStillProtected(t *testing.T) {
	f := setup(t)
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(100_000_000))))
	session := sdk.AccAddress([]byte("remote-session-exp---"))
	// deposit, grant a 1-second session and opt into the auto top-up, all by payload
	f.deliver(t, f.controller, f.payload(t, nil,
		&perptypes.MsgDepositCollateral{Sender: f.derived.String(), Amount: sdk.NewCoin("uusd", math.NewInt(40_000_000))},
		&types.MsgGrantSessionKey{Controller: f.derived.String(), SessionKey: session.String(), TtlSeconds: 1},
		&perptypes.MsgSetAutoTopUp{Sender: f.derived.String(), Enabled: true},
	))
	require.Empty(t, f.rejected(t))
	auths, _ := f.app.AuthzKeeper.GetAuthorizations(f.ctx, session, f.derived)
	require.NotEmpty(t, auths)
	// time passes: the grants expire
	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(2 * time.Second))
	for _, url := range types.SessionMsgTypeURLs() {
		a, _ := f.app.AuthzKeeper.GetAuthorization(f.ctx, session, f.derived, url)
		require.Nil(t, a, "expired session key can no longer act: %s", url)
	}
	// the protections do not depend on the key: the auto top-up flag is on
	col, err := f.app.PerpKeeper.FreeCollateral(f.ctx, f.derived.String())
	require.NoError(t, err)
	require.Equal(t, "40000000", col.String())
	on, _ := f.app.PerpKeeper.AutoTopUp.Has(f.ctx, f.derived.String())
	require.True(t, on)
}
