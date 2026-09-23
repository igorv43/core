package keeper_test

import (
	"encoding/binary"
	"math/big"
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	perptypes "github.com/classic-terra/core/v4/x/perp/types"
	"github.com/classic-terra/core/v4/x/remote/keeper"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

const (
	vaultB = uint32(8453)       // second vault chain
	portSo = uint32(1399811149) // port-of-entry chain (Solana)
)

func TestEncodeControlABI(t *testing.T) {
	body := types.EncodeControl(types.CONTROL_REBALANCE, types.RebalanceParams(vaultB, math.NewInt(150)), 7)
	require.Len(t, body, 32*4+64)
	require.Equal(t, uint64(1), binary.BigEndian.Uint64(body[24:32]), "action word")
	require.Equal(t, uint64(0x60), binary.BigEndian.Uint64(body[56:64]), "bytes offset")
	require.Equal(t, uint64(7), binary.BigEndian.Uint64(body[88:96]), "nonce word")
	require.Equal(t, uint64(64), binary.BigEndian.Uint64(body[120:128]), "params length")
	require.Equal(t, uint64(vaultB), binary.BigEndian.Uint64(body[152:160]))
	require.Equal(t, "150", new(big.Int).SetBytes(body[160:192]).String())
	// empty params still encode a length word
	require.Len(t, types.EncodeControl(types.CONTROL_PAUSE_LEG, nil, 1), 32*4)
	// enroll: domain, 32-byte router, 20-byte bridge right-aligned
	p := types.EnrollLegParams(vaultB, util.CreateMockHexAddress("r", 1), util.CreateMockHexAddress("b", 1))
	require.Len(t, p, 96)
	require.Equal(t, make([]byte, 12), p[64:76])
	// port sentinel
	s := types.PortSentinel(portSo)
	require.Equal(t, byte(0xcc), s[27])
	require.Equal(t, portSo, binary.BigEndian.Uint32(s[28:]))
}

func (f *fixture) controlEvents(t *testing.T) []map[string]string {
	t.Helper()
	var out []map[string]string
	for _, ev := range f.ctx.EventManager().Events() {
		if ev.Type == "terra.remote.v1.EventControlSent" {
			m := map[string]string{}
			for _, a := range ev.Attributes {
				m[a.Key] = a.Value
			}
			out = append(out, m)
		}
	}
	return out
}

func TestConsensusRebalanceEpoch(t *testing.T) {
	f := setup(t)
	gov := f.k.GetAuthority()
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(10_000_000_000))))
	params, _ := f.k.GetParams(f.ctx)
	params.RebalanceEpochBlocks = 1
	require.NoError(t, f.k.SetParams(f.ctx, params))

	exA := util.CreateMockHexAddress("executor-a", 1)
	exB := util.CreateMockHexAddress("executor-b", 1)
	require.NoError(t, f.k.SetExecutor(f.ctx, &types.MsgSetExecutor{Authority: gov, AppId: f.appId, Domain: originDom, Address: exA, TokenId: f.token, MinCollateral: math.NewInt(100), TargetCollateral: math.NewInt(200)}))
	require.NoError(t, f.k.SetExecutor(f.ctx, &types.MsgSetExecutor{Authority: gov, AppId: f.appId, Domain: vaultB, Address: exB, TokenId: f.token, MinCollateral: math.NewInt(100), TargetCollateral: math.NewInt(200)}))
	// registering pushes SET_LEG_LIMITS with nonce 1
	a, _ := f.k.Executors.Get(f.ctx, originDom)
	require.Equal(t, uint64(1), a.Nonce)
	// invalid limits refused
	require.Error(t, f.k.SetExecutor(f.ctx, &types.MsgSetExecutor{Authority: gov, AppId: f.appId, Domain: originDom, Address: exA, TokenId: f.token, MinCollateral: math.NewInt(300), TargetCollateral: math.NewInt(200)}))

	// the ledger says vault A backs 500 and vault B backs 50
	require.NoError(t, f.app.WarpLedgerKeeper.RecordReceived(f.ctx, f.token, originDom, math.NewInt(500)))
	require.NoError(t, f.app.WarpLedgerKeeper.RecordReceived(f.ctx, f.token, vaultB, math.NewInt(50)))

	require.NoError(t, f.k.EndBlocker(f.ctx))

	a, _ = f.k.Executors.Get(f.ctx, originDom)
	b, _ := f.k.Executors.Get(f.ctx, vaultB)
	// A: surplus 300/200 → capped at the band for deposits; B: deficit 150/200 → 75 % of the band on withdrawals
	require.Equal(t, uint32(50), a.DepositFeeBps)
	require.Equal(t, uint32(0), a.WithdrawFeeBps)
	require.Equal(t, uint32(0), b.DepositFeeBps)
	require.Equal(t, uint32(37), b.WithdrawFeeBps)
	// B below min → 150 moved from A to B, tracked as expected collateral
	require.Equal(t, "-150", a.NetRebalanced.String())
	require.Equal(t, "150", b.NetRebalanced.String())
	require.Equal(t, uint64(3), a.Nonce, "limits, fee, rebalance")
	require.Equal(t, uint64(2), b.Nonce, "limits, fee")
	evs := f.controlEvents(t)
	var rebalances []map[string]string
	for _, e := range evs {
		if e["action"] == "\"CONTROL_REBALANCE\"" || e["action"] == "CONTROL_REBALANCE" {
			rebalances = append(rebalances, e)
		}
	}
	require.Len(t, rebalances, 1)
	require.Contains(t, rebalances[0]["params"], "amount=150")

	// the query reports ledger and expected collateral
	res, err := keeper.NewQueryServerImpl(f.k).Executors(f.ctx, &types.QueryExecutorsRequest{})
	require.NoError(t, err)
	require.Len(t, res.Executors, 2)
	for _, v := range res.Executors {
		switch v.Executor.Domain {
		case originDom:
			require.Equal(t, "500", v.LedgerCollateral.String())
			require.Equal(t, "350", v.ExpectedCollateral.String())
		case vaultB:
			require.Equal(t, "200", v.ExpectedCollateral.String())
		}
	}

	// the next epoch re-prices A's deposit fee for its new surplus (150/200 → 37 bps) and moves nothing
	require.NoError(t, f.k.EndBlocker(f.ctx))
	a, _ = f.k.Executors.Get(f.ctx, originDom)
	require.Equal(t, uint32(37), a.DepositFeeBps)
	require.Equal(t, uint64(4), a.Nonce)
	require.Equal(t, "-150", a.NetRebalanced.String())
	// then a quiet epoch sends nothing
	before := a.Nonce
	require.NoError(t, f.k.EndBlocker(f.ctx))
	a, _ = f.k.Executors.Get(f.ctx, originDom)
	require.Equal(t, before, a.Nonce)

	// governance orders: enroll (nonce grows), pause (flag), unknown domain refused
	_, n, err := f.k.ExecutorControl(f.ctx, &types.MsgExecutorControl{Authority: gov, Domain: originDom, Action: types.CONTROL_ENROLL_LEG, LegDomain: vaultB, LegRouter: exB, LegBridge: util.CreateMockHexAddress("cctp", 1)})
	require.NoError(t, err)
	require.Equal(t, before+1, n)
	_, _, err = f.k.ExecutorControl(f.ctx, &types.MsgExecutorControl{Authority: gov, Domain: originDom, Action: types.CONTROL_PAUSE_LEG})
	require.NoError(t, err)
	a, _ = f.k.Executors.Get(f.ctx, originDom)
	require.True(t, a.Paused)
	_, _, err = f.k.ExecutorControl(f.ctx, &types.MsgExecutorControl{Authority: gov, Domain: originDom, Action: types.CONTROL_REBALANCE})
	require.ErrorIs(t, err, types.ErrInvalidParams)
	_, _, err = f.k.ExecutorControl(f.ctx, &types.MsgExecutorControl{Authority: gov, Domain: 4242, Action: types.CONTROL_PAUSE_LEG})
	require.ErrorIs(t, err, types.ErrGatewayNotFound)

	// genesis round trip
	gs, err := f.k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, gs.Executors, 2)
	require.NoError(t, gs.Validate())
}

