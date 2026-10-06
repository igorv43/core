package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultSettlementDenom is the settlement denom of the initial parameters:
// the multi-origin USDC basket synthetic `uusdc.lf` (spec §11.4, D-29). The
// solver bond and its slashes are denominated in it (§16.1, §21.5 rule 1:
// solver bonds only in the settlement asset, always).
const DefaultSettlementDenom = "uusdc.lf"

// ForbiddenBondDenom is USTC (`uusd`). Spec §11.3 (D-18), hard rule outside
// governance: USTC never denominates the insurance fund nor the bonds, in any
// phase. Changing this requires a software upgrade (§26.3 style limit in code).
const ForbiddenBondDenom = "uusd"

// DefaultParams returns the initial parameters of spec Annex B. The fee
// values follow the market comparison of spec v0.9.10 (Annex G, accessed
// 2026-10-05): user intents pay spot_fee_bps, solver levels the lower
// spot_solver_fee_bps (the batch has no resting book: solver levels are the
// liquidity supplied, intents the liquidity taken).
func DefaultParams() Params {
	return Params{
		CommitWindow:    2,
		PriceBand:       math.LegacyNewDecWithPrec(2, 2), // 2%
		IntentTtlBlocks: 600,
		// ~US$0.001 in the settlement asset: an anti-spam fee that does not
		// drift with the LUNC price and that remote users pay from the USDC
		// they brought (Hyperliquid prices an extra action at US$0.0005).
		IntentFee:              sdk.NewCoin(DefaultSettlementDenom, math.NewInt(1_000)),
		MaxIntentsPerBatch:     1_000,
		MaxSolversPerBatch:     20,
		MaxLevelsPerBid:        10,
		MaxActiveMarkets:       2,
		MaxResolutionPasses:    3,
		SpotFeeBps:             5,                                                                // protocol take of intent venues: CoW 2, UniswapX <= 5, Uniswap v2 5
		SpotSolverFeeBps:       0,                                                                // solvers price the spread themselves (CoW solvers pay no fee)
		BuilderFeeMaxBps:       10,                                                               // perp cap, = Hyperliquid builder codes on perps
		BuilderFeeMaxSpotBps:   50,                                                               // spot cap, below Hyperliquid and CoW (100 bps)
		SolverBondMin:          sdk.NewCoin(DefaultSettlementDenom, math.NewInt(20_000_000_000)), // 20,000 = 10x the largest slash (Annex B)
		SlashNoReveal:          sdk.NewCoin(DefaultSettlementDenom, math.NewInt(1_000_000_000)),  // 1,000; inconsistent reveal = 2x
		SolverUnbondBlocks:     100_800,                                                          // ~7 days at 6 s
		SolverSuspensionBlocks: 14_400,                                                           // ~1 day
		PruneDelayBlocks:       100_800,
		MaxFrontendApprovals:   10,
	}
}

// Validate validates the parameters against the absolute maxima of §12.2.
func (p Params) Validate() error {
	if p.CommitWindow < 1 || p.CommitWindow > 10 {
		return fmt.Errorf("commit_window must be within [1, 10]")
	}
	if p.PriceBand.IsNil() || !p.PriceBand.IsPositive() || p.PriceBand.GT(math.LegacyNewDecWithPrec(5, 1)) {
		return fmt.Errorf("price_band must be within (0, 0.5]")
	}
	if p.IntentTtlBlocks < 1 || p.IntentTtlBlocks > MaxIntentTTLAbsolute {
		return fmt.Errorf("intent_ttl_blocks must be within [1, %d]", MaxIntentTTLAbsolute)
	}
	if err := p.IntentFee.Validate(); err != nil {
		return fmt.Errorf("intent_fee: %w", err)
	}
	if p.MaxIntentsPerBatch == 0 || p.MaxIntentsPerBatch > MaxIntentsPerBatchAbsolute {
		return fmt.Errorf("max_intents_per_batch must be within [1, %d]", MaxIntentsPerBatchAbsolute)
	}
	if p.MaxSolversPerBatch == 0 || p.MaxSolversPerBatch > MaxSolversPerBatchAbsolute {
		return fmt.Errorf("max_solvers_per_batch must be within [1, %d]", MaxSolversPerBatchAbsolute)
	}
	if p.MaxLevelsPerBid == 0 || p.MaxLevelsPerBid > MaxLevelsPerBidAbsolute {
		return fmt.Errorf("max_levels_per_bid must be within [1, %d]", MaxLevelsPerBidAbsolute)
	}
	if p.MaxActiveMarkets == 0 || p.MaxActiveMarkets > MaxActiveMarketsAbsolute {
		return fmt.Errorf("max_active_markets must be within [1, %d]", MaxActiveMarketsAbsolute)
	}
	if p.MaxResolutionPasses == 0 || p.MaxResolutionPasses > MaxResolutionPassesAbsolute {
		return fmt.Errorf("max_resolution_passes must be within [1, %d]", MaxResolutionPassesAbsolute)
	}
	if p.SpotFeeBps > 1_000 {
		return fmt.Errorf("spot_fee_bps must not exceed 1000")
	}
	if p.SpotSolverFeeBps > p.SpotFeeBps {
		return fmt.Errorf("spot_solver_fee_bps must not exceed spot_fee_bps: solver levels never pay more than intents")
	}
	if p.BuilderFeeMaxBps > MaxBuilderFeeBpsAbsolute {
		return fmt.Errorf("builder_fee_max_bps must not exceed %d", MaxBuilderFeeBpsAbsolute)
	}
	if p.BuilderFeeMaxSpotBps > MaxBuilderFeeBpsAbsolute {
		return fmt.Errorf("builder_fee_max_spot_bps must not exceed %d", MaxBuilderFeeBpsAbsolute)
	}
	if err := p.SolverBondMin.Validate(); err != nil || !p.SolverBondMin.IsPositive() {
		return fmt.Errorf("solver_bond_min must be a positive coin")
	}
	if err := p.SlashNoReveal.Validate(); err != nil {
		return fmt.Errorf("slash_no_reveal: %w", err)
	}
	if p.SlashNoReveal.Denom != p.SolverBondMin.Denom {
		return fmt.Errorf("slash_no_reveal must be denominated like solver_bond_min")
	}
	if p.SolverBondMin.Denom == ForbiddenBondDenom {
		return fmt.Errorf("solver_bond_min must not be denominated in %s (USTC): bonds are in the settlement denom and USTC never denominates them (spec §11.3, D-18)", ForbiddenBondDenom)
	}
	if p.SolverUnbondBlocks < 1 || p.SolverSuspensionBlocks < 1 || p.PruneDelayBlocks < 1 {
		return fmt.Errorf("solver_unbond_blocks, solver_suspension_blocks and prune_delay_blocks must be positive")
	}
	if p.MaxFrontendApprovals == 0 || p.MaxFrontendApprovals > 100 {
		return fmt.Errorf("max_frontend_approvals must be within [1, 100]")
	}
	return nil
}
