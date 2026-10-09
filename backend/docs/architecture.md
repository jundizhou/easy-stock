# easy-stock 当前架构

本说明依据 2026-10-04 开源版 `oss-main` 的代码核对，描述已实现的运行结构。README 架构图的可编辑源文件为 [easy-stock-ai-architecture.svg](../../docs/assets/easy-stock-ai-architecture.svg)，配色与排版参考个股 AI 分析页面。

## 运行结构

Electron 启动本机 Go 服务，装配 Hermes、Codex、Python 与前端静态资源，同时提供内容采集、持久登录态和更新能力。React 工作台通过 Preload 获取后端地址与启动 Token，再使用 HTTP 查询数据、提交任务和轮询进度，通过 WebSocket 接收行情与 AI 对话事件。

Go 领域服务有两个并列依赖：数据与本地证据，以及统一 Agent 服务。行情查询、题材计算、情绪计算和量化速览直接使用数据与规则；需要 AI 的工作流经 `agent.Service` 调用所选运行时。模型请求由 Hermes 或原生 Codex 发往配置的服务商，Go 不另设模型代理网关。

```text
Electron desktop host
  ├─ React workbench ── HTTP / WebSocket ── Go httpapi
  │                                         ├─ Domain services
  │                                         │    ├─ Providers / foundation / SQLite
  │                                         │    └─ agent.Service (when AI is needed)
  │                                         └─ Interactive AI WebSocket relay
  ├─ Hermes / Python ◄── agent.Service ──► Codex App Server
  │       └─ selected model endpoint          └─ Responses endpoint
  ├─ Xueqiu / TaoGuba Browser Bridges ──► review ingestion
  ├─ WeChat FastAPI link parser ─────────► review importer
  └─ Persistent sessions / logs / updater / backups
```

Hermes 与 Codex 是可选运行时。应用共享模型连接、Skills 与 MCP 设置，原生会话和历史按运行时保存；一个业务任务开始后绑定其运行时、模型与配置快照。

## 模块与代码入口

| 模块 | 已实现职责 | 主要代码 |
| --- | --- | --- |
| 桌面宿主 | 服务启动、资源路径、Preload / IPC、子进程关闭 | [main.cjs](../../desktop/main.cjs)、[backend-process.cjs](../../desktop/backend-process.cjs)、[preload.cjs](../../desktop/preload.cjs) |
| 工作台与客户端 | 工作区切换、HTTP 请求、行情流、AI JSON-RPC 客户端、研究任务轮询 | [App.tsx](../../frontend/src/App.tsx)、[backend.ts](../../frontend/src/lib/backend.ts)、[agent.ts](../../frontend/src/lib/agent.ts)、[use-stock-research.ts](../../frontend/src/lib/use-stock-research.ts) |
| 后端装配与 API | Provider 注入、领域服务初始化、路由、鉴权、调度与生命周期 | [main.go](../cmd/server/main.go)、[server.go](../internal/httpapi/server.go)、[config.go](../internal/httpapi/config.go) |
| 数据模型与适配 | 标的规范化、行情 / K 线 / 资讯 / 财务 / 研报模型、来源元数据、多源回退 | [foundation/](../internal/foundation/)、[providers/](../internal/providers/) |
| 市场结构 | 行业与题材融合、成分映射、相对强度、涨停梯队、晋级和情绪历史 | [sector/](../internal/sector/)、[narrative/](../internal/narrative/)、[limit_up_ladder.go](../internal/httpapi/limit_up_ladder.go)、[market_emotion.go](../internal/httpapi/market_emotion.go) |
| 拐点评估 | 可解释锚点、环境与切换信号评估；另含离线回测和 ClickHouse 读取辅助代码 | [strategy/inflection/](../internal/strategy/inflection/) |
| 个股研究 | 量化基线、分级 AI 研究、证据压缩、补证、引用与价格锚点校验、检查点和条件核验 | [stockanalysis/](../internal/stockanalysis/)、[stock_research.go](../internal/httpapi/stock_research.go) |
| 持仓巡检 | 个股报告复用 / 共享任务 / 缺失补齐、组合事实计算、AI 评分、引用校验、恢复与明日预期 | [portfolioinspection/](../internal/portfolioinspection/)、[portfolio_research.go](../internal/httpapi/portfolio_research.go) |
| 复盘 | 订阅采集、链接导入、公共复盘同步、作者观点归纳、共识与明日预期、次日验证 | [review/](../internal/review/)、[review_market_data.go](../internal/httpapi/review_market_data.go)、[review_validation.go](../internal/httpapi/review_validation.go) |
| 游资心法 | 内置原文、远端资料缓存、上下文检索、Skill references 与 Hermes 记忆索引 | [methodology/](../internal/methodology/) |
| Agent 服务 | 双运行时选择、配置事务、任务绑定、Prompt 与进度、Skills / MCP、思考能力、用量 | [service.go](../internal/agent/service.go)、[runtime.go](../internal/agent/runtime.go)、[codex.go](../internal/agent/codex.go) |
| 设置与运行保障 | 模型配置、受控密钥读取、Token 统计、脱敏日志、更新备份与发行装配 | [appsettings/](../internal/appsettings/)、[token_usage.go](../internal/httpapi/token_usage.go)、[runtimelog/](../internal/runtimelog/)、[update-manager.cjs](../../desktop/update-manager.cjs)、[data-protection.cjs](../../desktop/data-protection.cjs)、[prepare-package.mjs](../../desktop/scripts/prepare-package.mjs) |

