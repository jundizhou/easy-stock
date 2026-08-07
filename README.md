<p align="center">
  <img src="./desktop/assets/easy-stock.png" width="128" alt="easy-stock Logo" />
</p>

<h1 align="center">easy-stock</h1>

<p align="center"><strong>AI 时代的 A 股行情分析与智能投研工作台</strong></p>

<p align="center">
  让 AI 看懂中国市场，让每一次判断都有证据，<br />
  把盘中观察、盘后复盘和长期认知沉淀为一套持续进化的研究系统。
</p>

## 核心产品能力

easy-stock 围绕 A 股研究者每天最真实的工作流设计：盘中发现题材和情绪变化，盘后自动收集观点、分析连板结构，再由 AI 汇总证据、提炼共识并形成下一交易日的观察清单。

### 1. 大 V 自动复盘：每天把分散观点收进一个工作台

大 V 的盘后复盘通常分散在雪球、淘股吧和微信公众号中，手工逐个打开主页、筛选当天文章、复制正文并整理观点非常耗时。easy-stock 将这些内容组织为统一的复盘时间流：

- 支持雪球、淘股吧和微信公众号三类内容入口
- 雪球、淘股吧支持大 V 主页订阅与每日定时自动同步
- 支持多套采集配置、多个账号和多个作者，浏览器登录态彼此隔离
- 可以手动点击「立即同步」，也可以交给后台按配置时间每日检查
- 自动识别作者、标题、发布时间和原文链接，并与本地文章去重
- 正文清洗后保存到本机 SQLite，原文、作者和抓取时间均可追溯
- 按平台、作者、日期和关键词检索，形成连续的个人复盘资料库
- 每篇文章可以交给 Hermes AI 提炼摘要、关键点、关联方向和后市预期

微信公众号当前支持具体文章链接导入；公众号主页历史文章自动订阅正在接入中。三类平台最终共享同一套归档、搜索与 AI 分析流程。

<p align="center">
  <img src="./docs/assets/easy-stock-auto-review.png" width="100%" alt="easy-stock 大 V 自动复盘与每日同步工作台" />
</p>

图中将数据源状态、自动订阅、作者列表、文章时间流、正文阅读和 AI 提炼放在同一页面。研究者不需要在多个网站之间反复切换，可以直接围绕当天文章完成阅读、筛选与归档。

### 2. 一键总结今日观点：从文章集合中提炼共识与分歧

文章收集完成后，可以一键生成「今日大 V 观点共识」。这不是简单地把所有文章拼接后做一次摘要，而是采用分步归纳流程：

1. 先按作者归纳当天有效文章，避免发文数量多的作者获得不合理权重。
2. 再跨作者比较观点，识别共同关注方向、主要分歧和混沌区域。
3. 区分盘面事实、作者预期、推断和下一交易日需要验证的条件。
4. 将结果缓存在本机，可以随时查看，也可以在新文章同步后重新生成。

AI 汇总结果包含：

- **今日盘面分析**：从多位作者的叙述中还原当天市场主线和结构特征
- **明日预期**：整理偏强、基础和偏弱情景，以及需要观察的触发条件
- **跨作者高频共识**：至少由多位独立作者共同支持的方向与证据
- **分歧与风险**：指出表面一致背后的分化、后排风险和情绪隐患
- **今日超预期个股**：保留文章中明确描述的主动走强或逆势表现
- **明日预期个股**：只保留具有逻辑、触发条件和失效条件的观察对象

<p align="center">
  <img src="./docs/assets/easy-stock-ai-daily-consensus.png" width="100%" alt="easy-stock AI 今日大 V 观点共识与明日预期" />
</p>

这套能力希望解决的不是“让 AI 猜明天涨什么”，而是把几十篇非结构化文章转化为一份可阅读、可核验、可在次日继续跟踪的观点地图。

### 3. 超短连板分析：看清梯队、晋级和情绪周期

超短交易不能只看今天有多少只股票涨停，更重要的是理解涨停发生在怎样的结构中。easy-stock 将涨停池、连板梯队、昨日反馈和情绪历史放在一个分析视图中：

- 展示涨停家数、连板家数、最高连板、开板回封和封板成交额
- 按高度整理首板、二板及更高梯队，并展示逐股炒作题材和板型
- 展示昨日连板股票的当前涨幅，观察高标、连板和首板的真实溢价
- 统计昨日到今日的 `1进2`、`2进3` 等晋级率，识别接力强弱
- 结合最终炸板率、昨日涨停反馈、昨日连板反馈和高度坍缩评估风险
- 保存每日情绪快照，形成连续时间轴，而不是只观察单日静态数据
- 区分低位活跃、高位退潮、情绪修复等状态，帮助理解周期所处位置
- 开盘啦负责当日涨停与题材归因，东方财富补充历史梯队和行情字段

