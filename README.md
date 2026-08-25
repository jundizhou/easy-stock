<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/desktop/assets/easy-stock.png" width="112" height="112" alt="easy-stock Logo" />
</p>

<h1 align="center">easy-stock：A股 AI 智能投研工作台</h1>

<p align="center"><strong>面向个人投资者的 A股行情分析、股票分析与 AI投研桌面应用</strong></p>

<p align="center">
  让 AI 看懂市场，让每一次判断都有证据。<br />
  把盘中观察、盘后复盘和长期认知，沉淀为一套持续进化的研究系统。
</p>

<p align="center">
  <a href="https://github.com/jundizhou/easy-stock/releases/latest"><strong>下载最新版</strong></a> ·
  <a href="#核心产品能力">查看核心能力</a> ·
  <a href="https://github.com/jundizhou/easy-stock/issues/new/choose">反馈问题</a> ·
  <a href="./ROADMAP.md">产品路线图</a>
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Backend-Go-00ADD8?logo=go&logoColor=white" />
  <img alt="React" src="https://img.shields.io/badge/Frontend-React%20%2B%20TypeScript-3178C6?logo=react&logoColor=white" />
  <img alt="Electron" src="https://img.shields.io/badge/Desktop-Electron-47848F?logo=electron&logoColor=white" />
  <img alt="Hermes" src="https://img.shields.io/badge/AI-Hermes-6D5BD0" />
  <img alt="Local First" src="https://img.shields.io/badge/Data-Local%20First-159A80" />
  <img alt="License" src="https://img.shields.io/badge/License-Non--Commercial-EA580C" />
  <a href="https://github.com/jundizhou/easy-stock/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/jundizhou/easy-stock?label=Release" /></a>
  <a href="https://github.com/jundizhou/easy-stock/stargazers"><img alt="GitHub stars" src="https://img.shields.io/github/stars/jundizhou/easy-stock?style=flat" /></a>
</p>

<p align="center">
  <a href="#为什么要做-easy-stock">为什么</a> ·
  <a href="#核心产品能力">核心能力</a> ·
  <a href="#ai-如何赋能-a-股研究">AI 研究方式</a> ·
  <a href="#ai-原生架构">系统架构</a> ·
  <a href="#快速开始">快速开始</a> ·
  <a href="https://github.com/jundizhou/easy-stock/discussions">社区讨论</a> ·
  <a href="./CONTRIBUTING.md">参与贡献</a> ·
  <a href="#许可与商业使用">许可</a>
</p>

<p align="center">
  <a href="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-product-overview.png">
    <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-product-overview.png" width="1280" alt="easy-stock A股 AI 智能投研工作台产品全景图，展示趋势题材、主线梯队、个股日 K 与龙头分析" />
  </a>
</p>

<p align="center"><sub>行情总览 · 大 V 自动复盘 · 趋势题材雷达 · 超短情绪 · 个股 AI 分析 · 本地研究记忆</sub></p>

---

## 为什么要做 easy-stock

AI 时代的炒股软件，不应该只是在传统行情终端旁边多放一个聊天框。

传统行情软件擅长展示价格、涨跌幅和成交额，但 A股交易真正困难的是理解价格背后的结构：今天的主线是什么、题材是否扩散、连板高度是否打开、昨日强势股是否获得溢价、趋势是否仍然成立、情绪处于修复还是退潮。

easy-stock 希望构建一套真正理解 A股语境的 AI 原生工作台，把行情分析、题材研究、股票分析、盘后复盘与可追溯证据放进同一个本地优先的研究流程：

| AI 原生能力 | easy-stock 的实现方式 |
| --- | --- |
| **感知市场** | 聚合开盘啦、东方财富、新浪、财联社等来源，统一行情、K 线、题材、涨停与资讯数据 |
| **理解结构** | 将趋势、题材、连板、情绪、量价和相对强度整理成可计算的领域模型 |
| **执行任务** | 通过定时调度、内置浏览器和 Agent 自动发现文章、去重、归档并提炼观点 |
| **形成记忆** | 将文章、摘要、情绪历史、分析记录、模型会话和研究缓存保存在本机 |
| **验证结论** | 保留评分维度、题材来源、原文地址、更新时间、延迟与降级状态，让结论可以回到证据层核验 |

> easy-stock 的目标不是替你做决定，而是让你在 AI 时代拥有更完整的感知、更高效的研究和更可复用的判断体系。

---

## 核心产品能力

### 01 · 大 V 自动复盘

#### 兼听则明，客观则赢：让 AI 自动收集和整理多位市场作者的观点

大 V 的盘后复盘通常分散在雪球、淘股吧和微信公众号中，手工逐个打开主页、筛选当天文章、复制正文并整理观点非常耗时。easy-stock 将这些内容组织为统一的复盘时间流：

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-auto-review.png" width="2560" height="1692" alt="easy-stock 大 V 自动复盘与每日同步工作台" />
</p>

