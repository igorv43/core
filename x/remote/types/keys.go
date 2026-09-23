package types

import (
	"encoding/binary"

	"cosmossdk.io/collections"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
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
	}
	for _, u := range SessionMsgTypeURLs() {
		urls[u] = true
	}
	return urls
}
