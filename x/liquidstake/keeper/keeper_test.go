package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	terraapp "github.com/classic-terra/core/v4/app"
	apptesting "github.com/classic-terra/core/v4/app/testing"
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

const chainID = "liquidstake-test"

type fixture struct {
	app     *terraapp.TerraApp
	ctx     sdk.Context
	user    sdk.AccAddress
	valAddr sdk.ValAddress
}

// setup boots the app with the repository test helper (which initialises
// module params explicitly), creates one bonded validator through the
// staking message server, funds a user and opens the first epoch.
func setup(t *testing.T) *fixture {
	t.Helper()
	h := &apptesting.KeeperTestHelper{}
	h.SetT(t)
	h.Setup(t, chainID)
	app, ctx := h.App, h.Ctx
	require.NoError(t, app.SlashingKeeper.SetParams(ctx, slashingtypes.DefaultParams()))

	fund := func(addr sdk.AccAddress, amount int64) {
		coins := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(amount)))
		require.NoError(t, app.BankKeeper.MintCoins(ctx, "mint", coins))
		require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", addr, coins))
	}

	// validator: 1,000,000 LUNC self-delegated so the module cap (20%) allows the tests
	priv := ed25519.GenPrivKey()
	valAddr := sdk.ValAddress(priv.PubKey().Address())
	fund(sdk.AccAddress(valAddr), 2_000_000_000_000)
	msg, err := stakingtypes.NewMsgCreateValidator(
		valAddr.String(), priv.PubKey(),
		sdk.NewCoin("uluna", math.NewInt(1_000_000_000_000)),
		stakingtypes.NewDescription("val", "", "", "", ""),
		stakingtypes.NewCommissionRates(math.LegacyNewDecWithPrec(5, 2), math.LegacyNewDecWithPrec(20, 2), math.LegacyNewDecWithPrec(1, 2)),
		math.OneInt(),
	)
	require.NoError(t, err)
	_, err = stakingkeeper.NewMsgServerImpl(app.StakingKeeper).CreateValidator(ctx, msg)
	require.NoError(t, err)
	_, err = app.StakingKeeper.ApplyAndReturnValidatorSetUpdates(ctx)
	require.NoError(t, err)
	val, err := app.StakingKeeper.GetValidator(ctx, valAddr)
	require.NoError(t, err)
	require.True(t, val.IsBonded())

	user := sdk.AccAddress([]byte("liquid-staker--------"))
	fund(user, 1_000_000_000)

	// the helper does not run InitGenesis: initialise the module like the upgrade does
	require.NoError(t, app.LiquidStakeKeeper.InitGenesis(ctx, types.DefaultGenesisState()))

	// first EndBlock opens epoch 1 without processing
	require.NoError(t, app.LiquidStakeKeeper.EndBlocker(ctx))
	e, err := app.LiquidStakeKeeper.GetEpoch(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), e.Number)

	// short epochs; a single validator may hold the whole module in this devnet
	p := types.DefaultParams()
	p.EpochBlocks = 10
	p.ValidatorCap = math.LegacyOneDec()
	require.NoError(t, app.LiquidStakeKeeper.SetParams(ctx, p))

	return &fixture{app: app, ctx: ctx, user: user, valAddr: valAddr}
}

func (f *fixture) advance(t *testing.T, blocks int64, d time.Duration) {
	t.Helper()
	h := f.ctx.BlockHeader()
	h.Height += blocks
	h.Time = h.Time.Add(d)
	f.ctx = f.ctx.WithBlockHeader(h).WithBlockHeight(h.Height).WithBlockTime(h.Time)
}

func (f *fixture) balance(denom string) math.Int {
	return f.app.BankKeeper.GetBalance(f.ctx, f.user, denom).Amount
}