文章收集完成后，可以一键生成「今日大 V 观点共识」，从文章集合中提炼共同关注方向、主要分歧、盘面事实与下一交易日需要验证的条件。

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-ai-daily-consensus.png" width="2560" height="1692" alt="easy-stock AI 今日大 V 观点共识与明日预期" />
</p>

这套能力希望解决的不是“让 AI 猜明天涨什么”，而是把几十篇非结构化文章转化为一份可阅读、可核验、可在次日继续跟踪的观点地图。

### 02 · 超短连板分析

#### 看清梯队、晋级和情绪周期，寻找真正的超短节点

系统将涨停池、连板梯队、昨日反馈、晋级率和情绪历史放在同一视图中,并设计一套算法分析情绪周期：

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-short-term-analysis.png" width="2560" height="1692" alt="easy-stock 超短连板、市场情绪与晋级结构分析" />
</p>

### 03 · 趋势题材雷达

#### 从板块涨跌中识别真正的市场主线：牛市进程分歧研究，熊市进程分歧防守
聚合题材排名、涨跌强度、资金流、上涨宽度和持续天数，通过题材地图拆解产业链、概念节点和细分方向。结合 AI 分析市场趋势、题材阶段和趋势股的条件化介入点

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-theme-radar.png" width="2560" height="1696" alt="easy-stock 趋势题材雷达、主线热度与个股梯队分析" />
</p>

### 04 · 行情总览

#### 把指数、资金、榜单和研究信号组织成一张可追溯的市场地图

行情总览将市场核心指数、新闻快讯、行业趋势强度、行业资金、题材概念、个股资金流入流出、龙虎榜、公告雷达、机构观点与产业透视统一到同一个研究入口。每个页面保留数据来源与抓取时间，并可将当前上下文直接交给 AI 解读，减少在多个行情终端和资讯页面之间来回切换。

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-market-overview-indices.png" width="2560" height="1696" alt="easy-stock 行情总览、市场核心指数与跨市场走势分析" />
</p>

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-market-overview-research.png" width="2560" height="1696" alt="easy-stock 行情总览、机构观点与研报证据检索" />
</p>

### 05 · 个股 AI 分析

#### 把趋势股与情绪股放进同一套可解释决策系统

个股 AI 分析会系统判断个股更接近情绪连板、趋势容量、趋势成长、震荡观察还是弱势风险路径，再为不同路径分配不同权重，结合你的个人操作经验，复盘文章，游资心得等差异化内容让ai定制你的专属个股报告。

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-stock-ai-analysis-overview.png" width="2560" height="1696" alt="easy-stock 个股 AI 分析总览、多维评分与题材定位" />
</p>

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-stock-ai-analysis-evidence.png" width="2560" height="1696" alt="easy-stock 个股 AI 分析条件决策、事实链与风险边界" />
</p>

<p align="center"><sub>截图用于展示页面结构；股票状态、题材标签、评分和风险参数会随交易日、缓存快照与数据源可用性动态变化。</sub></p>

### 06 · 游资心法与 AI Copilot

#### 把经验材料变成可持续研读的知识，让 AI 越用越懂你的研究方式

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-trading-mastery.png" width="2560" height="1696" alt="easy-stock 游资心法库、人物资料与 Hermes 深度研读" />
</p>

### 每日研究闭环

| 阶段 | easy-stock 提供的能力 |
| --- | --- |
| **盘中发现** | 行情总览、趋势题材、实时行情、题材地图、涨停梯队和数据源状态 |
| **个股研判** | 路径识别、多周期趋势、相对强度、题材归因、隔日情景和风险边界 |
| **收盘复盘** | 情绪时间轴、昨日反馈、晋级结构和连板梯队分析 |
| **信息收集** | 大 V 主页自动同步、文章导入、正文清洗和本地归档 |
| **AI 提炼** | 单篇摘要、作者归纳、跨作者共识、分歧和明日观察条件 |
| **长期积累** | 文章资料库、游资心法、个股分析记录、AI 会话和本地研究记忆 |

---

## AI 如何赋能 A 股研究

### 1. 从“看数据”升级为“理解市场结构”

easy-stock 先通过领域化数据模型整理题材、梯队、涨停原因、趋势、相对强度和情绪历史，再把这些结构交给 AI 解释。AI 面对的不再是一组孤立数字，而是一套带有 A 股语义的市场证据。

### 2. 从“手工翻网页”升级为“Agent 主动执行”

Hermes 可以配合 agent-browser 和 Electron 持久浏览器会话，按照订阅配置访问雪球或淘股吧主页，发现最新文章，识别作者和发布时间，完成去重、正文抓取、归档与 AI 提炼。

