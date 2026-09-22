package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	terraapp "github.com/classic-terra/core/v4/app"
	apptesting "github.com/classic-terra/core/v4/app/testing"
	"github.com/classic-terra/core/v4/x/warpledger/keeper"
	"github.com/classic-terra/core/v4/x/warpledger/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

const (
	testDenom  = "uluna"
	testDomain = uint32(56) // bsc
)

type fixture struct {
	app     *terraapp.TerraApp
	ctx     sdk.Context
	tokenId util.HexAddress
}

// setup creates an app with one collateral warp token for uluna so that the
// ledger can be exercised without a mailbox: caps, accounting, invariant.
func setup(t *testing.T) fixture {
	t.Helper()

	app := apptesting.SetupApp(t, "warpledger-test")
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: "warpledger-test", Height: 1})

	// register a collateral token directly in x/warp state
	tokenId := util.GenerateHexAddress([20]byte{'w', 'a', 'r', 'p'}, uint32(warptypes.HYP_TOKEN_TYPE_COLLATERAL), 1)
	token := warptypes.HypToken{
		Id:                tokenId,
		Owner:             authtypes.NewModuleAddress("gov").String(),
		TokenType:         warptypes.HYP_TOKEN_TYPE_COLLATERAL,
		OriginMailbox:     util.NewZeroAddress(),
		OriginDenom:       testDenom,
		CollateralBalance: math.ZeroInt(),
	}
	require.NoError(t, app.WarpKeeper.HypTokens.Set(ctx, tokenId.GetInternalId(), token))

	return fixture{app: app, ctx: ctx, tokenId: tokenId}
}

func TestDefaultCapBlocksOutbound(t *testing.T) {
	f := setup(t)
	k := f.app.WarpLedgerKeeper

	// default params: cap 0 -> any positive amount exceeds it
	err := k.AssertOutboundAllowed(f.ctx, f.tokenId, testDomain, math.NewInt(1))
	require.ErrorIs(t, err, types.ErrDomainCapExceeded)

	// governance sets a cap, transfers up to it are allowed
	require.NoError(t, k.SetDomainCap(f.ctx, f.tokenId, testDomain, math.NewInt(1000)))
	require.NoError(t, k.AssertOutboundAllowed(f.ctx, f.tokenId, testDomain, math.NewInt(1000)))
	require.ErrorIs(t, k.AssertOutboundAllowed(f.ctx, f.tokenId, testDomain, math.NewInt(1001)), types.ErrDomainCapExceeded)
}

func TestSentReceivedExposure(t *testing.T) {
	f := setup(t)
	k := f.app.WarpLedgerKeeper

	require.NoError(t, k.SetDomainCap(f.ctx, f.tokenId, testDomain, math.NewInt(1000)))
	require.NoError(t, k.RecordSent(f.ctx, f.tokenId, testDomain, math.NewInt(600)))
	require.NoError(t, k.RecordSent(f.ctx, f.tokenId, testDomain, math.NewInt(300)))
	require.NoError(t, k.RecordReceived(f.ctx, f.tokenId, testDomain, math.NewInt(200)))

	ledger, exists, err := k.GetLedger(f.ctx, f.tokenId, testDomain)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "900", ledger.Sent.String())
	require.Equal(t, "200", ledger.Received.String())
	require.Equal(t, "700", keeper.Exposure(ledger).String())

	// exposure 700 + 300 = 1000 is allowed, 301 is not
	require.NoError(t, k.AssertOutboundAllowed(f.ctx, f.tokenId, testDomain, math.NewInt(300)))
	require.ErrorIs(t, k.AssertOutboundAllowed(f.ctx, f.tokenId, testDomain, math.NewInt(301)), types.ErrDomainCapExceeded)

	// a second domain has its own ledger and its own (default) cap
	ledger2, exists2, err := k.GetLedger(f.ctx, f.tokenId, testDomain+1)
	require.NoError(t, err)
	require.False(t, exists2)
	require.True(t, keeper.Exposure(ledger2).IsZero())
}

