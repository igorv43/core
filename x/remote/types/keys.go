package types

import (
	"encoding/binary"

	"cosmossdk.io/collections"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	lstypes "github.com/classic-terra/core/v4/x/liquidstake/types"
	perptypes "github.com/classic-terra/core/v4/x/perp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
)

const (
	// ModuleName is the name of the x/remote module.
	ModuleName = "remote"
	// StoreKey is the store key of the module.
	StoreKey = ModuleName
	// RouterKey is the message route of the module.
	RouterKey = ModuleName

	// AppRouterID is the id of x/remote in the Hyperlane AppRouter (warp owns 1 and 2).
	AppRouterID uint8 = 3

	// MaxMsgsPerPayloadAbsolute is the code maximum of params.max_msgs_per_payload (spec §12).
	MaxMsgsPerPayloadAbsolute = 10
	// MaxWithdrawalsKept bounds the withdrawal records kept per account.
	MaxWithdrawalsKept = 50
	// NativeTokenSentinel is the 32-byte hex that names the chain's native
	// coin as token_out of a withdrawal (spec §14.7.2).
	NativeTokenSentinel = "0x000000000000000000000000eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

	// MaxPendingPerAccountAbsolute, MaxPendingTTLBlocksAbsolute,
	// MaxPendingExecsPerBlockAbsolute and MaxAutoReturnsPerEpochAbsolute are
	// the code maxima of the deposit-then-execute and auto-return params
	// (cross-chain liquid staking §4.3/§4.4, spec §12).
	MaxPendingPerAccountAbsolute    = 10
	MaxPendingTTLBlocksAbsolute     = 100_800 // ~7 days at 6 s
	MaxPendingExecsPerBlockAbsolute = 50
	MaxAutoReturnsPerEpochAbsolute  = 200
	// MaxPortExitFeeToleranceBpsAbsolute is the code maximum of
	// params.port_exit_fee_tolerance_bps (spec §11.6, v0.9.12): 1 %. A larger
	// allowance would let a port exit settle far below the user's net amount.
	MaxPortExitFeeToleranceBpsAbsolute = 100
	// MaxPortExitRefloors bounds the consented re-floors of one port exit
	// (MsgRefloorPortExit, spec v0.9.13 §11.6.4). Each one is a control
	// message whose interchain gas the paymaster advances; three consents
	// cover an issuer fee that rises in steps, and the lowest minimum is
	// bounded anyway by MaxPortExitFeeToleranceBpsAbsolute.
	MaxPortExitRefloors = 3
	// AutoReturnScanFactor bounds the opted-in accounts examined per epoch
	// to this multiple of max_auto_returns_per_epoch (accounts with nothing
	// matured cost a read, not a return).
	AutoReturnScanFactor = 4

	// PaymasterName derives the paymaster account that pays the gas of session keys.
	PaymasterName = "paymaster"
)

var (
	ParamsKey      = collections.NewPrefix(0)
	AppsKey        = collections.NewPrefix(1)
	GatewaysKey    = collections.NewPrefix(2)
	AccountsKey    = collections.NewPrefix(3)
	SessionsKey    = collections.NewPrefix(4)
	WithdrawalsKey = collections.NewPrefix(5)
	WithdrawSeqKey = collections.NewPrefix(6)
	BeaconsKey     = collections.NewPrefix(7)
	BeaconSeqKey   = collections.NewPrefix(8)
	// Conversion receipts (spec §14.7): by (account, seq), the per-account
	// sequence, and the (account, message id) → seq index used by updates.
	ReceiptsKey     = collections.NewPrefix(9)
	ReceiptSeqKey   = collections.NewPrefix(10)
	ReceiptByMsgKey = collections.NewPrefix(11)
	// ExecutorsKey and PortsKey hold the FabricExecutors (spec §11.5) and the
	// port-of-entry chains (spec §11.6), both by hyperlane domain.
	ExecutorsKey = collections.NewPrefix(12)
	PortsKey     = collections.NewPrefix(13)
	// Deposit-then-execute (cross-chain liquid staking §4.3): pending payloads
	// by id, their per-account index, the id sequence, the ids whose deposit
	// arrived, the deposit credits by (account, token, origin) and their
	// expiry index by height.
	PendingKey          = collections.NewPrefix(14)
	PendingByAccountKey = collections.NewPrefix(15)
	PendingSeqKey       = collections.NewPrefix(16)
	PendingReadyKey     = collections.NewPrefix(17)
	CreditsKey          = collections.NewPrefix(18)
	CreditExpiryKey     = collections.NewPrefix(19)
	// Auto-return (§4.4): the opted-in accounts, the round-robin cursor, the
	// last liquid-staking epoch processed and the paymaster budget.
	AutoReturnsKey      = collections.NewPrefix(20)
	AutoReturnCursorKey = collections.NewPrefix(21)
	AutoReturnEpochKey  = collections.NewPrefix(22)
	AutoReturnBudgetKey = collections.NewPrefix(23)
	// IntentFeeBudgetKey holds, per remote account, the intent fees of x/batch
	// the paymaster sponsored in the current paymaster period (§14.4 item 4).
	IntentFeeBudgetKey = collections.NewPrefix(24)
)

