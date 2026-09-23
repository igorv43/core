package keeper

import (
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/classic-terra/core/v4/x/remote/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetExecutor registers, updates or removes the FabricExecutor of a vault
// chain (spec §11.5.1). Limits are pushed to the executor by SET_LEG_LIMITS.
func (k Keeper) SetExecutor(ctx sdk.Context, msg *types.MsgSetExecutor) error {
	if msg.Address.IsZeroAddress() {
		return k.Executors.Remove(ctx, msg.Domain)
	}
	app, err := k.GetApp(ctx, msg.AppId)
	if err != nil {
		return err
	}
	if msg.MinCollateral.IsNil() || msg.MinCollateral.IsNegative() || msg.TargetCollateral.IsNil() || msg.TargetCollateral.LT(msg.MinCollateral) {
		return errorsmod.Wrap(types.ErrInvalidParams, "0 ≤ min_collateral ≤ target_collateral required")
	}
	ex, err := k.Executors.Get(ctx, msg.Domain)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		ex = types.Executor{Domain: msg.Domain, NetRebalanced: math.ZeroInt()}
	}
	ex.AppId, ex.Address, ex.TokenId = app.Id, msg.Address, msg.TokenId
	ex.MinCollateral, ex.TargetCollateral = msg.MinCollateral, msg.TargetCollateral
	if msg.ResetNetRebalanced {
		ex.NetRebalanced = math.ZeroInt()
	}
	if err := k.Executors.Set(ctx, msg.Domain, ex); err != nil {
		return err
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	_, err = k.dispatchControl(ctx, params, msg.Domain, types.CONTROL_SET_LEG_LIMITS, types.SetLegLimitsParams(ex.MinCollateral, ex.TargetCollateral),
		fmt.Sprintf("min=%s target=%s", ex.MinCollateral, ex.TargetCollateral))
	return err
}

// ExecutorControl dispatches a governance-only order (spec §11.5.2).
func (k Keeper) ExecutorControl(ctx sdk.Context, msg *types.MsgExecutorControl) (util.HexAddress, uint64, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return util.HexAddress{}, 0, err
	}
	var body []byte
	var desc string
	switch msg.Action {
	case types.CONTROL_ENROLL_LEG:
		if msg.LegDomain == 0 || msg.LegRouter.IsZeroAddress() || msg.LegBridge.IsZeroAddress() {
			return util.HexAddress{}, 0, errorsmod.Wrap(types.ErrInvalidParams, "ENROLL_LEG needs leg_domain, leg_router and leg_bridge")
		}
		body = types.EnrollLegParams(msg.LegDomain, msg.LegRouter, msg.LegBridge)
		desc = fmt.Sprintf("leg=%d router=%s bridge=%s", msg.LegDomain, msg.LegRouter.String(), msg.LegBridge.String())
	case types.CONTROL_PAUSE_LEG, types.CONTROL_UNPAUSE_LEG:
		body = nil
	default:
		return util.HexAddress{}, 0, errorsmod.Wrap(types.ErrInvalidParams, "only ENROLL_LEG, PAUSE_LEG and UNPAUSE_LEG are governance orders")
	}
	id, err := k.dispatchControl(ctx, params, msg.Domain, msg.Action, body, desc)
	if err != nil {
		return util.HexAddress{}, 0, err
	}
	ex, err := k.Executors.Get(ctx, msg.Domain)
	if err != nil {
		return util.HexAddress{}, 0, err
	}
	if msg.Action == types.CONTROL_PAUSE_LEG || msg.Action == types.CONTROL_UNPAUSE_LEG {
		ex.Paused = msg.Action == types.CONTROL_PAUSE_LEG
		if err := k.Executors.Set(ctx, msg.Domain, ex); err != nil {
			return util.HexAddress{}, 0, err
		}
	}
	return id, ex.Nonce, nil
}

