# 上游同步维护说明

本仓库是上游 [jundizhou/easy-stock](https://github.com/jundizhou/easy-stock) 的**本地二次开发分支**，只进不出。

## 硬性约定

- **上游只读**：只 `fetch`，永不 `push`（`upstream` 的 push 地址已设为 `DISABLED`，推送会直接失败）。
- **本地改动全部保留**：上游更新一律用 merge 合入，冲突只出现在我们改过的文件里。
- 只有上游**新增或改变了功能**时，才把界面上的本地调整重新贴到新结构上；纯样式调整一律保留我们的。

## 基线

| 项 | 值 |
| --- | --- |
| 分叉点 | `v1.5.3`（本地代码即由此版本改出） |
| 我们的改动 | 改过 75 个文件、新增 27 个文件，约 6729 行 |
| 分支 | `local`，上游跟踪 `upstream/main` |
| 完整改动清单 | 仓库外：`D:\easy-stock-upstream\local-changes\`（含逐文件 diff） |

## 日常命令

```powershell
$git = "D:\tools\PortableGit\cmd\git.exe"     # 便携版 git
Set-Location D:\easy-stock-main
& $git status                                  # 改之前先确认工作区干净
& $git add -A
& $git commit -m "说明这次改了什么"
```

## 上游更新流程

1. 拉取：`& $git fetch upstream --tags`
2. 看上游改了什么：`& $git log --oneline local..upstream/main`
3. 合入：`& $git merge upstream/main`
4. 冲突处理原则：冲突文件基本都是我们改过的那些（见下）。**纯样式**冲突取我们的；上游**功能或结构**变了的地方取上游的，再把我们的样式贴回去。
5. 验证：前端 `npx vitest run`、`npm run build`；桌面端用本地构建，再抽查顶部栏、表格列宽、报告子区块间距、LOGO。

## 改动集中区（合并时优先看）

- 前端样式：`frontend/src/styles.css`（占总改动一半以上）、`frontend/src/components/*.css`
- 前端组件：`frontend/src/App.tsx`、`components/MarketOverviewWorkspace.tsx`、`components/market/MarketDataViews.tsx`、`components/StockAIAnalysisWorkspace.tsx`、`components/ReviewDiary.tsx`
- 后端：`backend/internal/portfolioinspection/*`、`backend/internal/providers/eastmoney/*`
- 桌面端：`desktop/main.cjs`（Windows 无边框标题栏）
- 构建：`scripts/build-backend.mjs`、`desktop/scripts/dev-electron.mjs`（替换上游的 POSIX 写法）

## 不要做的事

- 不要 `git push`（已禁用）。确实要同步给团队时另说，不要用这个仓库直接推。
- 不要让桌面安装版的自动更新覆盖本地界面；要用本地改动就用本地构建。
