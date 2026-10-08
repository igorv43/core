package keeper_test

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	lstypes "github.com/classic-terra/core/v4/x/liquidstake/types"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestPortSettlementExitFromEndBlock pins which EndBlock work of x/remote can
// send the settlement asset of a D-33 port account (spec v0.9.11 §11.6.2)
// through its vault, i.e. start a CCTP exit outside any transaction, and
// that it emits exactly what the testenv relay (testenv/solana/client/
// tc-exits.mjs) reads from block_results:
//
//  1. the auto-return (§4.4 of docs/staking/CROSS-CHAIN-LIQUID-STAKING.md)
//     never does: x/liquidstake Claim pays the bond denom (uluna) and the
//     auto-return withdraws only a token whose denom carries the claim, so an
//     opt-in with the settlement token (accepted: it has a destination, the
//     vault) never withdraws anything, and the claim stays for a manual
//     MsgClaim;
//  2. a deposit-then-execute payload (§4.3) does: a payload from the port
//     domain's enrolled gateway that waits for a LUNC deposit and carries a
//     MsgWithdraw of the settlement token runs in EndBlock (processPending)
//     once the deposit arrives, and the withdrawal goes to the vault with the
//     port sentinel, the vault fee rounded up and burned, the net amount as
//     the exit's floor. The FabricGateway program of contracts/svm cannot
//     produce such a payload today (its after_deposit instructions carry only
//     MsgStake / MsgUnstake with a referenced withdrawal of LUNC / stLUNC),
//     but the chain accepts it from any enrolled gateway, so the relay must
//     handle EndBlock-origin exits.
//
// With REMOTE_RELAY_FIXTURE=<path> the EndBlock events of step 2 are written
// as a CometBFT block_results response (attribute "mode" = "EndBlock", as
// baseapp adds it), the fixture of the relay's unit test.
func TestPortSettlementExitFromEndBlock(t *testing.T) {
	s := setupStaking(t)
	gov := s.k.GetAuthority()
	// the vault (originDom) offers exits; the port domain has its own gateway
	factory := util.CreateMockHexAddress("exit-factory", 1)
	initCodeHash := make([]byte, 32)
	for i := range initCodeHash {
		initCodeHash[i] = byte(i)
	}
	require.NoError(t, s.k.SetGateway(s.ctx, s.appId, originDom, s.gateway, factory, initCodeHash))
	solGateway := util.CreateMockHexAddress("solana-gateway", 1)
	require.NoError(t, s.k.SetGateway(s.ctx, s.appId, portSo, solGateway, util.NewZeroAddress(), nil))
	usdc, usdcDenom := s.basketToken(t)
	require.NoError(t, s.enroll(s.token, portSo)) // LUNC is routed to the port, the settlement token is not
	require.NoError(t, s.app.WarpLedgerKeeper.SetDomainCap(s.ctx, s.token, portSo, math.NewInt(1_000_000_000_000)))
	require.NoError(t, s.k.SetPort(s.ctx, &types.MsgSetPort{Authority: gov, PortDomain: portSo, VaultDomain: originDom, CctpDomain: 5}))
	require.NoError(t, s.k.Executors.Set(s.ctx, originDom, types.Executor{
		AppId: s.appId, Domain: originDom, Address: util.CreateMockHexAddress("executor", 1), TokenId: usdc,
		MinCollateral: math.ZeroInt(), TargetCollateral: math.NewInt(1), WithdrawFeeBps: 100, NetRebalanced: math.ZeroInt(),
	}))

	user := util.CreateMockHexAddress("solana-user", 9)
	acc := types.DeriveAddress(portSo, user)
	require.NoError(t, s.app.BankKeeper.SendCoins(s.ctx, s.owner, acc, sdk.NewCoins(luna(200_000_000))))
	require.NoError(t, s.app.BankKeeper.MintCoins(s.ctx, "mint", sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(50_000_000)))))
	require.NoError(t, s.app.BankKeeper.SendCoinsFromModuleToAccount(s.ctx, "mint", acc, sdk.NewCoins(sdk.NewCoin(usdcDenom, math.NewInt(50_000_000)))))
	nonce := uint32(1000)
	fromSolana := func(body []byte) {
		t.Helper()
		nonce++
		require.NoError(t, s.k.Handle(s.ctx, s.mailbox, util.HyperlaneMessage{
			Version: 3, Nonce: nonce, Origin: portSo, Sender: solGateway, Destination: localDomain, Recipient: s.appId, Body: body,
		}))
		require.Empty(t, s.rejected(t))
	}
	payload := func(ad *types.AfterDeposit, msgs ...sdk.Msg) []byte {
		t.Helper()
		ctrl := s.controller
		s.controller = user
		defer func() { s.controller = ctrl }()
		return s.adPayload(t, ad, msgs...)
	}

	// --- 1. auto-return opted in with the settlement token: never pays it
	fromSolana(payload(nil,
		&lstypes.MsgStake{Sender: acc.String(), Amount: luna(100_000_000)},
		&types.MsgSetAutoReturn{Controller: acc.String(), Enabled: true, TokenId: usdc},
	))
	s.epoch(t) // delegates: the unstake below is queued
	fromSolana(payload(nil, &lstypes.MsgUnstake{Sender: acc.String(), Amount: st(100_000_000)}))
	s.mature(t)
	supply0 := s.app.BankKeeper.GetSupply(s.ctx, usdcDenom).Amount
	s.fresh()
	s.epoch(t)
	s.epoch(t)
	ws, err := s.k.WithdrawalsOf(s.ctx, acc.String())
	require.NoError(t, err)
	require.Empty(t, ws, "an auto-return with the settlement token withdraws nothing")
	rs, err := s.k.ReceiptsOf(s.ctx, acc.String())
	require.NoError(t, err)
	require.Empty(t, rs)
	require.Zero(t, s.count("terra.remote.v1.EventRemoteClaim"))
	require.Equal(t, "50000000", s.bal(acc, usdcDenom).String())
	require.Equal(t, supply0.String(), s.app.BankKeeper.GetSupply(s.ctx, usdcDenom).Amount.String())
	requests := 0
	require.NoError(t, s.app.LiquidStakeKeeper.Requests.Walk(s.ctx, nil, func(_ uint64, r lstypes.UnstakeRequest) (bool, error) {
		if r.Address == acc.String() {
			requests++
		}
		return false, nil
	}))
	require.Equal(t, 1, requests, "the matured request waits for a manual MsgClaim")
	// with the LUNC token the same request returns at the next epoch, directly
	// to the port domain (its route): no vault, no exit, no receipt
	fromSolana(payload(nil, &types.MsgSetAutoReturn{Controller: acc.String(), Enabled: true, TokenId: s.token}))
	s.fresh()
	s.epoch(t)
	require.Equal(t, "true", s.attr("terra.remote.v1.EventRemoteClaim", "auto"))
	require.Equal(t, "1399811149", s.attr("terra.remote.v1.EventRemoteWithdraw", "domain"))
	require.Zero(t, s.count("terra.remote.v1.EventConversionReceiptCreated"))
	ws, err = s.k.WithdrawalsOf(s.ctx, acc.String())
	require.NoError(t, err)
	require.Len(t, ws, 1)
	require.True(t, ws[0].TokenId.Equal(s.token))
	require.Equal(t, "100000000", ws[0].Amount.String())

	// --- 2. deposit-then-execute with a settlement withdrawal: the exit starts in EndBlock
	fromSolana(payload(&types.AfterDeposit{TokenId: s.token, Amount: math.NewInt(5_000_000)},
		&types.MsgWithdraw{Controller: acc.String(), TokenId: usdc, Amount: math.NewInt(40_000_001)},
	))
	require.Len(t, s.pending(t, acc.String()), 1, "waits for its LUNC deposit")
	require.Equal(t, "50000000", s.bal(acc, usdcDenom).String())
	// the LUNC deposit from the port domain (the auto-return above sent 100 LUNC there)
	nonce++
	body, err := warptypes.NewWarpPayload(acc.Bytes(), *big.NewInt(5_000_000))
	require.NoError(t, err)
	msg := util.HyperlaneMessage{
		Version: 3, Nonce: nonce, Origin: portSo, Sender: util.CreateMockHexAddress("router", int64(portSo)), Destination: localDomain,
		Recipient: s.token, Body: body.Bytes(),
	}
	process := &coretypes.MsgProcessMessage{MailboxId: s.mailbox, Relayer: s.owner.String(), Metadata: "0x00", Message: msg.String()}
	_, err = s.app.MsgServiceRouter().Handler(process)(s.ctx, process)
	require.NoError(t, err)
	require.Equal(t, "50000000", s.bal(acc, usdcDenom).String(), "nothing runs inside the delivery tx")

	supply1 := s.app.BankKeeper.GetSupply(s.ctx, usdcDenom).Amount
	s.advance(1, time.Second)
	s.fresh()
	require.NoError(t, s.k.EndBlocker(s.ctx))
	require.Empty(t, s.pending(t, acc.String()))
	// fee = ceil(40,000,001 x 100 / 10,000) = ceil(400,000.01) = 400,001, burned; net 39,600,000 to the vault
	require.Equal(t, "40000001", supply1.Sub(s.app.BankKeeper.GetSupply(s.ctx, usdcDenom).Amount).String())
	require.Equal(t, "9999999", s.bal(acc, usdcDenom).String())
	require.Equal(t, "39600000", sentTo(t, s.fixture, usdc, originDom))
	sentinel := types.PortSentinel(portSo)
	exit := types.ExitAddress(factory, initCodeHash, user, sentinel, math.NewInt(39_600_000), 2)
	rs, err = s.k.ReceiptsOf(s.ctx, acc.String())
	require.NoError(t, err)
	require.Len(t, rs, 1)
	require.Equal(t, sentinel.String(), rs[0].TokenOut)
	require.Equal(t, "39600000", rs[0].MinAccepted)
	require.Equal(t, "39600000", rs[0].UsdcAmount)
	require.Equal(t, exit, rs[0].ExitAddress)
	require.Equal(t, uint32(originDom), rs[0].OriginDomain)
	// exactly the events the relay consumes, once each
	require.Equal(t, 1, s.count("terra.remote.v1.EventConversionReceiptCreated"))
	require.Equal(t, 1, s.count("terra.remote.v1.EventRemoteExecuted"))
	require.Equal(t, "CONVERSION_WITHDRAW", s.attr("terra.remote.v1.EventConversionReceiptCreated", "direction"))
	require.Equal(t, sentinel.String(), s.attr("terra.remote.v1.EventConversionReceiptCreated", "token_out"))
	require.Equal(t, "39600000", s.attr("terra.remote.v1.EventConversionReceiptCreated", "min_accepted"))
	require.Equal(t, exit.String(), s.attr("terra.remote.v1.EventConversionReceiptCreated", "exit_address"))
	require.Equal(t, rs[0].MessageId.String(), s.attr("terra.remote.v1.EventConversionReceiptCreated", "message_id"))
	require.Equal(t, acc.String(), s.attr("terra.remote.v1.EventConversionReceiptCreated", "account"))
	require.Equal(t, acc.String(), s.attr("terra.remote.v1.EventRemoteExecuted", "account"))
	require.Equal(t, user.String(), s.attr("terra.remote.v1.EventRemoteExecuted", "controller"))
	require.Equal(t, "39600000", s.attr("terra.remote.v1.EventRemoteWithdraw", "amount"))
	require.Equal(t, exit.String(), s.attr("terra.remote.v1.EventRemoteWithdraw", "recipient"))
	ws, err = s.k.WithdrawalsOf(s.ctx, acc.String())
	require.NoError(t, err)
	require.Len(t, ws, 2)
	bySeq := map[uint64]types.Withdrawal{}
	for _, w := range ws {
		bySeq[w.Seq] = w
	}
	require.Equal(t, "39600000", bySeq[2].Amount.String(), "seq 2 is the exit's CREATE2 nonce")
	require.True(t, bySeq[2].TokenId.Equal(usdc))

	if path := os.Getenv("REMOTE_RELAY_FIXTURE"); path != "" {
		writeBlockResultsFixture(t, path, s.ctx, map[string]any{
			"source":      "core/x/remote/keeper/port_endblock_test.go TestPortSettlementExitFromEndBlock (EndBlock of x/remote)",
			"sentinel":    sentinel.String(),
			"exit":        exit.String(),
			"account":     acc.String(),
			"controller":  user.String(),
			"minAccepted": "39600000",
			"messageId":   rs[0].MessageId.String(),
			"seq":         2,
		})
	}
}

