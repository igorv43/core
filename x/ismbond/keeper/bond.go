package keeper

import (
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	"github.com/classic-terra/core/v4/x/ismbond/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetOperator returns a bonded operator.
func (k Keeper) GetOperator(ctx sdk.Context, address string) (types.Operator, error) {
	o, err := k.Operators.Get(ctx, address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Operator{}, types.ErrOperatorNotFound.Wrap(address)
		}
		return types.Operator{}, err
	}
	return o, nil
}

// Bond locks the bond of an ISM operator for a validator address (opt-in, spec §6.6 item 1).
func (k Keeper) Bond(ctx sdk.Context, operator string, validator string, bond sdk.Coin) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	validator, err = types.NormalizeValidator(validator)
	if err != nil {
		return err
	}
	if bond.Denom != params.BondMin.Denom || bond.Amount.LT(params.BondMin.Amount) {
		return errorsmod.Wrapf(types.ErrInsufficientBond, "minimum %s", params.BondMin)
	}
	if has, err := k.Operators.Has(ctx, operator); err != nil {
		return err
	} else if has {
		return types.ErrOperatorExists.Wrap(operator)
	}
	if has, err := k.ByValidator.Has(ctx, validator); err != nil {
		return err
	} else if has {
		return errorsmod.Wrapf(types.ErrOperatorExists, "validator %s already bonded", validator)
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sdk.MustAccAddressFromBech32(operator), types.ModuleName, sdk.NewCoins(bond)); err != nil {
		return err
	}
	if err := k.Operators.Set(ctx, operator, types.Operator{Address: operator, Validator: validator, Bond: bond, BondedHeight: ctx.BlockHeight()}); err != nil {
		return err
	}
	if err := k.ByValidator.Set(ctx, validator, operator); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventOperatorBonded{Operator: operator, Validator: validator, Bond: bond.String()})
}

// Unbond starts the exit: the bond stays slashable for unbond_delay_blocks (spec §6.6 item 4).
func (k Keeper) Unbond(ctx sdk.Context, operator string) (int64, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return 0, err
	}
	o, err := k.GetOperator(ctx, operator)
	if err != nil {
		return 0, err
	}
	if o.UnbondHeight != 0 {
		return 0, types.ErrUnbonding.Wrap(operator)
	}
	o.UnbondHeight = ctx.BlockHeight() + params.UnbondDelayBlocks
	if err := k.Operators.Set(ctx, operator, o); err != nil {
		return 0, err
	}
	return o.UnbondHeight, ctx.EventManager().EmitTypedEvent(&types.EventOperatorUnbonding{Operator: operator, ClaimHeight: o.UnbondHeight})
}

// Claim pays back a matured bond and removes the operator.
func (k Keeper) Claim(ctx sdk.Context, operator string) error {
	o, err := k.GetOperator(ctx, operator)
	if err != nil {
		return err
	}
	if o.UnbondHeight == 0 || o.UnbondHeight > ctx.BlockHeight() {
		return errorsmod.Wrapf(types.ErrNotClaimable, "claimable at height %d", o.UnbondHeight)
	}
	if o.Bond.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdk.MustAccAddressFromBech32(operator), sdk.NewCoins(o.Bond)); err != nil {
			return err
		}
	}
	if err := k.removeOperator(ctx, o); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventBondClaimed{Operator: operator, Amount: o.Bond.String()})
}

func (k Keeper) removeOperator(ctx sdk.Context, o types.Operator) error {
	if err := k.Operators.Remove(ctx, o.Address); err != nil {
		return err
	}
	return k.ByValidator.Remove(ctx, o.Validator)
}

// AllOperators lists the operators in address order.
func (k Keeper) AllOperators(ctx sdk.Context) ([]types.Operator, error) {
	out := []types.Operator{}
	err := k.Operators.Walk(ctx, nil, func(_ string, o types.Operator) (bool, error) {
		out = append(out, o)
		return false, nil
	})
	return out, err
}