## 个股研究与持仓复用

`collectStockResearch` 并行采集行情、日 K、基准指数、涨停与题材、主营与财务、公告、研报和新闻，再补充同业对照。`stockanalysis.Analyze` 计算规则基线，`BuildResearchSnapshot` 保存带时点、来源和版本的研究证据。

| 研究级别 | 处理方式 |
| --- | --- |
| 量化速览 | 返回规则与量价快照，不调用模型 |
| 快速研判 | 基于轻量证据形成初步判断，不补证、不生成交易计划 |
| 标准研判 | 分别生成核心判断与交易条件，再校验结构与引用 |
| 深度研究 | 生成问题与替代解释，由 Go 只读补证，再生成核心判断、交易条件并校验 |

深度研究的模型调用关闭工具。模型提出的问题由 `supplementStockResearch` 映射到允许的新闻、公告、研报、已有来源正文或本地方法论查询。最终报告通过来源编号、引文、条件与价格锚点校验后才标记为 AI 研究完成；失败时保留量化快照与已经完成的阶段。

`ResearchService` 管理后台任务、并发限流、取消和恢复；`ResearchStore` 保存任务、版本化证据、阶段检查点与后续条件核验。恢复时检查模型 / 思考配置、请求、提示词版本与证据摘要是否一致，已完成的模型阶段可以复用。报告绑定对话读取保存时点的快照，后续核验另存记录。

持仓巡检通过 `ResolveForPortfolio` 优先复用 24 小时内成功的个股报告，或等待同一股票已运行的研究任务，缺失时才创建研究。组合刷新报价并计算仓位、现金、集中度等事实后，AI 按四个维度给出解释与评分，后端校验事实和报告来源引用并计算加权总分。组合等待结束不取消独立的共享个股研究任务。持仓明日预期另外结合大 V 综合复盘与个股研究生成情景报告。

关键入口：[stock_analysis.go](../internal/httpapi/stock_analysis.go)、[research.go](../internal/stockanalysis/research.go)、[research_levels.go](../internal/stockanalysis/research_levels.go)、[research_checkpoint.go](../internal/stockanalysis/research_checkpoint.go)、[research_reuse.go](../internal/stockanalysis/research_reuse.go)、[scoring.go](../internal/portfolioinspection/scoring.go)。

## Agent 运行时与协议

| 能力 | Hermes | Codex |
| --- | --- | --- |
| 运行方式 | 包内 Python / Hermes Gateway | 包内原生 Codex App Server，通过 stdio JSON-RPC 适配应用协议 |
| 模型连接 | Chat Completions、Responses、Anthropic Messages | Responses；不支持的连接会明确拒绝选择 |
| 配置 | 应用管理的 `config.yaml`、`.env`、Skills 与 MCP | 由共享设置生成 `config.toml` 并投影 Skills / MCP |
| 交互会话 | 原生会话创建与续接 | 原生 thread / turn 映射为应用 session / prompt 协议，独立历史 |
| 后台研究 | 隔离任务、限定工具或关闭工具、进度和超时控制 | 隔离任务、关闭 shell 或全部工具、限定 MCP 工具、进度和超时控制 |