### 3. 从“单篇摘要”升级为“观点网络”

系统先归纳每位作者的核心观点，再汇总当日多位作者的共识与分歧，区分盘面事实、少数预期、共同关注方向和下一交易日需要验证的条件。

### 4. 从“通用问答”升级为“A 股研究 Copilot”

AI 会话统一经过本机 Hermes Runtime。Hermes 负责模型调用、会话续接、工具路由、知识技能和任务上下文，业务代码不直接绑定某一家模型厂商。

目前可以配置 OpenAI、DeepSeek、通义千问、Moonshot、Anthropic，以及兼容 OpenAI 协议的自定义服务。

### 5. 从“用完即走”升级为“长期研究飞轮”

| 感知 | 理解 | 行动 | 记忆 | 验证 |
| --- | --- | --- | --- | --- |
| 获取实时行情、题材和文章 | AI 识别结构、观点和分歧 | 自动同步、分析并生成预案 | 本地保存文章、历史和会话 | 下一交易日用行情与原文验证 |

每一次验证都会成为下一次研究的上下文。随着使用时间增加，软件积累的不只是更多数据，而是一套更贴近使用者的研究流程和认知资产。

---

## AI 原生架构

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-ai-architecture.svg" width="1680" height="1180" alt="easy-stock AI 原生架构图" />
</p>

| 层级 | 核心职责 |
| --- | --- |
| **AI 投研体验层** | 行情总览、趋势题材、短线连板、个股 AI 分析、游资心法、大 V 复盘与 AI Copilot |
| **业务编排层** | Go API、个股分析引擎、策略评估、复盘智能、调度任务和数据源状态 |
| **AI Agent 平台层** | Hermes Prompt、Skills、Session、Memory、Tool Router 与开放模型网关 |
| **数据智能层** | Provider Registry、统一市场模型、题材归因、证据元数据、SQLite 与缓存快照 |
| **外部生态层** | 行情数据源、内容平台、浏览器执行能力和用户选择的大模型服务 |
| **桌面运行边界** | Electron 生命周期、随机端口、一次性 Token、持久浏览器会话和本地资源装配 |
| **安全与治理** | 密钥隔离、来源追踪、弹性降级、运行状态与风险提示 |

---

## 快速开始

### 1. 用户

无需安装 Node.js、Go 或 Python。前往 [GitHub Releases](https://github.com/jundizhou/easy-stock/releases/latest) 下载适合当前系统的桌面安装包或压缩包。

### 2. 开发者

需要从源码运行、调试、测试或打包，请阅读 [开发者文档](./docs/development.md)。

## 社区与贡献

- 使用交流、研究方法和案例分享，请前往 [GitHub Discussions](https://github.com/jundizhou/easy-stock/discussions)。
- 遇到错误或数据异常，请使用 [Bug 报告](https://github.com/jundizhou/easy-stock/issues/new/choose)，并尽量附上系统版本、应用版本和复现步骤。
- 有产品建议或数据源需求，请提交 [功能建议](https://github.com/jundizhou/easy-stock/issues/new/choose)，说明使用场景和期望结果。
- 希望参与代码或文档建设，请先阅读 [贡献指南](./CONTRIBUTING.md) 和 [产品路线图](./ROADMAP.md)。
- 涉及安全问题时，请按照 [安全策略](./SECURITY.md) 私下报告，不要公开敏感细节。

---

## 许可与商业使用

easy-stock 项目原创的后端、前端、桌面端和文档采用 [PolyForm Noncommercial License 1.0.0](./LICENSE) 授权。

- 允许个人出于学习、研究、实验和其他非商业目的使用、修改及分发，但必须保留许可证与版权声明。
- 未经作者明确书面许可，不得用于任何直接或间接的商业用途，包括但不限于企业生产环境、收费服务、SaaS、付费咨询或培训、商业产品集成、二次销售以及以本项目获利。
- 如需商业使用，请通过 [GitHub 仓库](https://github.com/jundizhou/easy-stock) 联系作者 jundizhou，取得单独的商业授权。
- 第三方依赖、数据源和随包材料不受本项目许可证重新授权，仍分别遵循其原始许可证及服务条款。
- 欢迎在许可范围内提交 Issue、文档改进和代码贡献；贡献代码在被合并后按本项目相同许可证发布。

> 本项目属于源码可用（source-available）软件，并非 OSI 定义下允许商业使用的开源软件。

---

## 风险提示

> 本项目仅用于学习、研究和信息整理，不构成任何投资建议、收益承诺或交易依据。市场有风险，AI 输出和第三方数据也可能存在延迟、遗漏或错误，请始终结合原始信息独立判断并自行承担决策结果。

<p align="center"><sub>Local first · Evidence based · Human in control</sub></p>
