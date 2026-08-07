# Roadmap

## Phase 1: Data Foundation MVP

- Implement stable models for quote, K-line, news, and source metadata.
- Implement Sina realtime quote provider.
- Implement EastMoney K-line provider.
- Implement CLS news provider.
- Expose HTTP routes for health, source catalog, realtime quote, K-line, and news.
- Add mocked unit tests and opt-in live tests.

## Phase 2: Broader Market Data

- Add Tencent quote/index/HK provider.
- Add TradingView news provider.
- Add Wallstreetcn live/market/calendar provider.
- Add EastMoney market money-flow and stock-change providers.

## Phase 3: Stock Research Data

- Add EastMoney F10, finance, reports, and announcements.
- Add Iwencai screening and search.
- Add Xueqiu hot stock and hot event.
- Add fund data providers.

## Phase 4: Stock Research Agent

- Add session API and SSE stream.
- Emit research plan, tool call, evidence tree, and final conclusion events.
- Create `stock-research-skill/` with research workflow and evidence rules.

## Phase 5: Learning Loop

- Add feedback API.
- Summarize good and bad research sessions into cases.
- Maintain `stock-research-skill/cases/` and `index.md`.
