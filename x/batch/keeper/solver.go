package keeper

import (
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetSolver returns a registered solver.
func (k Keeper) GetSolver(ctx sdk.Context, addr string) (types.Solver, error) {
	s, err := k.Solvers.Get(ctx, addr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Solver{}, types.ErrSolverNotFound.Wrap(addr)
		}
		return types.Solver{}, err
	}
	return s, nil
}

// RegisterSolver locks the bond (spec §16.1). Permissionless.
func (k Keeper) RegisterSolver(ctx sdk.Context, addr string, bond sdk.Coin) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if bond.Denom != params.SolverBondMin.Denom || bond.Amount.LT(params.SolverBondMin.Amount) {
		return errorsmod.Wrapf(types.ErrInsufficientBond, "minimum bond is %s", params.SolverBondMin)
	}
	if has, err := k.Solvers.Has(ctx, addr); err != nil {
		return err
	} else if has {
		return types.ErrSolverExists.Wrap(addr)
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sdk.MustAccAddressFromBech32(addr), types.ModuleName, sdk.NewCoins(bond)); err != nil {
		return err
	}
	return k.Solvers.Set(ctx, addr, types.Solver{Address: addr, Bond: bond, WindowStart: ctx.BlockHeight()})
}

// UnbondSolver starts the exit; the bond is refunded after solver_unbond_blocks
// while penalties still apply (spec §16.3).
func (k Keeper) UnbondSolver(ctx sdk.Context, addr string) (int64, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return 0, err
	}
	s, err := k.GetSolver(ctx, addr)
	if err != nil {
		return 0, err
	}
	if s.UnbondHeight != 0 {
		return 0, types.ErrSolverUnbonding.Wrap(addr)
	}
	s.UnbondHeight = ctx.BlockHeight() + params.SolverUnbondBlocks
	return s.UnbondHeight, k.Solvers.Set(ctx, addr, s)
}

// refundUnbondedSolvers pays back bonds whose unbonding matured and returns
// the solver's escrow balances.
func (k Keeper) refundUnbondedSolvers(ctx sdk.Context) error {
	var done []types.Solver
	if err := k.Solvers.Walk(ctx, nil, func(_ string, s types.Solver) (bool, error) {
		if s.UnbondHeight != 0 && s.UnbondHeight <= ctx.BlockHeight() {
			done = append(done, s)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for _, s := range done {
		addr := sdk.MustAccAddressFromBech32(s.Address)
		refund := sdk.NewCoins()
		if s.Bond.IsPositive() {
			refund = refund.Add(s.Bond)
		}
		escrow, err := k.SolverEscrowBalances(ctx, s.Address)
		if err != nil {
			return err
		}
		refund = refund.Add(escrow...)
		if !refund.IsZero() {
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, addr, refund); err != nil {
				return err
			}
		}
		if err := k.SolverEscrow.Clear(ctx, collections.NewPrefixedPairRange[string, string](s.Address)); err != nil {
			return err
		}
		if err := k.Solvers.Remove(ctx, s.Address); err != nil {
			return err
		}
	}
	return nil
}

// DepositSolverEscrow adds trading balance to the solver's escrow.
func (k Keeper) DepositSolverEscrow(ctx sdk.Context, addr string, amount sdk.Coin) error {
	if _, err := k.GetSolver(ctx, addr); err != nil {
		return err
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sdk.MustAccAddressFromBech32(addr), types.ModuleName, sdk.NewCoins(amount)); err != nil {
		return err
	}
	return k.addEscrow(ctx, addr, amount.Denom, amount.Amount)
}

// WithdrawSolverEscrow returns trading balance to the solver.
func (k Keeper) WithdrawSolverEscrow(ctx sdk.Context, addr string, amount sdk.Coin) error {
	if _, err := k.GetSolver(ctx, addr); err != nil {
		return err
	}
	// balance reserved by unresolved revealed bids stays locked until resolution
	reserved, err := k.reservedEscrow(ctx, addr, amount.Denom)
	if err != nil {
		return err
	}
	bal, err := k.escrowBalance(ctx, addr, amount.Denom)
	if err != nil {
		return err
	}
	if bal.Sub(reserved).LT(amount.Amount) {
		return errorsmod.Wrapf(types.ErrInsufficientEscrow, "free %s, requested %s", bal.Sub(reserved), amount.Amount)
	}
	if err := k.addEscrow(ctx, addr, amount.Denom, amount.Amount.Neg()); err != nil {
		return err
	}
	return k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdk.MustAccAddressFromBech32(addr), sdk.NewCoins(amount))
}

func (k Keeper) escrowBalance(ctx sdk.Context, addr, denom string) (math.Int, error) {
	v, err := k.SolverEscrow.Get(ctx, collections.Join(addr, denom))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.Int{}, err
	}
	return v, nil
}

