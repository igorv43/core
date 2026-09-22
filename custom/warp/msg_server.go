package warp

import (
	"context"

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
}

// NewMsgServer returns a warp MsgServer that delegates to upstream.
func NewMsgServer(upstream warptypes.MsgServer, ledger warpledgerkeeper.Keeper) warptypes.MsgServer {
	return &MsgServer{MsgServer: upstream, ledger: ledger}
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
	return res, nil
}
