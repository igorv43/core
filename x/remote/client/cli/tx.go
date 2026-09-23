package cli

import (
	"encoding/hex"
	"fmt"
	"os"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/classic-terra/core/v4/x/remote/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
)

// NewTxCmd returns the x/remote transaction commands: the ones a local key
// can sign (revoking its own session, funding the paymaster) and helpers to
// build the payload a remote controller sends through Hyperlane.
func NewTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Remote accounts transactions and payload helpers",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(newRevokeOwnSessionCmd(), newFundPaymasterCmd(), newEncodePayloadCmd())
	return cmd
}

func newRevokeOwnSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "revoke-own-session [controller-account]", Short: "Revoke the session key you hold on a remote account", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &types.MsgRevokeOwnSessionKey{SessionKey: clientCtx.GetFromAddress().String(), Controller: args[0]})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func newFundPaymasterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "fund-paymaster [amount]", Short: "Fund the paymaster that pays the gas of session keys", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			coin, err := sdk.ParseCoinNormalized(args[0])
			if err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &types.MsgFundPaymaster{Sender: clientCtx.GetFromAddress().String(), Amount: coin})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// newEncodePayloadCmd builds the hex body of a remote control message from a
// JSON file of messages (offline helper for gateways and tests).
func newEncodePayloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "encode-payload [msgs.json] [on-behalf-of-hex]",
		Short: "Encode a RemotePayload (hex) from a JSON array of messages signed by the derived account",
		Long: `The JSON file holds an array of proto-JSON messages, e.g.
[{"@type":"/terra.perp.v1.MsgDepositCollateral","sender":"terra1...","amount":{"denom":"uusd","amount":"1000000"}}]
The optional on-behalf-of is the 32-byte hex controller a trusted gateway acts for.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			var anys []*codectypes.Any
			var list []map[string]interface{}
			if err := unmarshalList(raw, &list); err != nil {
				return err
			}
			for _, item := range list {
				bz, err := marshalItem(item)
				if err != nil {
					return err
				}
				var msg sdk.Msg
				if err := clientCtx.Codec.UnmarshalInterfaceJSON(bz, &msg); err != nil {
					return fmt.Errorf("message %s: %w", item["@type"], err)
				}
				a, err := codectypes.NewAnyWithValue(msg)
				if err != nil {
					return err
				}
				anys = append(anys, a)
			}
			payload := types.RemotePayload{Msgs: anys}
			if len(args) == 2 {
				h, err := util.DecodeHexAddress(args[1])
				if err != nil {
					return err
				}
				payload.OnBehalfOf = h.Bytes()
			}
			bz, err := clientCtx.Codec.Marshal(&payload)
			if err != nil {
				return err
			}
			cmd.Println(hex.EncodeToString(bz))
			return nil
		},
	}
	return cmd
}
