package rest

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/classic-terra/core/v2/x/oracle/types"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/rest"
)

func registerQueryRoutes(cliCtx client.Context, rtr *mux.Router) {
	rtr.HandleFunc(fmt.Sprintf("/oracle/denoms/{%s}/exchange_rate", RestDenom), queryExchangeRateHandlerFunction(cliCtx)).Methods("GET")
	rtr.HandleFunc(fmt.Sprintf("/oracle/denoms/{%s}/tobin_tax", RestDenom), queryTobinTaxHandlerFunction(cliCtx)).Methods("GET")
	rtr.HandleFunc("/oracle/denoms/actives", queryActivesHandlerFunction(cliCtx)).Methods("GET")
	rtr.HandleFunc("/oracle/denoms/exchange_rates", queryExchangeRatesHandlerFunction(cliCtx)).Methods("GET")
	rtr.HandleFunc("/oracle/denoms/vote_targets", queryVoteTargetsHandlerFunction(cliCtx)).Methods("GET")
	rtr.HandleFunc("/oracle/denoms/tobin_taxes", queryTobinTaxesHandlerFunction(cliCtx)).Methods("GET")
	rtr.HandleFunc(fmt.Sprintf("/oracle/voters/{%s}/feeder", RestVoter), queryFeederDelegationHandlerFunction(cliCtx)).Methods("GET")
	rtr.HandleFunc(fmt.Sprintf("/oracle/voters/{%s}/miss", RestVoter), queryMissHandlerFunction(cliCtx)).Methods("GET")
	rtr.HandleFunc(fmt.Sprintf("/oracle/voters/{%s}/aggregate_prevote", RestVoter), queryAggregatePrevoteHandlerFunction(cliCtx)).Methods("GET")
	rtr.HandleFunc(fmt.Sprintf("/oracle/voters/{%s}/aggregate_vote", RestVoter), queryAggregateVoteHandlerFunction(cliCtx)).Methods("GET")
	rtr.HandleFunc("/oracle/voters/aggregate_prevotes", queryAggregatePrevotesHandlerFunction(cliCtx)).Methods("GET")
	rtr.HandleFunc("/oracle/voters/aggregate_votes", queryAggregateVotesHandlerFunction(cliCtx)).Methods("GET")
	rtr.HandleFunc("/oracle/parameters", queryParamsHandlerFunction(cliCtx)).Methods("GET")
}

func queryExchangeRateHandlerFunction(cliCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cliCtx, ok := rest.ParseQueryHeightOrReturnBadRequest(w, cliCtx, r)
		if !ok {
			return
		}

		denom, ok := checkDenomVar(w, r)
		if !ok {
			return
		}

		params := types.NewQueryExchangeRateParams(denom)
		bz, err := cliCtx.LegacyAmino.MarshalJSON(params)
		if rest.CheckBadRequestError(w, err) {
			return
		}
		res, height, err := cliCtx.QueryWithData(fmt.Sprintf("custom/%s/%s", types.QuerierRoute, types.QueryExchangeRate), bz)
		if rest.CheckInternalServerError(w, err) {
			return
		}

		cliCtx = cliCtx.WithHeight(height)
		rest.PostProcessResponse(w, cliCtx, res)
	}
}

func queryExchangeRatesHandlerFunction(cliCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cliCtx, ok := rest.ParseQueryHeightOrReturnBadRequest(w, cliCtx, r)
		if !ok {
			return
		}

		res, height, err := cliCtx.QueryWithData(fmt.Sprintf("custom/%s/%s", types.QuerierRoute, types.QueryExchangeRates), nil)
		if rest.CheckInternalServerError(w, err) {
			return
		}

		cliCtx = cliCtx.WithHeight(height)
		rest.PostProcessResponse(w, cliCtx, res)
	}
}

