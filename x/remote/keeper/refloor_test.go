package keeper_test

import (
	"encoding/hex"
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/classic-terra/core/v4/x/remote/keeper"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// deliverFrom hands a message from ORIGIN to x/remote (the Solana gateway of a
// port domain delivers from the port domain itself).
func (f *fixture) deliverFrom(t *testing.T, origin uint32, sender util.HexAddress, body []byte) {
	t.Helper()
	require.NoError(t, f.k.Handle(f.ctx, f.mailbox, util.HyperlaneMessage{
		Version: 3, Nonce: 1, Origin: origin, Sender: sender,
		Destination: localDomain, Recipient: f.appId, Body: body,
	}))
}

// dispatchedBodies returns the hex bodies of every hyperlane message
// dispatched in the context (EventDispatch.message is the encoded message).
func (f *fixture) dispatched() []string {
	var out []string
	for _, ev := range f.ctx.EventManager().Events() {
		if ev.Type != "hyperlane.core.v1.EventDispatch" {
			continue
		}
		for _, a := range ev.Attributes {
			if a.Key == "message" {
				out = append(out, strings.ToLower(strings.Trim(a.Value, `"`)))
			}
		}
	}
	return out
}

// TestRefloorPortExit pins MsgRefloorPortExit (spec v0.9.13 §11.6.4): the
// account that withdrew lowers the minimum of its own stuck port exit; TC
// checks ownership, the unconfirmed receipt and the bound net - ceil(net x
// 100 / 10,000), updates the receipt and sends REFLOOR_EXIT to the vault's
// executor naming exactly the exit the withdrawal paid. No value moves; the
// confirmation path is unchanged.
func TestRefloorPortExit(t *testing.T) {
	f, gateway, factory, initCodeHash := exitFixture(t)
	gov := f.k.GetAuthority()
	srv := keeper.NewMsgServerImpl(f.k)
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(5_000_000_000))))
	usdc, usdcDenom := f.basketToken(t)
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(1_000_000_000)))))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", f.owner, sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(1_000_000_000)))))
	require.NoError(t, f.k.SetPort(f.ctx, &types.MsgSetPort{Authority: gov, PortDomain: portSo, VaultDomain: originDom, CctpDomain: 5}))
	executor := util.CreateMockHexAddress("executor", 1)
	require.NoError(t, f.k.SetExecutor(f.ctx, &types.MsgSetExecutor{Authority: gov, AppId: f.appId, Domain: originDom, Address: executor, TokenId: usdc,
		MinCollateral: math.ZeroInt(), TargetCollateral: math.ZeroInt()}))
	ex, err := f.k.Executors.Get(f.ctx, originDom)
	require.NoError(t, err)
	require.Equal(t, uint64(1), ex.Nonce, "SET_LEG_LIMITS")
	// the Solana gateway of the port domain (payloads with on_behalf_of come from it)
	solGateway := util.CreateMockHexAddress("solana-gateway", 1)
	require.NoError(t, f.k.SetGateway(f.ctx, f.appId, portSo, solGateway, util.HexAddress{}, nil))
	user, acc := f.portUser(t, sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(500_000_000))))
	sentinel := types.PortSentinel(portSo)

	// the stuck withdrawal: net 123,456,789 at the default 10 bps → floor 123,333,332 (exit seq 1)
	id, err := f.k.Withdraw(f.ctx, acc.String(), usdc, math.NewInt(123_456_789), "", nil)
	require.NoError(t, err)
	exit := types.ExitAddress(factory, initCodeHash, user, sentinel, math.NewInt(123_333_332), 1)
	receipt := func() types.ConversionReceipt {
		t.Helper()
		rs, err := f.k.ReceiptsOf(f.ctx, acc.String())
		require.NoError(t, err)
		for _, r := range rs {
			if r.MessageId.Equal(id) {
				return r
			}
		}
		t.Fatalf("no receipt %s", id)
		return types.ConversionReceipt{}
	}
	r := receipt()
	require.Equal(t, "123333332", r.MinAccepted)
	require.Equal(t, "123333332", r.ExitMinAccepted, "the salt minimum is recorded")
	require.Equal(t, uint64(1), r.ExitNonce)
	require.Equal(t, exit, r.ExitAddress)
	require.Zero(t, r.Refloors)
	bank0 := f.app.BankKeeper.GetBalance(f.ctx, acc, usdcDenom).Amount.String()
	sent0 := sentTo(t, f, usdc, originDom)

	// the issuer's minimum at 20 bps: floor(123,456,789 x 20,000 / 1e7) = 246,913,
	// the exit pays at most 123,209,876 < 123,333,332; the lowest consent is
	// 123,456,789 - ceil(1,234,567.89) = 122,222,221
	lowest := types.PortExitMinAccepted(math.NewInt(123_456_789), types.MaxPortExitFeeToleranceBpsAbsolute)
	require.Equal(t, "122222221", lowest.String())
	refloor := func(signer string, newMin int64) error {
		_, err := srv.RefloorPortExit(f.ctx, &types.MsgRefloorPortExit{Controller: signer, MessageId: id, NewMinAccepted: math.NewInt(newMin)})
		return err
	}

	// 1. refusals: another account cannot name this withdrawal; the minimum
	// can only go down, never below the bound; nothing changed
	other, otherAcc := util.CreateMockHexAddress("solana-user", 8), types.DeriveAddress(portSo, util.CreateMockHexAddress("solana-user", 8))
	require.NoError(t, f.k.Accounts.Set(f.ctx, otherAcc.String(), types.RemoteAccount{Address: otherAcc.String(), Domain: portSo, Controller: other}))
	require.ErrorContains(t, refloor(otherAcc.String(), 123_209_876), "no receipt")
	require.ErrorContains(t, refloor(acc.String(), 123_333_332), "must be lower")
	require.ErrorContains(t, refloor(acc.String(), 123_400_000), "must be lower")
	require.ErrorContains(t, refloor(acc.String(), 122_222_220), "below net")
	require.ErrorContains(t, refloor(f.owner.String(), 123_209_876), "not found", "not a remote account")
	require.Equal(t, "123333332", receipt().MinAccepted)
	ex, _ = f.k.Executors.Get(f.ctx, originDom)
	require.Equal(t, uint64(1), ex.Nonce, "no control message sent")
	// the ValidateBasic of the message itself
	require.Error(t, types.MsgRefloorPortExit{Controller: acc.String(), NewMinAccepted: math.NewInt(1)}.ValidateBasic(), "message id")
	require.Error(t, types.MsgRefloorPortExit{Controller: acc.String(), MessageId: id, NewMinAccepted: math.NewInt(-1)}.ValidateBasic())

	// 2. the consent, signed by the account: receipt re-floored, REFLOOR_EXIT
	// (nonce 2) names the exit by (controller, sentinel, 123,333,332, seq 1)
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	res, err := srv.RefloorPortExit(f.ctx, &types.MsgRefloorPortExit{Controller: acc.String(), MessageId: id, NewMinAccepted: math.NewInt(123_209_876)})
	require.NoError(t, err)
	require.Equal(t, uint64(2), res.Nonce)
	r = receipt()
	require.Equal(t, "123209876", r.MinAccepted, "the receipt shows the new minimum")
	require.Equal(t, "123333332", r.ExitMinAccepted, "the salt minimum (and the exit address) are unchanged")
	require.Equal(t, exit, r.ExitAddress)
	require.Equal(t, uint32(1), r.Refloors)
	require.False(t, r.Confirmed)
	body := types.EncodeControl(types.CONTROL_REFLOOR_EXIT,
		types.RefloorExitParams(user, sentinel, math.NewInt(123_333_332), 1, math.NewInt(123_209_876), exit), 2)
	require.Len(t, body, 32*4+6*32)
	found := false
	for _, m := range f.dispatched() {
		if strings.HasSuffix(m, hex.EncodeToString(body)) && strings.Contains(m, strings.TrimPrefix(executor.String(), "0x")) {
			found = true
		}
	}
	require.True(t, found, "REFLOOR_EXIT body dispatched to the executor")
	require.True(t, eventAttr(f, "terra.remote.v1.EventControlSent", "action", "CONTROL_REFLOOR_EXIT"))
	require.True(t, eventAttr(f, "terra.remote.v1.EventPortExitRefloored", "previous_min_accepted", "123333332"))
	require.True(t, eventAttr(f, "terra.remote.v1.EventPortExitRefloored", "new_min_accepted", "123209876"))
	require.True(t, eventAttr(f, "terra.remote.v1.EventPortExitRefloored", "lowest_allowed", "122222221"))
	require.True(t, eventAttr(f, "terra.remote.v1.EventPortExitRefloored", "exit_address", exit.String()))
	// no value moved on this chain
	require.Equal(t, bank0, f.app.BankKeeper.GetBalance(f.ctx, acc, usdcDenom).Amount.String())
	require.Equal(t, sent0, sentTo(t, f, usdc, originDom))

	// 3. the same consent from Solana: a payload of the port's gateway with
	// on_behalf_of = the user (fabric_gateway `dispatch`), lower again
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	f.deliverFrom(t, portSo, solGateway, f.payload(t, user.Bytes(),
		&types.MsgRefloorPortExit{Controller: acc.String(), MessageId: id, NewMinAccepted: math.NewInt(123_209_875)}))
	require.Empty(t, f.rejected(t))
	require.True(t, f.hasEvent("terra.remote.v1.EventRemoteExecuted"))
	r = receipt()
	require.Equal(t, "123209875", r.MinAccepted)
	require.Equal(t, uint32(2), r.Refloors)
	ex, _ = f.k.Executors.Get(f.ctx, originDom)
	require.Equal(t, uint64(3), ex.Nonce)
	// a payload of another Solana wallet cannot sign for this account
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	f.deliverFrom(t, portSo, solGateway, f.payload(t, other.Bytes(),
		&types.MsgRefloorPortExit{Controller: acc.String(), MessageId: id, NewMinAccepted: math.NewInt(123_000_000)}))
	require.Contains(t, f.rejected(t), "signed by")
	require.Equal(t, "123209875", receipt().MinAccepted)

	// 4. down to the bound exactly, then the per-exit maximum
	require.NoError(t, refloor(acc.String(), 122_222_221))
	require.Equal(t, uint32(3), receipt().Refloors)
	require.ErrorContains(t, refloor(acc.String(), 122_222_221), "maximum")

	// 5. the confirmation path is unchanged: the vault's update (port_domain)
	// confirms the receipt with amount_out = net - the fee executed; after it
	// a re-floor is refused
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	upd, err := f.app.AppCodec().Marshal(&types.RemotePayload{OnBehalfOf: user.Bytes(), PortDomain: portSo, Update: &types.ConversionUpdate{
		WithdrawMessageId: id.Bytes(), AmountOut: math.NewInt(123_209_876).BigInt().Bytes(), OriginBlock: 9,
	}})
	require.NoError(t, err)
	f.deliver(t, gateway, upd)
	require.Empty(t, f.rejected(t))
	r = receipt()
	require.True(t, r.Confirmed)
	require.Equal(t, "123209876", r.AmountOut)
	require.Equal(t, "122222221", r.MinAccepted)

	// 6. only port accounts have port exits: an EVM account (domain 97, not a port) is refused
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	f.deliver(t, f.controller, f.payload(t, nil, &types.MsgRefloorPortExit{Controller: f.derived.String(), MessageId: id, NewMinAccepted: math.ZeroInt()}))
	require.Contains(t, f.rejected(t), "not a port")
}

