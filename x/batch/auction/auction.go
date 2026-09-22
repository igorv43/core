// Package auction implements the uniform-price call auction of the Liquidity
// Fabric (spec §15.3) as pure, deterministic functions over plain orders. It
// has no store access so it can be tested exhaustively and reused by the
// perpetuals engine.
package auction

import (
	"sort"

	"cosmossdk.io/math"
	"github.com/classic-terra/core/v4/x/batch/types"
)

// Kind distinguishes user intents from solver levels (intents have priority
// at the marginal price, spec §15.3 fill rule 2).
type Kind int

const (
	KindIntent Kind = iota
	KindSolver
)

// Order is one side of the book at a limit price.
type Order struct {
	Key   string
	Kind  Kind
	Side  types.Side
	Limit math.LegacyDec
	// Qty is the base quantity for SELL orders and solver BUY levels.
	Qty math.Int
	// Quote is the quote budget of a BUY intent: its quantity at price p is
	// floor(Quote / p). Zero for every other order.
	Quote math.Int
	// SolverIndex orders solver levels at the marginal price (commit tx index).
	SolverIndex uint64
	// MinOut and AmountIn (intents only) give the proportional floor of
	// spec §14.2: a fill is valid when out·AmountIn ≥ MinOut·in.
	MinOut   math.Int
	AmountIn math.Int
}

// QtyAt returns the base quantity the order offers or demands at price p.
func (o Order) QtyAt(p math.LegacyDec) math.Int {
	if o.Side == types.SIDE_BUY && !o.Quote.IsNil() && o.Quote.IsPositive() {
		return math.LegacyNewDecFromInt(o.Quote).Quo(p).TruncateInt()
	}
	return o.Qty
}

// Fill is the cleared quantity of one order. In is what the order gives, Out
// what it receives, both before protocol and builder fees.
type Fill struct {
	Key   string
	Kind  Kind
	Side  types.Side
	Qty   math.Int
	Price math.LegacyDec
	In    math.Int
	Out   math.Int
}

// Result is the outcome of a resolution.
type Result struct {
	Executed bool
	Price    math.LegacyDec
	Volume   math.Int
	Fills    []Fill
	Passes   uint32
	// Excluded lists intents dropped for violating min_out (kept for the next batch).
	Excluded []string
	Reason   string
}

// Resolve runs the call auction: candidate prices inside the oracle band,
// maximal executable volume with the spec tie-breaks, allocation at the
// clearing price, min_out enforcement with re-resolution.
func Resolve(orders []Order, pref, band math.LegacyDec, maxPasses uint32) Result {
	if !pref.IsPositive() {
		return Result{Reason: "reference price unavailable"}
	}
	if maxPasses == 0 {
		maxPasses = 1
	}
	lower := pref.Mul(math.LegacyOneDec().Sub(band))
	upper := pref.Mul(math.LegacyOneDec().Add(band))

	active := make([]Order, len(orders))
	copy(active, orders)
	sort.SliceStable(active, func(i, j int) bool { return active[i].Key < active[j].Key })

	var excluded []string
	for pass := uint32(1); pass <= maxPasses; pass++ {
		price, volume, ok := clear(active, lower, upper, pref)
		if !ok {
			return Result{Passes: pass, Excluded: excluded, Reason: "no executable volume"}
		}
		fills := allocate(active, price, volume)

		var violators []string
		for _, f := range fills {
			if f.Kind != KindIntent {
				continue
			}
			o := find(active, f.Key)
			if o.MinOut.IsNil() || o.AmountIn.IsNil() || !o.AmountIn.IsPositive() {
				continue
			}
			// out·AmountIn ≥ MinOut·in
			if f.Out.Mul(o.AmountIn).LT(o.MinOut.Mul(f.In)) {
				violators = append(violators, f.Key)
			}
		}
		if len(violators) == 0 {
			total := math.ZeroInt()
			for _, f := range fills {
				if f.Side == types.SIDE_BUY {
					total = total.Add(f.Qty)
				}
			}
			return Result{Executed: true, Price: price, Volume: total, Fills: fills, Passes: pass, Excluded: excluded}
		}
		excluded = append(excluded, violators...)
		active = remove(active, violators)
	}
	return Result{Passes: maxPasses, Excluded: excluded, Reason: "min_out violations persisted"}
}

// clear picks the clearing price p* (spec §15.3 algorithm).
func clear(orders []Order, lower, upper, pref math.LegacyDec) (math.LegacyDec, math.Int, bool) {
	candidates := []math.LegacyDec{lower, upper}
	for _, o := range orders {
		if o.Limit.GTE(lower) && o.Limit.LTE(upper) {
			candidates = append(candidates, o.Limit)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].LT(candidates[j]) })
	// dedupe
	uniq := candidates[:0]
	for i, c := range candidates {
		if i == 0 || !c.Equal(candidates[i-1]) {
			uniq = append(uniq, c)
		}
	}

	var (
		best     math.LegacyDec
		bestVol  = math.ZeroInt()
		bestImb  math.Int
		bestDist math.LegacyDec
		found    bool
	)
	for _, p := range uniq {
		d, s := demandSupply(orders, p)
		v := math.MinInt(d, s)
		if !v.IsPositive() {
			continue
		}
		imb := d.Sub(s).Abs()
		dist := p.Sub(pref).Abs()
		better := !found ||
			v.GT(bestVol) ||
			(v.Equal(bestVol) && imb.LT(bestImb)) ||
			(v.Equal(bestVol) && imb.Equal(bestImb) && dist.LT(bestDist)) ||
			(v.Equal(bestVol) && imb.Equal(bestImb) && dist.Equal(bestDist) && p.LT(best))
		if better {
			best, bestVol, bestImb, bestDist, found = p, v, imb, dist, true
		}
	}
	if !found {
		return math.LegacyZeroDec(), math.ZeroInt(), false
	}
	return best, bestVol, true
}

