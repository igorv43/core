package circuit_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	circuittypes "cosmossdk.io/x/circuit/types"
	apptesting "github.com/classic-terra/core/v4/app/testing"
	customcircuittypes "github.com/classic-terra/core/v4/custom/circuit/types"
	liquidstaketypes "github.com/classic-terra/core/v4/x/liquidstake/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"
)

var (
	unstakeURL = sdk.MsgTypeURL(&liquidstaketypes.MsgUnstake{})
	claimURL   = sdk.MsgTypeURL(&liquidstaketypes.MsgClaim{})
	stakeURL   = sdk.MsgTypeURL(&liquidstaketypes.MsgStake{})
)

// routeMsg executes msg through the app's MsgServiceRouter, which is the path
// x/gov uses to execute the messages of a passed proposal (no ante handler).
func routeMsg(t *testing.T, ctx sdk.Context, router *baseapp.MsgServiceRouter, msg sdk.Msg) error {
	t.Helper()
	handler := router.Handler(msg)
	require.NotNil(t, handler, "no handler registered for %s", sdk.MsgTypeURL(msg))
	_, err := handler(ctx, msg)
	return err
}

// TestGovernanceCannotTripProtectedMsgs reproduces spec-conformance row B-04
// (prop 27): a MsgTripCircuitBreaker whose authority is x/gov, executed
// through the message router exactly as a passed proposal is, must not put
// MsgUnstake / MsgClaim on the disabled list. Tripping an allowed type keeps
// working.
func TestGovernanceCannotTripProtectedMsgs(t *testing.T) {
	app := apptesting.SetupApp(t, "circuit-protected-test")
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: "circuit-protected-test", Height: 1})
	router := app.MsgServiceRouter()
	gov := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	for _, url := range []string{unstakeURL, claimURL} {
		t.Run("gov trip of "+url+" is refused", func(t *testing.T) {
			err := routeMsg(t, ctx, router, &circuittypes.MsgTripCircuitBreaker{Authority: gov, MsgTypeUrls: []string{url}})
			require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
			require.ErrorContains(t, err, "can never be disabled by the circuit breaker")
			has, err := app.CircuitKeeper.DisableList.Has(ctx, url)
			require.NoError(t, err)
			require.False(t, has)
		})
	}

	t.Run("gov trip mixing an allowed and a protected type is refused atomically", func(t *testing.T) {
		err := routeMsg(t, ctx, router, &circuittypes.MsgTripCircuitBreaker{Authority: gov, MsgTypeUrls: []string{stakeURL, unstakeURL}})
		require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
		has, err := app.CircuitKeeper.DisableList.Has(ctx, stakeURL)
		require.NoError(t, err)
		require.False(t, has, "nothing may be disabled when the trip is refused")
	})

	t.Run("gov trip of an allowed type still works", func(t *testing.T) {
		require.NoError(t, routeMsg(t, ctx, router, &circuittypes.MsgTripCircuitBreaker{Authority: gov, MsgTypeUrls: []string{stakeURL}}))
		allowed, err := app.CircuitKeeper.IsAllowed(ctx, stakeURL)
		require.NoError(t, err)
		require.False(t, allowed)
		require.NoError(t, routeMsg(t, ctx, router, &circuittypes.MsgResetCircuitBreaker{Authority: gov, MsgTypeUrls: []string{stakeURL}}))
	})

	grantee := sdk.AccAddress([]byte("emergency-account----")).String()

	t.Run("gov authorize listing a protected type is refused", func(t *testing.T) {
		err := routeMsg(t, ctx, router, &circuittypes.MsgAuthorizeCircuitBreaker{
			Granter: gov,
			Grantee: grantee,
			Permissions: &circuittypes.Permissions{
				Level:         circuittypes.Permissions_LEVEL_SOME_MSGS,
				LimitTypeUrls: []string{stakeURL, unstakeURL},
			},
		})
		require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
		_, err = app.CircuitKeeper.Permissions.Get(ctx, sdk.MustAccAddressFromBech32(grantee))
		require.Error(t, err, "no permission may be stored when the grant is refused")
	})

	t.Run("super admin cannot trip a protected type either", func(t *testing.T) {
		require.NoError(t, routeMsg(t, ctx, router, &circuittypes.MsgAuthorizeCircuitBreaker{
			Granter:     gov,
			Grantee:     grantee,
			Permissions: &circuittypes.Permissions{Level: circuittypes.Permissions_LEVEL_SUPER_ADMIN},
		}))
		err := routeMsg(t, ctx, router, &circuittypes.MsgTripCircuitBreaker{Authority: grantee, MsgTypeUrls: []string{claimURL}})
		require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
		has, err := app.CircuitKeeper.DisableList.Has(ctx, claimURL)
		require.NoError(t, err)
		require.False(t, has)
	})
}

// TestPreSeededProtectedEntryDoesNotBlockTx covers entries already stored in
// the x/circuit disabled list (for example by prop 27 before this fix): the
// app's ante handler must ignore them for protected types while still
// enforcing every other disabled entry.
func TestPreSeededProtectedEntryDoesNotBlockTx(t *testing.T) {
	app := apptesting.SetupApp(t, "circuit-preseed-test")
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: "circuit-preseed-test", Height: 1})

	require.NoError(t, app.CircuitKeeper.DisableList.Set(ctx, unstakeURL))
	require.NoError(t, app.CircuitKeeper.DisableList.Set(ctx, stakeURL))

	priv := secp256k1.GenPrivKey()
	sender := sdk.AccAddress(priv.PubKey().Address()).String()
	amount := sdk.NewCoin("stluna", sdkmath.NewInt(1))

	// buildTx returns an unsigned-but-well-formed tx: one placeholder
	// signature so ValidateBasic passes and the circuit decorator (which runs
	// right after it) is reached; signature verification fails later.
	buildTx := func(msg sdk.Msg) sdk.Tx {
		b := app.GetTxConfig().NewTxBuilder()
		require.NoError(t, b.SetMsgs(msg))
		b.SetGasLimit(200000)
		b.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("uluna", sdkmath.NewInt(10000000))))
		require.NoError(t, b.SetSignatures(signing.SignatureV2{
			PubKey:   priv.PubKey(),
			Data:     &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT, Signature: []byte{}},
			Sequence: 0,
		}))
		return b.GetTx()
	}

	ante := app.AnteHandler()
	require.NotNil(t, ante)

	t.Run("MsgUnstake is not blocked by a stored disabled entry", func(t *testing.T) {
		_, err := ante(ctx.WithIsCheckTx(true), buildTx(&liquidstaketypes.MsgUnstake{Sender: sender, Amount: amount}), false)
		// The tx fails later (no account, empty signature), never at the circuit.
		require.Error(t, err)
		require.NotContains(t, err.Error(), "tx type not allowed")
	})

	t.Run("a disabled non-protected type is still blocked", func(t *testing.T) {
		_, err := ante(ctx.WithIsCheckTx(true), buildTx(&liquidstaketypes.MsgStake{Sender: sender, Amount: amount}), false)
		require.ErrorContains(t, err, "tx type not allowed")
	})

	t.Run("protected list is the one shared with the ante guard", func(t *testing.T) {
		require.ElementsMatch(t, []string{unstakeURL, claimURL}, customcircuittypes.ProtectedMsgTypeURLs())
	})
}
