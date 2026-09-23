package wasmbinding

import (
	"fmt"
	"sync"

	wasmvmtypes "github.com/CosmWasm/wasmvm/v3/types"
	batchtypes "github.com/classic-terra/core/v4/x/batch/types"
	markettypes "github.com/classic-terra/core/v4/x/market/types"
	oracletypes "github.com/classic-terra/core/v4/x/oracle/types"
	perptypes "github.com/classic-terra/core/v4/x/perp/types"
	treasurytypes "github.com/classic-terra/core/v4/x/treasury/types"
	warpledgertypes "github.com/classic-terra/core/v4/x/warpledger/types"
	"github.com/cosmos/cosmos-sdk/codec"
)

// stargateWhitelist keeps whitelist and its deterministic
// response binding for stargate queries.
//
// The query can be multi-thread, so we have to use
// thread safe sync.Map.
var stargateWhitelist sync.Map

func init() {
	// market
	setWhitelistedQuery("/terra.market.v1beta1.Query/Swap", &markettypes.QuerySwapResponse{})

	// treasury
	setWhitelistedQuery("/terra.treasury.v1beta1.Query/TaxCap", &treasurytypes.QueryTaxCapResponse{})
	setWhitelistedQuery("/terra.treasury.v1beta1.Query/TaxRate", &treasurytypes.QueryTaxRateResponse{})

	// oracle
	setWhitelistedQuery("/terra.oracle.v1beta1.Query/ExchangeRate", &oracletypes.QueryExchangeRateResponse{})
	setWhitelistedQuery("/terra.oracle.v1beta1.Query/Dispersion", &oracletypes.QueryDispersionResponse{})
	setWhitelistedQuery("/terra.oracle.v1beta1.Query/Twap", &oracletypes.QueryTwapResponse{})
	setWhitelistedQuery("/terra.oracle.v1beta1.Query/AssetPrice", &oracletypes.QueryAssetPriceResponse{})

	// warpledger (Proof of Collateralization, spec §9.7): lets CosmWasm
	// contracts (DEXes, integrators) read the solvency ledger on-chain
	setWhitelistedQuery("/terra.warpledger.v1.Query/Params", &warpledgertypes.QueryParamsResponse{})
	setWhitelistedQuery("/terra.warpledger.v1.Query/WarpLedger", &warpledgertypes.QueryWarpLedgerResponse{})
	setWhitelistedQuery("/terra.warpledger.v1.Query/DepositAddress", &warpledgertypes.QueryDepositAddressResponse{})

	// batch (spec §23.2): read-only market, pipeline and clearing data for
	// integrators; sealed commits stay sealed in the Batch response
	setWhitelistedQuery("/terra.batch.v1.Query/Params", &batchtypes.QueryParamsResponse{})
	setWhitelistedQuery("/terra.batch.v1.Query/Market", &batchtypes.QueryMarketResponse{})
	setWhitelistedQuery("/terra.batch.v1.Query/Markets", &batchtypes.QueryMarketsResponse{})
	setWhitelistedQuery("/terra.batch.v1.Query/Batch", &batchtypes.QueryBatchResponse{})
	setWhitelistedQuery("/terra.batch.v1.Query/Pipeline", &batchtypes.QueryPipelineResponse{})

	// perp (spec Annex E.3): read-only market, position and fund data
	setWhitelistedQuery("/terra.perp.v1.Query/Params", &perptypes.QueryParamsResponse{})
	setWhitelistedQuery("/terra.perp.v1.Query/Market", &perptypes.QueryMarketResponse{})
	setWhitelistedQuery("/terra.perp.v1.Query/Markets", &perptypes.QueryMarketsResponse{})
	setWhitelistedQuery("/terra.perp.v1.Query/Position", &perptypes.QueryPositionResponse{})
	setWhitelistedQuery("/terra.perp.v1.Query/InsuranceFund", &perptypes.QueryInsuranceFundResponse{})
	setWhitelistedQuery("/terra.perp.v1.Query/ADLRank", &perptypes.QueryADLRankResponse{})
}

// GetWhitelistedQuery returns the whitelisted query at the provided path.
// If the query does not exist, or it was setup wrong by the chain, this returns an error.
func GetWhitelistedQuery(queryPath string) (codec.ProtoMarshaler, error) {
	protoResponseAny, isWhitelisted := stargateWhitelist.Load(queryPath)
	if !isWhitelisted {
		return nil, wasmvmtypes.UnsupportedRequest{Kind: fmt.Sprintf("'%s' path is not allowed from the contract", queryPath)}
	}
	protoResponseType, ok := protoResponseAny.(codec.ProtoMarshaler)
	if !ok {
		return nil, wasmvmtypes.Unknown{}
	}
	return protoResponseType, nil
}

func setWhitelistedQuery(queryPath string, protoType codec.ProtoMarshaler) {
	stargateWhitelist.Store(queryPath, protoType)
}
