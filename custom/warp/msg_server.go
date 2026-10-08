package warp

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	warpledgerkeeper "github.com/classic-terra/core/v4/x/warpledger/keeper"
	warpledgertypes "github.com/classic-terra/core/v4/x/warpledger/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MsgServer wraps the upstream warp message server. All handlers are delegated
// as-is; RemoteTransfer is interposed by x/warpledger, which enforces the
// per-domain exposure cap before dispatch and records `sent_d` after it
// (spec §9.5); EnrollRemoteRouter refuses a route of a settlement basket
// token to a port-of-entry domain (spec §11.6 D-33, v0.9.11). This is the
// same idiom x/tax uses for x/bank.
type MsgServer struct {
	warptypes.MsgServer

	ledger warpledgerkeeper.Keeper
	// roots records the local merkle root after each dispatch for x/ismbond
	// (spec §6.6); optional.
	roots RootRecorder
	// ports is x/remote (port-of-entry domains); optional.
	ports warpledgertypes.PortRegistry
}

// RootRecorder is implemented by x/ismbond.
type RootRecorder interface {
	RecordRoot(ctx sdk.Context, mailboxId util.HexAddress) error
}

// NewMsgServer returns a warp MsgServer that delegates to upstream. The
// optional recorder receives the mailbox of every successful transfer.
func NewMsgServer(upstream warptypes.MsgServer, ledger warpledgerkeeper.Keeper, recorders ...RootRecorder) warptypes.MsgServer {
	return NewMsgServerWithPorts(upstream, ledger, nil, recorders...)
}

// NewMsgServerWithPorts also guards router enrollment with the port registry
// of x/remote (nil disables the guard).
func NewMsgServerWithPorts(upstream warptypes.MsgServer, ledger warpledgerkeeper.Keeper, ports warpledgertypes.PortRegistry,
	recorders ...RootRecorder,
) warptypes.MsgServer {
	s := &MsgServer{MsgServer: upstream, ledger: ledger, ports: ports}
	if len(recorders) > 0 {
		s.roots = recorders[0]
	}
	return s
}

// EnrollRemoteRouter refuses a router of a settlement basket token for a
// port-of-entry domain (spec §11.6 D-33, v0.9.11): a port has no vault of its
// own, the settlement asset reaches it only by CCTP from the vault that
// received it. Every other enrollment is delegated unchanged.
func (s *MsgServer) EnrollRemoteRouter(goCtx context.Context, msg *warptypes.MsgEnrollRemoteRouter) (*warptypes.MsgEnrollRemoteRouterResponse, error) {
	if s.ports != nil && msg.RemoteRouter != nil {
		ctx := sdk.UnwrapSDKContext(goCtx)
		basket, err := s.ledger.IsBasket(ctx, msg.TokenId)
		if err != nil {
			return nil, err
		}
		if basket {
			port, err := s.ports.IsPort(ctx, msg.RemoteRouter.ReceiverDomain)
			if err != nil {
				return nil, err
			}
			if port {
				return nil, errorsmod.Wrapf(warpledgertypes.ErrSettlementToPort, "%s is a settlement basket and domain %d is a port",
					msg.TokenId.String(), msg.RemoteRouter.ReceiverDomain)
			}
		}
	}
	return s.MsgServer.EnrollRemoteRouter(goCtx, msg)
}

// RemoteTransfer handles MsgRemoteTransfer with ledger accounting.
func (s *MsgServer) RemoteTransfer(goCtx context.Context, msg *warptypes.MsgRemoteTransfer) (*warptypes.MsgRemoteTransferResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// cap enforced at the edge, before any state change upstream
	if err := s.ledger.AssertOutboundAllowed(ctx, msg.TokenId, msg.DestinationDomain, msg.Amount); err != nil {
		return nil, err
	}

	res, err := s.MsgServer.RemoteTransfer(goCtx, msg)
	if err != nil {
		return nil, err
	}

	if err := s.ledger.RecordSent(ctx, msg.TokenId, msg.DestinationDomain, msg.Amount); err != nil {
		return nil, err
	}
	if s.roots != nil {
		if token, err := s.ledger.GetToken(ctx, msg.TokenId); err == nil {
			if err := s.roots.RecordRoot(ctx, token.OriginMailbox); err != nil {
				return nil, err
			}
		}
	}
	return res, nil
}
