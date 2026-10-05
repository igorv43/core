package circuit

import (
	"context"

	circuitkeeper "cosmossdk.io/x/circuit/keeper"
	circuittypes "cosmossdk.io/x/circuit/types"
	customcircuittypes "github.com/classic-terra/core/v4/custom/circuit/types"
)

// msgServer interposes the upstream x/circuit MsgServer to enforce, at
// execution and regardless of the signer (governance included), that the
// protected user-exit messages of spec §24.5 can never be disabled:
//
//   - MsgTripCircuitBreaker naming a protected type URL is refused;
//   - MsgAuthorizeCircuitBreaker whose permissions list a protected type URL
//     in limit_type_urls is refused.
//
// The ante handler (custom/auth/ante CircuitResetGuardDecorator) applies the
// same list to signed transactions for an early CheckTx rejection, but only
// this wrapper sees messages executed by x/gov proposals, which never pass
// through the ante handler.
type msgServer struct {
	circuittypes.MsgServer
}

var _ circuittypes.MsgServer = msgServer{}

// NewMsgServerImpl returns the guarded x/circuit MsgServer.
func NewMsgServerImpl(k circuitkeeper.Keeper) circuittypes.MsgServer {
	return msgServer{MsgServer: circuitkeeper.NewMsgServerImpl(k)}
}

// AuthorizeCircuitBreaker implements circuittypes.MsgServer.
func (s msgServer) AuthorizeCircuitBreaker(ctx context.Context, msg *circuittypes.MsgAuthorizeCircuitBreaker) (*circuittypes.MsgAuthorizeCircuitBreakerResponse, error) {
	if msg.Permissions != nil {
		if err := customcircuittypes.CheckNoProtected(msg.Permissions.LimitTypeUrls); err != nil {
			return nil, err
		}
	}
	return s.MsgServer.AuthorizeCircuitBreaker(ctx, msg)
}

// TripCircuitBreaker implements circuittypes.MsgServer.
func (s msgServer) TripCircuitBreaker(ctx context.Context, msg *circuittypes.MsgTripCircuitBreaker) (*circuittypes.MsgTripCircuitBreakerResponse, error) {
	if err := customcircuittypes.CheckNoProtected(msg.MsgTypeUrls); err != nil {
		return nil, err
	}
	return s.MsgServer.TripCircuitBreaker(ctx, msg)
}