<p align="center">
  <img src="./docs/assets/easy-stock-short-term-analysis.png" width="100%" alt="easy-stock 超短连板、市场情绪与晋级结构分析" />
</p>

页面不仅展示结果，也尽量解释结果：哪些梯队正在提供赚钱效应、风险来自高位还是后排、昨日强势股是否兑现溢价，以及市场当前更接近扩张、分化、修复还是退潮。

### 4. 趋势题材雷达：从板块涨跌中识别真正的市场主线

- 聚合题材排名、涨跌强度、资金流、上涨宽度和持续天数
- 保留开盘啦龙一至龙五的来源排序，并补充完整候选股池
- 通过题材地图拆解产业链、概念节点和细分方向
- 联动实时行情、日 K、近五日表现和领导力指标
- 支持成分股筛选、排序、搜索和多日趋势强度切换
- 当主数据源未更新或不可用时，展示沿用、兜底和降级原因

### 5. 游资心法与 AI Copilot：把经验材料变成可持续研读的知识

- 每日缓存公开的游资心法原始资料并按交易者建立索引
- 将本地资料同步为 Hermes 可使用的知识技能和记忆索引
- AI 回答时区分原文观点、模型推断和风险，不把历史经验包装为事实
- 支持 Hermes 流式对话、多轮会话续接和本机历史记录
- 支持 OpenAI、DeepSeek、通义千问、Moonshot、Anthropic 和兼容接口
- 心法研读、文章提炼、每日观点总结和通用研究对话共享同一模型底座

| 每日研究阶段 | easy-stock 提供的能力 |
| --- | --- |
| 盘中发现 | 趋势题材、实时行情、题材地图、涨停梯队和数据源状态 |
| 收盘复盘 | 情绪时间轴、昨日反馈、晋级结构和连板梯队分析 |
| 信息收集 | 大 V 主页自动同步、文章导入、正文清洗和本地归档 |
| AI 提炼 | 单篇摘要、作者归纳、跨作者共识、分歧和明日观察条件 |
| 长期积累 | 文章资料库、游资心法、AI 会话和本地研究记忆 |

## 为什么要做 easy-stock

AI 时代的炒股软件，不应该只是传统行情终端旁边多放一个聊天框。

真正适合 A 股的 AI 软件，需要理解题材轮动、涨停梯队、连板晋级、市场情绪、龙头与补涨、预期差和复盘语境；还需要能够主动获取信息、调用工具、保留上下文，并把结论重新连接到实时行情和原始证据。

easy-stock 希望构建这样一套 AI 原生工作台：

| AI 原生能力 | easy-stock 的实现方式 |
| --- | --- |
| **感知市场** | 聚合开盘啦、东方财富、新浪、财联社等来源，统一行情、K 线、题材、涨停与资讯数据 |
| **理解语境** | 用 Hermes 理解大 V 文章、游资心法和复盘材料，提取共识、分歧、关键点与后市预期 |
| **执行任务** | 通过定时调度、内置浏览器和 agent-browser 自动访问主页、发现文章、去重并归档 |
| **形成记忆** | 将文章、摘要、情绪历史、模型会话和研究缓存保存在本机，持续积累个人研究上下文 |
| **验证结论** | 保留来源、原文地址、抓取时间、延迟与降级状态，让 AI 输出可以回到证据层核验 |

它不是一个承诺预测涨跌的“神奇模型”，而是一套帮助研究者更快收集证据、更好理解市场、更稳定执行复盘流程的 AI 投研基础设施。

> easy-stock 的目标不是替你做决定，而是让你在 AI 时代拥有更完整的感知、更高效的研究和更可复用的判断体系。

## AI 如何赋能 A 股研究

### 1. 从“看数据”升级为“理解市场结构”

传统行情软件擅长展示价格、涨跌幅和成交额，但 A 股短线交易真正困难的是理解价格背后的结构：今天的主线是什么、题材是否扩散、连板高度是否打开、昨日强势股是否获得溢价、情绪处于修复还是退潮。

