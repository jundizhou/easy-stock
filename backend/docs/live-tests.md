# Live Tests

Normal tests do not hit public data sources:

```bash
cd backend
go test ./...
```

Live tests are opt-in:

```bash
cd backend
A_STOCK_LIVE_TEST=1 go test ./internal/httpapi ./internal/providers -run Live -v
```

Current live checks:

| Test | Expected Real Data |
| --- | --- |
| `TestLiveAPIRealtimeReturnsRealQuotesAcrossMarkets` | Unified HTTP API returns positive realtime prices for `000001.SZ`, `600000.SH`, and `300750.SZ`. |
| `TestLiveAPIKLineReturnsRealBarsWithFallback` | Unified HTTP API returns daily K-line bars and keeps source evidence even when it falls back from EastMoney to Sina. |
| `TestLiveAPINewsReturnsRealItems` | Unified HTTP API returns CLS news items with source evidence. |
| `TestLiveSinaRealtimeReturnsQuote` | `000001.SZ` realtime quote from Sina has a positive price. |
| `TestLiveSinaKLineReturnsBars` | `000001.SZ` daily K-line from Sina returns at least one bar with positive close. |
| `TestLiveEastMoneyKLineReportsHealthSignal` | EastMoney daily K-line either returns usable bars or logs a health signal so the API can fall back. |
| `TestLiveCLSNewsReturnsItems` | CLS latest news returns at least one item. |

## Notes

External financial endpoints can fail because of network restrictions, rate limits, anti-bot changes, or upstream schema changes. A live test failure should be treated as a data-source health signal, not necessarily as a deterministic code regression.

## Ten-holding model compaction

`TestLiveTenHoldingCompaction` runs the production aggregation path (automatic
compaction, scoring, one allowed repair, and validation) with a real configured
model and ten distinct saved research reports. It does not rerun stock research
or refresh quotes. Use a private `Report` JSON fixture containing `request` and
`holdings`; every holding must contain its original successful `analysis` and
matching symbol/weight. Set `evaluated_at` to freeze time-dependent validation
when comparing repeated runs. Do not fabricate holdings by renaming copies of a
single report.

```bash
cd backend
EASY_STOCK_LIVE_TEN_HOLDINGS=1 \
EASY_STOCK_TEN_HOLDINGS_FIXTURE=/private/path/fixture.json \
EASY_STOCK_TEN_HOLDINGS_OUTPUT=/private/path/run-output \
EASY_STOCK_REPLAY_SETTINGS=/private/path/settings.json \
EASY_STOCK_REPLAY_HERMES_HOME=/private/path/hermes-home \
EASY_STOCK_REPLAY_HERMES_ROOT=/path/to/desktop/resources/hermes-runtime \
EASY_STOCK_REPLAY_CODEX_ROOT=/path/to/desktop/resources/codex-runtime \
go test ./internal/portfolioinspection -run '^TestLiveTenHoldingCompaction$' -v -count=1 -timeout=125m
```

Add `EASY_STOCK_TEN_HOLDINGS_PREFLIGHT=1` to measure input size without model
calls or credentials. Runtime configuration is copied to a temporary home;
live settings and stores remain untouched. Private outputs include the
preflight, prompts/responses, validation attempts and compaction checkpoints in
`checkpoints.db`, the final report, token usage, input sizes and elapsed times.
Reusing the output directory allows unchanged valid summaries to be reused;
archive prior call files before rerunning if retaining all call history matters.
Add `EASY_STOCK_TEN_HOLDINGS_RESUME=1` after an incomplete scoring run to load
its saved draft and frozen evaluation time, exercising the production scoring
resume path as well as summary reuse. This remains a new bounded evaluation;
it does not convert a rejected draft into an accepted report locally.
The ordinary test suite skips this test unless explicitly enabled.
