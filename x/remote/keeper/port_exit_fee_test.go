package keeper_test

import (
	"encoding/hex"
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/classic-terra/core/v4/x/remote/keeper"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestPortExitFeeTolerance pins port_exit_fee_tolerance_bps (spec §11.6,
// v0.9.12): the default exit of a port account accepts the net amount less
// ceil(net x bps / 10,000), the allowance for the vault chain's CCTP minimum
// fee. The floor is part of the CREATE2 exit address, so the address follows
// the parameter in force at withdrawal time. Governance changes it with
// MsgUpdateParams within [0, 100].
func TestPortExitFeeTolerance(t *testing.T) {
	f, _, factory, initCodeHash := exitFixture(t)
	gov := f.k.GetAuthority()
	srv := keeper.NewMsgServerImpl(f.k)
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(1_000_000_000))))
	usdc, usdcDenom := f.basketToken(t)
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(1_000_000_000)))))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", f.owner, sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(1_000_000_000)))))
	require.NoError(t, f.k.SetPort(f.ctx, &types.MsgSetPort{Authority: gov, PortDomain: portSo, VaultDomain: originDom, CctpDomain: 5}))
	user, acc := f.portUser(t, sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(1_000_000_000))))
	sentinel := types.PortSentinel(portSo)

	params, err := f.k.GetParams(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint32(10), params.PortExitFeeToleranceBps, "default 10 bps")

	withdraw := func(amount int64) types.ConversionReceipt {
		t.Helper()
		id, err := f.k.Withdraw(f.ctx, acc.String(), usdc, math.NewInt(amount), "", nil)
		require.NoError(t, err)
		rs, err := f.k.ReceiptsOf(f.ctx, acc.String())
		require.NoError(t, err)
		for _, r := range rs {
			if r.MessageId.Equal(id) {
				return r
			}
		}
		t.Fatalf("no receipt for %s", id)
		return types.ConversionReceipt{}
	}

	// 1. default 10 bps, a non-divisible amount: allowance ceil(123,456.789) =
	// 123,457, floor 123,333,332. A CCTP minimum fee of exactly 10 bps
	// (getMinFeeAmount = floor(123,456,789 x 10,000 / 1e7) = 123,456) leaves
	// 123,333,333 >= the floor: the exit settles (contracts/evm Ports.t.sol
	// test_PortExitMinFeeWithinTolerance uses these numbers)
	r := withdraw(123_456_789)
	require.Equal(t, "123456789", r.UsdcAmount, "the whole net amount goes to the exit")
	require.Equal(t, "123333332", r.MinAccepted)
	require.Equal(t, sentinel.String(), r.TokenOut)
	require.Equal(t, types.ExitAddress(factory, initCodeHash, user, sentinel, math.NewInt(123_333_332), 1), r.ExitAddress)
	require.Equal(t, "123456789", sentTo(t, f, usdc, originDom), "the allowance moves no value on this chain")

	// 2. governance raises it to 25 bps: ceil(7,000,001 x 25 / 10,000) =
	// ceil(17,500.0025) = 17,501, floor 6,982,500
	p := params
	p.PortExitFeeToleranceBps = 25
	_, err = srv.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: gov, Params: p})
	require.NoError(t, err)
	r = withdraw(7_000_001)
	require.Equal(t, "7000001", r.UsdcAmount)
	require.Equal(t, "6982500", r.MinAccepted)
	require.Equal(t, types.ExitAddress(factory, initCodeHash, user, sentinel, math.NewInt(6_982_500), 2), r.ExitAddress)

	// 3. tiny amount: ceil(999 x 10 / 10,000) = ceil(0.999) = 1, floor 998.
	// Circle's minimum is at least 1 unit, so a truncated allowance (0) would
	// demand 999 and revert the exit: the allowance must round up
	p.PortExitFeeToleranceBps = 10
	_, err = srv.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: gov, Params: p})
	require.NoError(t, err)
	r = withdraw(999)
	require.Equal(t, "998", r.MinAccepted)

	// 4. tolerance 0 restores the pre-v0.9.12 floor: the full net amount
	p.PortExitFeeToleranceBps = 0
	_, err = srv.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: gov, Params: p})
	require.NoError(t, err)
	r = withdraw(40_000_000)
	require.Equal(t, "40000000", r.MinAccepted)
	require.Equal(t, types.ExitAddress(factory, initCodeHash, user, sentinel, math.NewInt(40_000_000), 4), r.ExitAddress)

	// 5. the absolute maximum is 100 bps (1 %); above it the update is refused
	// and the parameter in force is unchanged; only the authority may change it
	p.PortExitFeeToleranceBps = 100
	_, err = srv.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: gov, Params: p})
	require.NoError(t, err)
	r = withdraw(50_000_000) // ceil(500,000) = 500,000
	require.Equal(t, "49500000", r.MinAccepted)
	p.PortExitFeeToleranceBps = 101
	_, err = srv.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: gov, Params: p})
	require.ErrorIs(t, err, types.ErrInvalidParams)
	p.PortExitFeeToleranceBps = 5
	_, err = srv.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: f.owner.String(), Params: p})
	require.Error(t, err, "only governance")
	got, err := f.k.GetParams(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint32(100), got.PortExitFeeToleranceBps)

	// 6. a caller who names token_out (even the port sentinel) keeps their own
	// min_accepted: the tolerance only shapes the default exit
	_, err = f.k.Withdraw(f.ctx, acc.String(), usdc, math.NewInt(1_000_000), sentinel.String(), func() *math.Int { m := math.NewInt(1_000_000); return &m }())
	require.NoError(t, err)
	rs, err := f.k.ReceiptsOf(f.ctx, acc.String())
	require.NoError(t, err)
	found := false
	for _, x := range rs {
		if x.UsdcAmount == "1000000" {
			require.Equal(t, "1000000", x.MinAccepted)
			found = true
		}
	}
	require.True(t, found)
}