func TestPortOfEntryDepositAndWithdraw(t *testing.T) {
	f, gateway, factory, initCodeHash := exitFixture(t)
	gov := f.k.GetAuthority()
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(1_000_000_000))))
	user := util.CreateMockHexAddress("solana-user", 1) // 32-byte pubkey as-is
	portAccount := types.DeriveAddress(portSo, user)
	require.NoError(t, f.app.BankKeeper.SendCoins(f.ctx, f.owner, portAccount, sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(30_000_000)), sdk.NewCoin("uluna", math.NewInt(100_000_000)))))

	// an unregistered port is refused, and so is a port claimed by a direct sender
	body := f.conversionPayloadPort(t, user.Bytes(), portSo, &perptypes.MsgDepositCollateral{Sender: portAccount.String(), Amount: sdk.NewCoin("uusd", math.NewInt(1_000_000))})
	f.deliver(t, gateway, body)
	require.Contains(t, f.rejected(t), "not served")
	require.NoError(t, f.k.SetPort(f.ctx, &types.MsgSetPort{Authority: gov, PortDomain: portSo, VaultDomain: originDom, CctpDomain: 5}))
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	f.deliver(t, f.controller, f.conversionPayloadPort(t, nil, portSo, &perptypes.MsgDepositCollateral{Sender: portAccount.String(), Amount: sdk.NewCoin("uusd", math.NewInt(1_000_000))}))
	require.Contains(t, f.rejected(t), "not served")

	// the vault's gateway credits the port user's account
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	f.deliver(t, gateway, body)
	require.Empty(t, f.rejected(t))
	acc, err := f.k.GetAccount(f.ctx, portAccount.String())
	require.NoError(t, err)
	require.Equal(t, portSo, acc.Domain)
	require.Equal(t, user, acc.Controller)
	free, _ := f.app.PerpKeeper.FreeCollateral(f.ctx, portAccount.String())
	require.Equal(t, "1000000", free.String())

	// a port user's withdrawal leaves the vault chain by CCTP: exit on the vault with the port sentinel
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	id, err := f.k.Withdraw(f.ctx, portAccount.String(), f.token, math.NewInt(40_000_000), "", nil)
	require.NoError(t, err)
	rs, err := f.k.ReceiptsOf(f.ctx, portAccount.String())
	require.NoError(t, err)
	require.Len(t, rs, 1)
	require.Equal(t, id, rs[0].MessageId)
	require.Equal(t, uint32(originDom), rs[0].OriginDomain, "the vault serves the withdrawal")
	require.Equal(t, types.PortSentinel(portSo).String(), rs[0].TokenOut)
	require.Equal(t, "40000000", rs[0].MinAccepted)
	want := types.ExitAddress(factory, initCodeHash, user, types.PortSentinel(portSo), math.NewInt(40_000_000), 1)
	require.Equal(t, want, rs[0].ExitAddress)
	ledger, _, err := f.app.WarpLedgerKeeper.GetLedger(f.ctx, f.token, originDom)
	require.NoError(t, err)
	require.Equal(t, "40000000", ledger.Sent.String(), "collateral leaves the vault domain")
	ports, err := keeper.NewQueryServerImpl(f.k).Ports(f.ctx, &types.QueryPortsRequest{})
	require.NoError(t, err)
	require.Len(t, ports.Ports, 1)
}

