# easy-stock 开发者文档

本文面向需要从源码运行、调试、测试、扩展或打包 easy-stock 的开发者。普通用户请直接前往 [GitHub Releases](https://github.com/jundizhou/easy-stock/releases/latest) 下载桌面版本。

## 目录

- [开发环境](#开发环境)
- [获取代码与安装依赖](#获取代码与安装依赖)
- [启动项目](#启动项目)
- [项目结构](#项目结构)
- [配置与环境变量](#配置与环境变量)
- [测试](#测试)
- [构建与桌面打包](#构建与桌面打包)
- [本地数据与日志](#本地数据与日志)
- [相关技术文档](#相关技术文档)
- [常见问题](#常见问题)

## 开发环境

必需工具：

| 工具 | 建议版本 | 用途 |
| --- | --- | --- |
| Git | 当前稳定版 | 获取和管理源码 |
| Node.js | 22.x | 前端、Electron、工作区脚本和发布流水线 |
| npm | 随 Node.js 安装 | 管理根目录、`frontend` 和 `desktop` 工作区 |
| Go | 1.26，以 [`backend/go.mod`](../backend/go.mod) 为准 | Go 后端与桌面内置后端 |

以下工具只在使用对应能力时需要：

| 工具 | 使用场景 |
| --- | --- |
| Python 3.11 与 `uv` | 准备包含 Hermes 和微信公众号服务的桌面发布包 |
| Hermes Runtime | 本地调试 AI 对话、文章分析、每日共识和模型连接 |
| macOS `hdiutil` | 生成 macOS DMG |
| PowerShell | 生成 Windows 发布压缩包 |

桌面发布包必须在目标操作系统和目标 CPU 架构的原生环境中构建，不能在单台机器上跨架构打包包含原生运行时的完整版本。

## 获取代码与安装依赖

```bash
git clone https://github.com/jundizhou/easy-stock.git
cd easy-stock
npm ci
```

`npm ci` 会根据根目录的 `package-lock.json` 安装前端、Electron 和浏览器辅助依赖。日常更新代码后，如果锁文件发生变化，应重新运行该命令。

## 启动项目

### 一键启动 Web 工作台

```bash
npm run restart
```

该命令会构建前后端、停止默认端口上的旧开发进程，并在后台启动新的本地服务：

| 服务 | 默认地址 |
| --- | --- |
| 前端 | `http://127.0.0.1:20073` |
| 后端 | `http://127.0.0.1:20081` |
| 运行日志 | `.runtime/` |

适合快速验证完整 Web 工作台。修改代码后需要再次执行 `npm run restart` 才会使用新的构建结果。

### 分别启动前后端

需要前端热更新时，建议分别启动服务。

终端 1：

```bash
npm run dev:backend
```

终端 2：

```bash
VITE_A_STOCK_BACKEND_URL=http://127.0.0.1:20081 npm run dev:frontend
```

前端默认运行在 `127.0.0.1:20073`，后端默认运行在 `127.0.0.1:20081`。开发模式未设置 `A_STOCK_TOKEN` 时，本机 API 不要求鉴权。

### 启动 Electron 桌面开发模式

终端 1：

```bash
npm run dev:frontend
```

终端 2：

```bash
npm run dev:desktop
```

Electron 会自动选择本机空闲端口、生成一次性 Token，并启动 Go 后端。若 `desktop/bin/easy-stock-backend` 不存在，开发模式会使用 `go run ./cmd/server`。

## 项目结构

```text
backend/                 Go 数据基座、领域模型、Provider、HTTP/WebSocket API
  cmd/server/            后端入口
  internal/foundation/   通用行情模型、股票代码规范化和来源元数据
  internal/providers/    东方财富、新浪、财联社、短线侠等数据源
  internal/httpapi/      API 路由、缓存、鉴权和业务编排
  internal/stockanalysis 个股多路径分析引擎
  docs/                  后端架构、API、数据源和专项设计文档
frontend/                React + TypeScript + Vite 工作台
desktop/                 Electron 主进程、浏览器会话、打包和发布脚本
docs/                    项目级文档与 README 图片资源
scripts/                 本地构建、重启和开发辅助脚本
.github/workflows/       GitHub Actions 测试与桌面发布流水线
```

运行关系：

```text
React 页面
  -> Go HTTP / WebSocket API
     -> 行情与内容 Provider
     -> SQLite、本地缓存和设置
     -> Hermes Runtime

Electron 桌面模式
  -> 启动本机 Go 后端并注入一次性 Token
  -> 管理用户数据目录和持久浏览器会话
  -> 加载打包后的 React 页面
```

## 配置与环境变量

### Web 开发常用变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `A_STOCK_ADDR` | `127.0.0.1:20081` | 后端监听地址 |
| `A_STOCK_TOKEN` | 空 | 可选的本机 API Bearer Token |
| `A_STOCK_FRONTEND_HOST` | `127.0.0.1` | `npm run restart` 使用的前端地址 |
| `A_STOCK_FRONTEND_PORT` | `20073` | `npm run restart` 使用的前端端口 |
| `VITE_A_STOCK_BACKEND_URL` | `http://127.0.0.1:20081` | 前端连接的后端地址 |
| `VITE_A_STOCK_TOKEN` | 空 | 前端请求携带的后端 Token |

### 数据和运行时变量

| 变量 | 说明 |
| --- | --- |
| `A_STOCK_SETTINGS_PATH` | 设置文件路径 |
| `A_STOCK_REVIEW_DB` | 复盘文章 SQLite 路径 |
| `A_STOCK_MARKET_EMOTION_DB` | 市场情绪历史 SQLite 路径 |
| `A_STOCK_THEME_RADAR_DB` | 趋势题材与短线侠快照 SQLite 路径 |
| `A_STOCK_MASTERY_CACHE` | 游资心法缓存目录 |
| `A_STOCK_DUANXIANXIA_BASE_URL` | 短线侠服务地址覆盖，主要用于测试 |
| `A_STOCK_WECHAT_API_URL` | 微信公众号采集服务地址 |

### Hermes 开发配置

```bash
export A_STOCK_HERMES_RUNTIME_ROOT=/absolute/path/to/hermes-runtime
export A_STOCK_HERMES_HOME="$PWD/.runtime/hermes-home"
export A_STOCK_HERMES_WORKDIR="$PWD/.runtime/hermes-workspace"
```

可选的 `A_STOCK_HERMES_PYTHON` 用于指定 Runtime 中的 Python。随后在应用的「系统设置 → 模型服务」中配置服务商、模型、Base URL 和 API Key。

模型 API Key 只应保存在 Hermes Home 的 `.env` 中。不要把 `.runtime`、数据库、Cookie、浏览器状态、设置文件或任何密钥提交到 Git。

## 测试

### 默认测试

```bash
npm test
```

根目录测试会运行 Go 后端全部测试和前端 Vitest 测试。桌面主进程测试需单独执行：

```bash
npm --workspace desktop test
```

### 分模块测试与构建

```bash
cd backend && go test ./...
npm --workspace frontend test -- --run
npm --workspace frontend run build
npm --workspace desktop test
```

### 外部数据源实时测试

默认测试不会访问公网数据源。需要检查真实行情或内容接口时：

```bash
cd backend
A_STOCK_LIVE_TEST=1 go test ./internal/httpapi ./internal/providers -run Live -v
```

公网接口可能受到网络、限流、反爬和上游字段变化影响。实时测试失败首先应作为数据源健康信号排查，不应直接视为确定性代码回归。

## 构建与桌面打包

### 常规构建

```bash
npm run build:backend
npm run build:frontend
npm run build:desktop
```

`build:desktop` 会构建 Go 后端和前端，并运行 Electron 工作区的构建检查；它不会生成最终安装包。

### 准备桌面资源

```bash
npm run prepare:desktop-package
```

该步骤会构建前端和 Go 后端，并准备 Hermes、微信公众号服务及浏览器辅助程序到 `desktop/resources/`。

如果已经有验证过的 Hermes Runtime，可以显式指定：

```bash
HERMES_RUNTIME_SOURCE=/absolute/path/to/hermes-runtime npm run prepare:desktop-package
```

未指定 Runtime 来源时，脚本会通过 `uv` 创建 Python 3.11 可迁移环境并安装项目锁定的 Hermes 版本。

### macOS

```bash
npm run package:mac   # 生成 .app
npm run release:mac   # 生成 DMG 和 ZIP
```

通过 `A_STOCK_DESKTOP_ARCH=arm64` 或 `A_STOCK_DESKTOP_ARCH=x64` 指定架构；目标架构必须与当前构建机一致。

### Windows

```bash
npm run package:windows   # 生成应用目录
npm run release:windows   # 生成 ZIP
```

Windows 正式发布当前使用 `x64`。构建产物位于 `desktop/dist/`，该目录不会提交到 Git。

### GitHub Release

推送与 `desktop/package.json` 版本一致的 `v*` 标签后，[桌面发布工作流](../.github/workflows/release.yml) 会运行测试，并分别生成 macOS Intel、macOS Apple Silicon 和 Windows x64 产物，最后发布到 GitHub Releases。

## 本地数据与日志

Web 开发的一键启动模式使用：

```text
.runtime/backend.log
.runtime/frontend.log
.runtime/backend.pid
.runtime/frontend.pid
```

Go 后端在未显式配置路径时，会使用操作系统用户配置目录下的 `easy-stock` 数据目录。Electron 使用系统提供的 `userData` 目录保存：

- 应用设置；
- 复盘、情绪和题材数据库；
- Hermes 配置、密钥和会话；
- 游资心法缓存；
- 雪球、淘股吧等持久浏览器会话。

可使用 `A_STOCK_USER_DATA_DIR` 覆盖 Electron 用户数据目录。清理这些目录会删除本机配置、登录态和历史数据，操作前应先备份。

## 相关技术文档

- [后端架构](../backend/docs/architecture.md)
- [API 路由](../backend/docs/api-routes.md)
- [数据源与降级规则](../backend/docs/data-sources.md)
- [Hermes 集成](../backend/docs/hermes-integration.md)
- [趋势题材地图](../backend/docs/sector-map.md)
- [实时数据源测试](../backend/docs/live-tests.md)
- [个股拐点引擎](../backend/docs/inflection-engine.md)

## 常见问题

### 默认端口被占用

`npm run restart` 会停止默认端口上的旧监听进程。若不希望影响已有服务，请设置新的地址和端口：

```bash
A_STOCK_ADDR=127.0.0.1:21081 \
A_STOCK_FRONTEND_PORT=21073 \
npm run restart
```

### 页面显示后端未连接

先检查健康接口和后端日志：

```bash
curl http://127.0.0.1:20081/api/health
tail -f .runtime/backend.log
```

分别启动服务时，确认 `VITE_A_STOCK_BACKEND_URL` 与后端实际地址一致；启用了 `A_STOCK_TOKEN` 时，还需同时配置 `VITE_A_STOCK_TOKEN`。

### AI 功能不可用

确认 `A_STOCK_HERMES_RUNTIME_ROOT` 指向完整 Runtime，并在设置页执行模型连接测试。更详细的运行链路和密钥规则见 [Hermes 集成文档](../backend/docs/hermes-integration.md)。

### 行情或题材数据暂时为空

先查看页面显示的来源、更新时间和降级信息，再检查 [数据源文档](../backend/docs/data-sources.md)。东方财富、新浪、财联社和短线侠等公共接口可能临时限流或调整字段。
