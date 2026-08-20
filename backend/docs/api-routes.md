# Backend API Routes

## Auth

`GET /api/health` 始终公开。其他路由在服务设置 `A_STOCK_TOKEN` 时需要带 token：

```text
Authorization: Bearer <token>
```

WebSocket 可以通过 query 传 token：

```text
/api/v1/ws/stream?symbols=000001.SZ&token=<token>
```

未设置 `A_STOCK_TOKEN` 时所有本机开发路由保持开放。

## Routes

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/api/health` | Backend health check. |
| `GET` | `/api/v1/sources` | Data source catalog and coarse status. |
| `GET` | `/api/v1/quotes/realtime?symbols=000001.SZ,600000.SH` | Realtime quotes. Current implementation uses Sina. |
| `GET` | `/api/v1/quotes/kline?symbol=000001.SZ&period=day&limit=120` | K-line data. Current implementation tries EastMoney then falls back to Sina. |
| `GET` | `/api/v1/quotes/kline/batch?symbols=000001.SZ,600000.SH&period=day&limit=40` | Batch K-line histories for up to 30 symbols, with per-symbol fallback and error reporting. |
| `GET` | `/api/v1/market/margin-balance?limit=120` | Aggregated Shanghai, Shenzhen, and Beijing margin-financing and securities-lending balances by trading day. |
| `GET` | `/api/v1/market/news?source=cls&limit=20` | Market news. Current implementation supports `cls`. |
| `GET` | `/api/v1/stocks/directory` | Cached A-share stock names and codes for local fuzzy search. |
| `GET` | `/api/v1/stocks/hot-ranks` | Deduplicated union of the Tonghuashun and EastMoney A-share hot-stock Top 100 lists, including each source rank. |
| `GET` | `/api/v1/themes/overview` | One-snapshot overview of all configured themes, including average change, breadth, fund flow, and strongest node. |
| `GET` | `/api/v1/sector-map?theme=semiconductor_materials` | Industry chain map. Current implementation uses a local theme rule layer, EastMoney board quotes, and EastMoney board constituents. |
| `POST` | `/api/v1/strategy/inflections/evaluate` | Evaluate one market snapshot for old anchors, new carriers, and big/small inflection signals. |
| `GET` | `/api/v1/ws/stream?symbols=000001.SZ,600000.SH&interval_ms=3000` | WebSocket stream for quote snapshots. |

## Response Shape

Most successful data routes return:

```json
{
  "data": [
    {
      "symbol": "000001.SZ",
      "price": 11.06,
      "meta": {
        "source": "sina",
        "source_url": "https://hq.sinajs.cn/...",
        "fetched_at": "2026-06-15T15:00:01+08:00",
        "latency_ms": 188,
        "stale": false
      }
    }
  ]
}
```

Errors return:

```json
{
  "error": "symbols is required"
}
```

Sector map responses return one theme map:

```json
{
  "data": {
    "theme": "semiconductor_materials",
    "name": "半导体材料",
    "tabs": ["半导体", "半导体材料", "人形机器人"],
    "theme_tabs": [
      {"id": "semiconductor", "name": "半导体"},
      {"id": "semiconductor_materials", "name": "半导体材料"},
      {"id": "battery", "name": "电池"}
    ],
    "groups": [
      {
        "id": "materials_core",
        "name": "半导体材料",
        "nodes": [
          {
            "id": "photoresist",
            "name": "光刻胶",
            "board_code": "BK0891",
            "board_name": "光刻胶",
            "board_source": "eastmoney",
            "change_percent": -1.2,
            "main_net_inflow": -2000000,
            "stocks": [],
            "stock_source": "eastmoney:board-constituents",
            "match_status": "matched",
            "matched_by": ["keyword:光刻胶"],
            "warnings": []
          }
        ]
      }
    ],
    "meta": {
      "source": "sector-map:eastmoney",
      "fetched_at": "2026-06-23T15:00:01+08:00",
      "latency_ms": 188,
      "stale": false
    }
  }
}
```

WebSocket quote messages return:

```json
{
  "type": "quotes",
  "quotes": [
    {
      "symbol": "000001.SZ",
      "price": 11.06,
      "meta": {
        "source": "sina",
        "fetched_at": "2026-06-15T15:00:01+08:00",
        "latency_ms": 188,
        "stale": false
      }
    }
  ],
  "fetched_at": "2026-06-15T15:00:01+08:00"
}
```

## Inflection Evaluation

`POST /api/v1/strategy/inflections/evaluate` accepts a normalized market
snapshot and candidate anchors. Scores are explainable and return their factor
breakdown, selected anchors, ambiguity warnings, and whether a signal is only a
candidate or is confirmed by the current V1 rules.

The strategy semantics, field definitions, and example payload are documented
in [`inflection-engine.md`](./inflection-engine.md).

## Symbols

The MVP normalizes common A-share inputs:

| Input | Canonical | Sina | EastMoney `secid` |
| --- | --- | --- | --- |
| `000001.SZ` | `000001.SZ` | `sz000001` | `0.000001` |
| `sz000001` | `000001.SZ` | `sz000001` | `0.000001` |
| `600000` | `600000.SH` | `sh600000` | `1.600000` |
| `830799` | `830799.BJ` | `bj830799` | `0.830799` |
