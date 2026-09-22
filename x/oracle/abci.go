package oracle

import (
	"time"

	"cosmossdk.io/math"
	core "github.com/classic-terra/core/v4/types"
	"github.com/classic-terra/core/v4/x/oracle/keeper"
	"github.com/classic-terra/core/v4/x/oracle/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EndBlocker is called at the end of every block
func EndBlocker(ctx sdk.Context, k keeper.Keeper) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyEndBlocker)

	params := k.GetParams(ctx)
	if core.IsPeriodLastBlock(ctx, params.VotePeriod) {

		// Build claim map over all validators in active set
		validatorClaimMap := make(map[string]types.Claim)

		maxValidators, err := k.StakingKeeper.MaxValidators(ctx)
		if err != nil {
			return
		}

		iterator, err := k.StakingKeeper.ValidatorsPowerStoreIterator(ctx)
		if err != nil {
			return
		}
		defer iterator.Close()

		powerReduction := k.StakingKeeper.PowerReduction(ctx)

		i := 0
		for ; iterator.Valid() && i < int(maxValidators); iterator.Next() {
			validator, err := k.StakingKeeper.Validator(ctx, iterator.Value())
			if err != nil {
				continue
			}

			// Exclude not bonded validator
			if validator.IsBonded() {
				valAddrStr := validator.GetOperator()
				valAddr, err := sdk.ValAddressFromBech32(valAddrStr)
				if err != nil {
					continue
				}
				validatorClaimMap[valAddrStr] = types.NewClaim(validator.GetConsensusPower(powerReduction), 0, 0, valAddr)
				i++
			}
		}

		// Denom-TobinTax map
		voteTargets := make(map[string]math.LegacyDec)
		k.IterateTobinTaxes(ctx, func(denom string, tobinTax math.LegacyDec) bool {
			voteTargets[denom] = tobinTax
			return false
		})

		// Clear all exchange rates (and the latest rate samples: a denom that
		// fails this period must not keep a stale dispersion)
		k.IterateLunaExchangeRates(ctx, func(denom string, _ math.LegacyDec) (stop bool) {
			k.DeleteLunaExchangeRate(ctx, denom)
			k.DeleteRateSample(ctx, denom)
			return false
		})

		// Organize votes to ballot by denom
		// NOTE: **Filter out inactive or jailed validators**
		// NOTE: **Make abstain votes to have zero vote power**
		voteMap := k.OrganizeBallotByDenom(ctx, validatorClaimMap)

		if referenceTerra := PickReferenceTerra(ctx, k, voteTargets, voteMap); referenceTerra != "" {
			// make voteMap of Reference Terra to calculate cross exchange rates
			ballotRT := voteMap[referenceTerra]
			voteMapRT := ballotRT.ToMap()
			exchangeRateRT := ballotRT.WeightedMedian()

			// Iterate through ballots and update exchange rates; drop if not enough votes have been achieved.
			for denom, ballot := range voteMap {

				// Convert ballot to cross exchange rates
				if denom != referenceTerra {
					ballot = ballot.ToCrossRateWithSort(voteMapRT)
				}

				// Get weighted median of cross exchange rates
				exchangeRate := Tally(ballot, params.RewardBand, validatorClaimMap)

				// Transform into the original form uluna/stablecoin
				if denom != referenceTerra {
					exchangeRate = exchangeRateRT.Quo(exchangeRate)
				}

				// Set the exchange rate, emit ABCI event
				k.SetLunaExchangeRateWithEvent(ctx, denom, exchangeRate)

				// Liquidity Fabric spec §21.6 stage 1: persist the dispersion of the
				// original (non cross-rate) ballot and a bounded rate history for TWAP.
				original := voteMap[denom]
				k.SetRateSample(ctx, types.RateSample{
					Denom:        denom,
					ExchangeRate: exchangeRate,
					Dispersion:   original.Dispersion(original.WeightedMedian()),
					VotePeriod:   uint64(ctx.BlockHeight()) / params.VotePeriod,
					Height:       ctx.BlockHeight(),
					Time:         ctx.BlockTime(),
				})
			}
		}

		//---------------------------
		// Assets priced in USD (spec §21.6 stage 2): tallied without cross
		// rates; a failed ballot is not counted against the validators
		assetTargets := k.GetAssetTargets(ctx)
		k.IterateAssetPrices(ctx, func(asset string, _ math.LegacyDec) (stop bool) {
			k.DeleteAssetPrice(ctx, asset)
			k.DeleteRateSample(ctx, asset)
			return false
		})
		priceBallots, depthBallots := k.OrganizeAssetBallots(ctx, validatorClaimMap, assetTargets)
		passingAssets := TallyAssets(ctx, k, params, assetTargets, priceBallots, depthBallots, validatorClaimMap)

		//---------------------------
		// Do miss counting & slashing
		voteTargetsLen := len(voteTargets) + passingAssets
		for _, claim := range validatorClaimMap {
			// Skip abstain & valid voters
			if int(claim.WinCount) == voteTargetsLen {
				continue
			}

			// Increase miss counter
			k.SetMissCounter(ctx, claim.Recipient, k.GetMissCounter(ctx, claim.Recipient)+1)
		}

		// Distribute rewards to ballot winners
		k.RewardBallotWinners(
			ctx,
			(int64)(params.VotePeriod),
			(int64)(params.RewardDistributionWindow),
			voteTargets,
			validatorClaimMap,
		)

		// Clear the ballot
		k.ClearBallots(ctx, params.VotePeriod)

		// Update vote targets and tobin tax
		k.ApplyWhitelist(ctx, params.Whitelist, voteTargets)
		k.ApplyAssetWhitelist(ctx, params.AssetWhitelist)
	}

	// Do slash who did miss voting over threshold and
	// reset miss counters of all validators at the last block of slash window
	if core.IsPeriodLastBlock(ctx, params.SlashWindow) {
		k.SlashAndResetMissCounters(ctx)
	}
}
