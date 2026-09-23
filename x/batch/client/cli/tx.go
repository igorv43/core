package cli

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
)

const (
	flagMinOut   = "min-out"
	flagExpiry   = "expiry-height"
	flagFrontend = "frontend"
	flagSalt     = "salt"
)

// NewTxCmd returns the transaction commands of x/batch.
func NewTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Batch auction transactions (intents, solvers, integrators)",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(
		newSubmitIntentCmd(), newCancelIntentCmd(),
		newRegisterSolverCmd(), newUnbondSolverCmd(), newDepositEscrowCmd(), newWithdrawEscrowCmd(),
		newCommitBidCmd(), newRevealBidCmd(), newCommitmentCmd(),
		newRegisterFrontendCmd(), newApproveFrontendCmd(), newRevokeFrontendCmd(),
	)
	return cmd
}

func broadcast(cmd *cobra.Command, msg sdk.Msg) error {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}
	return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
}

func newSubmitIntentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "submit-intent [market-id] [buy|sell] [amount-in] [limit-price]",
		Short:   "Submit a limit order (amount-in is escrowed: quote for buy, base for sell)",
		Example: "terrad tx batch submit-intent uluna/uusd buy 1000000uusd 0.00005 --min-out 19000000000 --expiry-height 1200 --from mykey",
		Args:    cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			side := types.SIDE_BUY
			if args[1] == "sell" {
				side = types.SIDE_SELL
			} else if args[1] != "buy" {
				return fmt.Errorf("side must be buy or sell")
			}
			amount, err := sdk.ParseCoinNormalized(args[2])
			if err != nil {
				return err
			}
			price, err := math.LegacyNewDecFromStr(args[3])
			if err != nil {
				return err
			}
			minOutStr, _ := cmd.Flags().GetString(flagMinOut)
			minOut := math.ZeroInt()
			if minOutStr != "" {
				v, ok := math.NewIntFromString(minOutStr)
				if !ok {
					return fmt.Errorf("invalid min-out")
				}
				minOut = v
			}
			expiry, _ := cmd.Flags().GetInt64(flagExpiry)
			frontend, _ := cmd.Flags().GetString(flagFrontend)
			msg := &types.MsgSubmitIntent{
				Sender: clientCtx.GetFromAddress().String(), MarketId: args[0], Side: side,
				AmountIn: amount, LimitPrice: price, MinOut: minOut, ExpiryHeight: expiry, Frontend: frontend,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagMinOut, "", "minimum total amount to receive for the whole intent (base units)")
	cmd.Flags().Int64(flagExpiry, 0, "height at which the intent expires (required)")
	cmd.Flags().String(flagFrontend, "", "integrator address to attribute the order to")
	_ = cmd.MarkFlagRequired(flagExpiry)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func newCancelIntentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "cancel-intent [intent-id]", Short: "Cancel an open intent and refund its escrow", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			id, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &types.MsgCancelIntent{Sender: clientCtx.GetFromAddress().String(), IntentId: id})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
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

func newRegisterSolverCmd() *cobra.Command {
	return coinCmd("register-solver [bond]", "Register as a solver by locking a bond", func(s string, c sdk.Coin) sdk.Msg {
		return &types.MsgRegisterSolver{Sender: s, Bond: c}
	})
}

func newDepositEscrowCmd() *cobra.Command {
	return coinCmd("deposit-escrow [amount]", "Deposit trading balance into the solver escrow", func(s string, c sdk.Coin) sdk.Msg {
		return &types.MsgDepositSolverEscrow{Sender: s, Amount: c}
	})
}

func newWithdrawEscrowCmd() *cobra.Command {
	return coinCmd("withdraw-escrow [amount]", "Withdraw free trading balance from the solver escrow", func(s string, c sdk.Coin) sdk.Msg {
		return &types.MsgWithdrawSolverEscrow{Sender: s, Amount: c}
	})
}

func newUnbondSolverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "unbond-solver", Short: "Leave the solver set; the bond is refunded after the unbonding period", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &types.MsgUnbondSolver{Sender: clientCtx.GetFromAddress().String()})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func readBid(path string) (types.Bid, error) {
	var bid types.Bid
	raw, err := os.ReadFile(path)
	if err != nil {
		return bid, err
	}
	// proto JSON (enum names, decimal strings), the same encoding the tx carries
	if err := codec.NewProtoCodec(codectypes.NewInterfaceRegistry()).UnmarshalJSON(raw, &bid); err != nil {
		return bid, fmt.Errorf("bid file: %w", err)
	}
	return bid, nil
}

func newCommitmentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "commitment [bid.json] [salt-hex] [solver] [batch-id]",
		Short: "Compute the SHA256 commitment of a bid (offline helper)",
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			bid, err := readBid(args[0])
			if err != nil {
				return err
			}
			salt, err := hex.DecodeString(args[1])
			if err != nil {
				return err
			}
			batch, err := strconv.ParseUint(args[3], 10, 64)
			if err != nil {
				return err
			}
			c, err := types.Commitment(bid, salt, args[2], batch)
			if err != nil {
				return err
			}
			cmd.Println(hex.EncodeToString(c))
			return nil
		},
	}
}

func newCommitBidCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "commit-bid [batch-id] [market-id] [commitment-hex]", Short: "Commit a sealed bid for a batch", Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			batch, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}
			commitment, err := hex.DecodeString(args[2])
			if err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &types.MsgCommitBid{Solver: clientCtx.GetFromAddress().String(), BatchId: batch, MarketId: args[1], Commitment: commitment})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func newRevealBidCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "reveal-bid [batch-id] [bid.json]", Short: "Reveal a committed bid in the reveal block", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			batch, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}
			bid, err := readBid(args[1])
			if err != nil {
				return err
			}
			saltHex, _ := cmd.Flags().GetString(flagSalt)
			salt, err := hex.DecodeString(saltHex)
			if err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &types.MsgRevealBid{Solver: clientCtx.GetFromAddress().String(), BatchId: batch, Bid: bid, Salt: salt})
		},
	}
	cmd.Flags().String(flagSalt, "", "salt used in the commitment (hex)")
	_ = cmd.MarkFlagRequired(flagSalt)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func newRegisterFrontendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "register-frontend [fee-bps]", Short: "Register as an integrator with a fee in basis points", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			fee, err := strconv.ParseUint(args[0], 10, 32)
			if err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &types.MsgRegisterFrontend{Sender: clientCtx.GetFromAddress().String(), FeeBps: uint32(fee)})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func newApproveFrontendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "approve-frontend [frontend] [max-fee-bps]", Short: "Allow an integrator to charge up to a fee on your orders", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			fee, err := strconv.ParseUint(args[1], 10, 32)
			if err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &types.MsgApproveFrontend{Sender: clientCtx.GetFromAddress().String(), Frontend: args[0], MaxFeeBps: uint32(fee)})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func newRevokeFrontendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "revoke-frontend [frontend]", Short: "Revoke an integrator approval", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			return broadcast(cmd, &types.MsgRevokeFrontend{Sender: clientCtx.GetFromAddress().String(), Frontend: args[0]})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