// operatorOfValidator returns the operator bonded for a validator address.
func (k Keeper) operatorOfValidator(ctx sdk.Context, validator string) (types.Operator, error) {
	addr, err := k.ByValidator.Get(ctx, validator)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Operator{}, types.ErrValidatorNotBonded.Wrap(validator)
		}
		return types.Operator{}, err
	}
	return k.GetOperator(ctx, addr)
}

// IsBonded reports whether at least `threshold` validators of a multisig ISM
// hold an active bond (spec §6.6 item 1), with the details.
func (k Keeper) IsBonded(ctx sdk.Context, ismId util.HexAddress) (*types.QueryIsmBondedResponse, error) {
	res, err := k.coreKeeper.IsmQuery(ctx, &ismtypes.QueryIsmRequest{Id: ismId.String()})
	if err != nil {
		return nil, err
	}
	var ism ismtypes.HyperlaneInterchainSecurityModule
	if err := k.cdc.UnpackAny(&res.Ism, &ism); err != nil {
		return nil, err
	}
	var validators []string
	var threshold uint32
	switch m := ism.(type) {
	case *ismtypes.MessageIdMultisigISM:
		validators, threshold = m.Validators, m.Threshold
	case *ismtypes.MerkleRootMultisigISM:
		validators, threshold = m.Validators, m.Threshold
	default:
		return &types.QueryIsmBondedResponse{Bonded: false}, nil
	}
	out := &types.QueryIsmBondedResponse{Threshold: threshold, Validators: validators, BondedValidators: []string{}}
	for _, v := range validators {
		norm, err := types.NormalizeValidator(v)
		if err != nil {
			continue
		}
		o, err := k.operatorOfValidator(ctx, norm)
		if err == nil && o.UnbondHeight == 0 && o.Bond.IsPositive() {
			out.BondedValidators = append(out.BondedValidators, norm)
		}
	}
	out.Bonded = threshold > 0 && uint32(len(out.BondedValidators)) >= threshold
	return out, nil
}

// DistributeRewards splits an amount among the active operators pro rata to
// their bonds (spec §6.6 item 5: the bridge pays for its security).
func (k Keeper) DistributeRewards(ctx sdk.Context, sender sdk.AccAddress, amount sdk.Coin) error {
	operators, err := k.AllOperators(ctx)
	if err != nil {
		return err
	}
	total := math.ZeroInt()
	var active []types.Operator
	for _, o := range operators {
		if o.UnbondHeight == 0 && o.Bond.IsPositive() {
			active = append(active, o)
			total = total.Add(o.Bond.Amount)
		}
	}
	if len(active) == 0 || !total.IsPositive() {
		return errorsmod.Wrap(types.ErrOperatorNotFound, "no active operator to reward")
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sender, types.ModuleName, sdk.NewCoins(amount)); err != nil {
		return err
	}
	distributed := math.ZeroInt()
	for i, o := range active {
		share := amount.Amount.Mul(o.Bond.Amount).Quo(total)
		if i == len(active)-1 {
			share = amount.Amount.Sub(distributed) // the last one takes the rounding remainder
		}
		if !share.IsPositive() {
			continue
		}
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdk.MustAccAddressFromBech32(o.Address), sdk.NewCoins(sdk.NewCoin(amount.Denom, share))); err != nil {
			return err
		}
		distributed = distributed.Add(share)
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventRewardsDistributed{Amount: amount.String(), Operators: uint32(len(active))})
}

// IsmBondedForToken resolves the ISM of a warp token (its own or the mailbox
// default) and reports whether it is bonded (x/warpledger cap rule, §7.2).
func (k Keeper) IsmBondedForToken(ctx sdk.Context, ismId *util.HexAddress, mailboxId util.HexAddress) (bool, error) {
	if ismId == nil {
		mailbox, err := k.coreKeeper.GetMailbox(ctx, mailboxId)
		if err != nil {
			return false, err
		}
		ismId = &mailbox.DefaultIsm
	}
	res, err := k.IsBonded(ctx, *ismId)
	if err != nil {
		return false, err
	}
	return res.Bonded, nil
}