// SetPort registers or removes a port-of-entry chain (spec §11.6).
func (k Keeper) SetPort(ctx sdk.Context, msg *types.MsgSetPort) error {
	if msg.VaultDomain == 0 {
		return k.Ports.Remove(ctx, msg.PortDomain)
	}
	if msg.PortDomain == 0 || msg.PortDomain == msg.VaultDomain {
		return errorsmod.Wrap(types.ErrInvalidParams, "port_domain must be set and differ from vault_domain")
	}
	return k.Ports.Set(ctx, msg.PortDomain, types.Port{PortDomain: msg.PortDomain, VaultDomain: msg.VaultDomain, CctpDomain: msg.CctpDomain})
}

// dispatchControl sends one control order to the executor of a domain with
// the next nonce; gas comes from the paymaster (spec §11.5.2).
func (k Keeper) dispatchControl(ctx sdk.Context, params types.Params, domain uint32, action types.ControlAction, actionParams []byte, desc string) (util.HexAddress, error) {
	ex, err := k.Executors.Get(ctx, domain)
	if err != nil {
		return util.HexAddress{}, errorsmod.Wrapf(types.ErrGatewayNotFound, "no executor on domain %d", domain)
	}
	app, err := k.GetApp(ctx, ex.AppId)
	if err != nil {
		return util.HexAddress{}, err
	}
	paymaster := types.PaymasterAddress()
	maxFee := sdk.NewCoins()
	if params.ControlFeeCap.IsPositive() {
		if !k.bankKeeper.GetBalance(ctx, paymaster, params.ControlFeeCap.Denom).IsGTE(params.ControlFeeCap) {
			return util.HexAddress{}, errorsmod.Wrap(types.ErrFeeUnpaid, "paymaster cannot fund the control message gas")
		}
		maxFee = sdk.NewCoins(params.ControlFeeCap)
	}
	nonce := ex.Nonce + 1
	body := types.EncodeControl(action, actionParams, nonce)
	cacheCtx, write := ctx.CacheContext()
	id, err := k.coreKeeper.DispatchMessage(cacheCtx, app.MailboxId, app.Id, maxFee, ex.Domain, ex.Address, body,
		util.StandardHookMetadata{Address: paymaster, GasLimit: math.ZeroInt()}, nil)
	if err != nil {
		return util.HexAddress{}, err
	}
	if k.roots != nil {
		if err := k.roots.RecordRoot(cacheCtx, app.MailboxId); err != nil {
			return util.HexAddress{}, err
		}
	}
	write()
	ex.Nonce = nonce
	if err := k.Executors.Set(ctx, domain, ex); err != nil {
		return util.HexAddress{}, err
	}
	return id, ctx.EventManager().EmitTypedEvent(&types.EventControlSent{Domain: domain, Action: action.String(), Nonce: nonce, MessageId: id.String(), Params: desc})
}

// ExpectedCollateral is what the consensus believes a vault holds: the
// origin's ledger collateral plus the net of the rebalances it ordered.
func (k Keeper) ExpectedCollateral(ctx sdk.Context, ex types.Executor) (ledger, expected math.Int, err error) {
	ledger = math.ZeroInt()
	if k.ledgerKeeper != nil && !ex.TokenId.IsZeroAddress() {
		l, _, err := k.ledgerKeeper.GetLedger(ctx, ex.TokenId, ex.Domain)
		if err != nil {
			return math.Int{}, math.Int{}, err
		}
		if c := l.Received.Sub(l.Sent); c.IsPositive() {
			ledger = c
		}
	}
	return ledger, ledger.Add(ex.NetRebalanced), nil
}

// dynamicFees returns the deposit and withdrawal fees for a vault (spec
// §11.5.4): the side that worsens the imbalance pays, linearly up to the band.
func dynamicFees(expected, target math.Int, bandBps uint32) (depositBps, withdrawBps uint32) {
	if !target.IsPositive() || bandBps == 0 {
		return 0, 0
	}
	band := math.LegacyNewDec(int64(bandBps))
	if expected.GT(target) {
		surplus := math.LegacyNewDecFromInt(expected.Sub(target)).QuoInt(target)
		if surplus.GT(math.LegacyOneDec()) {
			surplus = math.LegacyOneDec()
		}
		return uint32(band.Mul(surplus).TruncateInt64()), 0
	}
	deficit := math.LegacyNewDecFromInt(target.Sub(expected)).QuoInt(target)
	return 0, uint32(band.Mul(deficit).TruncateInt64())
}

