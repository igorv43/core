package keeper

import (
	"strconv"
	"strings"
	"time"

	"github.com/classic-terra/core/v4/x/oracle/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Liquidity Fabric spec §26.3: a change of the oracle whitelist approved by
// governance activates no earlier than 7 days after the approval.
//
// The oracle parameters live in a legacy x/params subspace, so a whitelist
// change reaches the module as a parameter write (ParameterChangeProposal or
// an upgrade handler) that cannot be intercepted. The module therefore keeps
// its own copy of the whitelist in force (the active snapshot) and treats
// params.whitelist / params.asset_whitelist as the latest *approved* lists:
//
//   - every EndBlock compares the approved lists with the active snapshot and
//     the pending change; a new difference is recorded as the pending change
//     with activation = approval + WhitelistActivationDelay blocks and
//     + WhitelistActivationPeriod of block time (both code constants);
//   - a later, different approval replaces the pending change and restarts
//     the clock; approving the active lists again cancels it;
//   - once both bounds are reached the pending change becomes the active
//     snapshot, and the vote targets are synced to it at the end of the vote
//     period exactly as they used to be synced to the parameters.
//
// Genesis is not a change: InitGenesis activates the genesis lists at once.

// GetActiveWhitelist returns the active whitelist snapshot.
func (k Keeper) GetActiveWhitelist(ctx sdk.Context) (types.WhitelistSnapshot, bool) {
	return k.getWhitelistSnapshot(ctx, types.ActiveWhitelistKey)
}

// SetActiveWhitelist stores the active whitelist snapshot.
func (k Keeper) SetActiveWhitelist(ctx sdk.Context, s types.WhitelistSnapshot) {
	ctx.KVStore(k.storeKey).Set(types.ActiveWhitelistKey, k.cdc.MustMarshal(&s))
}

// GetPendingWhitelist returns the approved whitelist change waiting for
// activation, if any.
func (k Keeper) GetPendingWhitelist(ctx sdk.Context) (types.WhitelistSnapshot, bool) {
	return k.getWhitelistSnapshot(ctx, types.PendingWhitelistKey)
}

// SetPendingWhitelist stores the pending whitelist change.
func (k Keeper) SetPendingWhitelist(ctx sdk.Context, s types.WhitelistSnapshot) {
	ctx.KVStore(k.storeKey).Set(types.PendingWhitelistKey, k.cdc.MustMarshal(&s))
}

// DeletePendingWhitelist removes the pending whitelist change.
func (k Keeper) DeletePendingWhitelist(ctx sdk.Context) {
	ctx.KVStore(k.storeKey).Delete(types.PendingWhitelistKey)
}

func (k Keeper) getWhitelistSnapshot(ctx sdk.Context, key []byte) (types.WhitelistSnapshot, bool) {
	bz := ctx.KVStore(k.storeKey).Get(key)
	if bz == nil {
		return types.WhitelistSnapshot{}, false
	}
	var s types.WhitelistSnapshot
	k.cdc.MustUnmarshal(bz, &s)
	// protobuf decodes an empty repeated field as nil: keep lists non-nil so
	// genesis export/import round-trips exactly
	if s.Whitelist == nil {
		s.Whitelist = types.DenomList{}
	}
	if s.AssetWhitelist == nil {
		s.AssetWhitelist = types.AssetList{}
	}
	return s, true
}

// UpdateWhitelistSchedule tracks the approved whitelist (params) against the
// active snapshot and the pending change, then activates the pending change
// when it is due. It returns the active snapshot after the update. Called at
// every EndBlock; the work is O(len(whitelist) + len(asset_whitelist)).
func (k Keeper) UpdateWhitelistSchedule(ctx sdk.Context, params types.Params) types.WhitelistSnapshot {
	height, now := ctx.BlockHeight(), ctx.BlockTime()

	active, ok := k.GetActiveWhitelist(ctx)
	if !ok {
		// Chain upgraded from before §26.3 (or state built without
		// InitGenesis): the lists in force are the current parameters.
		active = types.NewImmediateWhitelistSnapshot(params.Whitelist, params.AssetWhitelist, height, now)
		k.SetActiveWhitelist(ctx, active)
		return active
	}

	pending, hasPending := k.GetPendingWhitelist(ctx)
	switch {
	case active.Matches(params.Whitelist, params.AssetWhitelist):
		if hasPending {
			// governance approved the active lists again: nothing will change
			k.DeletePendingWhitelist(ctx)
			hasPending = false
			emitWhitelistEvent(ctx, types.EventTypeWhitelistChangeCancelled, pending)
		}
	case hasPending && pending.Matches(params.Whitelist, params.AssetWhitelist):
		// the approved change is still waiting for its activation
	default:
		// a new approval: (re)start the §26.3 clock
		pending = types.NewWhitelistSnapshot(params.Whitelist, params.AssetWhitelist, height, now)
		hasPending = true
		k.SetPendingWhitelist(ctx, pending)
		emitWhitelistEvent(ctx, types.EventTypeWhitelistChangeScheduled, pending)
	}

	if hasPending && pending.IsDue(height, now) {
		k.SetActiveWhitelist(ctx, pending)
		k.DeletePendingWhitelist(ctx)
		emitWhitelistEvent(ctx, types.EventTypeWhitelistActivated, pending)
		return pending
	}
	return active
}

// InitWhitelistSchedule sets the schedule from genesis. Without an explicit
// active snapshot the genesis parameters are active immediately.
func (k Keeper) InitWhitelistSchedule(ctx sdk.Context, params types.Params, active, pending *types.WhitelistSnapshot) types.WhitelistSnapshot {
	var a types.WhitelistSnapshot
	if active != nil {
		a = *active
	} else {
		a = types.NewImmediateWhitelistSnapshot(params.Whitelist, params.AssetWhitelist, ctx.BlockHeight(), ctx.BlockTime())
	}
	k.SetActiveWhitelist(ctx, a)
	if pending != nil {
		k.SetPendingWhitelist(ctx, *pending)
	} else {
		k.DeletePendingWhitelist(ctx)
	}
	return a
}

func emitWhitelistEvent(ctx sdk.Context, eventType string, s types.WhitelistSnapshot) {
	ctx.EventManager().EmitEvent(sdk.NewEvent(eventType,
		sdk.NewAttribute(types.AttributeKeyWhitelist, denomNames(s.Whitelist)),
		sdk.NewAttribute(types.AttributeKeyAssetWhitelist, assetNames(s.AssetWhitelist)),
		sdk.NewAttribute(types.AttributeKeyApprovedHeight, strconv.FormatInt(s.ApprovedHeight, 10)),
		sdk.NewAttribute(types.AttributeKeyActivationHeight, strconv.FormatInt(s.ActivationHeight, 10)),
		sdk.NewAttribute(types.AttributeKeyActivationTime, s.ActivationTime.UTC().Format(time.RFC3339)),
	))
}

func denomNames(l types.DenomList) string {
	names := make([]string, len(l))
	for i, d := range l {
		names[i] = d.Name
	}
	return strings.Join(names, ",")
}

func assetNames(l types.AssetList) string {
	names := make([]string, len(l))
	for i, a := range l {
		names[i] = a.Name
	}
	return strings.Join(names, ",")
}
