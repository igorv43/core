package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	perptypes "github.com/classic-terra/core/v4/x/perp/types"
	"github.com/classic-terra/core/v4/x/remote/keeper"
	"github.com/classic-terra/core/v4/x/remote/types"
	warpledgertypes "github.com/classic-terra/core/v4/x/warpledger/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// basketToken creates a synthetic settlement basket token (spec §11.4 D-29)
// routed to the vault domain originDom and returns its id and denom.
func (f *fixture) basketToken(t *testing.T) (util.HexAddress, string) {
	t.Helper()
	warpSrv := warpkeeper.NewMsgServerImpl(f.app.WarpKeeper)
	tok, err := warpSrv.CreateSyntheticToken(f.ctx, &warptypes.MsgCreateSyntheticToken{Owner: f.owner.String(), OriginMailbox: f.mailbox})
	require.NoError(t, err)
	_, err = warpSrv.EnrollRemoteRouter(f.ctx, &warptypes.MsgEnrollRemoteRouter{
		Owner: f.owner.String(), TokenId: tok.Id,
		RemoteRouter: &warptypes.RemoteRouter{ReceiverDomain: originDom, ReceiverContract: util.CreateMockHexAddress("usdc-router", 1), Gas: math.ZeroInt()},
	})
	require.NoError(t, err)
	require.NoError(t, f.app.WarpLedgerKeeper.SetDomainCap(f.ctx, tok.Id, originDom, math.NewInt(1_000_000_000_000)))
	require.NoError(t, f.app.WarpLedgerKeeper.SetBasketToken(f.ctx, tok.Id, true))
	token, err := f.app.WarpLedgerKeeper.GetToken(f.ctx, tok.Id)
	require.NoError(t, err)
	return tok.Id, token.OriginDenom
}

// enroll adds a router of the token for the domain through the app's message
// router, i.e. through the custom/warp wrapper and its port guard.
func (f *fixture) enroll(token util.HexAddress, domain uint32) error {
	msg := &warptypes.MsgEnrollRemoteRouter{
		Owner: f.owner.String(), TokenId: token,
		RemoteRouter: &warptypes.RemoteRouter{ReceiverDomain: domain, ReceiverContract: util.CreateMockHexAddress("router", int64(domain)), Gas: math.ZeroInt()},
	}
	_, err := f.app.MsgServiceRouter().Handler(msg)(f.ctx, msg)
	return err
}

// portUser registers a remote account of the port domain holding the coins.
func (f *fixture) portUser(t *testing.T, coins sdk.Coins) (util.HexAddress, sdk.AccAddress) {
	t.Helper()
	user := util.CreateMockHexAddress("solana-user", 7) // 32-byte pubkey as-is
	acc := types.DeriveAddress(portSo, user)
	require.NoError(t, f.k.Accounts.Set(f.ctx, acc.String(), types.RemoteAccount{Address: acc.String(), Domain: portSo, Controller: user}))
	require.NoError(t, f.app.BankKeeper.SendCoins(f.ctx, f.owner, acc, coins))
	return user, acc
}

func sentTo(t *testing.T, f *fixture, token util.HexAddress, domain uint32) string {
	t.Helper()
	l, _, err := f.app.WarpLedgerKeeper.GetLedger(f.ctx, token, domain)
	require.NoError(t, err)
	return l.Sent.String()
}