// PaymasterAddress is the account that grants fee allowances to session keys.
func PaymasterAddress() sdk.AccAddress {
	return sdk.AccAddress(address.Module(ModuleName, []byte(PaymasterName)))
}

// DeriveAddress returns the account controlled by (domain, controller):
// address.Module("remote", be32(domain) ‖ controller) (spec §14.4 item 1).
func DeriveAddress(domain uint32, controller util.HexAddress) sdk.AccAddress {
	key := make([]byte, 4, 4+32)
	binary.BigEndian.PutUint32(key, domain)
	key = append(key, controller.Bytes()...)
	return sdk.AccAddress(address.Module(ModuleName, key))
}

// SessionMsgTypeURLs is the closed scope of a session key (spec §14.4 item 3):
// orders and their cancellation, never custody, transfers or grants.
func SessionMsgTypeURLs() []string {
	return []string{
		sdk.MsgTypeURL(&perptypes.MsgSubmitPerpIntent{}),
		sdk.MsgTypeURL(&batchtypes.MsgSubmitIntent{}),
		sdk.MsgTypeURL(&batchtypes.MsgCancelIntent{}),
		sdk.MsgTypeURL(&perptypes.MsgSubmitTriggerOrder{}),
		sdk.MsgTypeURL(&perptypes.MsgCancelTriggerOrder{}),
	}
}

// PayloadMsgTypeURLs is the closed whitelist of messages a remote payload may
// carry (spec §14.4 item 1), in code and not governable.
func PayloadMsgTypeURLs() map[string]bool {
	urls := map[string]bool{
		sdk.MsgTypeURL(&perptypes.MsgDepositCollateral{}):  true,
		sdk.MsgTypeURL(&perptypes.MsgWithdrawCollateral{}): true,
		sdk.MsgTypeURL(&perptypes.MsgSetAutoTopUp{}):       true,
		sdk.MsgTypeURL(&batchtypes.MsgApproveFrontend{}):   true,
		sdk.MsgTypeURL(&batchtypes.MsgRevokeFrontend{}):    true,
		sdk.MsgTypeURL(&MsgGrantSessionKey{}):              true,
		sdk.MsgTypeURL(&MsgRevokeSessionKey{}):             true,
		sdk.MsgTypeURL(&MsgWithdraw{}):                     true,
		// cross-chain liquid staking (§4.1): principal moves, never in the
		// session-key scope
		sdk.MsgTypeURL(&lstypes.MsgStake{}):   true,
		sdk.MsgTypeURL(&lstypes.MsgUnstake{}): true,
		sdk.MsgTypeURL(&lstypes.MsgClaim{}):   true,
		sdk.MsgTypeURL(&MsgSetAutoReturn{}):   true,
		// consented re-floor of the account's own stuck port exit (spec
		// v0.9.13 §11.6.4): a principal decision, never in the session scope
		sdk.MsgTypeURL(&MsgRefloorPortExit{}): true,
	}
	for _, u := range SessionMsgTypeURLs() {
		urls[u] = true
	}
	return urls
}

// ResultMsgTypeURLs is the closed list of messages whose typed response a
// following MsgWithdraw may reference with AMOUNT_FROM_PREVIOUS_RESULT
// (cross-chain liquid staking §4.2).
func ResultMsgTypeURLs() map[string]bool {
	return map[string]bool{
		sdk.MsgTypeURL(&lstypes.MsgStake{}):   true,
		sdk.MsgTypeURL(&lstypes.MsgUnstake{}): true,
		sdk.MsgTypeURL(&lstypes.MsgClaim{}):   true,
	}
}