easy-stock 先通过领域化数据模型整理题材、梯队、涨停原因、板型和情绪历史，再把这些结构交给 AI 解释。AI 面对的不再是一堆孤立数字，而是一套带有 A 股语义的市场证据。

### 2. 从“手工翻网页”升级为“Agent 主动执行”

AI 的价值不仅是生成文字，更重要的是完成任务。

在大 V 复盘场景中，Hermes 可以配合 agent-browser 和 Electron 持久浏览器会话：按照订阅配置访问雪球或淘股吧主页，发现最新文章，识别作者和发布时间，完成去重、正文抓取、归档与 AI 提炼。用户可以立即同步，也可以让系统每天定时执行。

### 3. 从“单篇摘要”升级为“观点网络”

一篇文章的摘要只是起点。easy-stock 可以先归纳每位作者的核心观点，再汇总当日多位作者的共识与分歧，帮助研究者回答：

- 哪些题材被多位作者共同关注？
- 哪些观点只是少数人的预期？
- 对同一方向，市场存在怎样的分歧？
- 哪些判断需要在下一个交易日继续验证？

随着文章和复盘持续归档，这些内容将成为可检索、可比较、可追溯的个人市场记忆。

### 4. 从“通用问答”升级为“A 股研究 Copilot”

easy-stock 的 AI 会话统一经过本机 Hermes Runtime。Hermes 负责模型调用、会话续接、工具路由、知识技能和任务上下文，业务代码不直接绑定某一家模型厂商。

目前可以配置：

- OpenAI
- DeepSeek
- 通义千问
- Moonshot
- Anthropic
- OpenAI 兼容的自定义服务

同一个工作台可以根据成本、速度、上下文长度和推理能力选择合适模型，而不需要重写业务功能。

### 5. 从“用完即走”升级为“长期研究飞轮”

easy-stock 将每天的研究过程组织成一个持续循环：

| 感知 | 理解 | 行动 | 记忆 | 验证 |
| --- | --- | --- | --- | --- |
| 获取实时行情、题材和文章 | AI 识别结构、观点和分歧 | 自动同步、提炼、生成复盘 | 本地保存文章、历史和会话 | 下一交易日用行情与原文验证 |

每一次验证都会成为下一次研究的上下文。随着使用时间增加，软件积累的不只是更多数据，而是一套更贴近使用者的研究流程和认知资产。

## 企业级 AI 原生架构

<p align="center">
  <img src="./docs/assets/easy-stock-ai-architecture.svg" width="100%" alt="easy-stock 企业级 AI 原生架构图" />
</p>

架构采用分层和横切治理设计：

1. **AI 投研体验层**：趋势题材、短线连板、游资心法、大 V 复盘和 AI Copilot 共享一致的桌面交互与研究上下文。
2. **业务编排层**：Go 服务统一处理 API、调度器、策略评估、复盘智能、数据源健康和领域模型，避免前端直接耦合外部网站。
3. **AI Agent 平台层**：Hermes 管理 Prompt、Skills、Session、Memory、Tool Router 和模型网关，让模型具备调用数据与浏览器工具的能力。
4. **数据智能层**：Provider Registry 将多数据源转换为统一市场模型，并通过 Evidence Metadata、SQLite、缓存和快照保留证据链。
5. **外部生态层**：连接行情网站、内容平台和用户选择的大模型服务；每一种外部依赖都可以独立替换或降级。
6. **桌面运行边界**：Electron 负责进程生命周期、随机端口、一次性 Token、持久浏览器会话和安装包资源装配。
7. **安全与治理**：密钥隔离、来源追踪、弹性降级和运行可观测性横跨所有层级。

### 核心调用链

```text
研究者
  └─ easy-stock React 工作台
       ├─ HTTP / WebSocket ──> Go 领域服务
       │                         ├─ 行情与资讯 Provider
       │                         ├─ 题材 / 连板 / 情绪 / 复盘服务
       │                         ├─ SQLite 与本地缓存
       │                         └─ Hermes Runtime
       │                              ├─ Prompt / Skill / Memory
       │                              ├─ 用户配置的大模型
       │                              └─ agent-browser
       │                                   └─ Electron Browser Bridge
       │                                        ├─ 雪球持久会话
       │                                        └─ 淘股吧持久会话
       └─ Electron Preload <── 随机端口与一次性 Token ── Electron 主进程
```

## 技术栈