// TestPortWithdrawRouting pins the v0.9.11 rule of spec §11.6.2: on a port
// domain a token routed to that domain (LUNC, stLUNC) withdraws directly;
// the settlement basket token, which has no route to a port, goes to the
// vault with the CCTP exit (port sentinel); any other token is refused.
func TestPortWithdrawRouting(t *testing.T) {
	f, gateway, factory, initCodeHash := exitFixture(t)
	gov := f.k.GetAuthority()
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(1_000_000_000))))
	usdc, usdcDenom := f.basketToken(t)
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(100_000_000)))))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", f.owner, sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(100_000_000)))))

	// LUNC (f.token) is routed to the port domain; the settlement token is not
	require.NoError(t, f.enroll(f.token, portSo))
	require.NoError(t, f.app.WarpLedgerKeeper.SetDomainCap(f.ctx, f.token, portSo, math.NewInt(1_000_000_000_000)))
	require.NoError(t, f.k.SetPort(f.ctx, &types.MsgSetPort{Authority: gov, PortDomain: portSo, VaultDomain: originDom, CctpDomain: 5}))
	// a 1 % withdrawal fee on the vault: charged only on what leaves through it
	require.NoError(t, f.k.Executors.Set(f.ctx, originDom, types.Executor{
		AppId: f.appId, Domain: originDom, Address: util.CreateMockHexAddress("executor", 1), TokenId: usdc,
		MinCollateral: math.ZeroInt(), TargetCollateral: math.NewInt(1), WithdrawFeeBps: 100, NetRebalanced: math.ZeroInt(),
	}))
	user, acc := f.portUser(t, sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(300_000_000)), sdk.NewCoin(usdcDenom, math.NewInt(50_000_000))))

	// 1. LUNC goes directly to the port chain, to the controller, with no exit,
	// no receipt and no vault fee: exact amount on the port domain's ledger
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	_, err := f.k.Withdraw(f.ctx, acc.String(), f.token, math.NewInt(123_456_789), "", nil)
	require.NoError(t, err)
	require.Equal(t, "123456789", sentTo(t, f, f.token, portSo))
	require.Equal(t, "0", sentTo(t, f, f.token, originDom), "nothing through the vault")
	rs, err := f.k.ReceiptsOf(f.ctx, acc.String())
	require.NoError(t, err)
	require.Empty(t, rs, "a direct withdrawal writes no conversion receipt")
	ws, err := f.k.WithdrawalsOf(f.ctx, acc.String())
	require.NoError(t, err)
	require.Len(t, ws, 1)
	require.Equal(t, "123456789", ws[0].Amount.String())
	require.True(t, eventAttr(f, "terra.remote.v1.EventRemoteWithdraw", "recipient", user.String()), "recipient is the controller")
	require.True(t, eventAttr(f, "terra.remote.v1.EventRemoteWithdraw", "domain", "1399811149"))
	require.Equal(t, "176543211", f.app.BankKeeper.GetBalance(f.ctx, acc, "uluna").Amount.String())

	// 2. the settlement token goes to the vault and leaves by CCTP: the 1 %
	// fee (400,000 of 40,000,000) is burned, 39,600,000 cross to the vault; the
	// exit's floor is 39,600,000 - ceil(39,600 CCTP fee allowance at 10 bps) =
	// 39,560,400; the exit pays the controller on the port chain
	supplyBefore := f.app.BankKeeper.GetSupply(f.ctx, usdcDenom).Amount
	id, err := f.k.Withdraw(f.ctx, acc.String(), usdc, math.NewInt(40_000_000), "", nil)
	require.NoError(t, err)
	require.Equal(t, "40000000", supplyBefore.Sub(f.app.BankKeeper.GetSupply(f.ctx, usdcDenom).Amount).String(), "fee burned + amount burned by warp")
	require.Equal(t, "39600000", sentTo(t, f, usdc, originDom))
	require.Equal(t, "10000000", f.app.BankKeeper.GetBalance(f.ctx, acc, usdcDenom).Amount.String())
	rs, err = f.k.ReceiptsOf(f.ctx, acc.String())
	require.NoError(t, err)
	require.Len(t, rs, 1)
	require.Equal(t, id, rs[0].MessageId)
	require.Equal(t, uint32(originDom), rs[0].OriginDomain, "the vault serves it")
	require.Equal(t, types.PortSentinel(portSo).String(), rs[0].TokenOut)
	require.Equal(t, "39560400", rs[0].MinAccepted)
	require.Equal(t, "39600000", rs[0].UsdcAmount)
	require.Equal(t, types.ExitAddress(factory, initCodeHash, user, types.PortSentinel(portSo), math.NewInt(39_560_400), 2), rs[0].ExitAddress)

	// the vault's confirmation of the CCTP exit carries port_domain and lands on the port account's
	// receipt (amount_out = what the exit burned to the port chain)
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	upd, err := f.app.AppCodec().Marshal(&types.RemotePayload{OnBehalfOf: user.Bytes(), PortDomain: portSo, Update: &types.ConversionUpdate{
		WithdrawMessageId: id.Bytes(), AmountOut: math.NewInt(39_600_000).BigInt().Bytes(), OriginBlock: 77,
	}})
	require.NoError(t, err)
	f.deliver(t, gateway, upd)
	require.Empty(t, f.rejected(t))
	rs, err = f.k.ReceiptsOf(f.ctx, acc.String())
	require.NoError(t, err)
	require.True(t, rs[0].Confirmed)
	require.Equal(t, "39600000", rs[0].AmountOut)
	// without port_domain it would address the vault-derived account, which has no such receipt
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	upd, err = f.app.AppCodec().Marshal(&types.RemotePayload{OnBehalfOf: user.Bytes(), Update: &types.ConversionUpdate{
		WithdrawMessageId: id.Bytes(), AmountOut: math.NewInt(1).BigInt().Bytes(),
	}})
	require.NoError(t, err)
	f.deliver(t, gateway, upd)
	require.Contains(t, f.rejected(t), "no receipt")

	// 3. a token with no route to the port and that is not the settlement
	// asset has no way to the port chain: refused, nothing moves
	other, err := warpkeeper.NewMsgServerImpl(f.app.WarpKeeper).CreateCollateralToken(f.ctx, &warptypes.MsgCreateCollateralToken{
		Owner: f.owner.String(), OriginMailbox: f.mailbox, OriginDenom: "uluna",
	})
	require.NoError(t, err)
	require.NoError(t, f.enroll(other.Id, originDom))
	_, err = f.k.Withdraw(f.ctx, acc.String(), other.Id, math.NewInt(1_000_000), "", nil)
	require.ErrorIs(t, err, types.ErrPortConflict)
	require.Equal(t, "176543211", f.app.BankKeeper.GetBalance(f.ctx, acc, "uluna").Amount.String())

	// the auto-return follows the same rule: LUNC is accepted (direct route),
	// the other token is refused
	require.NoError(t, f.k.SetAutoReturn(f.ctx, acc.String(), true, f.token))
	require.ErrorIs(t, f.k.SetAutoReturn(f.ctx, acc.String(), true, other.Id), types.ErrPortConflict)
}

