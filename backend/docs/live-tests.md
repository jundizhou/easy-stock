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
