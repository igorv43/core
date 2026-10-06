package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	remotetypes "github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/stretchr/testify/require"
)

// spec §14.4 item 4: a remote account holding only the settlement asset
// submits a perp intent through its session key; the x/remote paymaster pays
// the x/batch intent fee (uluna). A local trader still pays its own.
func TestPerpIntentFeeSponsoredForRemoteAccount(t *testing.T) {
	f := setup(t)
	require.NoError(t, f.app.RemoteKeeper.InitGenesis(f.ctx, remotetypes.DefaultGenesisState()))
	controller := util.CreateMockHexAddress("evm-perp-user", 1)
	acct := remotetypes.DeriveAddress(97, controller)
	require.NoError(t, f.app.RemoteKeeper.Accounts.Set(f.ctx, acct.String(), remotetypes.RemoteAccount{
		Domain: 97, Controller: controller, Address: acct.String(),
	}))
	settlement := sdk.NewCoins(sdk.NewCoin("uusdc.lf", math.NewInt(200_000_000)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", settlement))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", acct, settlement))
	require.NoError(t, f.k.Deposit(f.ctx, acct, sdk.NewCoin("uusdc.lf", math.NewInt(100_000_000))))
	pm := remotetypes.PaymasterAddress()
	float := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(50_000_000)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", float))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", pm, float))

	session := sdk.AccAddress([]byte("perp-session-key-----"))
	_, err := f.app.RemoteKeeper.GrantSession(f.ctx, acct.String(), session.String(), 0)
	require.NoError(t, err)
	msg := authz.NewMsgExec(session, []sdk.Msg{&types.MsgSubmitPerpIntent{
		Sender: acct.String(), MarketId: marketID, Side: batchtypes.SIDE_BUY, Qty: math.NewInt(1_000),
		LimitPrice: math.LegacyNewDec(60_000), ExpiryHeight: f.ctx.BlockHeight() + 100,
	}})
	_, err = f.app.AuthzKeeper.Exec(f.ctx, &msg)
	require.NoError(t, err)
	require.True(t, f.app.BankKeeper.GetBalance(f.ctx, acct, "uluna").IsZero(), "the account never held uluna")
	require.Equal(t, float.AmountOf("uluna").Sub(f.bp.IntentFee.Amount).String(),
		f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount.String(), "the paymaster paid the intent fee")
	intents, err := f.bk.IntentsOfMarket(f.ctx, marketID, 10)
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, acct.String(), intents[0].Sender)

	// a local trader pays its own intent fee
	before := f.app.BankKeeper.GetBalance(f.ctx, f.long, "uluna").Amount
	pmBefore := f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount
	f.submit(t, f.long, batchtypes.SIDE_SELL, 1_000, math.LegacyNewDec(60_000), false)
	require.Equal(t, f.bp.IntentFee.Amount.String(), before.Sub(f.app.BankKeeper.GetBalance(f.ctx, f.long, "uluna").Amount).String())
	require.Equal(t, pmBefore.String(), f.app.BankKeeper.GetBalance(f.ctx, pm, "uluna").Amount.String())
}
