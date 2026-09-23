# mevreplay

Replay harness for the execution-cost metric of spec §15.5 (see `docs/benchmarks/MEV-15.5-2026-09-22.md`).

```
GOTOOLCHAIN=go1.24.7 go run . -data data/luncusdt-1m.json -oracle-lag 1
```

`data/luncusdt-1m.json` holds 1,000 real 1-minute LUNC/USDT candles (Binance klines format). Flags: `-band`, `-solver-spread`, `-intent-noise`, `-intents`, `-oracle-lag`, `-amm-spread`, `-seed`.