// TestRefloorPortExitGuards: a confirmed receipt, an explicit token_out exit
// of another port, a missing executor and a missing paymaster balance are
// refused without touching the receipt.
func TestRefloorPortExitGuards(t *testing.T) {
	f, _, _, _ := exitFixture(t)
	gov := f.k.GetAuthority()
	srv := keeper.NewMsgServerImpl(f.k)
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(1_000_000_000))))
	usdc, usdcDenom := f.basketToken(t)
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(100_000_000)))))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", f.owner, sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(100_000_000)))))
	require.NoError(t, f.k.SetPort(f.ctx, &types.MsgSetPort{Authority: gov, PortDomain: portSo, VaultDomain: originDom, CctpDomain: 5}))
	_, acc := f.portUser(t, sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(50_000_000))))

	// no executor on the vault: refused, receipt untouched
	id, err := f.k.Withdraw(f.ctx, acc.String(), usdc, math.NewInt(10_000_000), "", nil)
	require.NoError(t, err)
	_, err = srv.RefloorPortExit(f.ctx, &types.MsgRefloorPortExit{Controller: acc.String(), MessageId: id, NewMinAccepted: math.NewInt(9_980_000)})
	require.ErrorIs(t, err, types.ErrGatewayNotFound)
	rs, _ := f.k.ReceiptsOf(f.ctx, acc.String())
	require.Equal(t, "9990000", rs[0].MinAccepted)

	// an exit with the sentinel of ANOTHER port domain (explicit token_out) is not this account's port exit
	require.NoError(t, f.k.SetExecutor(f.ctx, &types.MsgSetExecutor{Authority: gov, AppId: f.appId, Domain: originDom, Address: util.CreateMockHexAddress("executor", 1),
		TokenId: usdc, MinCollateral: math.ZeroInt(), TargetCollateral: math.ZeroInt()}))
	m := math.NewInt(5_000_000)
	id2, err := f.k.Withdraw(f.ctx, acc.String(), usdc, math.NewInt(5_000_000), types.PortSentinel(portSo+1).String(), &m)
	require.NoError(t, err)
	_, err = srv.RefloorPortExit(f.ctx, &types.MsgRefloorPortExit{Controller: acc.String(), MessageId: id2, NewMinAccepted: math.NewInt(4_990_000)})
	require.ErrorContains(t, err, "not a port exit")

	// the paymaster cannot fund the control message: refused (the payload fails as a whole)
	pm := types.PaymasterAddress()
	bal := f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna")
	require.NoError(t, f.app.BankKeeper.SendCoins(f.ctx, pm, f.owner, sdk.NewCoins(bal)))
	_, err = srv.RefloorPortExit(f.ctx, &types.MsgRefloorPortExit{Controller: acc.String(), MessageId: id, NewMinAccepted: math.NewInt(9_980_000)})
	require.ErrorIs(t, err, types.ErrFeeUnpaid)
	rs, _ = f.k.ReceiptsOf(f.ctx, acc.String())
	for _, r := range rs {
		if r.MessageId.Equal(id) {
			require.Equal(t, "9990000", r.MinAccepted)
			require.Zero(t, r.Refloors)
		}
	}
}