func TestStakeMintsAtRateOneThenDelegatesAtEpoch(t *testing.T) {
	f := setup(t)
	k := f.app.LiquidStakeKeeper

	minted, rate, err := k.Stake(f.ctx, f.user, sdk.NewCoin("uluna", math.NewInt(100_000_000)))
	require.NoError(t, err)
	require.True(t, rate.Equal(math.LegacyOneDec()))
	require.Equal(t, "100000000", minted.Amount.String())
	require.Equal(t, "100000000", f.balance(types.StDenom).String())

	rate, totals, err := k.ExchangeRate(f.ctx)
	require.NoError(t, err)
	require.True(t, rate.Equal(math.LegacyOneDec()))
	require.Equal(t, "100000000", totals.Buffer().String())
	require.True(t, totals.Delegated.IsZero())

	// epoch boundary: everything above the 2% buffer is delegated
	f.advance(t, 10, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx))
	_, totals, err = k.ExchangeRate(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "98000000", totals.Delegated.String())
	require.Equal(t, "2000000", totals.Buffer().String())
	require.NoError(t, k.CheckInvariants(f.ctx))

	st, err := k.Validators.Get(f.ctx, f.valAddr.String())
	require.NoError(t, err)
	require.True(t, st.Eligible, st.Reason)
}

func TestUnstakeInstantFromBufferAndQueued(t *testing.T) {
	f := setup(t)
	k := f.app.LiquidStakeKeeper
	_, _, err := k.Stake(f.ctx, f.user, sdk.NewCoin("uluna", math.NewInt(100_000_000)))
	require.NoError(t, err)
	f.advance(t, 10, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx)) // delegates 98M, buffer 2M

	before := f.balance("uluna")
	res, err := k.Unstake(f.ctx, f.user, sdk.NewCoin(types.StDenom, math.NewInt(1_000_000)))
	require.NoError(t, err)
	require.True(t, res.Instant, "1M within the 2M buffer is paid instantly")
	require.Equal(t, "1000000", res.Amount.Amount.String())
	require.Equal(t, before.Add(math.NewInt(1_000_000)).String(), f.balance("uluna").String())

	before = f.balance("uluna")
	res, err = k.Unstake(f.ctx, f.user, sdk.NewCoin(types.StDenom, math.NewInt(50_000_000)))
	require.NoError(t, err)
	require.False(t, res.Instant, "50M above the buffer is queued")
	require.Equal(t, uint64(1), res.RequestID)
	require.Equal(t, before.String(), f.balance("uluna").String())
	owed, err := k.GetOwed(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "50000000", owed.String())
	require.Equal(t, "49000000", f.balance(types.StDenom).String())
	require.NoError(t, k.CheckInvariants(f.ctx))

	rate, _, err := k.ExchangeRate(f.ctx)
	require.NoError(t, err)
	require.True(t, rate.Equal(math.LegacyOneDec()), "burned receipts do not move the rate: %s", rate)

	_, _, err = k.Claim(f.ctx, f.user)
	require.ErrorIs(t, err, types.ErrNothingToClaim)

	// epoch: one undelegation batch for the queue
	f.advance(t, 10, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx))
	req, err := k.Requests.Get(f.ctx, 1)
	require.NoError(t, err)
	require.True(t, req.Undelegated)
	require.True(t, req.CompletionTime.After(f.ctx.BlockTime()))
	_, totals, err := k.ExchangeRate(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "50000000", totals.Unbonding.String())
	require.NoError(t, k.CheckInvariants(f.ctx))

	// mature the unbonding and claim
	unbonding, err := f.app.StakingKeeper.UnbondingTime(f.ctx)
	require.NoError(t, err)
	f.advance(t, 1, unbonding+time.Second)
	_, err = f.app.StakingKeeper.CompleteUnbonding(f.ctx, k.ModuleAddress(), f.valAddr)
	require.NoError(t, err)

	before = f.balance("uluna")
	paid, ids, err := k.Claim(f.ctx, f.user)
	require.NoError(t, err)
	require.Equal(t, []uint64{1}, ids)
	require.Equal(t, "50000000", paid.Amount.String())
	require.Equal(t, before.Add(math.NewInt(50_000_000)).String(), f.balance("uluna").String())
	owed, err = k.GetOwed(f.ctx)
	require.NoError(t, err)
	require.True(t, owed.IsZero())
	require.NoError(t, k.CheckInvariants(f.ctx))
}

