package types_test

import (
	"encoding/hex"
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	lstypes "github.com/classic-terra/core/v4/x/liquidstake/types"
	"github.com/classic-terra/core/v4/x/remote/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Byte vectors of the cross-chain liquid-staking payloads the EVM FabricGateway
// encodes on chain (docs/staking/CROSS-CHAIN-LIQUID-STAKING.md §4.5). The same
// hex strings are asserted by contracts/evm/test/Staking.t.sol, so the Solidity
// encoder and the Go types are pinned to one wire format.

const (
	vecController = "0x000000000000000000000000d8da6bf26964af9d7eed9e03e53415d37aa96045"
	vecDomain     = uint32(31337)
	vecLuncToken  = "0x726f757465725f61707000000000000000000000000000010000000000000000"
	vecStToken    = "0x726f757465725f61707000000000000000000000000000010000000000000002"
	// derived account of (31337, vecController), bech32 "terra" (== LCD /terra/remote/v1/derive)
	vecDerived = "terra1qqcx2sd3a4aws3phazzgtavvxspf49zclqh076kfxqx2hpzy63tswuv2ra"
)

const (
	vecBech32Zero = "terra1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq2aqsjg"
	vecBech32Ones = "terra1lllllllllllllllllllllllllllllllllllllllllllllllllllsgrrpml"
)

const (
	vecStakeHex      = "0a20000000000000000000000000d8da6bf26964af9d7eed9e03e53415d37aa9604512770a1e2f74657272612e6c69717569647374616b652e76312e4d73675374616b6512550a4074657272613171716378327364336134617773337068617a7a67746176767873706634397a636c71683037366b667871783268707a793633747377757632726112110a05756c756e611208313030303030303012b5010a1c2f74657272612e72656d6f74652e76312e4d736757697468647261771294010a4074657272613171716378327364336134617773337068617a7a67746176767873706634397a636c71683037366b667871783268707a793633747377757632726112423078373236663735373436353732356636313730373030303030303030303030303030303030303030303030303030303031303030303030303030303030303030321a013030013a0739303030303030324e0a4230783732366637353734363537323566363137303730303030303030303030303030303030303030303030303030303030313030303030303030303030303030303012083130303030303030"
	vecUnstakeHex    = "0a20000000000000000000000000d8da6bf26964af9d7eed9e03e53415d37aa9604512790a202f74657272612e6c69717569647374616b652e76312e4d7367556e7374616b6512550a4074657272613171716378327364336134617773337068617a7a67746176767873706634397a636c71683037366b667871783268707a793633747377757632726112110a0673746c756e6112073530303030303012b5010a1c2f74657272612e72656d6f74652e76312e4d736757697468647261771294010a4074657272613171716378327364336134617773337068617a7a67746176767873706634397a636c71683037366b667871783268707a793633747377757632726112423078373236663735373436353732356636313730373030303030303030303030303030303030303030303030303030303031303030303030303030303030303030301a013030013a0735303030303030324d0a42307837323666373537343635373235663631373037303030303030303030303030303030303030303030303030303030303130303030303030303030303030303032120735303030303030"
	vecClaimHex      = "0a20000000000000000000000000d8da6bf26964af9d7eed9e03e53415d37aa9604512640a1e2f74657272612e6c69717569647374616b652e76312e4d7367436c61696d12420a4074657272613171716378327364336134617773337068617a7a67746176767873706634397a636c71683037366b667871783268707a793633747377757632726112ac010a1c2f74657272612e72656d6f74652e76312e4d73675769746864726177128b010a4074657272613171716378327364336134617773337068617a7a67746176767873706634397a636c71683037366b667871783268707a793633747377757632726112423078373236663735373436353732356636313730373030303030303030303030303030303030303030303030303030303031303030303030303030303030303030301a01303001"
	vecAutoReturnHex = "0a20000000000000000000000000d8da6bf26964af9d7eed9e03e53415d37aa9604512ae010a212f74657272612e72656d6f74652e76312e4d73675365744175746f52657475726e1288010a4074657272613171716378327364336134617773337068617a7a67746176767873706634397a636c71683037366b667871783268707a793633747377757632726110011a42307837323666373537343635373235663631373037303030303030303030303030303030303030303030303030303030303130303030303030303030303030303030"
)

func vecAny(t *testing.T, m sdk.Msg) *codectypes.Any {
	t.Helper()
	a, err := codectypes.NewAnyWithValue(m)
	require.NoError(t, err)
	return a
}

func vecHex(t *testing.T, p types.RemotePayload) string {
	t.Helper()
	bz, err := p.Marshal()
	require.NoError(t, err)
	// round trip: the chain decodes what the gateway sends
	var back types.RemotePayload
	require.NoError(t, back.Unmarshal(bz))
	return hex.EncodeToString(bz)
}

func intp(v int64) *math.Int { i := math.NewInt(v); return &i }

func TestGatewayStakingVectors(t *testing.T) {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount("terra", "terrapub")

	controller, err := util.DecodeHexAddress(vecController)
	require.NoError(t, err)
	lunc, err := util.DecodeHexAddress(vecLuncToken)
	require.NoError(t, err)
	st, err := util.DecodeHexAddress(vecStToken)
	require.NoError(t, err)
	derived := types.DeriveAddress(vecDomain, controller).String()
	t.Logf("derived %s", derived)

	ob := controller.Bytes()

	stake := types.RemotePayload{
		OnBehalfOf: ob,
		Msgs: []*codectypes.Any{
			vecAny(t, &lstypes.MsgStake{Sender: derived, Amount: sdk.NewInt64Coin("uluna", 10_000_000)}),
			vecAny(t, &types.MsgWithdraw{Controller: derived, TokenId: st, AmountFrom: types.AMOUNT_FROM_PREVIOUS_RESULT, MinAmount: intp(9_000_000)}),
		},
		AfterDeposit: &types.AfterDeposit{TokenId: lunc, Amount: math.NewInt(10_000_000)},
	}
	unstake := types.RemotePayload{
		OnBehalfOf: ob,
		Msgs: []*codectypes.Any{
			vecAny(t, &lstypes.MsgUnstake{Sender: derived, Amount: sdk.NewInt64Coin("stluna", 5_000_000)}),
			vecAny(t, &types.MsgWithdraw{Controller: derived, TokenId: lunc, AmountFrom: types.AMOUNT_FROM_PREVIOUS_RESULT, MinAmount: intp(5_000_000)}),
		},
		AfterDeposit: &types.AfterDeposit{TokenId: st, Amount: math.NewInt(5_000_000)},
	}
	// minLuna 0: min_amount omitted
	claim := types.RemotePayload{
		OnBehalfOf: ob,
		Msgs: []*codectypes.Any{
			vecAny(t, &lstypes.MsgClaim{Sender: derived}),
			vecAny(t, &types.MsgWithdraw{Controller: derived, TokenId: lunc, AmountFrom: types.AMOUNT_FROM_PREVIOUS_RESULT}),
		},
	}
	autoReturn := types.RemotePayload{
		OnBehalfOf: ob,
		Msgs:       []*codectypes.Any{vecAny(t, &types.MsgSetAutoReturn{Controller: derived, Enabled: true, TokenId: lunc})},
	}

	got := map[string]string{
		"stake": vecHex(t, stake), "unstake": vecHex(t, unstake), "claim": vecHex(t, claim), "autoReturn": vecHex(t, autoReturn),
	}
	for k, v := range got {
		t.Logf("%s %s", k, v)
	}
	require.Equal(t, vecDerived, derived)
	// bech32 edge vectors of the Solidity encoder (contracts/evm/src/Bech32.sol)
	ones := make([]byte, 32)
	for i := range ones {
		ones[i] = 0xff
	}
	t.Logf("bech32 zero %s ones %s", sdk.AccAddress(make([]byte, 32)).String(), sdk.AccAddress(ones).String())
	require.Equal(t, vecBech32Zero, sdk.AccAddress(make([]byte, 32)).String())
	require.Equal(t, vecBech32Ones, sdk.AccAddress(ones).String())
	require.Equal(t, vecStakeHex, got["stake"])
	require.Equal(t, vecUnstakeHex, got["unstake"])
	require.Equal(t, vecClaimHex, got["claim"])
	require.Equal(t, vecAutoReturnHex, got["autoReturn"])
}
