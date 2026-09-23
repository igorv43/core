package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/classic-terra/core/v4/x/warpledger/keeper"
	"github.com/classic-terra/core/v4/x/warpledger/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

const (
	originEth  = uint32(1)
	originBase = uint32(8453)
	originArb  = uint32(42161)
)

// syntheticFixture adds a synthetic warp token (the settlement basket of
// spec §11.4) next to the collateral token of setup.
func syntheticFixture(t *testing.T) (fixture, util.HexAddress) {
	t.Helper()
	f := setup(t)
	id := util.GenerateHexAddress([20]byte{'w', 'a', 'r', 'p'}, uint32(warptypes.HYP_TOKEN_TYPE_SYNTHETIC), 2)
	require.NoError(t, f.app.WarpKeeper.HypTokens.Set(f.ctx, id.GetInternalId(), warptypes.HypToken{
		Id: id, Owner: authtypes.NewModuleAddress("gov").String(), TokenType: warptypes.HYP_TOKEN_TYPE_SYNTHETIC,
		OriginMailbox: util.NewZeroAddress(), OriginDenom: "uusdc.lf", CollateralBalance: math.ZeroInt(),
	}))
	return f, id
}

func TestBasketRequiresSyntheticToken(t *testing.T) {
	f, synthetic := syntheticFixture(t)
	k := f.app.WarpLedgerKeeper
	require.ErrorIs(t, k.SetBasketToken(f.ctx, f.tokenId, true), types.ErrNotSyntheticToken)
	require.NoError(t, k.SetBasketToken(f.ctx, synthetic, true))
	basket, err := k.IsBasket(f.ctx, synthetic)
	require.NoError(t, err)
	require.True(t, basket)
	require.NoError(t, k.SetBasketToken(f.ctx, synthetic, false))
	basket, err = k.IsBasket(f.ctx, synthetic)
	require.NoError(t, err)
	require.False(t, basket)
}

// TestOriginShareCapBoundsDeposits: with the 40 % default, the first origin
// can back everything while alone? No: a single origin of a basket can never
// exceed 40 % of the circulation, so the basket only fills once at least
// three origins contribute (spec §11.4 item 4, "the weakest link").
func TestOriginShareCapBoundsDeposits(t *testing.T) {
	f, synthetic := syntheticFixture(t)
	k := f.app.WarpLedgerKeeper
	require.NoError(t, k.SetBasketToken(f.ctx, synthetic, true))

	// a token that is not a basket is unconstrained
	require.NoError(t, k.AssertInboundAllowed(f.ctx, f.tokenId, originEth, math.NewInt(1_000_000)))

	// first deposit into an empty basket: 100 % share > 40 %
	require.ErrorIs(t, k.AssertInboundAllowed(f.ctx, synthetic, originEth, math.NewInt(100)), types.ErrSourceCapExceeded)

	// bootstrap: governance lifts the caps to 100 % for the seeding phase
	one := math.LegacyOneDec()
	for _, d := range []uint32{originEth, originBase, originArb} {
		require.NoError(t, k.SetOriginPolicy(f.ctx, synthetic, d, &one, false))
	}
	require.NoError(t, k.AssertInboundAllowed(f.ctx, synthetic, originEth, math.NewInt(100)))
	require.NoError(t, k.RecordReceived(f.ctx, synthetic, originEth, math.NewInt(100)))
	require.NoError(t, k.RecordReceived(f.ctx, synthetic, originBase, math.NewInt(100)))
	require.NoError(t, k.RecordReceived(f.ctx, synthetic, originArb, math.NewInt(100)))

	// back to the default 40 %: each origin is at 33.3 %
	for _, d := range []uint32{originEth, originBase, originArb} {
		require.NoError(t, k.SetOriginPolicy(f.ctx, synthetic, d, nil, false))
	}
	// headroom of one origin: (0.4·300 − 100) / 0.6 = 33
	l, _, err := k.GetLedger(f.ctx, synthetic, originEth)
	require.NoError(t, err)
	circ, err := k.Circulation(f.ctx, synthetic)
	require.NoError(t, err)
	require.Equal(t, "300", circ.String())
	require.Equal(t, "33", keeper.Headroom(keeper.Collateral(l), circ, math.LegacyNewDecWithPrec(40, 2)).String())
	require.NoError(t, k.AssertInboundAllowed(f.ctx, synthetic, originEth, math.NewInt(33)))
	require.ErrorIs(t, k.AssertInboundAllowed(f.ctx, synthetic, originEth, math.NewInt(34)), types.ErrSourceCapExceeded)

	// a smaller cap for an origin without native issuance (item 3)
	tenPct := math.LegacyNewDecWithPrec(10, 2)
	require.NoError(t, k.SetOriginPolicy(f.ctx, synthetic, originBase, &tenPct, false))
	require.ErrorIs(t, k.AssertInboundAllowed(f.ctx, synthetic, originBase, math.NewInt(1)), types.ErrSourceCapExceeded)

	// redemptions reduce the origin's collateral and free headroom again
	require.NoError(t, k.RecordSent(f.ctx, synthetic, originBase, math.NewInt(90)))
	require.NoError(t, k.AssertInboundAllowed(f.ctx, synthetic, originBase, math.NewInt(1)))
}