func queryActivesHandlerFunction(cliCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cliCtx, ok := rest.ParseQueryHeightOrReturnBadRequest(w, cliCtx, r)
		if !ok {
			return
		}

		res, height, err := cliCtx.QueryWithData(fmt.Sprintf("custom/%s/%s", types.QuerierRoute, types.QueryActives), nil)
		if rest.CheckInternalServerError(w, err) {
			return
		}

		cliCtx = cliCtx.WithHeight(height)
		rest.PostProcessResponse(w, cliCtx, res)
	}
}

func queryParamsHandlerFunction(cliCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cliCtx, ok := rest.ParseQueryHeightOrReturnBadRequest(w, cliCtx, r)
		if !ok {
			return
		}

		res, height, err := cliCtx.QueryWithData(fmt.Sprintf("custom/%s/%s", types.QuerierRoute, types.QueryParameters), nil)
		if rest.CheckInternalServerError(w, err) {
			return
		}

		cliCtx = cliCtx.WithHeight(height)
		rest.PostProcessResponse(w, cliCtx, res)
	}
}

func queryFeederDelegationHandlerFunction(cliCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cliCtx, ok := rest.ParseQueryHeightOrReturnBadRequest(w, cliCtx, r)
		if !ok {
			return
		}

		voterAddr, ok := checkVoterAddressVar(w, r)
		if !ok {
			return
		}

		params := types.NewQueryFeederDelegationParams(voterAddr)
		bz, err := cliCtx.LegacyAmino.MarshalJSON(params)
		if rest.CheckBadRequestError(w, err) {
			return
		}
		res, height, err := cliCtx.QueryWithData(fmt.Sprintf("custom/%s/%s", types.QuerierRoute, types.QueryFeederDelegation), bz)
		if rest.CheckInternalServerError(w, err) {
			return
		}

		cliCtx = cliCtx.WithHeight(height)
		rest.PostProcessResponse(w, cliCtx, res)
	}
}

func queryMissHandlerFunction(cliCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cliCtx, ok := rest.ParseQueryHeightOrReturnBadRequest(w, cliCtx, r)
		if !ok {
			return
		}

		voterAddr, ok := checkVoterAddressVar(w, r)
		if !ok {
			return
		}

		params := types.NewQueryMissCounterParams(voterAddr)
		bz, err := cliCtx.LegacyAmino.MarshalJSON(params)
		if rest.CheckBadRequestError(w, err) {
			return
		}
		res, height, err := cliCtx.QueryWithData(fmt.Sprintf("custom/%s/%s", types.QuerierRoute, types.QueryMissCounter), bz)
		if rest.CheckInternalServerError(w, err) {
			return
		}

		cliCtx = cliCtx.WithHeight(height)
		rest.PostProcessResponse(w, cliCtx, res)
	}
}

func queryAggregatePrevoteHandlerFunction(cliCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cliCtx, ok := rest.ParseQueryHeightOrReturnBadRequest(w, cliCtx, r)
		if !ok {
			return
		}

		voterAddr, ok := checkVoterAddressVar(w, r)
		if !ok {
			return
		}

		params := types.NewQueryAggregatePrevoteParams(voterAddr)
		bz, err := cliCtx.LegacyAmino.MarshalJSON(params)
		if rest.CheckBadRequestError(w, err) {
			return
		}
		res, height, err := cliCtx.QueryWithData(fmt.Sprintf("custom/%s/%s", types.QuerierRoute, types.QueryAggregatePrevote), bz)
		if rest.CheckInternalServerError(w, err) {
			return
		}

		cliCtx = cliCtx.WithHeight(height)
		rest.PostProcessResponse(w, cliCtx, res)
	}
}

func queryAggregateVoteHandlerFunction(cliCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cliCtx, ok := rest.ParseQueryHeightOrReturnBadRequest(w, cliCtx, r)
		if !ok {
			return
		}

		voterAddr, ok := checkVoterAddressVar(w, r)
		if !ok {
			return
		}

		params := types.NewQueryAggregateVoteParams(voterAddr)
		bz, err := cliCtx.LegacyAmino.MarshalJSON(params)
		if rest.CheckBadRequestError(w, err) {
			return
		}
		res, height, err := cliCtx.QueryWithData(fmt.Sprintf("custom/%s/%s", types.QuerierRoute, types.QueryAggregateVote), bz)
		if rest.CheckInternalServerError(w, err) {
			return
		}

		cliCtx = cliCtx.WithHeight(height)
		rest.PostProcessResponse(w, cliCtx, res)
	}
}