模型密钥集中保存在应用管理的 Hermes `.env`，通用设置与 Codex 配置投影不保存模型密钥；Codex 模型进程通过子进程环境获取密钥。模型配置、思考策略与 Skills 在任务开始时绑定，重试和嵌套调用沿用该配置。`aiChatWebSocket` 在浏览器 WebSocket 与运行时 stdio 之间转发应用 JSON-RPC，前端处理正文、思考、工具进度、用量、授权和澄清事件。

交互对话和无人值守任务的工具策略分别处理。个股分级研究关闭模型工具；其他后台任务使用隔离工作区和允许的工具集。Codex 的 `mcp_launcher.py` 负责 MCP 传输与受限工具适配。

## 内容采集与调度

雪球、淘股吧订阅优先通过桌面 Browser Bridge 读取已登录的 Electron 页面，Go 整理元数据、去重并归档原文。没有桌面桥接时，通过所选 Agent 与 `agent-browser` 复用导出的登录态进行采集。登录态按来源与配置隔离。

微信链接由本机 Python / FastAPI 解析服务或配置的兼容服务导入。当前内置服务支持已知文章链接解析；代码明确处理公众号历史文章列表不可用的状态。

公共每日复盘由 `RemoteDailySync` 拉取清单与文章，验证结构和正文摘要后写入本地复盘库。游资心法优先使用包内资料，定时刷新远端缓存，并生成 Skills references 与 Hermes `MEMORY.md` 的资料索引。

后端入口启动四类调度：订阅复盘同步、公共每日复盘同步、市场情绪同步与心法缓存刷新。个股研究、持仓巡检、综合复盘及次日验证通过 API 提交后台任务，前端轮询进度；行情和 AI 交互使用 WebSocket。

关键入口：[automation.go](../internal/review/automation.go)、[remote_daily.go](../internal/review/remote_daily.go)、[daily_summary.go](../internal/review/daily_summary.go)、[daily_validation.go](../internal/review/daily_validation.go)、[xueqiu-browser-bridge.cjs](../../desktop/xueqiu-browser-bridge.cjs)、[taoguba-browser-bridge.cjs](../../desktop/taoguba-browser-bridge.cjs)、[wechat-service.cjs](../../desktop/wechat-service.cjs)。

## 本地持久化与运行保障

| 存储 | 内容 |
| --- | --- |
| `reviews.db` | 作者、订阅、文章、分析、综合复盘、次日验证与任务记录 |
| `stock-research.db` | 个股研究任务、证据快照版本、阶段检查点、报告复用索引与条件核验 |
| `portfolio-inspections.db` | 持仓巡检、组合报告与明日预期任务 |
| `market-emotion.db` | 情绪历史和同步状态 |
| `theme-radar.db` | 开盘啦题材 / 涨停快照与渐进加载缓存 |
| `settings.json` 与用量记录 | 应用设置、模型连接配置、复盘配置与 Token 统计 |
| Hermes / Codex Home | 应用管理的 Agent 配置、Skills、运行时会话与历史；Hermes 资料记忆索引 |
| 心法目录 / 浏览器会话 / 前端 localStorage | 原文缓存、登录态、对话记录、持仓草稿与界面偏好 |

Provider 结果通过 `foundation.SourceMeta` 保留来源、抓取时间、延迟、过期、交易日、快照与回退原因。题材、梯队和情绪支持先显示缓存或阶段结果，再渐进更新。

Electron 在本机可用端口启动 Go 服务，每次启动生成 Token，并通过 Preload 提供客户端配置。桌面、前端与后端日志按组件保存并脱敏、轮转。更新安装前停止本机服务、持久化浏览器会话并备份用户目录。macOS 当前使用手动下载安装流程，Windows 支持应用内更新安装。

发行资源由 `desktop/scripts` 装配，包含前端、Go 二进制、Hermes / Python、原生 Codex、agent-browser 与微信解析服务；平台运行时按目标系统与架构准备和验证。