func TestInvariantTripsCircuit(t *testing.T) {
	f := setup(t)
	k := f.app.WarpLedgerKeeper

	// exposure recorded without any collateral in the warp module account
	require.NoError(t, k.SetDomainCap(f.ctx, f.tokenId, testDomain, math.NewInt(1000)))
	require.NoError(t, k.RecordSent(f.ctx, f.tokenId, testDomain, math.NewInt(500)))

	allowed, err := f.app.CircuitKeeper.IsAllowed(f.ctx, keeper.RemoteTransferMsgURL)
	require.NoError(t, err)
	require.True(t, allowed, "transfers must be allowed before the invariant check")

	require.NoError(t, k.EndBlocker(f.ctx))

	allowed, err = f.app.CircuitKeeper.IsAllowed(f.ctx, keeper.RemoteTransferMsgURL)
	require.NoError(t, err)
	require.False(t, allowed, "invariant violation must trip MsgRemoteTransfer")

	// sweeping is refused while transfers are paused
	_, err = k.SweepMigration(f.ctx, f.tokenId, testDomain, util.CreateMockHexAddress("recipient", 1))
	require.ErrorIs(t, err, types.ErrTransfersPaused)
}

func TestInvariantHoldsWithCollateral(t *testing.T) {
	f := setup(t)
	k := f.app.WarpLedgerKeeper

	// fund the warp module account with the collateral that backs the exposure
	warpAddr := authtypes.NewModuleAddress(warptypes.ModuleName)
	coins := sdk.NewCoins(sdk.NewCoin(testDenom, math.NewInt(500)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, warptypes.ModuleName, coins))
	require.Equal(t, "500", f.app.BankKeeper.GetBalance(f.ctx, warpAddr, testDenom).Amount.String())

	require.NoError(t, k.SetDomainCap(f.ctx, f.tokenId, testDomain, math.NewInt(1000)))
	require.NoError(t, k.RecordSent(f.ctx, f.tokenId, testDomain, math.NewInt(500)))
	require.NoError(t, k.EndBlocker(f.ctx))

	allowed, err := f.app.CircuitKeeper.IsAllowed(f.ctx, keeper.RemoteTransferMsgURL)
	require.NoError(t, err)
	require.True(t, allowed, "a fully backed exposure must not trip the breaker")
}

func TestDepositAddressIsDeterministic(t *testing.T) {
	recipient := util.CreateMockHexAddress("recipient", 7)
	tokenA := util.CreateMockHexAddress("token", 1)
	tokenB := util.CreateMockHexAddress("token", 2)

	a1 := types.DeriveDepositAddress(tokenA, testDomain, recipient)
	a2 := types.DeriveDepositAddress(tokenA, testDomain, recipient)
	require.Equal(t, a1, a2)
	require.Len(t, a1, 32)

	require.NotEqual(t, a1, types.DeriveDepositAddress(tokenB, testDomain, recipient))
	require.NotEqual(t, a1, types.DeriveDepositAddress(tokenA, testDomain+1, recipient))
	require.NotEqual(t, a1, types.DeriveDepositAddress(tokenA, testDomain, util.CreateMockHexAddress("recipient", 8)))
}

func TestSweepRequiresBalanceAndCollateralToken(t *testing.T) {
	f := setup(t)
	k := f.app.WarpLedgerKeeper
	recipient := util.CreateMockHexAddress("recipient", 1)

	_, err := k.SweepMigration(f.ctx, f.tokenId, testDomain, recipient)
	require.ErrorIs(t, err, types.ErrNothingToSweep)

	_, err = k.SweepMigration(f.ctx, util.CreateMockHexAddress("missing", 9), testDomain, recipient)
	require.ErrorIs(t, err, types.ErrTokenNotFound)
}

func TestGenesisRoundTrip(t *testing.T) {
	f := setup(t)
	k := f.app.WarpLedgerKeeper

	require.NoError(t, k.SetDomainCap(f.ctx, f.tokenId, testDomain, math.NewInt(1000)))
	require.NoError(t, k.RecordSent(f.ctx, f.tokenId, testDomain, math.NewInt(42)))

	exported, err := k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.Ledgers, 1)
	require.NoError(t, exported.Validate())

	// re-import into a fresh context and compare
	f2 := setup(t)
	require.NoError(t, f2.app.WarpLedgerKeeper.InitGenesis(f2.ctx, exported))
	ledger, exists, err := f2.app.WarpLedgerKeeper.GetLedger(f2.ctx, f.tokenId, testDomain)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "42", ledger.Sent.String())
	require.True(t, ledger.HasCap)
	require.Equal(t, "1000", ledger.Cap.String())
}
