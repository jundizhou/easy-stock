# 上游同步维护说明

本仓库是上游 [jundizhou/easy-stock](https://github.com/jundizhou/easy-stock) 的**本地二次开发分支**，只进不出。

## 硬性约定

- **上游只读**：只 `fetch`，永不 `push`（`upstream` 的 push 地址已设为 `DISABLED`，推送会直接失败）。
- **本地改动全部保留**：上游更新一律用 merge 合入，冲突只出现在我们改过的文件里。
- 只有上游**新增或改变了功能**时，才把界面上的本地调整重新贴到新结构上；纯样式调整一律保留我们的。

## 当前基线

| 项 | 值 |
| --- | --- |
| 分叉点 | 上游提交 `71c0a0a`（1.5.3 之后、1.5.4 之前的第 4 个提交） |
| 已同步 | 上游 `v1.5.4`（本地标记 `local-1.5.4`） |
| 我们的改动 | 44 个文件（新增 8、修改 36），+3470 / −2409 行 |
| 分支 | `local`，上游跟踪 `upstream/main` |
| 本仓库 git | 便携版 `D:\tools\PortableGit\cmd\git.exe` |

> 找分叉点的办法：对候选提交逐个跑 `git diff --shortstat <提交>`，差值最小的那个就是分叉点。
> 最初误把基线当成 `v1.5.3` 标签，结果把上游自己的一部分提交也算进了"我们的改动"。

## 日常命令

```powershell
$env:Path = "D:\tools\PortableGit\cmd;$env:Path"   # 新终端里先执行一次
Set-Location D:\easy-stock-main
git status                                          # 改之前先确认工作区干净
git add -A
git commit -m "说明这次改了什么"
```

## 上游更新流程

1. 拉取：`git fetch upstream --tags`
2. 看待合入的提交：`git log --oneline local..upstream/main`
3. 先试合：`git merge --no-commit --no-ff upstream/main`，用
   `git status --porcelain | ? { $_ -match '^(UU|AA|DU|UD|AU|UA|DD)' }` 看冲突；不合就 `git merge --abort`
4. 正式合：`git merge upstream/main`
5. 冲突处理：**纯样式**取我们的；上游**功能/结构**变了的地方取上游的，再把我们的样式贴回去
6. 验证：前端 `npx vitest run`、`npm run build:frontend`、`npm run build:backend`
7. 合完打标记：`git tag local-<版本>`

## 本机的测试基线（重要：别把这些当成改坏了）

前端 vitest 全过。以下失败是这台 Windows 环境固有的，纯净的上游代码同样失败：

- 后端 `internal/agent`：测试需要 `python` 在 PATH 里（本机没有）；
- 后端 `internal/appsettings`：期望 `chmod 600`，Windows 上恒为 666；
- 后端 `internal/sqliteutil`：临时库偶发 "out of memory"，与内存压力有关；
- 后端 `internal/httpapi`：某个用例结束时临时目录被占用（Windows 文件锁）；
- 桌面端：符号链接需要特权（`EPERM`）、浏览器登录态相关计数依赖本机真实数据。

其余包全部 `ok`。

## 我们改动的集中区（合并时优先看）

- 前端样式：`frontend/src/styles.css`（占一半以上）、`frontend/src/typography.css`、`frontend/src/components/*.css`
- 前端组件：`frontend/src/App.tsx`、`components/MarketOverviewWorkspace.tsx`、`components/market/MarketDataViews.tsx`、`components/StockAIAnalysisWorkspace.tsx`、`components/ReviewDiary.tsx`、`components/LimitUpWorkspace.tsx`
- 前端基础：`frontend/src/main.tsx`、`lib/article.ts`、`lib/backend.ts`、`lib/theme.ts`、`public/easy-stock-mark.svg`、`frontend/package.json`（Inter 字体本地托管）
- 后端：`backend/internal/httpapi/market_overview*.go`、`foundation/market_overview.go`、`providers/eastmoney/*`（东财优先 IPv4、报告正文接口）
- 桌面端：`desktop/main.cjs`（Windows 无边框标题栏）、`desktop/scripts/dev-electron.mjs`
- 构建：`scripts/build-backend.mjs`（替换上游的 `mkdir -p` / `cd` POSIX 写法）

## 升级记录

### 1.5.4（本次）

上游增量 4 个提交（动作优先级校验、巡检上下文与修复范围优化、评分卡等高、发布提交）。

- 冲突 1 个：`frontend/src/components/portfolio-inspection.css`
  （上游要评分卡等高，我们要保留自己的字号变量；处理方式：采用上游的 `align-items:stretch` + KPI 纵向拉伸，字号仍用 `--fs-body` / `--lh-reading`）
- 其余 27 个文件自动合并、12 个文件自动新增；合完我们的代码 = 上游 v1.5.4 + 44 个文件的本地改动。

## 不要做的事

- 不要 `git push`（已禁用）。确实要同步给团队时另说，不要用这个仓库直接推。
- 不要让桌面安装版的自动更新覆盖本地界面；要用本地改动就用本地构建。