func queryAggregatePrevotesHandlerFunction(cliCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cliCtx, ok := rest.ParseQueryHeightOrReturnBadRequest(w, cliCtx, r)
		if !ok {
			return
		}

		res, height, err := cliCtx.QueryWithData(fmt.Sprintf("custom/%s/%s", types.QuerierRoute, types.QueryAggregatePrevotes), nil)
		if rest.CheckInternalServerError(w, err) {
			return
		}

		cliCtx = cliCtx.WithHeight(height)
		rest.PostProcessResponse(w, cliCtx, res)
	}
}

func queryAggregateVotesHandlerFunction(cliCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cliCtx, ok := rest.ParseQueryHeightOrReturnBadRequest(w, cliCtx, r)
		if !ok {
			return
		}

		res, height, err := cliCtx.QueryWithData(fmt.Sprintf("custom/%s/%s", types.QuerierRoute, types.QueryAggregateVotes), nil)
		if rest.CheckInternalServerError(w, err) {
			return
		}

		cliCtx = cliCtx.WithHeight(height)
		rest.PostProcessResponse(w, cliCtx, res)
	}
}

func queryVoteTargetsHandlerFunction(cliCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cliCtx, ok := rest.ParseQueryHeightOrReturnBadRequest(w, cliCtx, r)
		if !ok {
			return
		}

		res, height, err := cliCtx.QueryWithData(fmt.Sprintf("custom/%s/%s", types.QuerierRoute, types.QueryVoteTargets), nil)
		if rest.CheckInternalServerError(w, err) {
			return
		}

		cliCtx = cliCtx.WithHeight(height)
		rest.PostProcessResponse(w, cliCtx, res)
	}
}

func queryTobinTaxesHandlerFunction(cliCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cliCtx, ok := rest.ParseQueryHeightOrReturnBadRequest(w, cliCtx, r)
		if !ok {
			return
		}

		res, height, err := cliCtx.QueryWithData(fmt.Sprintf("custom/%s/%s", types.QuerierRoute, types.QueryTobinTaxes), nil)
		if rest.CheckInternalServerError(w, err) {
			return
		}

		cliCtx = cliCtx.WithHeight(height)
		rest.PostProcessResponse(w, cliCtx, res)
	}
}

func queryTobinTaxHandlerFunction(cliCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cliCtx, ok := rest.ParseQueryHeightOrReturnBadRequest(w, cliCtx, r)
		if !ok {
			return
		}

		denom, ok := checkDenomVar(w, r)
		if !ok {
			return
		}

		params := types.NewQueryTobinTaxParams(denom)
		bz, err := cliCtx.LegacyAmino.MarshalJSON(params)
		if rest.CheckBadRequestError(w, err) {
			return
		}
		res, height, err := cliCtx.QueryWithData(fmt.Sprintf("custom/%s/%s", types.QuerierRoute, types.QueryTobinTax), bz)
		if rest.CheckInternalServerError(w, err) {
			return
		}

		cliCtx = cliCtx.WithHeight(height)
		rest.PostProcessResponse(w, cliCtx, res)
	}
}

func checkDenomVar(w http.ResponseWriter, r *http.Request) (string, bool) {
	denom := mux.Vars(r)[RestDenom]
	err := sdk.ValidateDenom(denom)
	if rest.CheckBadRequestError(w, err) {
		return "", false
	}

	return denom, true
}

func checkVoterAddressVar(w http.ResponseWriter, r *http.Request) (sdk.ValAddress, bool) {
	addr, err := sdk.ValAddressFromBech32(mux.Vars(r)[RestVoter])
	if rest.CheckBadRequestError(w, err) {
		return nil, false
	}

	return addr, true
}
