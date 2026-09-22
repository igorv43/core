package keeper

import (
	"sort"

	"cosmossdk.io/math"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/classic-terra/core/v4/x/warpledger/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// RemoteTransferMsgURL is the message type URL tripped when the invariant breaks.
var RemoteTransferMsgURL = sdk.MsgTypeURL(&warptypes.MsgRemoteTransfer{})

// EndBlocker asserts the local solvency invariant for every collateral denom
// (spec §9.2, §9.3):
//
//	BankBalance(warp module account, denom) >= sum over tokens and domains of (sent - received)
//
// On violation it trips MsgRemoteTransfer in x/circuit (reset is governance
// only) and emits EventInvariantBroken. The chain is never halted.
//
// Work is bounded by the number of ledgers, which is capped per token by
// MaxDomainsPerToken and per chain by the governance-controlled token set.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	exposures := make(map[string]math.Int)

	err := k.IterateLedgers(ctx, func(ledger types.DomainLedger) (bool, error) {
		token, err := k.warpKeeper.HypTokens.Get(ctx, ledger.TokenId.GetInternalId())
		if err != nil {
			// a ledger without a token cannot be asserted; it is reported and skipped
			k.Logger(ctx).Error("ledger references unknown warp token", "token_id", ledger.TokenId.String())
			return false, nil
		}
		if token.TokenType != warptypes.HYP_TOKEN_TYPE_COLLATERAL {
			return false, nil
		}
		sum, ok := exposures[token.OriginDenom]
		if !ok {
			sum = math.ZeroInt()
		}
		exposures[token.OriginDenom] = sum.Add(Exposure(ledger))
		return false, nil
	})
	if err != nil {
		return err
	}

	if len(exposures) == 0 {
		return nil
	}

	denoms := make([]string, 0, len(exposures))
	for denom := range exposures {
		denoms = append(denoms, denom)
	}
	sort.Strings(denoms)

	warpAddr := authtypes.NewModuleAddress(warptypes.ModuleName)
	for _, denom := range denoms {
		sum := exposures[denom]
		balance := k.bankKeeper.GetBalance(ctx, warpAddr, denom).Amount
		if !balance.LT(sum) {
			continue
		}

		if err := k.circuitKeeper.DisableMsg(ctx, RemoteTransferMsgURL); err != nil {
			return err
		}
		k.Logger(ctx).Error("warp ledger invariant broken: outbound transfers tripped",
			"denom", denom, "bank_balance", balance.String(), "sum_exposure", sum.String())

		if err := ctx.EventManager().EmitTypedEvent(&types.EventInvariantBroken{
			Denom:          denom,
			BankBalance:    balance.String(),
			SumExposure:    sum.String(),
			Height:         ctx.BlockHeight(),
			DisabledMsgUrl: RemoteTransferMsgURL,
		}); err != nil {
			return err
		}
	}

	return nil
}
