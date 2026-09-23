package interchaintest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/chain/cosmos"
	"github.com/cosmos/interchaintest/v10/ibc"
	"github.com/cosmos/interchaintest/v10/testreporter"
	"github.com/cosmos/interchaintest/v10/testutil"
	"github.com/icza/dyno"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// fabricModules are the Liquidity Fabric modules every node must serve.
var fabricModules = []string{"warpledger", "liquidstake", "batch", "perp", "remote", "ismbond"}

// fabricGenesis applies the standard genesis changes and pins the staking
// bond denom to the chain denom so x/liquidstake accepts it.
func fabricGenesis() func(ibc.ChainConfig, []byte) ([]byte, error) {
	base := ModifyGenesis()
	return func(cfg ibc.ChainConfig, genbz []byte) ([]byte, error) {
		genbz, err := base(cfg, genbz)
		if err != nil {
			return nil, err
		}
		g := make(map[string]interface{})
		if err := json.Unmarshal(genbz, &g); err != nil {
			return nil, err
		}
		if err := dyno.Set(g, cfg.Denom, "app_state", "staking", "params", "bond_denom"); err != nil {
			return nil, fmt.Errorf("failed to set bond denom: %w", err)
		}
		// epochs every few blocks so the delegation of the stake is observable
		if err := dyno.Set(g, "5", "app_state", "liquidstake", "params", "epoch_blocks"); err != nil {
			return nil, fmt.Errorf("failed to set liquidstake epoch: %w", err)
		}
		return json.Marshal(g)
	}
}

// TestFabric starts a chain from the local image and exercises the Liquidity
// Fabric modules end to end: every module answers its params query, the
// insurance fund and the batch pipeline are readable, and a user can stake
// LUNC into stLUNC, see the module delegate it at the epoch, and unstake.
func TestFabric(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	t.Parallel()

	numVals, numFullNodes := 1, 1
	config, err := createConfig()
	require.NoError(t, err)
	config.ModifyGenesis = fabricGenesis()

	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		{Name: "terra", ChainConfig: config, NumValidators: &numVals, NumFullNodes: &numFullNodes},
	})
	chains, err := cf.Chains(t.Name())
	require.NoError(t, err)
	terra := chains[0].(*cosmos.CosmosChain)

	ic := interchaintest.NewInterchain().AddChain(terra)
	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)
	ctx := context.Background()
	client, network := interchaintest.DockerSetup(t)
	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName: t.Name(), Client: client, NetworkID: network, SkipPathCreation: true,
	}))
	t.Cleanup(func() { _ = ic.Close() })
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, terra))

	node := terra.GetNode()

	// 1. every Fabric module is wired: params are served with sane defaults
	for _, m := range fabricModules {
		stdout, _, err := node.ExecQuery(ctx, m, "params")
		require.NoError(t, err, m)
		var res map[string]interface{}
		require.NoError(t, json.Unmarshal(stdout, &res), m)
		require.Contains(t, res, "params", m)
	}
	assertField := func(module string, field, want string, args ...string) {
		stdout, _, err := node.ExecQuery(ctx, append([]string{module}, args...)...)
		require.NoError(t, err)
		var res map[string]interface{}
		require.NoError(t, json.Unmarshal(stdout, &res))
		got, err := dyno.GetString(res, "params", field)
		require.NoError(t, err, "%s.%s", module, field)
		require.Equal(t, want, got, "%s.%s", module, field)
	}
	assertField("perp", "settlement_denom", "uusd", "params")
	assertField("perp", "st_denom", "stluna", "params")
	assertField("liquidstake", "epoch_blocks", "5", "params")
	assertField("batch", "commit_window", "2", "params")

	// 2. derived views answer before any activity
	stdout, _, err := node.ExecQuery(ctx, "perp", "insurance-fund")
	require.NoError(t, err)
	require.Contains(t, string(stdout), "target")
	stdout, _, err = node.ExecQuery(ctx, "batch", "pipeline")
	require.NoError(t, err)
	require.NotEmpty(t, stdout)
	stdout, _, err = node.ExecQuery(ctx, "liquidstake", "exchange-rate")
	require.NoError(t, err)
	var er map[string]interface{}
	require.NoError(t, json.Unmarshal(stdout, &er))
	rate, err := dyno.GetString(er, "exchange_rate")
	require.NoError(t, err)
	// autocli renders a LegacyDec either as a decimal string or as its 1e18 integer form
	require.Contains(t, []string{"1.000000000000000000", "1000000000000000000"}, rate, "exchange rate starts at 1")

	// 3. liquid staking round trip
	users := interchaintest.GetAndFundTestUsers(t, ctx, "fabric", sdkmath.NewInt(genesisWalletAmount), terra)
	user := users[0]
	require.NoError(t, testutil.WaitForBlocks(ctx, 1, terra))

	staked := sdkmath.NewInt(1_000_000_000) // 1,000 LUNC
	_, err = node.ExecTx(ctx, user.KeyName(), "liquidstake", "stake", staked.String()+terra.Config().Denom)
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 1, terra))

	st, err := terra.GetBalance(ctx, user.FormattedAddress(), "stluna")
	require.NoError(t, err)
	require.True(t, st.Equal(staked), "stLUNC minted 1:1 at the initial rate, got %s", st)

	// the epoch delegates the buffer to eligible validators
	require.NoError(t, testutil.WaitForBlocks(ctx, 7, terra))
	stdout, _, err = node.ExecQuery(ctx, "liquidstake", "delegations")
	require.NoError(t, err)
	var del map[string]interface{}
	require.NoError(t, json.Unmarshal(stdout, &del))
	delegations, err := dyno.GetSlice(del, "delegations")
	require.NoError(t, err)
	require.NotEmpty(t, delegations, "stake delegated at the epoch: %s", stdout)

	// unstake half: the receipt is burned and the redemption is either paid
	// instantly from the undelegated buffer (one validator with a 10 % cap
	// leaves most of the stake in the buffer) or queued for the user
	half := staked.QuoRaw(2)
	lunaBefore, err := terra.GetBalance(ctx, user.FormattedAddress(), terra.Config().Denom)
	require.NoError(t, err)
	_, err = node.ExecTx(ctx, user.KeyName(), "liquidstake", "unstake", half.String()+"stluna")
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 1, terra))
	st, err = terra.GetBalance(ctx, user.FormattedAddress(), "stluna")
	require.NoError(t, err)
	require.True(t, st.Equal(staked.Sub(half)), "half of the stLUNC burned on unstake, got %s", st)
	lunaAfter, err := terra.GetBalance(ctx, user.FormattedAddress(), terra.Config().Denom)
	require.NoError(t, err)
	stdout, _, err = node.ExecQuery(ctx, "liquidstake", "unstake-queue", user.FormattedAddress())
	require.NoError(t, err)
	queued := string(stdout)
	instant := lunaAfter.GT(lunaBefore) // the payout (500 LUNC) dwarfs the tx fee
	require.True(t, instant || contains(queued, user.FormattedAddress()), "redemption neither paid (%s -> %s) nor queued (%s)", lunaBefore, lunaAfter, queued)

	// 4. the x/remote derivation is deterministic and served by the node
	stdout, _, err = node.ExecQuery(ctx, "remote", "derive", "97", "000000000000000000000000000000000000000000000000000000000000abcd")
	require.NoError(t, err)
	require.Contains(t, string(stdout), "terra1", "derived remote account")
}
