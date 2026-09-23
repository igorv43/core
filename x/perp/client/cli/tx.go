package cli

import (
	"fmt"
	"strconv"

	"cosmossdk.io/math"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	"github.com/classic-terra/core/v4/x/perp/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
)

const (
	flagExpiry     = "expiry-height"
	flagReduceOnly = "reduce-only"
	flagFrontend   = "frontend"
	flagQty        = "qty"
	flagSlippage   = "slippage"
)

// NewTxCmd returns the x/perp transaction commands.
func NewTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Perpetuals transactions (collateral, orders, triggers)",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(
		coinCmd("deposit [amount]", "Deposit settlement collateral", func(s string, c sdk.Coin) sdk.Msg {
			return &types.MsgDepositCollateral{Sender: s, Amount: c}
		}),
		coinCmd("withdraw [amount]", "Withdraw available collateral", func(s string, c sdk.Coin) sdk.Msg {
			return &types.MsgWithdrawCollateral{Sender: s, Amount: c}
		}),
		coinCmd("fund-insurance [amount]", "Donate settlement to the insurance fund (irreversible)", func(s string, c sdk.Coin) sdk.Msg {
			return &types.MsgFundInsurance{Sender: s, Amount: c}
		}),
		newSubmitCmd(),
		newTriggerCmd(),
		newCancelTriggerCmd(),
		newAutoTopUpCmd(),
	)
	return cmd
}

func coinCmd(use, short string, build func(sender string, coin sdk.Coin) sdk.Msg) *cobra.Command {
	cmd := &cobra.Command{
		Use: use, Short: short, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			coin, err := sdk.ParseCoinNormalized(args[0])
			if err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), build(clientCtx.GetFromAddress().String(), coin))
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func newSubmitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "submit [market-id] [buy|sell] [qty] [limit-price]",
		Short:   "Submit a perpetual order to the auction (margin reserved unless --reduce-only)",
		Example: "terrad tx perp submit ubtc-perp/uusd buy 1000000 65000 --expiry-height 1200 --from mykey",
		Args:    cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			side := batchtypes.SIDE_BUY
			if args[1] == "sell" {
				side = batchtypes.SIDE_SELL
			} else if args[1] != "buy" {
				return fmt.Errorf("side must be buy or sell")
			}
			qty, ok := math.NewIntFromString(args[2])
			if !ok {
				return fmt.Errorf("invalid qty")
			}
			price, err := math.LegacyNewDecFromStr(args[3])
			if err != nil {
				return err
			}
			expiry, _ := cmd.Flags().GetInt64(flagExpiry)
			reduceOnly, _ := cmd.Flags().GetBool(flagReduceOnly)
			frontend, _ := cmd.Flags().GetString(flagFrontend)
			msg := &types.MsgSubmitPerpIntent{Sender: clientCtx.GetFromAddress().String(), MarketId: args[0], Side: side, Qty: qty,
				LimitPrice: price, ExpiryHeight: expiry, ReduceOnly: reduceOnly, Frontend: frontend}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().Int64(flagExpiry, 0, "height at which the order expires (required)")
	cmd.Flags().Bool(flagReduceOnly, false, "only reduce the position; reserves no margin")
	cmd.Flags().String(flagFrontend, "", "integrator address to attribute the order to")
	_ = cmd.MarkFlagRequired(flagExpiry)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func newTriggerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "trigger [market-id] [trigger-price] [above|below]",
		Short:   "Register a resident reduce-only trigger (stop-loss / take-profit) on your position",
		Example: "terrad tx perp trigger ubtc-perp/uusd 60000 below --qty 500000 --slippage 0.01 --from mykey",
		Args:    cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			price, err := math.LegacyNewDecFromStr(args[1])
			if err != nil {
				return err
			}
			var above bool
			switch args[2] {
			case "above":
				above = true
			case "below":
			default:
				return fmt.Errorf("direction must be above or below")
			}
			qty := math.ZeroInt()
			if s, _ := cmd.Flags().GetString(flagQty); s != "" {
				v, ok := math.NewIntFromString(s)
				if !ok {
					return fmt.Errorf("invalid qty")
				}
				qty = v
			}
			slippage := math.LegacyZeroDec()
			if s, _ := cmd.Flags().GetString(flagSlippage); s != "" {
				v, err := math.LegacyNewDecFromStr(s)
				if err != nil {
					return err
				}
				slippage = v
			}
			msg := &types.MsgSubmitTriggerOrder{Sender: clientCtx.GetFromAddress().String(), MarketId: args[0], TriggerPrice: price,
				FireAbove: above, Qty: qty, Slippage: slippage}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagQty, "", "quantity to close (default: whole position)")
	cmd.Flags().String(flagSlippage, "", "limit slippage from the trigger price (default: parameter)")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func newCancelTriggerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "cancel-trigger [trigger-id]", Short: "Cancel a trigger order", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			id, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &types.MsgCancelTriggerOrder{Sender: clientCtx.GetFromAddress().String(), TriggerId: id})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func newAutoTopUpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "auto-top-up [on|off]", Short: "Opt in or out of the automatic margin top-up from free collateral", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			var enabled bool
			switch args[0] {
			case "on":
				enabled = true
			case "off":
			default:
				return fmt.Errorf("argument must be on or off")
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &types.MsgSetAutoTopUp{Sender: clientCtx.GetFromAddress().String(), Enabled: enabled})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
