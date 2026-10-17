# 字体排版规范

本规范由 `frontend/src/typography.css` 统一定义。组件样式只能引用这里的变量，不再写死字号、行高和字距。

## 字号标尺

| 变量 | 字号 | 用途 |
| --- | --- | --- |
| `--fs-display` | 22px | 页面名（顶部标题） |
| `--fs-title` | 18px | 页内标题（hero、区块、文章标题） |
| `--fs-heading` | 16px | 面板标题、长文小节标题 |
| `--fs-subtitle` | 14px | 分组标题、卡片标题、长文阅读正文 |
| `--fs-body` | 12px | 正文、列表名、表格数据、按钮、输入框 |
| `--fs-caption` | 11px | 表头、眉标、标签、时间 |
| `--fs-desc` | 11px | 说明文字（与标签靠颜色和字重区分） |
| `--fs-micro` | 10px | 徽标、计数芯片，最小字号 |
| `--fs-metric` | 26px | 指标数字（KPI） |

规则：
- 同一层级只用一个字号，不新增中间值。
- 最小字号为 10px，中文说明文字不低于 11px。
- 数字指标使用 `--fs-metric`，单位（亿、万、手）用 `.num-unit` 拆分，使用正文字体、`--fs-caption`。
- 状态类指标（“修复”“商用车”这类非数值）在 KPI 位不要用 `--fs-title` 硬撑成数字大小，统一渲染为 `.kpi-chip` 标签，避免同一行里字号忽大忽小。
- 长文阅读区（`.ai-markdown`、`.mastery-markdown`）正文为 `--fs-subtitle`，小节标题必须比正文大一级：`h3`/`h4` 用 `--fs-heading`，`h2` 用 `--fs-title`，`h1` 用 `--fs-display`。

## 字重

- 400：正文、说明文字
- 600：列表名、标签、表头
- 700：标题、强调数字

## 行高

| 变量 | 数值 | 用途 |
| --- | --- | --- |
| `--lh-metric` | 1.1 | 指标数字 |
| `--lh-tight` | 1.25 | 标题 |
| `--lh-snug` | 1.45 | 短文本、说明、密集列表 |
| `--lh-body` | 1.6 | 正文段落 |
| `--lh-reading` | 1.8 | 长文阅读（AI 回复、心法文档） |

## 字体栈

- `--font-ui`：界面字体，Inter 优先，中文回退 PingFang SC / 微软雅黑。
- `--font-num`：数字、价格、指标，与 `--font-ui` 同族（Inter），配合全局 `font-variant-numeric: tabular-nums`，保证数字等宽对齐。
- `--font-code`：代码块、行内代码，使用系统等宽字体。
- 不使用裸 `monospace`，也不在含中文的标题上使用等宽字体。

规则：
- Inter 通过 `@fontsource/inter` 自托管（`main.tsx` 引入 400/600/700 三个字重），不依赖用户机器是否安装，保证所有平台的西文与数字字形一致。
- 数字统一使用 `font-variant-numeric: tabular-nums`（定义在 `:root`），表格与指标按位对齐。
- 数字与后缀拆开：数字用 `--font-num`，后缀（`· C`、`亿`、`板`）用 `--font-ui` 并降一级字号。

## 字距

- 中文一律 `letter-spacing: 0`。
- 仅英文眉标使用 `--tracking-eyebrow`（0.08em）。

## 桌面端框架

桌面端（Windows）使用无边框窗口，样式约定如下：

| 规则 | 取值 |
| --- | --- |
| 标题栏高度 | 40px（`titleBarOverlay` 与渲染层一致） |
| 拖动区 | `.desktop-titlebar`，`-webkit-app-region: drag`，按钮需 `no-drag` |
| 滚动范围 | 只有内容区 `.app-shell` 滚动，文档本身不滚动 |
| 滚动条 | 全局细滚动条，默认透明，滚动时显示 |

- 标记类 `desktop-titlebar-overlay` 由主进程在 `dom-ready` 注入，不依赖 User-Agent。
- 窗口按钮颜色随主题切换（`setTitleBarTheme`）。
- 侧边栏文字禁止选中，避免桌面端出现网页式的选择行为。

## 新增组件检查清单