func TestPausedOriginRefusesDepositsAndRedemptions(t *testing.T) {
	f, synthetic := syntheticFixture(t)
	k := f.app.WarpLedgerKeeper
	require.NoError(t, k.SetBasketToken(f.ctx, synthetic, true))
	one := math.LegacyOneDec()
	require.NoError(t, k.SetOriginPolicy(f.ctx, synthetic, originEth, &one, false))
	require.NoError(t, k.RecordReceived(f.ctx, synthetic, originEth, math.NewInt(100)))

	require.NoError(t, k.SetOriginPolicy(f.ctx, synthetic, originEth, &one, true))
	require.ErrorIs(t, k.AssertInboundAllowed(f.ctx, synthetic, originEth, math.NewInt(1)), types.ErrOriginPaused)
	require.ErrorIs(t, k.AssertOutboundAllowed(f.ctx, synthetic, originEth, math.NewInt(1)), types.ErrOriginPaused)

	// the other origins keep serving redemptions (item 6)
	require.NoError(t, k.SetDomainCap(f.ctx, synthetic, originBase, math.NewInt(1_000)))
	require.NoError(t, k.AssertOutboundAllowed(f.ctx, synthetic, originBase, math.NewInt(1)))

	require.NoError(t, k.SetOriginPolicy(f.ctx, synthetic, originEth, &one, false))
	require.NoError(t, k.AssertInboundAllowed(f.ctx, synthetic, originEth, math.NewInt(1)))
}

func TestBasketQueryAndGenesis(t *testing.T) {
	f, synthetic := syntheticFixture(t)
	k := f.app.WarpLedgerKeeper
	require.NoError(t, k.SetBasketToken(f.ctx, synthetic, true))
	one := math.LegacyOneDec()
	require.NoError(t, k.SetOriginPolicy(f.ctx, synthetic, originEth, &one, false))
	require.NoError(t, k.RecordReceived(f.ctx, synthetic, originEth, math.NewInt(100)))
	require.NoError(t, k.SetOriginPolicy(f.ctx, synthetic, originEth, nil, false))

	qs := keeper.NewQueryServerImpl(k)
	res, err := qs.WarpLedger(f.ctx, &types.QueryWarpLedgerRequest{TokenId: synthetic.String()})
	require.NoError(t, err)
	require.True(t, res.Basket)
	require.Equal(t, "100", res.Circulation.String())
	require.Len(t, res.Domains, 1)
	require.Equal(t, "100", res.Domains[0].Collateral.String())
	require.Equal(t, "1.000000000000000000", res.Domains[0].Share.String())
	require.Equal(t, "0.400000000000000000", res.Domains[0].ShareCap.String())
	require.Equal(t, "0", res.Domains[0].Headroom.String())

	exported, err := k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Equal(t, []string{synthetic.String()}, exported.BasketTokens)
	require.NoError(t, exported.Validate())
	f2, _ := syntheticFixture(t)
	require.NoError(t, f2.app.WarpLedgerKeeper.InitGenesis(f2.ctx, exported))
	basket, err := f2.app.WarpLedgerKeeper.IsBasket(f2.ctx, synthetic)
	require.NoError(t, err)
	require.True(t, basket)
}