// writeBlockResultsFixture writes the context's events as the
// finalize_block_events of a CometBFT /block_results response.
func writeBlockResultsFixture(t *testing.T, path string, ctx sdk.Context, meta map[string]any) {
	t.Helper()
	type attr struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		Index bool   `json:"index"`
	}
	type event struct {
		Type       string `json:"type"`
		Attributes []attr `json:"attributes"`
	}
	var evs []event
	for _, e := range ctx.EventManager().ABCIEvents() {
		ev := event{Type: e.Type}
		for _, a := range e.Attributes {
			ev.Attributes = append(ev.Attributes, attr{Key: a.Key, Value: a.Value, Index: true})
		}
		ev.Attributes = append(ev.Attributes, attr{Key: "mode", Value: "EndBlock", Index: true})
		evs = append(evs, ev)
	}
	out := map[string]any{
		"_fixture": meta,
		"jsonrpc":  "2.0",
		"id":       -1,
		"result": map[string]any{
			"height":                  ctx.BlockHeight(),
			"txs_results":             []any{},
			"finalize_block_events":   evs,
			"validator_updates":       []any{},
			"consensus_param_updates": nil,
			"app_hash":                "",
		},
	}
	bz, err := json.MarshalIndent(out, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(bz, '\n'), 0o644))
}
