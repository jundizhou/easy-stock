# Sector Map

`/api/v1/sector-map` turns public market data into a screenshot-style industry chain map.

## How Fine-Grained Nodes Are Obtained

The细分节点 are not copied from one upstream tree. They are maintained as a local theme rule layer:

- `backend/internal/sector/theme.go` defines themes, tabs, groups, nodes, and matching rules.
- `backend/internal/sector/mapper.go` fetches EastMoney board data and matches nodes to real boards.
- `backend/internal/providers/eastmoney/board.go` hydrates matched boards with board行情, 主力净流入, and成分股.

This means “光刻胶”, “存储/HBM”, “先进封装材料”, “电子特气” and similar nodes can stay close to the research taxonomy, while the prices and stocks remain real-time.

## Theme Catalog

The implemented theme catalog covers the top tabs used by the frontend:

| Theme ID | Name | API |
| --- | --- | --- |
| `semiconductor` | `半导体` | `/api/v1/sector-map?theme=semiconductor` |
| `semiconductor_materials` | `半导体材料` | `/api/v1/sector-map?theme=semiconductor_materials` |
| `humanoid_robot` | `人形机器人` | `/api/v1/sector-map?theme=humanoid_robot` |
| `physical_ai` | `物理AI` | `/api/v1/sector-map?theme=physical_ai` |
| `aerospace` | `航天` | `/api/v1/sector-map?theme=aerospace` |
| `healthcare` | `医疗/医药` | `/api/v1/sector-map?theme=healthcare` |
| `electric_power` | `电力` | `/api/v1/sector-map?theme=electric_power` |
| `grid_equipment` | `电网设备` | `/api/v1/sector-map?theme=grid_equipment` |
| `battery` | `电池` | `/api/v1/sector-map?theme=battery` |
| `nonferrous_metals` | `有色金属` | `/api/v1/sector-map?theme=nonferrous_metals` |
| `chemical` | `化工` | `/api/v1/sector-map?theme=chemical` |
| `consumer_electronics` | `消费电子` | `/api/v1/sector-map?theme=consumer_electronics` |
| `tourism` | `旅游` | `/api/v1/sector-map?theme=tourism` |
| `film_media` | `影视` | `/api/v1/sector-map?theme=film_media` |
| `consumption` | `消费` | `/api/v1/sector-map?theme=consumption` |
| `optical_communication` | `光通信` | `/api/v1/sector-map?theme=optical_communication` |

The API returns `theme_tabs` with these IDs and labels so the frontend can switch themes without hard-coding route parameters.

The `semiconductor_materials` groups are:

| Group ID | Name |
| --- | --- |
| `demand_entry` | `需求入口` |
| `materials_core` | `半导体材料` |
| `manufacturing_packaging` | `制造与封测材料` |
| `equipment_related` | `设备相关材料` |

## Adding A New Node

Add a node in `theme.go`:

```go
{ID: "photoresist", Name: "光刻胶", BoardKeywords: []string{"光刻胶", "光刻机"}}
```

Matching priority:

1. Exact `BoardCode`, when configured.
2. Exact EastMoney board name keyword.
3. Substring board name keyword.

After matching, the mapper calls `BoardStocks` for the selected board code and returns top constituents in the node.

## Node Data Contract

Each node exposes both market values and provenance fields:

| Field | Meaning |
| --- | --- |
| `match_status` | `matched` when the node matched an EastMoney board; `unmatched` otherwise. |
| `board_source` | Source that supplied the board quote or fund-flow data, for example `eastmoney` or `eastmoney:bkzj`. |
| `stock_source` | `eastmoney:board-constituents` for board constituents, or `sina:representative` when the node used representative stock symbols from local rules. |
| `matched_by` | Matching rule that selected the board, such as `keyword:光刻胶` or `code:BK0891`. |
| `warnings` | Non-fatal fallback notes. A node can still be useful when this field is present. |

The frontend uses these fields to filter matched nodes, show fallback badges, and surface warnings without failing the whole map.

## Testing

Normal tests do not hit the public internet:

```bash
go test ./internal/sector -count=1
go test ./internal/providers/eastmoney -run 'TestClientBoards|TestClientBoardStocks' -count=1
```

Live validation can be added behind `A_STOCK_LIVE_TEST=1` for EastMoney board availability because public endpoints can occasionally return empty or EOF under load.