func demandSupply(orders []Order, p math.LegacyDec) (math.Int, math.Int) {
	d, s := math.ZeroInt(), math.ZeroInt()
	for _, o := range orders {
		switch {
		case o.Side == types.SIDE_BUY && o.Limit.GTE(p):
			d = d.Add(o.QtyAt(p))
		case o.Side == types.SIDE_SELL && o.Limit.LTE(p):
			s = s.Add(o.Qty)
		}
	}
	return d, s
}

// allocate fills orders at price p for volume v (spec §15.3 filling rules):
// strictly better orders in full; at the marginal price the excess side is
// rationed — intents first, pro-rata by quantity (largest remainder), then
// solver levels by commit order. Both sides always sum to the same quantity.
func allocate(orders []Order, p math.LegacyDec, v math.Int) []Fill {
	var buys, sells []Order
	for _, o := range orders {
		if o.Side == types.SIDE_BUY && o.Limit.GTE(p) {
			buys = append(buys, o)
		} else if o.Side == types.SIDE_SELL && o.Limit.LTE(p) {
			sells = append(sells, o)
		}
	}
	d, s := demandSupply(orders, p)

	buyQty := sideAllocation(buys, p, v, d.GT(s))
	sellQty := sideAllocation(sells, p, v, s.GT(d))

	var fills []Fill
	for _, o := range buys {
		q := buyQty[o.Key]
		if q.IsNil() || !q.IsPositive() {
			continue
		}
		in := math.LegacyNewDecFromInt(q).Mul(p).Ceil().TruncateInt()
		if o.Kind == KindIntent && !o.Quote.IsNil() && in.GT(o.Quote) {
			in = o.Quote
		}
		fills = append(fills, Fill{Key: o.Key, Kind: o.Kind, Side: o.Side, Qty: q, Price: p, In: in, Out: q})
	}
	for _, o := range sells {
		q := sellQty[o.Key]
		if q.IsNil() || !q.IsPositive() {
			continue
		}
		out := math.LegacyNewDecFromInt(q).Mul(p).TruncateInt()
		fills = append(fills, Fill{Key: o.Key, Kind: o.Kind, Side: o.Side, Qty: q, Price: p, In: q, Out: out})
	}
	return fills
}

// sideAllocation returns the quantity per order of one side. When the side
// is in excess, marginal orders are rationed to reach exactly v.
func sideAllocation(side []Order, p math.LegacyDec, v math.Int, inExcess bool) map[string]math.Int {
	out := make(map[string]math.Int, len(side))
	if !inExcess {
		for _, o := range side {
			out[o.Key] = o.QtyAt(p)
		}
		return out
	}
	residual := v
	var marginal []Order
	for _, o := range side {
		if !o.Limit.Equal(p) {
			q := o.QtyAt(p)
			out[o.Key] = q
			residual = residual.Sub(q)
		} else {
			marginal = append(marginal, o)
		}
	}
	if residual.IsNegative() {
		residual = math.ZeroInt()
	}
	// intents first, pro-rata with largest-remainder rounding; then solvers by index
	var intents, solvers []Order
	for _, o := range marginal {
		if o.Kind == KindIntent {
			intents = append(intents, o)
		} else {
			solvers = append(solvers, o)
		}
	}
	residual = prorata(intents, p, residual, out)
	sort.SliceStable(solvers, func(i, j int) bool {
		if solvers[i].SolverIndex != solvers[j].SolverIndex {
			return solvers[i].SolverIndex < solvers[j].SolverIndex
		}
		return solvers[i].Key < solvers[j].Key
	})
	for _, o := range solvers {
		if !residual.IsPositive() {
			break
		}
		take := math.MinInt(o.QtyAt(p), residual)
		out[o.Key] = take
		residual = residual.Sub(take)
	}
	return out
}

// prorata rations `residual` among orders proportionally to their quantity,
// rounding down and distributing the leftover units in key order. Returns the
// unallocated remainder (zero when capacity suffices).
func prorata(orders []Order, p math.LegacyDec, residual math.Int, out map[string]math.Int) math.Int {
	if len(orders) == 0 || !residual.IsPositive() {
		return residual
	}
	capacity := math.ZeroInt()
	caps := make([]math.Int, len(orders))
	for i, o := range orders {
		caps[i] = o.QtyAt(p)
		capacity = capacity.Add(caps[i])
	}
	if capacity.LTE(residual) {
		for i, o := range orders {
			out[o.Key] = caps[i]
		}
		return residual.Sub(capacity)
	}
	allocated := math.ZeroInt()
	for i, o := range orders {
		q := residual.Mul(caps[i]).Quo(capacity)
		out[o.Key] = q
		allocated = allocated.Add(q)
	}
	left := residual.Sub(allocated)
	for i := 0; left.IsPositive(); i = (i + 1) % len(orders) {
		o := orders[i]
		if out[o.Key].LT(caps[i]) {
			out[o.Key] = out[o.Key].Add(math.OneInt())
			left = left.Sub(math.OneInt())
		}
	}
	return math.ZeroInt()
}

func find(orders []Order, key string) Order {
	for _, o := range orders {
		if o.Key == key {
			return o
		}
	}
	return Order{}
}

func remove(orders []Order, keys []string) []Order {
	drop := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		drop[k] = struct{}{}
	}
	out := orders[:0:0]
	for _, o := range orders {
		if _, ok := drop[o.Key]; !ok {
			out = append(out, o)
		}
	}
	return out
}
