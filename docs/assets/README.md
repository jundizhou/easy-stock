# README 配图维护

2026-10-09 按开源版 `main`（功能基线 `e7f02a7`）更新，以下记录配图来源与维护方式。

| 图片 | 来源与用途 |
| --- | --- |
| `easy-stock-short-term-2026-10.jpg` | 2026-10-09 桌面应用行情页面截图，仅包含公开行情；压缩至 1600 像素宽 |
| `easy-stock-stock-research-2026-10.jpg` | 当前 React 组件与虚构数据，截取研究用途、周期、核心判断和正反证据 |
| `easy-stock-portfolio-setup-2026-10.jpg` | 当前持仓配置组件与虚构持仓，展示风格、周期、复用规则和滑杆 |
| `easy-stock-portfolio-report-2026-10.jpg` | 当前巡检组件与虚构数据，截取综合评分、四维明细和风险摘要 |
| `easy-stock-portfolio-optimization-2026-10.jpg` | 当前优化组件与虚构数据，展示评分来源、左右配置对比与资金边界 |
| `easy-stock-theme-index.jpg` | 沿用 v1.4.1 的题材指数 K 线截图，新增到 README 功能介绍 |
| `easy-stock-ai-architecture.svg` | 原架构图同步补充持仓优化与检查点，保留可编辑 SVG |

其余旧图保留供复盘文章及历史文档引用；本次不批量覆盖或删除。README 使用仓库相对路径，使分支预览和版本标签读取各自的图片，避免 CDN 的 `main` 缓存导致图文不一致。

## 重新生成组件预览

在仓库根目录执行：

```sh
npm run build:frontend
node scripts/build-readme-previews.mjs
python3 -m http.server 20119 --bind 127.0.0.1 --directory .runtime/readme-previews
```

访问 `http://127.0.0.1:20119/`。脚本直接渲染当前组件，样式来自刚构建的前端，输出为静态预览页；控件不执行应用操作。它不连接后端、不调用模型，也不读取个人数据库或持仓。

用浏览器截图工具检查桌面布局并保存 JPG。当前截图采用 1440 像素宽的桌面视口；个股研究截至 `.stock-research-arguments` 下沿，巡检报告截至 `.portfolio-report-grid` 下沿，各保留 8 像素底边；配置和优化图保留整页。始终保留顶部「界面示例 · 虚构数据」标记，不得将演示分数标成真实评估结果。

更新配图后同时核对中英文 README、评分维度名称与权重、版本范围、文件路径和图片布局。SVG 修改后也须渲染查看。