func TestModuleCapBlocksDeposits(t *testing.T) {
	f := setup(t)
	k := f.app.LiquidStakeKeeper
	p, err := k.GetParams(f.ctx)
	require.NoError(t, err)
	bonded, err := f.app.StakingKeeper.TotalBondedTokens(f.ctx)
	require.NoError(t, err)
	cap := p.MaxShare.MulInt(bonded).TruncateInt()

	coins := sdk.NewCoins(sdk.NewCoin("uluna", cap.Add(math.OneInt())))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", coins))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToAccount(f.ctx, "mint", f.user, coins))

	_, _, err = k.Stake(f.ctx, f.user, sdk.NewCoin("uluna", cap.Add(math.OneInt())))
	require.ErrorIs(t, err, types.ErrModuleCapReached)
	_, _, err = k.Stake(f.ctx, f.user, sdk.NewCoin("uluna", cap))
	require.NoError(t, err)
}

func TestRewardsRaiseTheRateAndFeeIsBurned(t *testing.T) {
	f := setup(t)
	k := f.app.LiquidStakeKeeper
	_, _, err := k.Stake(f.ctx, f.user, sdk.NewCoin("uluna", math.NewInt(100_000_000)))
	require.NoError(t, err)
	f.advance(t, 10, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx))

	// an epoch of rewards: 10M uluna allocated to the validator one block later
	// (x/distribution accrues nothing for the block in which a delegation starts)
	f.advance(t, 1, time.Minute)
	reward := sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(10_000_000)))
	require.NoError(t, f.app.BankKeeper.MintCoins(f.ctx, "mint", reward))
	require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToModule(f.ctx, "mint", "distribution", reward))
	val, err := f.app.StakingKeeper.GetValidator(f.ctx, f.valAddr)
	require.NoError(t, err)
	require.NoError(t, f.app.DistrKeeper.AllocateTokensToValidator(f.ctx, val, sdk.NewDecCoinsFromCoins(reward...)))

	rateBefore, totalsBefore, err := k.ExchangeRate(f.ctx)
	require.NoError(t, err)
	require.True(t, totalsBefore.PendingRewards.IsPositive(), "pending rewards are priced in before the epoch")
	require.True(t, rateBefore.GT(math.LegacyOneDec()))

	burnAddr := f.app.AccountKeeper.GetModuleAddress("burn")
	burnBefore := f.app.BankKeeper.GetBalance(f.ctx, burnAddr, "uluna").Amount
	f.advance(t, 10, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx))
	burnAfter := f.app.BankKeeper.GetBalance(f.ctx, burnAddr, "uluna").Amount
	require.True(t, burnAfter.GT(burnBefore), "20% of the 5% fee reaches the burn account")

	rateAfter, totals, err := k.ExchangeRate(f.ctx)
	require.NoError(t, err)
	require.True(t, rateAfter.GT(math.LegacyOneDec()))
	require.True(t, totals.PendingRewards.IsZero(), "rewards were withdrawn at the epoch")
	require.NoError(t, k.CheckInvariants(f.ctx))
}

func TestGenesisRoundTrip(t *testing.T) {
	f := setup(t)
	k := f.app.LiquidStakeKeeper
	_, _, err := k.Stake(f.ctx, f.user, sdk.NewCoin("uluna", math.NewInt(10_000_000)))
	require.NoError(t, err)
	_, err = k.Unstake(f.ctx, f.user, sdk.NewCoin(types.StDenom, math.NewInt(9_000_000)))
	require.NoError(t, err) // instant: everything is still in the buffer before the first epoch
	f.advance(t, 10, time.Minute)
	require.NoError(t, k.EndBlocker(f.ctx)) // delegates the rest
	res, err := k.Unstake(f.ctx, f.user, sdk.NewCoin(types.StDenom, math.NewInt(1_000_000)))
	require.NoError(t, err)
	require.False(t, res.Instant, "buffer is only 2% now")

	gs, err := k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, gs.Validate())
	require.Len(t, gs.UnstakeRequests, 1)
	require.Equal(t, uint64(2), gs.NextRequestId)
	require.True(t, gs.Owed.Equal(math.NewInt(1_000_000)))
}
