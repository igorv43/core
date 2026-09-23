// Command mevreplay measures the execution cost of the call auction against
// an external mid price by replaying market data (spec §15.5, D-09):
//
//	cost(batch) = |p* − mid(t_settlement)| / mid(t_settlement)
//
// and compares its median with half the median effective spread, the
// acceptance criterion of the spec. It uses the real auction engine
// (x/batch/auction) on synthetic order flow around each candle: retail
// intents with ±noise limits, solvers quoting at the external mid with a
// spread, and P_ref lagged by the oracle vote period. Input: Binance-style
// klines JSON ([openTime, open, high, low, close, volume, ...]).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"sort"

	sdkmath "cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/auction"
	"github.com/classic-terra/core/v4/x/batch/types"
)

func main() {
	file := flag.String("data", "data/luncusdt-1m.json", "klines JSON")
	lag := flag.Int("oracle-lag", 1, "candles between the oracle vote and settlement (P_ref staleness)")
	band := flag.Float64("band", 0.02, "oracle band δ")
	spread := flag.Float64("solver-spread", 0.002, "solver quote half-spread around the external mid")
	noise := flag.Float64("intent-noise", 0.005, "retail limit noise around the mid")
	intents := flag.Int("intents", 40, "retail intents per batch")
	seed := flag.Int64("seed", 1, "rng seed")
	ammSpread := flag.Float64("amm-spread", 0.003, "median effective spread of the Terra Classic AMMs for the pair (fee + slippage)")
	flag.Parse()

	raw, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var klines [][]interface{}
	if err := json.Unmarshal(raw, &klines); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mids := make([]float64, 0, len(klines))
	spreads := make([]float64, 0, len(klines))
	for _, k := range klines {
		var o, h, l, c float64
		fmt.Sscan(k[1].(string), &o)
		fmt.Sscan(k[2].(string), &h)
		fmt.Sscan(k[3].(string), &l)
		fmt.Sscan(k[4].(string), &c)
		mid := (o + c) / 2
		mids = append(mids, mid)
		// effective spread proxy of the candle: its range relative to the mid
		spreads = append(spreads, (h-l)/mid)
	}
	rng := rand.New(rand.NewSource(*seed))
	var costs []float64
	executed := 0
	for i := *lag; i < len(mids); i++ {
		pref := mids[i-*lag] // the last oracle vote
		external := mids[i]  // the external mid at settlement
		orders := make([]auction.Order, 0, *intents+2)
		for j := 0; j < *intents; j++ {
			side := types.SIDE_BUY
			if j%2 == 1 {
				side = types.SIDE_SELL
			}
			limit := external * (1 + (rng.Float64()*2-1)*(*noise))
			o := auction.Order{Key: fmt.Sprintf("i:%d", j), Kind: auction.KindIntent, Side: side, Limit: dec(limit), Qty: sdkmath.NewInt(1_000_000)}
			if side == types.SIDE_BUY {
				o.Quote = sdkmath.NewInt(int64(limit * 1_000_000))
			}
			orders = append(orders, o)
		}
		// solvers quote the external mid: bid below, ask above
		orders = append(orders,
			auction.Order{Key: "s:bid", Kind: auction.KindSolver, Side: types.SIDE_BUY, Limit: dec(external * (1 - *spread)), Qty: sdkmath.NewInt(100_000_000)},
			auction.Order{Key: "s:ask", Kind: auction.KindSolver, Side: types.SIDE_SELL, Limit: dec(external * (1 + *spread)), Qty: sdkmath.NewInt(100_000_000)},
		)
		res := auction.Resolve(orders, dec(pref), dec(*band), 3)
		if !res.Executed {
			continue
		}
		executed++
		pstar := res.Price.MustFloat64()
		c := pstar - external
		if c < 0 {
			c = -c
		}
		costs = append(costs, c/external)
	}
	sort.Float64s(costs)
	sort.Float64s(spreads)
	med := func(x []float64) float64 {
		if len(x) == 0 {
			return 0
		}
		return x[len(x)/2]
	}
	fmt.Printf("candles=%d batches_executed=%d\n", len(mids), executed)
	fmt.Printf("median execution cost      = %.5f%%\n", med(costs)*100)
	fmt.Printf("p90 execution cost         = %.5f%%\n", pct(costs, 0.9)*100)
	fmt.Printf("median candle range (spread proxy) = %.5f%%\n", med(spreads)*100)
	fmt.Printf("criterion vs candle range/2: %.5f%% ≤ %.5f%% → %v\n", med(costs)*100, med(spreads)/2*100, med(costs) <= med(spreads)/2)
	fmt.Printf("criterion vs AMM spread/2 : %.5f%% ≤ %.5f%% → %v\n", med(costs)*100, *ammSpread/2*100, med(costs) <= *ammSpread/2)
}

func pct(x []float64, p float64) float64 {
	if len(x) == 0 {
		return 0
	}
	i := int(float64(len(x)-1) * p)
	return x[i]
}

func dec(f float64) sdkmath.LegacyDec {
	d, err := sdkmath.LegacyNewDecFromStr(fmt.Sprintf("%.12f", f))
	if err != nil {
		panic(err)
	}
	return d
}