// TestNonPortWithdrawUnchanged: an account of a domain that is not a port
// withdraws any routed token, the settlement token included, to its own
// domain, exactly as before v0.9.11.
func TestNonPortWithdrawUnchanged(t *testing.T) {
	f := setup(t)
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(1_000_000_000))))
	usdc, usdcDenom := f.basketToken(t)
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(7_000_001)))))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", f.derived, sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(7_000_001)))))
	// a port exists elsewhere: it does not affect this account
	require.NoError(t, f.k.SetPort(f.ctx, &types.MsgSetPort{Authority: f.k.GetAuthority(), PortDomain: portSo, VaultDomain: originDom, CctpDomain: 5}))
	f.deliver(t, f.controller, f.payload(t, nil, &perptypes.MsgSetAutoTopUp{Sender: f.derived.String(), Enabled: true}))

	_, err := f.k.Withdraw(f.ctx, f.derived.String(), usdc, math.NewInt(7_000_001), "", nil)
	require.NoError(t, err)
	require.Equal(t, "7000001", sentTo(t, f, usdc, originDom))
	_, err = f.k.Withdraw(f.ctx, f.derived.String(), f.token, math.NewInt(3), "", nil)
	require.NoError(t, err)
	require.Equal(t, "3", sentTo(t, f, f.token, originDom))
	rs, err := f.k.ReceiptsOf(f.ctx, f.derived.String())
	require.NoError(t, err)
	require.Empty(t, rs, "no exit without token_out")
}

