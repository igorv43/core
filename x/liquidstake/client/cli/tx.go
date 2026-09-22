package cli

import (
	"github.com/classic-terra/core/v4/x/liquidstake/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
)

// NewTxCmd returns the transaction commands of x/liquidstake. They are
// hand-written because the AutoCLI of this SDK line cannot parse
// cosmos.base.v1beta1.Coin positional arguments of gogoproto messages.
func NewTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Liquid staking transactions (stLUNC)",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(NewStakeCmd(), NewUnstakeCmd(), NewClaimCmd())
	return cmd
}

// NewStakeCmd locks uluna and mints stLUNC.
func NewStakeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "stake [amount]",
		Short:   "Lock uluna and mint stLUNC at the current exchange rate",
		Example: "terrad tx liquidstake stake 1000000uluna --from mykey",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			amount, err := sdk.ParseCoinNormalized(args[0])
			if err != nil {
				return err
			}
			msg := &types.MsgStake{Sender: clientCtx.GetFromAddress().String(), Amount: amount}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewUnstakeCmd burns stLUNC; paid instantly from the buffer or queued.
func NewUnstakeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "unstake [amount]",
		Short:   "Burn stLUNC; paid instantly from the buffer when possible, otherwise queued for the epoch undelegation batch",
		Example: "terrad tx liquidstake unstake 1000000stluna --from mykey",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			amount, err := sdk.ParseCoinNormalized(args[0])
			if err != nil {
				return err
			}
			msg := &types.MsgUnstake{Sender: clientCtx.GetFromAddress().String(), Amount: amount}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewClaimCmd withdraws every matured unstake request of the sender.
func NewClaimCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "claim",
		Short:   "Withdraw every matured unstake request",
		Example: "terrad tx liquidstake claim --from mykey",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			msg := &types.MsgClaim{Sender: clientCtx.GetFromAddress().String()}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
