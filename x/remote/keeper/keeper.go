package keeper

import (
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/log"
	feegrantkeeper "cosmossdk.io/x/feegrant/keeper"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/classic-terra/core/v4/x/remote/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper implements the remote accounts of spec §14.4 (D-19) and §14.6
// (D-27): a Hyperlane application (router id 3) whose verified messages
// control accounts derived from (origin domain, origin sender), with
// session keys through x/authz, a paymaster through x/feegrant and
// withdrawals locked to the controller.
type Keeper struct {
	cdc       codec.Codec
	authority string

	bankKeeper     types.BankKeeper
	coreKeeper     types.CoreKeeper
	authzKeeper    types.AuthzKeeper
	feegrantKeeper feegrantkeeper.Keeper
	perpKeeper     types.PerpKeeper
	router         baseapp.MessageRouter
	// optional collaborators of the beacons (spec §14.6 item 3)
	lsKeeper     types.LiquidStakeKeeper
	ledgerKeeper types.WarpLedgerKeeper
	roots        types.RootRecorder
	warpAddress  sdk.AccAddress

	Schema   collections.Schema
	Params   collections.Item[types.Params]
	Apps     collections.Map[uint64, types.RemoteApp]
	Gateways collections.Map[collections.Pair[uint64, uint32], types.Gateway]
	// Receipts are the conversion receipts of spec §14.7 by (account, seq);
	// ReceiptSeq is the per-account sequence; ReceiptByMsg maps
	// (account, message id) to the seq for updates from the origin.
	Receipts     collections.Map[collections.Pair[string, uint64], types.ConversionReceipt]
	ReceiptSeq   collections.Map[string, uint64]
	ReceiptByMsg collections.Map[collections.Pair[string, []byte], uint64]
	Accounts     collections.Map[string, types.RemoteAccount]
	Sessions     collections.Map[collections.Pair[string, string], types.Session]
	Withdrawals  collections.Map[collections.Pair[string, uint64], types.Withdrawal]
	WithdrawSeq  collections.Map[string, uint64]
	Beacons      collections.Map[uint64, types.Beacon]
	BeaconSeq    collections.Sequence
}

// NewKeeper creates the x/remote keeper and registers it as Hyperlane app 3.
func NewKeeper(
	cdc codec.Codec,
	storeService storetypes.KVStoreService,
	authority string,
	bankKeeper types.BankKeeper,
	coreKeeper types.CoreKeeper,
	authzKeeper types.AuthzKeeper,
	feegrantKeeper feegrantkeeper.Keeper,
	perpKeeper types.PerpKeeper,
	router baseapp.MessageRouter,
) Keeper {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic(fmt.Errorf("invalid remote authority address: %w", err))
	}
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		cdc: cdc, authority: authority, bankKeeper: bankKeeper, coreKeeper: coreKeeper, authzKeeper: authzKeeper,
		feegrantKeeper: feegrantKeeper, perpKeeper: perpKeeper, router: router,
		Params: collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Apps:   collections.NewMap(sb, types.AppsKey, "apps", collections.Uint64Key, codec.CollValue[types.RemoteApp](cdc)),
		Gateways: collections.NewMap(sb, types.GatewaysKey, "gateways",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint32Key), codec.CollValue[types.Gateway](cdc)),
		Receipts: collections.NewMap(sb, types.ReceiptsKey, "receipts",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key), codec.CollValue[types.ConversionReceipt](cdc)),
		ReceiptSeq: collections.NewMap(sb, types.ReceiptSeqKey, "receipt_seq", collections.StringKey, collections.Uint64Value),
		ReceiptByMsg: collections.NewMap(sb, types.ReceiptByMsgKey, "receipt_by_msg",
			collections.PairKeyCodec(collections.StringKey, collections.BytesKey), collections.Uint64Value),
		Accounts: collections.NewMap(sb, types.AccountsKey, "accounts", collections.StringKey, codec.CollValue[types.RemoteAccount](cdc)),
		Sessions: collections.NewMap(sb, types.SessionsKey, "sessions",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey), codec.CollValue[types.Session](cdc)),
		Withdrawals: collections.NewMap(sb, types.WithdrawalsKey, "withdrawals",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key), codec.CollValue[types.Withdrawal](cdc)),
		WithdrawSeq: collections.NewMap(sb, types.WithdrawSeqKey, "withdraw_seq", collections.StringKey, collections.Uint64Value),
		Beacons:     collections.NewMap(sb, types.BeaconsKey, "beacons", collections.Uint64Key, codec.CollValue[types.Beacon](cdc)),
		BeaconSeq:   collections.NewSequence(sb, types.BeaconSeqKey, "beacon_seq"),
	}
	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema
	coreKeeper.AppRouter().RegisterModule(types.AppRouterID, &k)
	return k
}