func TestWithdrawFeeIsBurned(t *testing.T) {
	f := setup(t)
	require.NoError(t, f.k.FundPaymaster(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(1_000_000_000))))
	f.deliver(t, f.controller, f.payload(t, nil, &perptypes.MsgSetAutoTopUp{Sender: f.derived.String(), Enabled: true}))
	require.NoError(t, f.k.Executors.Set(f.ctx, originDom, types.Executor{
		AppId: f.appId, Domain: originDom, Address: util.CreateMockHexAddress("executor", 1), TokenId: f.token,
		MinCollateral: math.ZeroInt(), TargetCollateral: math.NewInt(1), WithdrawFeeBps: 100, NetRebalanced: math.ZeroInt(),
	}))
	supplyBefore := f.app.BankKeeper.GetSupply(f.ctx, "uluna").Amount
	_, err := f.k.Withdraw(f.ctx, f.derived.String(), f.token, math.NewInt(500_000_000), "", nil)
	require.NoError(t, err)
	supplyAfter := f.app.BankKeeper.GetSupply(f.ctx, "uluna").Amount
	require.Equal(t, "5000000", supplyBefore.Sub(supplyAfter).String(), "1 % fee burned")
	ledger, _, err := f.app.WarpLedgerKeeper.GetLedger(f.ctx, f.token, originDom)
	require.NoError(t, err)
	require.Equal(t, "495000000", ledger.Sent.String(), "the net amount left through warp")
}

func (f *fixture) conversionPayloadPort(t *testing.T, onBehalfOf []byte, port uint32, msgs ...sdk.Msg) []byte {
	t.Helper()
	bz := f.payload(t, onBehalfOf, msgs...)
	var p types.RemotePayload
	require.NoError(t, f.app.AppCodec().Unmarshal(bz, &p))
	p.PortDomain = port
	out, err := f.app.AppCodec().Marshal(&p)
	require.NoError(t, err)
	return out
}
