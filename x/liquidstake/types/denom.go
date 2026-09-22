package types

import banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

// StDenomMetadata is the bank metadata of the liquid staking token.
func StDenomMetadata() banktypes.Metadata {
	return banktypes.Metadata{
		Description: "Liquid staked Luna Classic: non-rebasing receipt of LUNC delegated by the x/liquidstake module. Its value in uluna is the module exchange rate.",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: StDenom, Exponent: 0},
			{Denom: StDisplayDenom, Exponent: StExponent},
		},
		Base:    StDenom,
		Display: StDisplayDenom,
		Name:    "Liquid staked LUNC",
		Symbol:  StDisplayDenom,
	}
}