func (k Keeper) addEscrow(ctx sdk.Context, addr, denom string, delta math.Int) error {
	bal, err := k.escrowBalance(ctx, addr, denom)
	if err != nil {
		return err
	}
	bal = bal.Add(delta)
	if bal.IsNegative() {
		return errorsmod.Wrapf(types.ErrInsufficientEscrow, "%s %s", addr, denom)
	}
	if bal.IsZero() {
		return k.SolverEscrow.Remove(ctx, collections.Join(addr, denom))
	}
	return k.SolverEscrow.Set(ctx, collections.Join(addr, denom), bal)
}

// SolverEscrowBalances lists a solver's escrow.
func (k Keeper) SolverEscrowBalances(ctx sdk.Context, addr string) (sdk.Coins, error) {
	var out sdk.Coins
	err := k.SolverEscrow.Walk(ctx, collections.NewPrefixedPairRange[string, string](addr),
		func(key collections.Pair[string, string], v math.Int) (bool, error) {
			out = out.Add(sdk.NewCoin(key.K2(), v))
			return false, nil
		})
	return out, err
}

// reservedEscrow sums what the solver's revealed, unresolved bids need in a denom.
func (k Keeper) reservedEscrow(ctx sdk.Context, addr, denom string) (math.Int, error) {
	reserved := math.ZeroInt()
	err := k.Commits.Walk(ctx, nil, func(_ collections.Triple[uint64, string, string], c types.Commit) (bool, error) {
		if c.Solver != addr || !c.Revealed || c.Bid == nil {
			return false, nil
		}
		market, err := k.GetMarket(ctx, c.MarketId)
		if err != nil || market.Type != types.MARKET_TYPE_SPOT {
			// perp levels reserve margin in x/perp, not escrow
			return false, nil
		}
		reserved = reserved.Add(bidRequirement(market, *c.Bid, denom))
		return false, nil
	})
	return reserved, err
}

// bidRequirement returns how much of `denom` a bid needs in escrow.
func bidRequirement(market types.Market, bid types.Bid, denom string) math.Int {
	need := math.ZeroInt()
	for _, l := range bid.Levels {
		switch {
		case l.Side == types.SIDE_BUY && denom == market.QuoteDenom:
			need = need.Add(math.LegacyNewDecFromInt(l.Qty).Mul(l.Price).Ceil().TruncateInt())
		case l.Side == types.SIDE_SELL && denom == market.BaseDenom:
			need = need.Add(l.Qty)
		}
	}
	return need
}

// slashSolver takes `amount` from the bond into the fee sink (spec §16.2).
func (k Keeper) slashSolver(ctx sdk.Context, addr string, amount sdk.Coin, reason string) error {
	s, err := k.GetSolver(ctx, addr)
	if err != nil {
		return err
	}
	if amount.Denom != s.Bond.Denom {
		return nil
	}
	take := math.MinInt(amount.Amount, s.Bond.Amount)
	if !take.IsPositive() {
		return nil
	}
	s.Bond.Amount = s.Bond.Amount.Sub(take)
	if err := k.Solvers.Set(ctx, addr, s); err != nil {
		return err
	}
	if err := k.feeSink.Deposit(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(amount.Denom, take))); err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventSolverSlashed{Solver: addr, Amount: sdk.NewCoin(amount.Denom, take).String(), Reason: reason})
}

// rollSolverWindow applies the reveal-rate rule (spec §16.2): below 90% over
// RevealRateWindowBlocks the solver is suspended.
func (k Keeper) rollSolverWindow(ctx sdk.Context, s *types.Solver, suspension int64) error {
	if ctx.BlockHeight()-s.WindowStart < types.RevealRateWindowBlocks {
		return nil
	}
	if s.Commits > 0 && s.Reveals*10_000 < s.Commits*types.MinRevealRateBps {
		s.SuspendedUntil = ctx.BlockHeight() + suspension
		if err := ctx.EventManager().EmitTypedEvent(&types.EventSolverSuspended{Solver: s.Address, UntilHeight: s.SuspendedUntil}); err != nil {
			return err
		}
	}
	s.Commits, s.Reveals, s.WindowStart = 0, 0, ctx.BlockHeight()
	return nil
}