// GetAuthority returns the module authority.
func (k Keeper) GetAuthority() string { return k.authority }

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

// GetParams returns the parameters.
func (k Keeper) GetParams(ctx sdk.Context) (types.Params, error) {
	p, err := k.Params.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.DefaultParams(), nil
		}
		return types.Params{}, err
	}
	return p, nil
}

// SetParams validates and stores the parameters.
func (k Keeper) SetParams(ctx sdk.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return k.Params.Set(ctx, p)
}

// GetApp returns a remote app by hyperlane id.
func (k Keeper) GetApp(ctx sdk.Context, id util.HexAddress) (types.RemoteApp, error) {
	app, err := k.Apps.Get(ctx, id.GetInternalId())
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.RemoteApp{}, types.ErrAppNotFound.Wrap(id.String())
		}
		return types.RemoteApp{}, err
	}
	return app, nil
}

// CreateApp registers a new remote app for a mailbox (governance).
func (k Keeper) CreateApp(ctx sdk.Context, owner string, mailboxId util.HexAddress, ismId *util.HexAddress) (util.HexAddress, error) {
	if _, err := k.coreKeeper.GetMailbox(ctx, mailboxId); err != nil {
		return util.HexAddress{}, err
	}
	id, err := k.coreKeeper.AppRouter().GetNextSequence(ctx, types.AppRouterID)
	if err != nil {
		return util.HexAddress{}, err
	}
	app := types.RemoteApp{Id: id, MailboxId: mailboxId, IsmId: ismId, Owner: owner}
	return id, k.Apps.Set(ctx, id.GetInternalId(), app)
}

// SetGateway enrols (or removes with the zero address) the trusted gateway of a domain.
func (k Keeper) SetGateway(ctx sdk.Context, appId util.HexAddress, domain uint32, gateway util.HexAddress, exitFactory util.HexAddress, exitInitCodeHash []byte) error {
	if _, err := k.GetApp(ctx, appId); err != nil {
		return err
	}
	key := collections.Join(appId.GetInternalId(), domain)
	if gateway.IsZeroAddress() {
		return k.Gateways.Remove(ctx, key)
	}
	if !exitFactory.IsZeroAddress() && len(exitInitCodeHash) != 32 {
		return errorsmod.Wrap(types.ErrInvalidParams, "exit_init_code_hash must be 32 bytes")
	}
	return k.Gateways.Set(ctx, key, types.Gateway{AppId: appId, Domain: domain, Address: gateway, ExitFactory: exitFactory, ExitInitCodeHash: exitInitCodeHash})
}

// gateway returns the enrolled gateway of a domain, if any.
func (k Keeper) gateway(ctx sdk.Context, appId util.HexAddress, domain uint32) (util.HexAddress, bool, error) {
	g, err := k.Gateways.Get(ctx, collections.Join(appId.GetInternalId(), domain))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return util.HexAddress{}, false, nil
		}
		return util.HexAddress{}, false, err
	}
	return g.Address, true, nil
}

// exitGateway returns the first enrolled gateway of a domain that offers
// withdrawal exits (spec §14.7.2), whatever the app.
func (k Keeper) exitGateway(ctx sdk.Context, domain uint32) (types.Gateway, bool, error) {
	var found types.Gateway
	ok := false
	err := k.Gateways.Walk(ctx, nil, func(key collections.Pair[uint64, uint32], g types.Gateway) (bool, error) {
		if key.K2() == domain && !g.ExitFactory.IsZeroAddress() && len(g.ExitInitCodeHash) == 32 {
			found, ok = g, true
			return true, nil
		}
		return false, nil
	})
	return found, ok, err
}

// GetAccount returns a remote account by derived address.
func (k Keeper) GetAccount(ctx sdk.Context, address string) (types.RemoteAccount, error) {
	a, err := k.Accounts.Get(ctx, address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.RemoteAccount{}, types.ErrAccountNotFound.Wrap(address)
		}
		return types.RemoteAccount{}, err
	}
	return a, nil
}

// SetBeaconSources registers the state readers of the beacons and the root
// recorder; warpAddress is the warp module account that custodies collateral.
func (k *Keeper) SetBeaconSources(ls types.LiquidStakeKeeper, ledger types.WarpLedgerKeeper, roots types.RootRecorder, warpAddress sdk.AccAddress) {
	k.lsKeeper, k.ledgerKeeper, k.roots, k.warpAddress = ls, ledger, roots, warpAddress
}