// rebalanceEpoch is the consensus rebalancing of spec §11.5.2: fees follow
// the imbalance of every vault, and vaults below their minimum are refilled
// up to target from vaults above target, oldest-domain first, bounded by
// max_control_msgs_per_block.
func (k Keeper) rebalanceEpoch(ctx sdk.Context, params types.Params) error {
	var exs []types.Executor
	if err := k.Executors.Walk(ctx, nil, func(_ uint32, ex types.Executor) (bool, error) {
		exs = append(exs, ex)
		return false, nil
	}); err != nil {
		return err
	}
	sent := uint32(0)
	expected := make([]math.Int, len(exs))
	for i := range exs {
		_, e, err := k.ExpectedCollateral(ctx, exs[i])
		if err != nil {
			return err
		}
		expected[i] = e
		dep, wd := dynamicFees(e, exs[i].TargetCollateral, params.DynamicFeeBandBps)
		if (dep != exs[i].DepositFeeBps || wd != exs[i].WithdrawFeeBps) && sent < params.MaxControlMsgsPerBlock && !exs[i].Paused {
			if _, err := k.dispatchControl(ctx, params, exs[i].Domain, types.CONTROL_SET_FEE, types.SetFeeParams(dep, wd), fmt.Sprintf("deposit_bps=%d withdraw_bps=%d", dep, wd)); err != nil {
				k.Logger(ctx).Error("SET_FEE not sent", "domain", exs[i].Domain, "err", err)
				continue
			}
			sent++
			ex, _ := k.Executors.Get(ctx, exs[i].Domain)
			ex.DepositFeeBps, ex.WithdrawFeeBps = dep, wd
			if err := k.Executors.Set(ctx, ex.Domain, ex); err != nil {
				return err
			}
		}
	}
	for i := range exs {
		if exs[i].Paused || expected[i].GTE(exs[i].MinCollateral) {
			continue
		}
		need := exs[i].TargetCollateral.Sub(expected[i])
		for j := range exs {
			if i == j || exs[j].Paused || !need.IsPositive() || sent >= params.MaxControlMsgsPerBlock {
				continue
			}
			surplus := expected[j].Sub(exs[j].TargetCollateral)
			if !surplus.IsPositive() {
				continue
			}
			amount := math.MinInt(need, surplus)
			if _, err := k.dispatchControl(ctx, params, exs[j].Domain, types.CONTROL_REBALANCE, types.RebalanceParams(exs[i].Domain, amount),
				fmt.Sprintf("to=%d amount=%s", exs[i].Domain, amount)); err != nil {
				k.Logger(ctx).Error("REBALANCE not sent", "from", exs[j].Domain, "to", exs[i].Domain, "err", err)
				continue
			}
			sent++
			from, _ := k.Executors.Get(ctx, exs[j].Domain)
			from.NetRebalanced = from.NetRebalanced.Sub(amount)
			if err := k.Executors.Set(ctx, from.Domain, from); err != nil {
				return err
			}
			to, _ := k.Executors.Get(ctx, exs[i].Domain)
			to.NetRebalanced = to.NetRebalanced.Add(amount)
			if err := k.Executors.Set(ctx, to.Domain, to); err != nil {
				return err
			}
			expected[j] = expected[j].Sub(amount)
			expected[i] = expected[i].Add(amount)
			need = need.Sub(amount)
		}
	}
	for _, ex := range exs {
		cur, err := k.Executors.Get(ctx, ex.Domain)
		if err != nil {
			continue
		}
		cur.LastEpochHeight = ctx.BlockHeight()
		if err := k.Executors.Set(ctx, ex.Domain, cur); err != nil {
			return err
		}
	}
	return nil
}