| 层级 | 技术 |
| --- | --- |
| 桌面运行时 | Electron 38、Preload、持久 Session、Loopback 服务 |
| 前端 | React 19、TypeScript 5、Vite 7、Lucide Icons |
| 后端 | Go 1.26、HTTP、WebSocket、任务调度 |
| AI Agent | Hermes Agent、TUI Gateway、流式 JSON-RPC |
| 浏览器执行 | agent-browser、Electron Browser Bridge |
| 数据存储 | SQLite、本机 JSON 设置、文章与行情缓存 |
| 测试 | Go Test、Vitest、Node Test Runner |
| 桌面打包 | Electron Packager、macOS `.app` 与 DMG |

## 当前数据来源

| 来源 | 主要用途 | 接入策略 |
| --- | --- | --- |
| 短线侠 / 开盘啦 | 题材排名、龙一至龙五、涨停池、连板和逐股题材 | 当日题材和短线结构优先来源 |
| 东方财富 | K 线、板块、成分股、资金流和历史涨停数据 | 补充字段与降级兜底 |
| 新浪财经 | A 股实时行情、部分 K 线 | 实时价格与代表股兜底 |
| 财联社 | 市场快讯 | 资讯流 |
| 雪球 | 大 V 主页和文章 | 内置浏览器登录后由 Agent 同步 |
| 淘股吧 | 大 V 主页和文章 | 内置浏览器登录后由 Agent 同步 |
| 微信公众号 | 已知文章链接 | 链接导入；主页订阅开发中 |

外部数据源可能受到网络、限频、登录验证或页面变化影响。easy-stock 会尽量报告真实来源、更新时间、延迟、错误原因和降级状态，而不是静默返回看似正常的数据。

## 快速开始

### 环境要求

- Node.js 与 npm
- Go 1.26（以后端 `go.mod` 为准）
- AI 功能需要 Hermes Runtime 和可用的模型 API Key
- macOS 桌面打包需要 Electron 对应架构的运行环境

### 安装依赖

```bash
git clone <your-repository-url>
cd easy-stock
npm install
```

如果 Electron 下载较慢，可以先安装其余依赖：

```bash
npm install --ignore-scripts
npm rebuild electron
```

### 一键启动 Web 工作台

```bash
npm run restart
```

默认地址：

- 前端：`http://127.0.0.1:20073`
- 后端：`http://127.0.0.1:20081`
- 日志：`.runtime/`

### 分别启动前后端

```bash
# 终端 1
npm run dev:backend
```

```bash
# 终端 2
VITE_A_STOCK_BACKEND_URL=http://127.0.0.1:20081 npm run dev:frontend
```

### 启动桌面应用

```bash
# 终端 1：Vite 页面
npm run dev:frontend
```

```bash
# 终端 2：Electron 桌面端
npm run dev:desktop
```

Electron 会自动分配本机端口、生成一次性 Token 并启动 Go 后端，不需要用户手动维护服务进程。

## 配置 Hermes 与模型

开发环境可以指定已经准备好的 Hermes Runtime：

```bash
export A_STOCK_HERMES_RUNTIME_ROOT=/absolute/path/to/hermes-runtime
export A_STOCK_HERMES_HOME="$PWD/.runtime/hermes-home"
export A_STOCK_HERMES_WORKDIR="$PWD/.runtime/hermes-workspace"
```

在应用的「设置 → 模型服务」中：

1. 选择模型服务商和接口协议。
2. 填写 Base URL、模型名称与 API Key。
3. 获取模型列表或运行连接测试。
4. 保存后，AI 对话、文章提炼和每日观点总结将统一复用该配置。

模型 API Key 只写入 Hermes 用户目录下的 `.env`，不会保存到前端页面、通用 `settings.json` 或应用日志。

## 配置大 V 自动同步

1. 打开「设置 → 大 V 复盘采集」。
2. 为雪球或淘股吧创建一套或多套采集配置。
3. 点击登录按钮，在内置浏览器中完成登录和可能出现的验证。
4. 点击「我已完成登录」，保存当前设备上的浏览器状态。
5. 在「大 V 复盘日记」中添加作者主页并选择采集配置。
6. 点击「立即同步」，或者等待每日定时任务自动执行。

每套配置拥有隔离的浏览器 Session，适合管理不同平台、不同账号或不同同步策略。

## 核心 API

