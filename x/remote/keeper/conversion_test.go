package keeper_test

import (
	"encoding/hex"
	"math/big"
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	perptypes "github.com/classic-terra/core/v4/x/perp/types"
	"github.com/classic-terra/core/v4/x/remote/keeper"
	"github.com/classic-terra/core/v4/x/remote/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
)

func be32(v *big.Int) []byte {
	out := make([]byte, 32)
	v.FillBytes(out)
	return out
}

func (f *fixture) conversionPayload(t *testing.T, onBehalfOf []byte, c *types.ConversionData, u *types.ConversionUpdate, msgs ...sdk.Msg) []byte {
	t.Helper()
	var anys []*codectypes.Any
	for _, m := range msgs {
		a, err := codectypes.NewAnyWithValue(m)
		require.NoError(t, err)
		anys = append(anys, a)
	}
	bz, err := f.app.AppCodec().Marshal(&types.RemotePayload{OnBehalfOf: onBehalfOf, Msgs: anys, Conversion: c, Update: u})
	require.NoError(t, err)
	return bz
}

// exitFixture enrolls a gateway that offers withdrawal exits on the origin.
func exitFixture(t *testing.T) (*fixture, util.HexAddress, util.HexAddress, []byte) {
	f := setup(t)
	gateway := util.CreateMockHexAddress("gateway", 1)
	factory := util.CreateMockHexAddress("exit-factory", 1)
	initCodeHash := make([]byte, 32)
	for i := range initCodeHash {
		initCodeHash[i] = byte(i)
	}
	require.NoError(t, f.k.SetGateway(f.ctx, f.appId, originDom, gateway, factory, initCodeHash))
	return f, gateway, factory, initCodeHash
}

// TestExitAddressVector pins the CREATE2 derivation: salt = keccak(abi.encode(
// controller, tokenOut, minOut, nonce)); address = keccak(0xff‖factory‖salt‖
// initCodeHash)[12:]. The Solidity factory of contracts/evm asserts the same
// numbers.
func TestExitAddressVector(t *testing.T) {
	controller, err := util.DecodeHexAddress("0x000000000000000000000000d8da6bf26964af9d7eed9e03e53415d37aa96045")
	require.NoError(t, err)
	native, err := util.DecodeHexAddress(types.NativeTokenSentinel)
	require.NoError(t, err)
	factory, err := util.DecodeHexAddress("0x0000000000000000000000001111111111111111111111111111111111111111")
	require.NoError(t, err)
	// EIP-1167 clone init code of implementation 0x2222…2222
	initCode, err := hex.DecodeString("3d602d80600a3d3981f3363d3d373d3d3d363d7322222222222222222222222222222222222222225af43d82803e903d91602b57fd5bf3")
	require.NoError(t, err)
	initCodeHash := keccakOf(initCode)
	salt := types.ExitSalt(controller, native, math.NewInt(1_000_000), 1)
	addr := types.ExitAddress(factory, initCodeHash, controller, native, math.NewInt(1_000_000), 1)
	t.Logf("initCodeHash=%x salt=%x exit=%s", initCodeHash, salt, addr.String())
	require.Len(t, salt, 32)
	require.Equal(t, make([]byte, 12), addr.Bytes()[:12], "20-byte address left-padded")
	// deterministic and parameter-sensitive
	require.Equal(t, addr, types.ExitAddress(factory, initCodeHash, controller, native, math.NewInt(1_000_000), 1))
	require.NotEqual(t, addr, types.ExitAddress(factory, initCodeHash, controller, native, math.NewInt(1_000_001), 1))
	require.NotEqual(t, addr, types.ExitAddress(factory, initCodeHash, controller, native, math.NewInt(1_000_000), 2))
}

func TestConvertedDepositWritesReceipt(t *testing.T) {
	f, gateway, _, _ := exitFixture(t)
	conv := &types.ConversionData{
		TokenIn: "native", DecimalsIn: 18,
		AmountIn:    be32(big.NewInt(2_000_000_000_000_000_000)), // 2 BNB
		UsdcAmount:  be32(big.NewInt(1_234_000_000)),             // 1,234 USDC
		DexFee:      be32(big.NewInt(3_000_000)),
		RouteFee:    be32(big.NewInt(0)),
		MinAccepted: be32(big.NewInt(1_200_000_000)),
		OriginBlock: 4242,
	}
	// a pure deposit receipt (no messages) from the enrolled gateway
	f.deliver(t, gateway, f.conversionPayload(t, f.controller.Bytes(), conv, nil))
	require.Empty(t, f.rejected(t))
	rs, err := f.k.ReceiptsOf(f.ctx, f.derived.String())
	require.NoError(t, err)
	require.Len(t, rs, 1)
	r := rs[0]
	require.Equal(t, types.CONVERSION_DEPOSIT, r.Direction)
	require.Equal(t, "native", r.TokenIn)
	require.Equal(t, "2000000000000000000", r.AmountIn)
	require.Equal(t, "1234000000", r.UsdcAmount)
	require.Equal(t, "617.000000000000000000", r.EffectiveRate.String(), "1,234 USDC for 2 BNB = 617 USDC per BNB")
	require.Equal(t, "3000000", r.DexFee)
	require.Equal(t, "1200000000", r.MinAccepted)
	require.Equal(t, uint64(4242), r.OriginBlock)
	require.Equal(t, uint32(originDom), r.OriginDomain)
	require.False(t, r.MessageId.IsZeroAddress())

	// conversion data from a direct sender (not the gateway) is rejected
	f.deliver(t, f.controller, f.conversionPayload(t, nil, conv, nil))
	require.Contains(t, f.rejected(t), "enrolled gateway")

	// onboard: conversion plus a message in the same payload
	body := f.conversionPayload(t, f.controller.Bytes(), conv, nil, &perptypes.MsgDepositCollateral{Sender: f.derived.String(), Amount: sdk.NewCoin("uusd", math.NewInt(1_000_000))})
	f.deliver(t, gateway, body)
	rs, err = f.k.ReceiptsOf(f.ctx, f.derived.String())
	require.NoError(t, err)
	require.Len(t, rs, 2)

	// paginated query, newest first
	qs := keeper.NewQueryServerImpl(f.k)
	res, err := qs.ConversionReceipts(f.ctx, &types.QueryConversionReceiptsRequest{Address: f.derived.String(), Pagination: &query.PageRequest{Limit: 1}})
	require.NoError(t, err)
	require.Len(t, res.Receipts, 1)
	require.NotNil(t, res.Pagination)
	require.NotEmpty(t, res.Pagination.NextKey)
}

