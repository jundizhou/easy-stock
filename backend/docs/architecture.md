# easy-stock Data Foundation Architecture

## Goal

`easy-stock` 的第一阶段是股票数据基座，而不是直接做聊天界面。后端统一封装外部行情、K 线、新闻和后续 F10/研报/公告/资金流数据源，对上层 Agent 和前端暴露稳定 API。

这对应 Hermes-SRE 的“数据源 + 证据 + 会话”模式：

- Hermes-SRE 查询 APO/Datadog 证据。
- easy-stock 查询东财/新浪/腾讯/Tushare/财联社等股票证据。
- Agent 只能通过数据基座拿证据，不能绕过统一 API 随意抓取。

## Current Modules

```text
desktop/
  main.cjs                     Electron main process, starts backend child process
  preload.cjs                  exposes backendUrl/token to renderer
  backend-process.cjs          local port allocation and backend process launcher
  scripts/                     Hermes runtime preparation and installer packaging

frontend/
  src/App.tsx                  stock workbench
  src/lib/backend.ts           HTTP and WebSocket client helpers
  src/lib/hermes.ts            Hermes TUI gateway JSON-RPC client

backend/
  cmd/server/                  HTTP server entrypoint
  internal/foundation/         shared data models and symbol normalization
  internal/providers/eastmoney EastMoney K-line provider
  internal/providers/sina      Sina realtime quote provider
  internal/providers/cls       CLS market news provider
  internal/strategy/inflection explainable anchor and inflection evaluation engine
  internal/hermes              Hermes config, process lifecycle, and prompt adapter
  internal/httpapi             HTTP routes, token auth, WebSocket stream
  docs/                        architecture, API, source, and test docs
```

## Desktop Runtime

The desktop client does not ask users to start the backend manually.

```text
Electron main process
  -> allocates 127.0.0.1:<random-port>
  -> generates one-time A_STOCK_TOKEN
  -> starts Go backend child process
  -> points backend at packaged Hermes runtime and userData/hermes-home
  -> waits for /api/health
  -> exposes backendUrl/token through preload
  -> renderer uses HTTP for queries and WebSocket for quotes and Hermes chat
```

The backend only needs to listen on loopback. HTTP remains the interface for request/response data, while WebSocket handles quote snapshots and the streaming Hermes JSON-RPC relay.

## Hermes Runtime

Model vendors are configuration targets behind Hermes, not direct dependencies of the Go HTTP layer. The desktop package includes a relocatable Python/Hermes runtime. Model settings are rendered to Hermes `config.yaml`; the model key is stored only in Hermes `.env`. Chat sessions persist Hermes' stored session ID so follow-up prompts use `session.resume`.

## API Principles

- Every data response carries `meta.source`, `meta.source_url`, `meta.fetched_at`, and `meta.latency_ms`.
- Provider-specific parsing stays below `internal/providers`.
- HTTP handlers only validate request parameters, call providers, and render JSON.
- WebSocket messages use the same foundation models as HTTP responses.
- Token auth is optional for local development and enabled by `A_STOCK_TOKEN` for desktop mode.
- Live tests are opt-in because external sources can fail due to network, rate limit, or anti-bot behavior.

## Next Architecture Steps

1. Add provider capability registry and source health probes.
2. Add Tencent quote/index provider.
3. Add TradingView and Wallstreetcn news providers.
4. Add EastMoney F10/report/announcement providers.
5. Add session stream and stock evidence tree events.
6. Add `stock-research-skill/` for research process, evidence rules, and case learning.