1. 字号只用上面的变量，不要依赖浏览器默认的 `h1`–`h6` 字号（`typography.css` 已统一重置）。
2. 含单位的数值使用 `.num-unit`（或 `.kpi-unit`）拆分单位。
3. 说明文字不低于 11px，不要用 10px 放正文。
4. 标题不设字距，行高用 `--lh-tight`。
5. 区块之间用 22px 上下间距，同一区块内部用 12–16px，形成层级。
6. 页面标题只在顶栏出现一次；工作区 hero 不再重复页面名，只保留英文眉标、说明与操作。
7. 面板标题用 `--fs-heading`，卡片/列表标题用 `--fs-subtitle` 或 `--fs-body`，不要和页面标题同级。

## 对齐与留空

- **等高行内对齐**：同一行里的卡片必须让内部行位置一致。做法是给卡片显式声明行轨道（如 `.stock-ai-signals article { grid-template-rows: auto auto minmax(0, 1fr) }`），不要让默认的 `align-content: stretch` 把多余高度按比例分给标题行——那会让同一行的进度条高低不齐。
- **列数要能整除子项**：`grid` 的列数与子项数量必须匹配（6 项用 3 列、4 项用 4 列），或用 `article:nth-child(5):nth-last-child(2)` 这类规则让末行项目跨列铺满，禁止出现空单元格。
- **可伸缩列表用 `auto-fit`**：连板梯队这类“每行 1–N 个”的网格用 `repeat(auto-fit, minmax(300px, 1fr))`，只有 1 个子项时自动铺满整行。
- **空数据要显式表达**：无数据面板用虚线框 + 居中说明文字（`.stock-ai-panel-empty`、`.stock-ai-news-empty`、`.stock-ai-theme-columns .empty`），并让该面板 `align-self: start` 收成自然高度，不要为了对齐把空白撑成一大块。
- **禁止靠拉高对齐**：不要用 `align-items: stretch` 强行把“内容很少”的面板和“内容很多”的面板拉成一样高。
- **行情总览数据表（`.market-data-table` / `.market-kline-table`）**：数据列与名称类列统一用等宽轨道（`repeat(N, minmax(84px, 1fr))`），禁止出现 `70px / 70px / 140px…` 这种宽窄混合的列宽。只有“排名/序号”这类序数列可以窄（`56px`）。列宽不平均会让间距看起来一团乱。
- **表头标题与列内容同轴**：表头单元格就是网格项，`text-align: center` 对它们无效；排序按钮必须用 `display: grid; grid-template-columns: minmax(0,1fr) minmax(0,auto) minmax(0,1fr)` 把标题放在中间列，箭头放在第三列，这样标题正对列中心，箭头不参与居中。名称类列（首列）表头与内容一起左对齐。
- **首列左、其余居中**：数据表首列（名称/代码、日期）表头与内容都左对齐；其余列的数值与表头一律 `text-align: center`，保证“标题—数值”上下同轴。
- **长文列表原地展开**：新闻快讯、公告雷达、机构观点、产业透视这类长文列表，点击标题在卡片下方原地展开完整内容（按钮带 `aria-expanded`，同一列表只展开一条），不要跳转外部页面；展开时页面整体拉伸，同行的其他面板用 `align-items: start` 保持自然高度，只有正文里的「查看原文」才是外链。
- **正文按需拉取**：列表接口不返回正文时（研报），前端在展开那条时才请求 `/api/v1/research/report-content`，并给加载中/失败（可重试）状态；公告正文随列表返回，不重复请求。
- **顶部栏各页同高**：所有工作台共用同一套顶部结构——折叠把手行（20px 高）+ 12px + 标题区（`min-height: 52px`），页面标题区高度不随标题/说明文字长短变化；`个股分析` 也走同一套结构，不允许某一页没有把手或标题区更靠上。
- **顶部栏与内容间距统一**：间距只由 `.workspace-topbar.collapsible` 的 `margin-bottom`（26px）提供，工作台根部不要再自己加 `padding-top`，否则该页会比其他页更挤或更松。
- **无顶部栏的页面也要留白**：AI 对话页沿用全高聊天栅格、不设顶部栏，`.ai-chat-workspace` 只保留 20px 顶部外边距（与页面左右/底部留白一致），避免卡片贴住窗口标题栏。
- **标题图标按页面语义**：顶部栏标题左侧的 40px 图标按工作台语义取（趋势题材 `LayoutDashboard`、短线连板 `Flame`、大V复盘日记 `BookOpen`、个股分析 `BrainCircuit`、持仓AI巡检 `WalletCards`、行情总览 `BarChart3`、游资心法 `BookMarked`、Token统计 `Gauge`），色调用 `.brand-mark.<tone>` 的 soft/solid 组，不再重复使用侧边栏的应用 LOGO。