func TestWithdrawToExitAndConfirmation(t *testing.T) {
	f, gateway, factory, initCodeHash := exitFixture(t)
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(1_000_000_000))))
	f.deliver(t, f.controller, f.payload(t, nil, &perptypes.MsgSetAutoTopUp{Sender: f.derived.String(), Enabled: true}))
	require.Empty(t, f.rejected(t))

	native, err := util.DecodeHexAddress(types.NativeTokenSentinel)
	require.NoError(t, err)
	minOut := math.NewInt(5_000_000)
	// the exit of the first withdrawal (seq 1) is known in advance
	want := types.ExitAddress(factory, initCodeHash, f.controller, native, minOut, 1)

	id, err := f.k.Withdraw(f.ctx, f.derived.String(), f.token, math.NewInt(400_000_000), types.NativeTokenSentinel, &minOut)
	require.NoError(t, err)
	rs, err := f.k.ReceiptsOf(f.ctx, f.derived.String())
	require.NoError(t, err)
	require.Len(t, rs, 1)
	r := rs[0]
	require.Equal(t, types.CONVERSION_WITHDRAW, r.Direction)
	require.Equal(t, id, r.MessageId)
	require.Equal(t, want, r.ExitAddress)
	require.Equal(t, native.String(), r.TokenOut)
	require.Equal(t, "5000000", r.MinAccepted)
	require.Equal(t, "400000000", r.UsdcAmount)
	require.False(t, r.Confirmed)
	// the warp transfer went to the exit, not to the controller
	var recipient string
	for _, ev := range f.ctx.EventManager().Events() {
		if ev.Type == "terra.remote.v1.EventRemoteWithdraw" {
			for _, a := range ev.Attributes {
				if a.Key == "recipient" {
					recipient = a.Value
				}
			}
		}
	}
	require.Contains(t, recipient, want.String())

	// min_accepted is mandatory with token_out
	_, err = f.k.Withdraw(f.ctx, f.derived.String(), f.token, math.NewInt(1), types.NativeTokenSentinel, nil)
	require.ErrorIs(t, err, types.ErrInvalidWithdraw)

	// the origin confirms the exit through the gateway
	upd := &types.ConversionUpdate{WithdrawMessageId: id.Bytes(), AmountOut: be32(big.NewInt(5_100_000)), FallbackToUsdc: false, OriginBlock: 9000}
	f.deliver(t, gateway, f.conversionPayload(t, f.controller.Bytes(), nil, upd))
	require.Empty(t, f.rejected(t))
	rs, err = f.k.ReceiptsOf(f.ctx, f.derived.String())
	require.NoError(t, err)
	require.True(t, rs[0].Confirmed)
	require.Equal(t, "5100000", rs[0].AmountOut)
	require.Equal(t, uint64(9000), rs[0].OriginBlock)
	// a second confirmation is rejected
	f.deliver(t, gateway, f.conversionPayload(t, f.controller.Bytes(), nil, upd))
	require.Contains(t, f.rejected(t), "unconfirmed withdrawal")

	// genesis round trip keeps receipts and the gateway's exit factory
	gs, err := f.k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, gs.Receipts, 1)
	require.Len(t, gs.Gateways, 1)
	require.Equal(t, factory, gs.Gateways[0].ExitFactory)
	f2 := setup(t)
	require.NoError(t, f2.k.InitGenesis(f2.ctx, gs))
	rs2, err := f2.k.ReceiptsOf(f2.ctx, f.derived.String())
	require.NoError(t, err)
	require.Len(t, rs2, 1)
	require.True(t, rs2[0].Confirmed)
}

// no exits offered on the domain: token_out is refused, plain withdrawals work
func TestWithdrawTokenOutNeedsExitFactory(t *testing.T) {
	f := setup(t)
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(1_000_000_000))))
	f.deliver(t, f.controller, f.payload(t, nil, &perptypes.MsgSetAutoTopUp{Sender: f.derived.String(), Enabled: true}))
	minOut := math.NewInt(1)
	_, err := f.k.Withdraw(f.ctx, f.derived.String(), f.token, math.NewInt(1_000_000), types.NativeTokenSentinel, &minOut)
	require.ErrorIs(t, err, types.ErrGatewayNotFound)
	_, err = f.k.Withdraw(f.ctx, f.derived.String(), f.token, math.NewInt(1_000_000), "", nil)
	require.NoError(t, err)
	rs, err := f.k.ReceiptsOf(f.ctx, f.derived.String())
	require.NoError(t, err)
	require.Empty(t, rs, "plain withdrawals write no receipt")
}
