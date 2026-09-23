package cli

import (
	"fmt"
	"os"

	"github.com/classic-terra/core/v4/x/ismbond/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
)

// NewTxCmd returns the x/ismbond transaction commands.
func NewTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "ISM operator bonds and evidence",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(newBondCmd(), newSimpleCmd("unbond", "Start unbonding your operator bond", func(s string) sdk.Msg { return &types.MsgUnbondOperator{Operator: s} }),
		newSimpleCmd("claim", "Claim a matured bond", func(s string) sdk.Msg { return &types.MsgClaimBond{Operator: s} }),
		newEvidenceCmd(), newDistributeCmd())
	return cmd
}

func newBondCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "bond [validator-eth-address] [bond]", Short: "Bond LUNC as the operator of a Hyperlane validator", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			coin, err := sdk.ParseCoinNormalized(args[1])
			if err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &types.MsgBondOperator{Operator: clientCtx.GetFromAddress().String(), Validator: args[0], Bond: coin})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func newSimpleCmd(use, short string, build func(sender string) sdk.Msg) *cobra.Command {
	cmd := &cobra.Command{
		Use: use, Short: short, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), build(clientCtx.GetFromAddress().String()))
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func newEvidenceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "submit-evidence [evidence.json]",
		Short: "Submit evidence of equivocation or of an invalid outbound checkpoint (proto JSON of MsgSubmitEvidence without submitter)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			var msg types.MsgSubmitEvidence
			if err := clientCtx.Codec.UnmarshalJSON(raw, &msg); err != nil {
				return fmt.Errorf("evidence file: %w", err)
			}
			msg.Submitter = clientCtx.GetFromAddress().String()
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func newDistributeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "distribute-rewards [amount]", Short: "Split an amount among the active operators pro rata to their bonds", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			coin, err := sdk.ParseCoinNormalized(args[0])
			if err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &types.MsgDistributeRewards{Sender: clientCtx.GetFromAddress().String(), Amount: coin})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
