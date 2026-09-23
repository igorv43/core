package warp

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	warpledgerkeeper "github.com/classic-terra/core/v4/x/warpledger/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MsgServer wraps the upstream warp message server. All handlers are delegated
// as-is; RemoteTransfer is interposed by x/warpledger, which enforces the
// per-domain exposure cap before dispatch and records `sent_d` after it
// (spec §9.5). This is the same idiom x/tax uses for x/bank.
type MsgServer struct {
	warptypes.MsgServer

	ledger warpledgerkeeper.Keeper
	// roots records the local merkle root after each dispatch for x/ismbond
	// (spec §6.6); optional.
	roots RootRecorder
}

// RootRecorder is implemented by x/ismbond.
type RootRecorder interface {
	RecordRoot(ctx sdk.Context, mailboxId util.HexAddress) error
}

// NewMsgServer returns a warp MsgServer that delegates to upstream. The
// optional recorder receives the mailbox of every successful transfer.
func NewMsgServer(upstream warptypes.MsgServer, ledger warpledgerkeeper.Keeper, recorders ...RootRecorder) warptypes.MsgServer {
	s := &MsgServer{MsgServer: upstream, ledger: ledger}
	if len(recorders) > 0 {
		s.roots = recorders[0]
	}
	return s
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
