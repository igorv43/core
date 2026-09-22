package auction

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/types"
	"github.com/stretchr/testify/require"
)

func dec(s string) math.LegacyDec { return math.LegacyMustNewDecFromStr(s) }
func in(v int64) math.Int         { return math.NewInt(v) }

func buyIntent(key string, quote int64, limit string, minOut int64) Order {
	return Order{Key: key, Kind: KindIntent, Side: types.SIDE_BUY, Limit: dec(limit), Quote: in(quote), AmountIn: in(quote), MinOut: in(minOut)}
}

func sellIntent(key string, qty int64, limit string, minOut int64) Order {
	return Order{Key: key, Kind: KindIntent, Side: types.SIDE_SELL, Limit: dec(limit), Qty: in(qty), AmountIn: in(qty), MinOut: in(minOut)}
}

func level(key string, side types.Side, qty int64, price string, idx uint64) Order {
	return Order{Key: key, Kind: KindSolver, Side: side, Limit: dec(price), Qty: in(qty), SolverIndex: idx}
}

func sums(fills []Fill) (buy, sell math.Int) {
	buy, sell = math.ZeroInt(), math.ZeroInt()
	for _, f := range fills {
		if f.Side == types.SIDE_BUY {
			buy = buy.Add(f.Qty)
		} else {
			sell = sell.Add(f.Qty)
		}
	}
	return
}

func TestUsersCrossWithoutSolvers(t *testing.T) {
	// P_ref 1.00, band 2%: a buyer with 1,000 quote at 1.01 and a seller of 800 base at 0.99
	orders := []Order{
		buyIntent("i:1", 1_000, "1.01", 0),
		sellIntent("i:2", 800, "0.99", 0),
	}
	res := Resolve(orders, dec("1.00"), dec("0.02"), 3)
	require.True(t, res.Executed, res.Reason)
	require.Equal(t, "800", res.Volume.String())
	buy, sell := sums(res.Fills)
	require.True(t, buy.Equal(sell))
	// max volume 800 is reachable on [0.99, 1.01]; ties -> smaller imbalance, then closest to P_ref, then lower price
	require.True(t, res.Price.GTE(dec("0.99")) && res.Price.LTE(dec("1.01")), res.Price.String())
}

func TestBandExcludesOutsidePrices(t *testing.T) {
	orders := []Order{
		buyIntent("i:1", 1_000, "1.10", 0), // limit above the band: still participates at band prices
		sellIntent("i:2", 500, "1.05", 0),  // asks above the band: cannot clear
	}
	res := Resolve(orders, dec("1.00"), dec("0.02"), 3)
	require.False(t, res.Executed)
	require.Equal(t, "no executable volume", res.Reason)
}

func TestMarginalPriorityIntentsBeforeSolvers(t *testing.T) {
	// one seller of 100 at 1.00; two buyers at exactly 1.00: an intent wanting 80 and a solver level of 80
	orders := []Order{
		sellIntent("i:1", 100, "1.00", 0),
		buyIntent("i:2", 80, "1.00", 0),
		level("s:a:0", types.SIDE_BUY, 80, "1.00", 1),
	}
	res := Resolve(orders, dec("1.00"), dec("0.02"), 3)
	require.True(t, res.Executed, res.Reason)
	require.Equal(t, "100", res.Volume.String())
	q := map[string]string{}
	for _, f := range res.Fills {
		q[f.Key] = f.Qty.String()
	}
	require.Equal(t, "80", q["i:2"], "the intent is filled first")
	require.Equal(t, "20", q["s:a:0"], "the solver gets the residual")
	require.Equal(t, "100", q["i:1"])
}

func TestProRataAmongMarginalIntentsSumsExactly(t *testing.T) {
	orders := []Order{
		sellIntent("i:1", 100, "1.00", 0),
		buyIntent("i:2", 70, "1.00", 0),
		buyIntent("i:3", 70, "1.00", 0),
		buyIntent("i:4", 70, "1.00", 0),
	}
	res := Resolve(orders, dec("1.00"), dec("0.02"), 3)
	require.True(t, res.Executed)
	buy, sell := sums(res.Fills)
	require.Equal(t, "100", buy.String())
	require.True(t, buy.Equal(sell), "both sides must balance after largest-remainder rounding")
}

func TestMinOutExcludesAndReresolves(t *testing.T) {
	// seller demands at least 1.02 per unit through min_out while the market clears at 1.00
	orders := []Order{
		sellIntent("i:1", 100, "0.98", 102), // min_out 102 for 100 base -> needs price ≥ 1.02
		sellIntent("i:2", 50, "0.98", 0),
		buyIntent("i:3", 200, "1.00", 0),
	}
	res := Resolve(orders, dec("1.00"), dec("0.02"), 3)
	require.True(t, res.Executed, res.Reason)
	require.Equal(t, []string{"i:1"}, res.Excluded)
	require.Equal(t, uint32(2), res.Passes)
	require.Equal(t, "50", res.Volume.String())
}

func TestRoundingFavoursTheBookNotTheParticipant(t *testing.T) {
	// price 0.333...: buyer pays ceil(qty·p), seller receives floor(qty·p)
	orders := []Order{
		buyIntent("i:1", 100, "0.334", 0),
		sellIntent("i:2", 299, "0.332", 0),
	}
	res := Resolve(orders, dec("0.333"), dec("0.01"), 3)
	require.True(t, res.Executed, res.Reason)
	var buyIn, sellOut math.Int
	for _, f := range res.Fills {
		if f.Side == types.SIDE_BUY {
			buyIn = f.In
			require.True(t, f.In.LTE(in(100)), "a buyer never pays more than its escrow")
		} else {
			sellOut = f.Out
		}
	}
	require.True(t, buyIn.GTE(sellOut), "quote paid by buyers covers quote received by sellers; the dust is residual")
}

func TestNoReferencePrice(t *testing.T) {
	res := Resolve([]Order{buyIntent("i:1", 10, "1", 0)}, math.LegacyZeroDec(), dec("0.02"), 3)
	require.False(t, res.Executed)
}

func TestDeterministicOrderIndependence(t *testing.T) {
	a := []Order{sellIntent("i:1", 100, "1.00", 0), buyIntent("i:2", 70, "1.00", 0), buyIntent("i:3", 70, "1.00", 0)}
	b := []Order{a[2], a[0], a[1]}
	ra := Resolve(a, dec("1.00"), dec("0.02"), 3)
	rb := Resolve(b, dec("1.00"), dec("0.02"), 3)
	require.Equal(t, ra.Price.String(), rb.Price.String())
	require.Equal(t, len(ra.Fills), len(rb.Fills))
	for i := range ra.Fills {
		require.Equal(t, ra.Fills[i], rb.Fills[i])
	}
}