// TestPortExitFeeToleranceWithVaultFee: the allowance applies to the amount
// after the vault's withdrawal fee (both round up, against the user): 1 % of
// 40,000,001 = ceil(400,000.01) = 400,001 burned, net 39,600,000, allowance
// ceil(39,600) = 39,600, floor 39,560,400; the event carries the floor.
func TestPortExitFeeToleranceWithVaultFee(t *testing.T) {
	f, _, factory, initCodeHash := exitFixture(t)
	gov := f.k.GetAuthority()
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(1_000_000_000))))
	usdc, usdcDenom := f.basketToken(t)
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(100_000_000)))))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", f.owner, sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(100_000_000)))))
	require.NoError(t, f.k.SetPort(f.ctx, &types.MsgSetPort{Authority: gov, PortDomain: portSo, VaultDomain: originDom, CctpDomain: 5}))
	require.NoError(t, f.k.Executors.Set(f.ctx, originDom, types.Executor{
		AppId: f.appId, Domain: originDom, Address: util.CreateMockHexAddress("executor", 1), TokenId: usdc,
		MinCollateral: math.ZeroInt(), TargetCollateral: math.NewInt(1), WithdrawFeeBps: 100, NetRebalanced: math.ZeroInt(),
	}))
	user, acc := f.portUser(t, sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(50_000_000))))
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	_, err := f.k.Withdraw(f.ctx, acc.String(), usdc, math.NewInt(40_000_001), "", nil)
	require.NoError(t, err)
	require.Equal(t, "9999999", f.app.BankKeeper.GetBalance(f.ctx, acc, usdcDenom).Amount.String())
	require.Equal(t, "39600000", sentTo(t, f, usdc, originDom))
	rs, err := f.k.ReceiptsOf(f.ctx, acc.String())
	require.NoError(t, err)
	require.Len(t, rs, 1)
	require.Equal(t, "39600000", rs[0].UsdcAmount)
	require.Equal(t, "39560400", rs[0].MinAccepted)
	exit := types.ExitAddress(factory, initCodeHash, user, types.PortSentinel(portSo), math.NewInt(39_560_400), 1)
	require.Equal(t, exit, rs[0].ExitAddress)
	require.True(t, eventAttr(f, "terra.remote.v1.EventConversionReceiptCreated", "min_accepted", "39560400"))
	require.True(t, eventAttr(f, "terra.remote.v1.EventConversionReceiptCreated", "usdc_amount", "39600000"))
	require.True(t, eventAttr(f, "terra.remote.v1.EventRemoteWithdraw", "recipient", exit.String()))
}

// TestPortExitAddressVector pins a port exit of the v0.9.12 floor for the
// Solidity factory (contracts/evm Ports.t.sol test_PortExitAddressVector):
// factory 0x1111…, implementation 0x2222…, controller 0xab…ab (a 32-byte
// Solana key), token_out = PortSentinel(1399811152), min_accepted =
// PortExitMinAccepted(123,456,789, 10) = 123,333,332, nonce 1.
func TestPortExitAddressVector(t *testing.T) {
	controller, err := util.DecodeHexAddress("0x" + hexRepeat("ab", 32))
	require.NoError(t, err)
	factory, err := util.DecodeHexAddress("0x0000000000000000000000001111111111111111111111111111111111111111")
	require.NoError(t, err)
	initCode, err := hex.DecodeString("3d602d80600a3d3981f3363d3d373d3d3d363d7322222222222222222222222222222222222222225af43d82803e903d91602b57fd5bf3")
	require.NoError(t, err)
	min := types.PortExitMinAccepted(math.NewInt(123_456_789), 10)
	require.Equal(t, "123333332", min.String())
	addr := types.ExitAddress(factory, keccakOf(initCode), controller, types.PortSentinel(1399811152), min, 1)
	t.Logf("port exit vector: sentinel=%s exit=%s", types.PortSentinel(1399811152), addr)
	require.Equal(t, "0x000000000000000000000000"+portExitVector, addr.String())
}

// portExitVector is the address the Solidity ExitAddress.compute returns for
// the parameters of TestPortExitAddressVector (lower-case hex, 20 bytes).
const portExitVector = "96e8a211c60b5cf92817b444ee0d8413cf10977e"

func hexRepeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
