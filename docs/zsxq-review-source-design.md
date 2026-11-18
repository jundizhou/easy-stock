# 知识星球(zsxq)作为「大V复盘日记」数据源：调研与实现

> 状态：已落地并端到端验证（2026-11-13/14 调研，2026-11-16 实现）。zsxq-cli 0.5.1，「丁丁的信息分享」由星主开通权限后复测通过。

## 结论

**技术上已验证可接入**：接口稳定、结构清晰、解析成本低，30 条真实主题全部解析成功，数据是实时的（复测时最新一条为 3 小时前发布）。

**唯一的门槛是权限，而且它不是技术问题**：知识星球的内容读取权限由**星主按星球开通**，成员身份不自带。开通后立即可用，未开通时所有内容接口统一拒绝。

这意味着覆盖率不取决于 easy-stock 的实现质量，而取决于星主是否愿意开通。可以接入，但**不能假设"用户加入的星球都能拉"**——产品上必须把「需星主开通」做成订阅前的前置校验，而不是同步失败后才暴露。详见[1b 节](#1b-官方权限模型与文档依据)。

另有一处必须先改：现有同步假设「一个订阅 = 一位作者」，星球是「一个星球 = 多位作者」。**已在真实数据中实证**：该星球 30 条主题来自 2 个不同账号（波段之王 29 条、丁丁船长 1 条）。直接复用现有分支会把两人合并成一位作者，作者观点卡和共识/分歧归纳失真。

聚合层无需改动：`ListPostsBetween` 只按时间窗口取文，不区分 source；作者分组按 `source + author_id`。采集结果能正确落成 `Post`，后续链路全部自动生效。容量也足够：`maxDailySummaryPosts = 1000`、`maxDailySummaryAuthors = 30`，而该星球实测日均 15-20 条、2 位作者。

## 实测结果

### 1. 权限：星主级开关，所有读取通道统一拦截

| 星球 | group_id | 类型 | 成员 | 可读 |
|---|---|---|---|---|
| 光谷码农 | 455118812828 | pay | 38 | ✅（2019 年后停更） |
| **丁丁的信息分享** | **225418445851** | pay | 105 | ✅ 开通后可用，更新活跃 |
| 西七楼 | 881251425252 | pay | 40 | ❌ 未开通 |
| 卓越投资圈 | 15285552145182 | pay | 3943 | ❌ 未开通 |
| 架构师之路 | 51112514588184 | free | 7308 | ❌ 未开通 |

未开通时，尝试过的绕行方式均失败，说明拦截在服务端 MCP 层，不是 CLI 本地限制：

- `group +topics` → 拒绝
- `topic +search` → 拒绝
- `api raw --method GET --path /v2/groups/{id}/topics` → 拒绝

星主开通后同一星球立刻可读，无需改配置、无需重新登录——**即权限是唯一变量，且生效是实时的**。

### 1b. 官方权限模型与文档依据

官方文档对这件事有明确表述，**成员身份不包含内容读取权**：

> 星主：浏览/搜索星球内容、发帖/评论/回答提问、设置精华与标签、查看成员足迹、管理个人笔记
> **成员：发布个人笔记；星主开启访问权限后，也可通过 AI 查看星球主题和评论**

来源：知识星球帮助中心 <https://doc.zsxq.com/zsxq-skill-ai-tool-guide.html>

仓库内还有两处提到该错误：

- `references/endpoint-catalog.md:8`：权限拒绝（未开通 Skill 权限、查他人足迹等）不走信封，直接返回 `{success: false, error: "…"}`
- `references/scenarios/triage-comments.md:76`：报 `该星球暂未开通 Skill 权限` → **提示星主在星球设置中申请开通**后再跑，不硬试

注意「**申请**开通」的措辞，可能不是星主自助一键开关，而是需要走申请流程。这一点需实际接触星主时确认。

**权限粒度实测**（同一无权限星球）：

| 接口 | 结果 |
|---|---|
| `GET /v2/groups/{id}` 星球详情 | ✅ 可读（含 admin_ids、到期时间等） |
| `GET /v2/groups/{id}/topics` 主题 | ❌ 拒绝 |
| `GET /v2/groups/{id}/members` 成员 | ❌ 拒绝 |
| `GET /v2/search/groups/{id}/topics` 搜索 | ❌ 拒绝 |

说明锁的是**内容类接口**，星球基础元信息仍可读。这也意味着「探测某星球是否可读」不需要尝试拉主题——`/topics` 一次探测即可，成本很低。

### 1c. 与产品定位的错配

easy-stock 的「大V复盘日记」是**读者视角**：我订阅我关注的大V，聚合他们的观点。

知识星球的能力模型偏向**星主视角**：星主用自己的账号读自己的星球，做运营复盘。

两者不重合，但实测证明只要星主愿意开，读者视角也能跑通（本星球就是用户找星主开通后立刻可用）。所以问题不是"能不能"，而是"**覆盖率靠沟通争取，不靠代码解决**"。

落地影响：

- 订阅流程必须前置校验：加订阅时先探一次 `/topics`，明确告诉用户「可读 / 需联系星主开通」，不要等到同步失败。
- 产品文案不能承诺「支持知识星球」就完事，要说明「需星主开通权限」。
- 反过来看，这也筛出了高质量来源：愿意开通的星主，通常也是愿意让自己的内容被系统化复盘的。

### 1d. 权限开通后的复测数据

星主开通后，同一账号、同一配置，立刻拉到 30 条实时主题：

| 指标 | 实测值 |
|---|---|
| 条数 | 30（`has_more: true`） |
| 时间跨度 | 2026-11-12 10:46 → 2026-11-14 07:45（约 2 天） |
| 日均发布 | 15-20 条（11-13 单日 21 条） |
| 作者 | **2 个不同账号**：波段之王 29 条、丁丁船长 1 条 |
| 类型 | 全部 `talk` |
| `content` 为空 | 0 / 30 |
| 正文长度 | 最大 668 字符，无长文 |
| `<e>` 标签 | 仅 1 个 `type="web"`（30 条里只有 1 个链接） |

**作者拆分是硬需求**，不是假设：同一个星球里两个账号分别发文，`user_id` 不同（`28221528458551` / `28221528211181`）。现有分支会把它们合并成一位作者。

另一个值得注意的现象：星球 owner 是「股海东哥」，但主力发帖人 `alias` 是「波段之王」（`name` 为「东方之龙」）。**用户认知里的名字是 alias，不是 name**，作者显示名应 `alias || name`。

```text
owner: { alias: "波段之王", name: "东方之龙", user_id: "28221528458551" }
```

内容形态偏「碎片化短评」：一天 21 条里有不少一句话点评（如「还有东山精密。」）。对复盘归纳有两个影响——素材充足，但需要按最小长度或条数做过滤，否则作者观点卡会被大量碎片稀释。

### 2. 返回结构（与文档不一致）

实际字段名是 `topics_brief`，不是文档写的 `topics`：

```json
{
  "count": 5,
  "has_more": true,
  "next_end_time": "2019-04-23T14:43:52.714+0800",
  "success": true,
  "topics_brief": [ ... ]
}
```

单条 topic 的实测字段：

```text
topic_id, type, title, content, create_time, modify_time,
owner{alias, name, user_id, avatar_url},
group{group_id, name},
counts{comments, likes, readers, reading, rewards},
digested, sticky, stick_time, text_type, annotation,
images[], files[], vote, question, checkin, referenced_topic, creation_statement
```

三个解析陷阱：

**a. `title` 一定被截断**

不是偶发，是必然：

```text
"11.14市场研究汇总 （仅供..."     ← 丁丁星球样本
"光谷码农·每日新闻(2019-..."       ← 光谷码农样本
```

丁丁星球 30 条主题，**30 条 `title` 全部以 `...` 结尾**。`Post.Title` 不能直接用这个字段。

应从 `content` 首行取。实测 6 条里 5 条首行等于完整标题；但有一条首行是「以下信息，仅供参考。」——即首行也可能是一句免责声明而非标题。所以策略应为：

```text
title = 首行（若长度 >= 6 且不以句号结尾且不含换行）
      ?? 截断 title 去掉尾部 "..."
      ?? content 前 40 字
```

**b. `<e>` 标签的 href/title 是 URL 编码**

```text
<e type="web" href="https%3A%2F%2Fguanggu-coder.cn"
   title="%E5%85%89%E8%B0%B7%E7%A0%81%E5%86%9C%C2%B7..." cache="" />
```

必须 `url.QueryUnescape` 后再用，否则得到一串 `%E5%...`。

**c. 现有 `cleanDocument` 会整段吞掉链接文字**

`content` 是「纯文本 + 内联 `<e>` 标签」，不是 HTML。而 `importer.go` 的 `tagPattern`（`<[^>]+>`）会把整个 `<e ... />` 匹配掉，正文里的链接标题随之消失。

两个星球合计样本共 13 个 `<e type="web">`，全部会丢失。好消息是密度很低：丁丁星球 30 条只有 1 个链接，清洗压力远小于雪球/淘股吧。

需要新增 `zsxqContentToText`：先把 `<e type=web>` 转成 `标题 (链接)`、`<e type=hashtag>` 转成 `#标签`，再交给现有清洗。

### 3. 分页：游标含等于，边界必然重复

实测两页 `--limit 3`：

```text
page1: 422141855811418, 244151852142241, 544822224121184
page2: 544822224121184, 844411814245422, 244411812112251
                        ↑ 与 page1 末条相同
```

`next_end_time` 取本页最后一条的 `create_time`，而 `end_time` 是**含等于**的，所以下一页首条必然是上一页末条。**必须按 `topic_id` 去重**，否则重复拉取、重复计数。

### 4. 时间与作者

- `create_time` 实测格式 `2019-05-09T10:39:02.091+0800`，Go layout 为 `2006-01-02T15:04:05.000-0700`，不能直接用 RFC3339。
- `owner.alias` 可为 `null`。两个实测样本正好覆盖两种情形：

  | 场景 | alias | name | 应显示 |
  |---|---|---|---|
  | 光谷码农 | `null` | 柴树杉 | 柴树杉 |
  | 丁丁星球 | 波段之王 | 东方之龙 | **波段之王** |

  用户认知里的称呼是 alias（星球里看到的名字），所以作者显示名用 `alias || name`，而不是反过来。

### 5. 未验证项

- **长文主题 `inline_article_url`**：丁丁星球 30 条 + 光谷码农若干条，正文最长的才 668 字符，没有一条是长文主题，无法验证。实现时必须按「`content` 为空则跳过或单独抓取」做兜底，不要假设 CLI 一定回填。
- **429 限流阈值**：本次调用频率很低（十几秒一次）未触发，批量同步的实际安全间隔需上线后观察。
- **评论区**：本次只拉了主题与正文，未验证 `get_topic_comments`。若后续要把评论也纳入复盘素材，需单独验证。
- 注意「丁丁的信息分享」的 owner：`name` 是「股海东哥」、`alias` 是「丁丁船长」。**联系星主申请权限时报「股海东哥」**。

## 现有三种来源机制

| source | 采集方式 | 鉴权 | 实现位置 |
|---|---|---|---|
| `official` | 拉 OSS 托管的 `authors.json` + 按作者下载当日 JSON | 无 | `backend/internal/review/remote_daily.go` |
| `xueqiu` / `taoguba` | Electron 内置浏览器登录态 → 本地 bridge HTTP 服务 → Hermes 归一化 | 页面 Cookie（按 profile 分 partition） | `desktop/*-browser-bridge.cjs` + `automation.go` |
| `wechat` | 单篇链接 HTTP 抓取 | 无 | `backend/internal/review/importer.go` |

`wechat` 明确不提供自动订阅（`WechatArticleListUnavailableMessage`），只能粘贴链接导入。

## 三种接入方案对比

### 方案 A：后端 exec `zsxq-cli`（推荐）

```
Automation.syncOneUnlocked
  → zsxqCollector.Collect(group_id)
      → exec: zsxq-cli group +topics --group-id <id> --json
      → 解析 topics_brief[] → 清洗 → []Post
  → UpsertPost（author 按 owner 分配）
```

- 优点：不新增 Electron bridge；鉴权用官方 OAuth；结构化 JSON，无需页面解析。
- 代价：依赖 Node/CLI；**受星主 Skill 权限开关限制**。
- 登录态探测：`zsxq-cli auth status --json` → `data.loggedIn`。

### 方案 B：Electron 内置浏览器 bridge

新增 `desktop/zsxq-browser-bridge.cjs`，复用 `openReviewSourceLogin` 打开 `wx.zsxq.com` 登录，用登录态读页面。

- 优点：成员在 App/网页里本就能看到内容，理论覆盖率不受星主 API 开关限制。
- 缺点：星球是 SPA，要么等 DOM（脆弱）、要么拦接口（等于重做 CLI）；还要再写一套归一化 prompt。
- **合规提示**：星主未开通 Skill 权限，通常意味着不希望内容被程序化读取。用浏览器自动化抓取在技术上可行，但违背星主意愿，是否要做需要产品侧明确决策，不应作为默认实现。

### 方案 C：沿用 `official` 远程托管

服务端预采集后放 OSS，客户端零改动。但付费内容集中存储有版权风险，且无法让用户订阅「自己的星球」，语义不符。仅适合公共精选。

**建议：A 为主。若实测覆盖率长期低于可接受水平，再评估 B，并同步做好星主授权说明。**

## 必须处理的设计问题

### 1. 作者粒度（阻塞项）

现有代码同步后无条件覆盖作者：

```go
post.AuthorName = firstNonEmpty(resolvedName, post.AuthorName)
post.AuthorID = stableID(sub.Source + "\n" + ...)[:24]
```

`dailySummaryAuthorGroups` 的 key 是 `source + "\x00" + author_id`，这个覆盖会把整个星球归一成一位作者。

改法：zsxq 走独立分支，作者来自 topic：

```go
post.AuthorName = firstNonEmpty(topic.Owner.Alias, topic.Owner.Name)
post.AuthorID = stableID("zsxq\n" + topic.Owner.UserID)[:24]
```

订阅的 `Name` 只作为星球显示名，不再冒充作者名。

### 2. 正文清洗

见实测 2c，需要 `zsxqContentToText` 预处理 `<e>` 标签，并做 URL 解码。`<e>` 实测只出现 `web` 一种，但 `hashtag`/`mention` 在文档中存在，需一并兼容，未知 type 降级为丢弃标签保留文案。

### 3. 长文主题

文档称 MCP 通道不回填 `inline_article_url` 正文、CLI 通道由服务端回填。未实测，实现时需按「content 为空则跳过或单独抓取」做兜底。

### 4. 时间与 URL

- `create_time` 用 `2006-01-02T15:04:05.000-0700` 解析。
- `review_posts.original_url` 有 UNIQUE 约束，用 `topic_id` 构造稳定链接（如 `https://wx.zsxq.com/dweb2/index/topic_detail/{topic_id}`），保证重复同步只更新不新增。

### 5. 限流

`SyncAll` 已串行（`a.mu`），多星球时仍建议每星球间隔 1-2 秒，429 指数退避重试 2 次，失败只标记该订阅 `last_status=error`，不阻塞其他星球。

## 改动清单

| 层 | 文件 | 改动 |
|---|---|---|
| 后端采集 | `backend/internal/review/zsxq.go`（新增） | CLI 调用、`topics_brief` 解析、`<e>` 清洗、topic_id 去重、429 退避 |
| 后端同步 | `backend/internal/review/automation.go` | `AddSubscription` 放行 zsxq；`syncOneUnlocked` 增加分支；`browserSourceLabel` / `browserStatePath` 扩展 |
| 后端导入 | `backend/internal/review/importer.go` | `classifyURL` 支持 zsxq 域名；`sourceName` 增加「知识星球」 |
| 后端接口 | `backend/internal/httpapi/review_diary.go` | `reviewSources` 增加 zsxq，状态取自 `auth status`；**未开通权限时给出明确文案** |
| 配置 | `backend/internal/appsettings/store.go` | 默认 profile `zsxq-default` |
| 前端 | `frontend/src/components/ReviewDiary.tsx` | `sourceTabs`(:53)、订阅下拉(:559)、`sourceLabel`(:1006)、`sourceFromText`(:1011) |
| 桌面端 | `desktop/main.cjs` | CLI 路径探测 / `ELECTRON_RUN_AS_NODE` 兜底；CLI 未装时的引导 |

`store.go` 无需迁移：`review_posts.source` 是自由文本列。

## 风险与边界

1. **覆盖率取决于星主意愿（最高风险）**：不是技术问题，代码无法解决。实测 5 个星球里 3 个仍未开通。产品上必须在订阅页明示，并给出「联系星主开通」的引导，不能假装能拉。
2. **内容碎片化**：丁丁星球单日 21 条，含大量一句话点评。直接全量喂给作者归纳会被稀释，需要按最小长度或条数上限过滤。
3. **版权**：星球多为付费内容，仅限「用户自己已加入的星球 + 本地个人阅读」，不得二次分发，不得进入 `official` 公共托管链路。
4. **平台条款**：官方 CLI 是官方路径，风险低于抓包；但仍应保守限速。
5. **平台限制**：无桌面端/无 Node 的部署不可用，该来源应标 `limited`，与 `wechat` 同级降级。
6. **文档漂移**：官方端点文档写 `topics`，实际返回 `topics_brief`；权限错误码只在 `endpoint-catalog.md` 一句话带过、主 SKILL.md 未提。解析必须防御性降级。
7. **开源/商业边界**：zsxq 接入是通用采集能力，可进开源版；不涉及商业凭据或商业服务地址。

## 建议的实施顺序

1. **P0 权限探测优先**：先做 `auth status` + 逐星球探测可读性，把「哪些星球能拉」做成订阅页的显式状态。这一步决定了功能是否有内容可用，应早于采集实现。
2. **P1 骨架**：source 常量、`classifyURL`、`reviewSources`、前端入口、默认 profile。
3. **P2 采集**：`zsxq.go` 打通 `group +topics`，用「丁丁的信息分享」做端到端验证（已开通且活跃，30 条真实数据可用）。
4. **P3 作者拆分**：独立同步分支，按 `owner.user_id` 分配作者。用该星球验证「波段之王 29 : 丁丁船长 1」是否被正确拆成两张作者卡。
5. **P4 正文质量**：`<e>` 清洗 + URL 解码 + 截断标题回退 + 碎片短帖过滤。

## 附：验证用命令

```bash
npm install -g zsxq-cli
zsxq-cli auth login --no-browser          # 输出授权链接，浏览器确认
zsxq-cli auth status --json               # {"loggedIn":true,...}
zsxq-cli group +list --json               # 星球清单与 group_id
zsxq-cli group +topics --group-id <id> --limit 5 --json
```

凭据存系统 Keychain，不落明文，不需要在配置里填 token。

## 运行前置：Node 与 CLI

知识星球来源**不随应用打包**，它依赖本机的 Node 运行时和官方 `zsxq-cli`：

| 条件 | 说明 |
|---|---|
| Node 18+ | `zsxq-cli` 是 npm 包，入口 shebang 为 `#!/usr/bin/env node`，没有 Node 就跑不起来 |
| `npm install -g zsxq-cli` | 装到 npm 全局 bin；默认前缀是 `/opt/homebrew/bin`、`/usr/local/bin` 或 `~/.npm-global/bin` |
| 一次授权 | `zsxq-cli auth login` 或页面上扫码；凭据写系统钥匙串，之后无需重复 |

两个容易踩的坑，代码里已经处理：

1. **找得到 CLI，却执行失败**。从 Finder 启动的桌面应用 PATH 只有 `/usr/bin:/bin:/usr/sbin:/sbin`，`env node` 找不到解释器，报 `env: node: No such file or directory`。`zsxqProcessEnv` 会在 PATH 里查不到 node 时，把 CLI 所在目录（brew / nvm / volta 都把 node 和 CLI 装在同一个 bin）前置进子进程 PATH。
2. **nvm 用户找不到 CLI**。nvm 把每个版本装在 `~/.nvm/versions/node/<ver>/bin`，固定路径清单覆盖不到；`zsxqSearchDirs` 用 glob 扫一遍并按版本倒序，新版本优先。

CLI 缺失时该来源会降级为 `limited`，提示"未安装 zsxq-cli"，不影响其他来源。想指定非标准安装位置，设 `A_STOCK_ZSXQ_CLI`（或 `ZSXQ_CLI_PATH`）指向可执行文件即可。

## 桌面分发更新（2026-11-17）

上述 npm 全局安装要求仅适用于独立后端。桌面端现统一内置官方 `@zsxq/cli-<platform>-<arch>` 原生二进制，固定 0.5.1 版本和各平台 SHA-512 校验值，不依赖用户的 Node/npm 或终端 PATH。`dev:desktop` 自动准备运行时，`prepare:package` 把原生 CLI、版本清单和 Apache 2.0 许可复制进 resources/zsxq-runtime；打包后优先执行该目录内的原生文件。

`verify-release-package.mjs` 在仅含系统目录的 PATH 下执行包内 CLI 的版本和设备授权帮助命令。CLI 缺失、版本或架构清单不匹配、设备授权参数不可用都会阻止发布验证通过；macOS DMG/ZIP 和 Windows 安装包解包验证共用此检查。

## 实现状态（2026-11-16）

P0–P4 已落地，本地端到端跑通。

| 层 | 文件 | 内容 |
|---|---|---|
| 采集 | `backend/internal/review/zsxq.go`（新增） | CLI 调用与路径探测、`topics_brief` 解析、`<e>` 清洗、`topic_id` 去重、429 退避、权限/未登录错误翻译 |
| 同步 | `backend/internal/review/automation.go` | `AddSubscription` 前置探测权限；`syncZsxqUnlocked` 独立分支，作者取自 `owner` |
| 导入 | `backend/internal/review/importer.go` | `sourceName` 增加知识星球；星球链接给出「按星球同步」的引导而不是抓空壳页面 |
| 接口 | `backend/internal/httpapi/review_diary.go` | `reviews/sources` 增加 zsxq 状态；新增 `GET /api/v1/reviews/zsxq/groups` |
| 配置 | `backend/internal/appsettings/store.go` | `zsxq-default` 默认配置，旧设置文件缺失时自动补 |
| 前端 | `ReviewDiary.tsx` / `SettingsDrawer.tsx` / `styles.css` | 来源页签、订阅表单、星球权限探测面板、采集配置卡片 |
| 桌面端 | `desktop/main.cjs` | 注入 `A_STOCK_ZSXQ_CLI`，解决 Finder 启动时 PATH 缺失 |

实测（`A_STOCK_ADDR=127.0.0.1:20191`，真实账号）：

```text
GET  /reviews/sources        → zsxq configured / sync_ready=true
GET  /reviews/zsxq/groups    → 丁丁的信息分享 readable=true，其余 3 个 limited
POST /reviews/subscriptions  → 自动补名「丁丁的信息分享」，external_id=225418445851
POST .../sync                → found=14 imported=14
GET  /reviews/posts?source=zsxq → total=14，标题为完整句子，链接由 topic_id 生成
再次同步                      → found=7 imported=0（URL 幂等，重复不新增）
无权限星球添加                 → HTTP 400，提示需星主申请开通
```

两处实现与原方案的差异：

1. **标题还原改为「首句补全」**。原先按「首行优先」会拿到 `美国要出台算力出口措施。那么很` 这种平台截断值。现在先判断截断标题是否是首行前缀，是则把首行补到第一个句末标点，得到 `美国要出台算力出口措施。`。
2. **`+` 用 `url.PathUnescape` 解码**，不用 `QueryUnescape`，否则标题里的 `GopherChina+2019` 会变成空格。

仍未覆盖：长文主题 `inline_article_url`（暂无样本）、429 阈值（未触发）、评论区。


## 页面内扫码授权（2026-11-16 追加）

原先需要用户在终端执行 `zsxq-cli auth login`，对非开发用户不可用。现在授权搬到设置页，用户只需要扫码。

### 接口

| 接口 | 行为 |
|---|---|
| `POST /api/v1/reviews/zsxq/auth/device` | 执行 `auth login --no-browser --no-wait --json`，返回确认码与授权链接；10 分钟内重复调用返回同一会话 |
| `GET /api/v1/reviews/zsxq/auth/device` | 返回当前会话快照（`pending/authorized/denied/expired/failed`），页面每 2.5 秒轮询 |
| `DELETE /api/v1/reviews/zsxq/auth/device` | 取消并回收轮询子进程 |

`device_code` 只留在后端内存，前端拿不到；二维码内容为 `verification_uri_complete`，与确认码等效。

### 三个必须按实测处理的行为

1. **`--json` 不是纯 JSON**。CLI 会先打印中文横幅，再输出 JSON，最后由 `After completing authorization...` 收尾。整段 `Unmarshal` 会失败，必须截取第一个配对的 `{...}`（`extractZsxqJSONObject`）。
2. **`--device-code` 是阻塞轮询**，不能放在 HTTP handler 里。实现为一个后台 goroutine，结果写回会话；`runZsxqCLI` 的 90 秒默认超时需要让位于外层的 10 分钟 deadline，否则永远等不到授权成功。
3. **pending 会落盘，并让 `auth status` 阻塞**。CLI 把未完成的设备码加密存到 `~/Library/Application Support/zsxq-cli/<user_id>.enc`；存在 pending 时，`auth status` 会「Found pending device authorization, resuming…」并继续等待。因此：
   - 授权进行中，`/reviews/sources` 直接返回等待态，不探测 CLI（实测 7ms 返回，否则要卡满 5 秒）；
   - 用户取消后进程被杀，pending 仍可能残留，此时探测超时会提示「有未完成的扫码授权，请重新生成二维码」，而不是「稍后重试」。
   - 本地把会话有效期定为 10 分钟；CLI 不返回 `expires_in`，以页面提示为准。

### 前端

`SettingsDrawer.tsx` 的 `ZsxqAuthPanel`：`qrcode.react`（零运行时依赖）渲染 164px 二维码，展示确认码、浏览器打开链接、取消按钮；授权成功后自动刷新来源状态，无需重启应用。