| 路径 | 说明 |
| --- | --- |
| `GET /api/health` | 本地服务健康检查 |
| `GET /api/v1/sources` | 数据源目录与状态 |
| `GET /api/v1/quotes/realtime` | 实时行情 |
| `GET /api/v1/quotes/kline` | 单股 K 线 |
| `GET /api/v1/themes/overview` | 趋势题材总览 |
| `GET /api/v1/themes/screen` | 题材成分筛选 |
| `GET /api/v1/sector-map` | 题材产业链地图 |
| `GET /api/v1/short-term/limit-up-ladder` | 涨停和连板梯队 |
| `GET /api/v1/short-term/emotion-history` | 市场情绪历史 |
| `GET /api/v1/reviews/*` | 文章、订阅、同步和 AI 分析 |
| `GET /api/v1/ai/ws` | Hermes AI 流式会话 |
| `POST /api/v1/strategy/inflections/evaluate` | 可解释拐点策略评估 |

完整说明见 [API 路由文档](./backend/docs/api-routes.md)。

## 项目结构

```text
easy-stock/
├── frontend/                 React AI 投研工作台
│   └── src/
│       ├── components/       题材、连板、复盘、心法、AI 与设置
│       └── lib/              API、WebSocket 与 Hermes 客户端
├── backend/                  Go 数据与领域服务
│   ├── cmd/server/           服务入口
│   ├── internal/providers/   行情、K 线与资讯 Provider
│   ├── internal/httpapi/     API、调度和业务编排
│   ├── internal/hermes/      AI Runtime、模型配置与 Prompt 适配
│   ├── internal/strategy/    可解释策略引擎
│   └── docs/                 架构、接口与数据源文档
├── desktop/                  Electron、浏览器桥接和打包脚本
├── docs/assets/              品牌与企业级架构图
├── integrations/             第三方服务本地集成
├── scripts/                  构建与开发脚本
└── package.json              Monorepo 命令入口
```

## 测试与构建

运行默认测试：

```bash
npm test
```

分别验证各层：

```bash
cd backend && go test ./...
npm --workspace frontend test -- --run
npm --workspace frontend run build
npm --workspace desktop test
```

真实联网测试默认关闭：

```bash
cd backend
A_STOCK_LIVE_TEST=1 go test ./internal/httpapi ./internal/providers -run Live -v
```

构建桌面端：

```bash
npm run build:desktop
```

生成 macOS 应用和 DMG：

```bash
HERMES_RUNTIME_SOURCE=/absolute/path/to/hermes-runtime npm run package:mac
HERMES_RUNTIME_SOURCE=/absolute/path/to/hermes-runtime npm run installer:mac
```

输出应用名称为 `easy-stock.app`，安装包名称为 `easy-stock-<arch>.dmg`。

## 本地优先与安全边界

- Go 后端只监听 loopback，桌面模式使用随机端口和一次性 Token。
- 模型 API Key 隔离保存在 Hermes `.env` 中。
- 雪球和淘股吧登录态按采集配置隔离并保存在本机。
- 文章、行情缓存、AI 会话和设置默认保存在当前设备。
- 数据响应保留来源、时间、延迟、过期和降级信息。
- Cookie、API Key、数据库、浏览器状态和 Runtime 目录不应提交到版本控制。

## Roadmap

easy-stock 正在向更完整的 AI 投研操作系统演进：

- 建立行情、公告、研报、F10、资金流和全球市场的统一证据层。
- 让 AI 自动生成盘前计划、盘中异动解释和盘后复盘草稿。
- 将题材轮动、情绪周期和拐点信号组织为可解释、可验证的研究链路。
- 建立跨交易日的观点跟踪，自动提示昨日预期是否兑现。
- 完善 Agent 任务进度、失败重试、浏览器诊断和增量同步。
- 推进微信公众号主页订阅的稳定接入。
- 扩展个性化研究记忆、策略模板和证据树。
- 增强桌面自动更新、跨平台打包和团队研究协作能力。

AI 正在重新定义软件，也会重新定义个人投资者获取信息、组织观点和复盘市场的方式。easy-stock 希望从 A 股最真实、最具体的研究工作流出发，把这种变化做成每天都能使用的产品。

## 延伸文档

- [系统架构](./backend/docs/architecture.md)
- [API 路由](./backend/docs/api-routes.md)
- [数据源说明](./backend/docs/data-sources.md)
- [Hermes 集成](./backend/docs/hermes-integration.md)
- [项目路线图](./backend/docs/roadmap.md)

## 风险提示

本项目仅用于学习、研究和信息整理，不构成任何投资建议、收益承诺或交易依据。市场有风险，AI 输出和第三方数据也可能存在延迟、遗漏或错误，请始终结合原始信息独立判断并自行承担决策结果。
