package hyperlane

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warpledgerkeeper "github.com/classic-terra/core/v4/x/warpledger/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MsgServer wraps the upstream hyperlane core message server. All handlers are
// delegated as-is; ProcessMessage is interposed by x/warpledger, which records
// `received_d` for messages delivered to warp tokens (spec §9.5).
type MsgServer struct {
	coretypes.MsgServer

	ledger warpledgerkeeper.Keeper
}

// NewMsgServer returns a hyperlane core MsgServer that delegates to upstream.
func NewMsgServer(upstream coretypes.MsgServer, ledger warpledgerkeeper.Keeper) coretypes.MsgServer {
	return &MsgServer{MsgServer: upstream, ledger: ledger}
}

// ProcessMessage handles MsgProcessMessage with inbound ledger accounting.
func (s *MsgServer) ProcessMessage(goCtx context.Context, msg *coretypes.MsgProcessMessage) (*coretypes.MsgProcessMessageResponse, error) {
	res, err := s.MsgServer.ProcessMessage(goCtx, msg)
	if err != nil {
		return nil, err
	}

	// upstream accepted the message, so the encoding is valid; decode it again
	// to attribute the amount to its origin domain
	raw, err := util.DecodeEthHex(msg.Message)
	if err != nil {
		return nil, err
	}
	message, err := util.ParseHyperlaneMessage(raw)
	if err != nil {
		return nil, err
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := s.ledger.RecordInboundMessage(ctx, message); err != nil {
		return nil, err
	}
	return res, nil
}
