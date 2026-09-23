package keeper

import (
	"bytes"
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	pdtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/types"
	"github.com/classic-terra/core/v4/x/ismbond/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// RecordRoot stores the root of the mailbox's required merkle tree hook after
// an outbound dispatch (spec §6.6: the chain knows the state the checkpoints
// attest; survey §6.4: upstream keeps no root history). Called by the warp
// wrapper after every RemoteTransfer.
func (k Keeper) RecordRoot(ctx sdk.Context, mailboxId util.HexAddress) error {
	mailbox, err := k.coreKeeper.GetMailbox(ctx, mailboxId)
	if err != nil {
		return err
	}
	if mailbox.RequiredHook == nil {
		return nil
	}
	res, err := k.coreKeeper.MerkleTreeHookQuery(ctx, &pdtypes.QueryMerkleTreeHookRequest{Id: mailbox.RequiredHook.String()})
	if err != nil || res.MerkleTreeHook.MerkleTree == nil || res.MerkleTreeHook.MerkleTree.Count == 0 {
		return nil // the required hook is not a merkle tree hook or nothing was inserted
	}
	tree := res.MerkleTreeHook.MerkleTree
	index := tree.Count - 1
	rec := types.RootRecord{MailboxId: mailboxId, Index: index, Root: tree.Root, Height: ctx.BlockHeight()}
	if err := k.Roots.Set(ctx, collections.Join(mailboxId.GetInternalId(), index), rec); err != nil {
		return err
	}
	if err := k.pruneRoots(ctx, mailboxId, index); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventRootRecorded{MailboxId: mailboxId.String(), Index: index, Root: util.EncodeEthHex(tree.Root)})
}

// pruneRoots drops records older than the dispute window (by height) and
// beyond MaxRootsPerMailbox, walking from the oldest index.
func (k Keeper) pruneRoots(ctx sdk.Context, mailboxId util.HexAddress, latest uint32) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	cutoff := ctx.BlockHeight() - params.DisputeWindowBlocks
	var drop []collections.Pair[uint64, uint32]
	err = k.Roots.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint32](mailboxId.GetInternalId()),
		func(key collections.Pair[uint64, uint32], r types.RootRecord) (bool, error) {
			if r.Height < cutoff || latest-key.K2() >= types.MaxRootsPerMailbox {
				drop = append(drop, key)
				return false, nil
			}
			return true, nil // records are in index order: the first kept ends the sweep
		})
	if err != nil {
		return err
	}
	for _, key := range drop {
		if err := k.Roots.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// GetRoot returns the recorded root of (mailbox, index).
func (k Keeper) GetRoot(ctx sdk.Context, mailboxId util.HexAddress, index uint32) (types.RootRecord, error) {
	r, err := k.Roots.Get(ctx, collections.Join(mailboxId.GetInternalId(), index))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.RootRecord{}, types.ErrRootNotRecorded.Wrapf("%s/%d", mailboxId, index)
		}
		return types.RootRecord{}, err
	}
	return r, nil
}

// SubmitEvidence judges evidence on chain (spec §6.6 items 2–3) and slashes
// the whole bond of the signing validator's operator: evidence_reward to the
// submitter, the remainder to the community pool. Permissionless.
func (k Keeper) SubmitEvidence(ctx sdk.Context, submitter string, kind types.EvidenceKind, a, b types.Checkpoint) (validator string, slashed, reward math.Int, err error) {
	signer, err := a.Signer()
	if err != nil {
		return "", math.Int{}, math.Int{}, err
	}
	switch kind {
	case types.EVIDENCE_KIND_EQUIVOCATION:
		if !a.SameSlot(b) {
			return "", math.Int{}, math.Int{}, errorsmod.Wrap(types.ErrInvalidEvidence, "checkpoints attest different slots")
		}
		if bytes.Equal(a.Root, b.Root) {
			return "", math.Int{}, math.Int{}, errorsmod.Wrap(types.ErrInvalidEvidence, "checkpoints carry the same root")
		}
		signerB, err := b.Signer()
		if err != nil {
			return "", math.Int{}, math.Int{}, err
		}
		if signerB != signer {
			return "", math.Int{}, math.Int{}, errorsmod.Wrap(types.ErrInvalidEvidence, "checkpoints signed by different validators")
		}
	case types.EVIDENCE_KIND_INVALID_OUTBOUND:
		res, err := k.coreKeeper.MerkleTreeHookQuery(ctx, &pdtypes.QueryMerkleTreeHookRequest{Id: a.MerkleTreeHook.String()})
		if err != nil {
			return "", math.Int{}, math.Int{}, errorsmod.Wrap(types.ErrInvalidEvidence, "unknown local merkle tree hook")
		}
		mailboxId, err := util.DecodeHexAddress(res.MerkleTreeHook.MailboxId)
		if err != nil {
			return "", math.Int{}, math.Int{}, err
		}
		mailbox, err := k.coreKeeper.GetMailbox(ctx, mailboxId)
		if err != nil {
			return "", math.Int{}, math.Int{}, err
		}
		if mailbox.LocalDomain != a.Origin {
			return "", math.Int{}, math.Int{}, errorsmod.Wrap(types.ErrInvalidEvidence, "checkpoint origin is not the local domain")
		}
		rec, err := k.GetRoot(ctx, mailboxId, a.Index)
		if err != nil {
			return "", math.Int{}, math.Int{}, err
		}
		if bytes.Equal(rec.Root, a.Root) {
			return "", math.Int{}, math.Int{}, errorsmod.Wrap(types.ErrInvalidEvidence, "checkpoint matches the local root")
		}
	default:
		return "", math.Int{}, math.Int{}, errorsmod.Wrap(types.ErrInvalidEvidence, "unknown evidence kind")
	}
	o, err := k.operatorOfValidator(ctx, signer)
	if err != nil {
		return "", math.Int{}, math.Int{}, err
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return "", math.Int{}, math.Int{}, err
	}
	slashed = o.Bond.Amount
	reward = params.EvidenceReward.MulInt(slashed).TruncateInt()
	remainder := slashed.Sub(reward)
	if reward.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdk.MustAccAddressFromBech32(submitter), sdk.NewCoins(sdk.NewCoin(o.Bond.Denom, reward))); err != nil {
			return "", math.Int{}, math.Int{}, err
		}
	}
	if remainder.IsPositive() {
		if err := k.distrKeeper.FundCommunityPool(ctx, sdk.NewCoins(sdk.NewCoin(o.Bond.Denom, remainder)), authtypes.NewModuleAddress(types.ModuleName)); err != nil {
			return "", math.Int{}, math.Int{}, err
		}
	}
	if err := k.removeOperator(ctx, o); err != nil {
		return "", math.Int{}, math.Int{}, err
	}
	return signer, slashed, reward, ctx.EventManager().EmitTypedEvent(&types.EventOperatorSlashed{
		Operator: o.Address, Validator: signer, Amount: sdk.NewCoin(o.Bond.Denom, slashed).String(), Reward: reward.String(), Submitter: submitter, Kind: kind.String(),
	})
}