// TestPortSettlementRouteRefused: a port is a chain without a vault of its own
// (spec §11.6 D-33, v0.9.11). MsgSetPort refuses a domain where a settlement
// basket token already has a route, a route of a basket token to a port
// domain cannot be enrolled, and a token routed to a port cannot become a
// basket. Routes of other tokens (LUNC, stLUNC) never conflict with a port.
func TestPortSettlementRouteRefused(t *testing.T) {
	f := setup(t)
	gov := f.k.GetAuthority()
	ms := keeper.NewMsgServerImpl(f.k)
	usdc, _ := f.basketToken(t)

	// the vault domain has a settlement route: it cannot also be a port
	_, err := ms.SetPort(f.ctx, &types.MsgSetPort{Authority: gov, PortDomain: originDom, VaultDomain: vaultB, CctpDomain: 6})
	require.ErrorIs(t, err, types.ErrPortConflict)
	// a domain with a LUNC route only is a valid port
	require.NoError(t, f.enroll(f.token, portSo))
	_, err = ms.SetPort(f.ctx, &types.MsgSetPort{Authority: gov, PortDomain: portSo, VaultDomain: originDom, CctpDomain: 5})
	require.NoError(t, err)
	ok, err := f.k.IsPort(f.ctx, portSo)
	require.NoError(t, err)
	require.True(t, ok)

	// the settlement token cannot be routed to the port afterwards
	require.ErrorIs(t, f.enroll(usdc, portSo), warpledgertypes.ErrSettlementToPort)
	_, err = f.app.WarpLedgerKeeper.GetRemoteRouter(f.ctx, usdc, portSo)
	require.Error(t, err, "nothing enrolled")
	// other tokens still can, and the basket can be routed to non-port domains
	require.NoError(t, f.enroll(f.token, vaultB))
	require.NoError(t, f.enroll(usdc, vaultB))

	// a token routed to the port cannot become a basket
	syn, err := warpkeeper.NewMsgServerImpl(f.app.WarpKeeper).CreateSyntheticToken(f.ctx, &warptypes.MsgCreateSyntheticToken{Owner: f.owner.String(), OriginMailbox: f.mailbox})
	require.NoError(t, err)
	require.NoError(t, f.enroll(syn.Id, portSo))
	require.ErrorIs(t, f.app.WarpLedgerKeeper.SetBasketToken(f.ctx, syn.Id, true), warpledgertypes.ErrSettlementToPort)

	// once the port is removed the settlement route can be enrolled, and the
	// domain can then no longer be registered as a port
	_, err = ms.SetPort(f.ctx, &types.MsgSetPort{Authority: gov, PortDomain: portSo})
	require.NoError(t, err)
	require.NoError(t, f.enroll(usdc, portSo))
	_, err = ms.SetPort(f.ctx, &types.MsgSetPort{Authority: gov, PortDomain: portSo, VaultDomain: originDom, CctpDomain: 5})
	require.ErrorIs(t, err, types.ErrPortConflict)
}

// eventAttr reports whether an event of the type carries the attribute value
// (JSON-quoted strings and numbers are matched as-is or quoted).
func eventAttr(f *fixture, typ, key, value string) bool {
	for _, ev := range f.ctx.EventManager().Events() {
		if ev.Type != typ {
			continue
		}
		for _, a := range ev.Attributes {
			if a.Key == key && (a.Value == value || a.Value == `"`+value+`"`) {
				return true
			}
		}
	}
	return false
}