// TestRefloorExitParamsABI pins the REFLOOR_EXIT params for FabricExecutor
// (abi.encode(bytes32, bytes32, uint256, uint64, uint256, address)).
func TestRefloorExitParamsABI(t *testing.T) {
	controller := util.CreateMockHexAddress("c", 1)
	exit, err := util.DecodeHexAddress("0x000000000000000000000000" + "96e8a211c60b5cf92817b444ee0d8413cf10977e")
	require.NoError(t, err)
	p := types.RefloorExitParams(controller, types.PortSentinel(1399811152), math.NewInt(123_333_332), 1, math.NewInt(123_209_876), exit)
	require.Len(t, p, 6*32)
	require.Equal(t, controller.Bytes(), p[0:32])
	require.Equal(t, "000000000000000000000000000000000000000000000000000000cc536f6c50", hex.EncodeToString(p[32:64]), "port sentinel of 1399811152")
	require.Equal(t, "000000000000000000000000000000000000000000000000000000000759ead4", hex.EncodeToString(p[64:96]), "123,333,332")
	require.Equal(t, "0000000000000000000000000000000000000000000000000000000000000001", hex.EncodeToString(p[96:128]), "nonce 1")
	require.Equal(t, "0000000000000000000000000000000000000000000000000000000007580894", hex.EncodeToString(p[128:160]), "123,209,876")
	require.Equal(t, "00000000000000000000000096e8a211c60b5cf92817b444ee0d8413cf10977e", hex.EncodeToString(p[160:192]))

	// the whole control body for controller 0xab…ab, nonce 2, as FabricExecutor.handle decodes it:
	// keccak256(abi.encode(uint8(7), abi.encode(...), uint64(2))), asserted by contracts/evm Refloor.t.sol
	ab, err := util.DecodeHexAddress("0x" + hexRepeat("ab", 32))
	require.NoError(t, err)
	body := types.EncodeControl(types.CONTROL_REFLOOR_EXIT,
		types.RefloorExitParams(ab, types.PortSentinel(1399811152), math.NewInt(123_333_332), 1, math.NewInt(123_209_876), exit), 2)
	t.Logf("REFLOOR_EXIT body keccak256 = %x", keccakOf(body))
	require.Equal(t, refloorBodyVector, hex.EncodeToString(keccakOf(body)))
}

// refloorBodyVector is keccak256 of the REFLOOR_EXIT control body of
// TestRefloorExitParamsABI (same value in contracts/evm Refloor.t.sol).
const refloorBodyVector = "39db274a03ca02474a7943b86178d27fa096149a580b56e62e4f3ab466945827"
