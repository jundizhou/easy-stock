# 开源版个股证据研究

## 使用与边界

个股分析现在以 AI 研究为主：选择观察、新开仓或已有持仓，设置研究周期，持仓成本选填。分析在后端持续运行，切换页面、刷新页面不会取消任务。研究记录保存在本机 SQLite 中。后台重启会将未完成任务标为中断，不假装已经完成。

AI 先提出关键问题和替代解释，在一次受限补证后形成主判断、支持证据、反对证据、条件情景和失效条件。支持暂不形成交易计划；不会强制生成目标价、胜率或账户仓位。已有报告的追问携带报告编号、原始时点及证据，不静默混入新行情。

模型输入使用独立的确定性证据压缩包：完整来源继续保存在不可变快照中，问题识别阶段只接收相关证据卡片，最终综合阶段接收更完整的关键片段和补证增量。压缩不会改写来源编号，引用和引文仍针对完整快照校验。报告记录压缩版本及压缩前后的来源数和正文字节数。

规则评分作为独立量化基线保留，AI 可以反对其解读，但不能修改原评分。来源编号、引文和价格结构校验不等于完整语义认证。新闻、研报和历史经验不能自动升级为公司事实。

## 执行约束

- 正常两次研究阶段请求：问题识别、最终研判。最多再加一次结构修复。连接失败不当作 JSON 格式错误反复修复。Hermes/供应商内部的网络重试不计入此阶段次数。
- 每次模型阶段最多 3 分钟；模型流程总预算 9 分钟；任务含等待总预算 12 分钟。两个工作槽，最多八个排队或执行任务；同请求执行中去重。
- 补证最多三个问题，每项 12 秒，只开放本股公告、研报、已有公告正文和本地方法资料。模型没有任意网络、文件写入或终端工具。
- 问题识别证据包最多 24 条、正文约 24KB；最终综合证据包最多 32 条、正文约 46KB。正式财务、业务、行情和披露优先保留，风险与反证材料保留配额；完整原文不因压缩而删除。
- 截止时点后的披露排除；未知发布时间单独标记。财务字段明确累计口径、营业总收入和归母净利润，不能混作单季或现金流总额。
- 价格方案只能引用程序已有价格锚点；检查先后顺序、时效、样本长度及与现价偏离。超短研究不提供静态价格交易方案。目标缺失不补造 1R/2R 目标。
- AI 不可用、中断或无合格结论时只展示量化快照，不沿用规则交易建议冒充 AI 结果。持仓巡检会保留数据覆盖和 AI 覆盖的区别；未知止损不等于零风险。

## 存储与接口

`A_STOCK_RESEARCH_DB` 可指定数据库，默认应用数据目录下的 `stock-research.db`。报告、快照版本和后续核验分开存储，同版本快照不能覆写；删除记录会删除对应快照和核验记录。未停止任务不能直接删除。

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| POST | `/api/v1/stocks/research` | 提交，返回任务 ID |
| GET | `/api/v1/stocks/research` | 最近 50 条记录 |
| GET | `/api/v1/stocks/research/{id}` | 阶段与报告 |
| GET | `/api/v1/stocks/research/{id}/snapshot` | 原始量化与来源快照 |
| POST | `/api/v1/stocks/research/{id}/cancel` | 取消执行 |
| POST | `/api/v1/stocks/research/{id}/verify` | 核对后续条件 |
| DELETE | `/api/v1/stocks/research/{id}` | 删除已结束记录 |

请求字段为 `symbol`、`purpose`（`observe/new_position/holding`）、`horizon`（`short/swing/medium`）、可选正数 `cost_price`（仅持仓）。原 `/stocks/ai-analysis` 保留兼容；`quick` 返回旧量化分析，`full` 等待同一后台研究任务。

后续核验使用完整日线与指数交易日序列。缺失窗口内个股日线、缺少原始历史重叠或复权/历史修订造成价格变化时，不跳日或强行比较；公告语义、竞价与开盘条件标记为需人工核实。核验条件是否触发不是预测准确率，也不是交易回测。

## 可重复验证

```sh
cd backend
go test ./...
go test -race ./internal/stockanalysis ./internal/httpapi ./internal/portfolioinspection ./internal/hermes
cd ..
npm --workspace frontend run test -- --run
npm --workspace frontend run build
RESEARCH_API_URL=http://127.0.0.1:20082 RESEARCH_OUTPUT_DIR=.runtime/research-evaluation-final node scripts/verify-stock-research.mjs
node scripts/check-stock-research-facts.mjs .runtime/research-evaluation-final
```

真实案例脚本会保存任务和快照，并检查调用预算、来源引用、原评分、时间截断与价格锚点。任何案例没有成功 AI 报告都会以失败退出，不能把降级当通过。固定日期的事实参照位于 `docs/verification/stock-research-2026-09-08.json`，不要拿它校验别的交易日期。

`scripts/verify-stock-research-ui.mjs` 使用明确标为 `UI TEST FIXTURE` 的模拟报告测试历史恢复、引用展开、核验、复制、PNG 导出、报告追问、持仓成本、取消和删除。它不伪装成真实模型准确性测试。需要 Playwright 和 Chrome，可用 `PLAYWRIGHT_MODULE` 指定已安装的 Playwright 模块路径，`RESEARCH_CASE_FILE` 指定上述真实行情快照。

可选 `TestResearchLiveToolFreeJSON` 需要显式设置 `EASY_STOCK_LIVE_HERMES=1`、隔离的 `A_STOCK_SETTINGS_PATH`、`A_STOCK_HERMES_HOME` 和 `A_STOCK_HERMES_RUNTIME_ROOT`；只在独立测试目录运行，不覆盖日常设置。
