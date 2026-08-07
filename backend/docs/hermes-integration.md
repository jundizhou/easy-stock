# Hermes AI 底座集成

## 目标

项目内所有需要大模型推理的能力统一经过 Hermes：

- 侧边栏 AI 对话；
- 设置页真实模型连接探针；
- 大V复盘文章 AI 提炼。

Go 后端和浏览器不再直接拼装 OpenAI、DeepSeek、Anthropic 等厂商请求。服务商、模型、Base URL 与接口协议仍由用户选择，但它们被转换成 Hermes 的运行配置。

## 运行链路

```text
React AI 对话
  -> GET /api/v1/ai/ws（WebSocket）
  -> Go 后端启动随安装包分发的 Python
  -> python -m tui_gateway.entry
  -> Hermes JSON-RPC
       session.create / session.resume
       prompt.submit
       message.delta / message.complete
       session.interrupt
  -> 用户配置的模型服务
```

每次前端生成请求建立一条本地 WebSocket，并启动一个 Hermes TUI gateway 子进程。前端保存 Hermes 的 `stored_session_id`，下一轮用 `session.resume` 恢复上下文；停止生成时发送 `session.interrupt`。

复盘提炼和连接探针使用同一个 Hermes Runtime 的一次性 Prompt 接口，因此不会回退到模型厂商直连。

## 配置与密钥

桌面模式下，Electron 为后端注入：

- `A_STOCK_HERMES_RUNTIME_ROOT`：安装包内的 `resources/hermes-runtime`；
- `A_STOCK_HERMES_HOME`：Electron `userData/hermes-home`；
- `A_STOCK_HERMES_WORKDIR`：Electron `userData/hermes-workspace`；
- `A_STOCK_SETTINGS_PATH` 和 `A_STOCK_REVIEW_DB`：Electron `userData` 内的应用数据。

保存模型设置时：

- 服务商、模型、Base URL、协议写入 `hermes-home/config.yaml`；
- 模型 API Key 只写入 `hermes-home/.env` 的 `MODEL_API_KEY`；
- 应用通用 `settings.json` 不保存模型 API Key；
- Hermes 文件权限设置为仅当前用户可读写。

启动 TUI gateway 时，Go 后端安全读取 `hermes-home/.env`，仅把 `MODEL_API_KEY` 注入 Hermes 子进程，并覆盖同名的宿主 shell 变量。这样清除设置后不会意外继续使用开发机环境中的旧密钥，页面和日志也不会获得密钥原文。

旧版本若曾在 `settings.json` 保存模型密钥，后端启动时会把它迁移到 Hermes `.env`，随后清除通用设置中的副本。

## 开发模式

准备好 Hermes Runtime 后可直接指定：

```bash
export A_STOCK_HERMES_RUNTIME_ROOT=/path/to/hermes-runtime
export A_STOCK_HERMES_HOME="$PWD/.runtime/hermes-home"
npm run restart
```

Electron 开发模式同样读取这两个变量。若 `desktop/resources/hermes-runtime` 已存在，桌面主进程会自动使用它。

## 安装包

运行时准备脚本支持两种来源：

1. `HERMES_RUNTIME_SOURCE=/path/to/hermes-runtime`：复制一套已验证的 Runtime，并从已安装包读取真实 Hermes 版本；
2. 未指定来源：使用 `uv` 创建 Python 3.11 可迁移环境并安装 `hermes-agent[all]==0.18.2`。

准备脚本会把 Runtime 内的符号链接实体化，并在打包前拒绝任何仍指向 Runtime 目录之外的链接。这样 Electron 复制资源时不会把相对链接改写为开发机绝对路径，DMG 安装到其他目录或其他电脑后仍可直接启动。`runtime-manifest.json` 中记录的是实际安装版本，而不是脚本期望版本。

生成 macOS 应用：

```bash
HERMES_RUNTIME_SOURCE=/path/to/hermes-runtime npm run package:mac
```

生成 DMG 安装包：

```bash
HERMES_RUNTIME_SOURCE=/path/to/hermes-runtime npm run installer:mac
```

Hermes Runtime、Go 后端和前端静态文件都被放入 Electron 的 `Resources/resources`，安装后的应用不依赖开发目录。

## 安全边界

- 后端仅监听 loopback，并由 Electron 随机 token 保护；WebSocket token 通过查询参数传递。
- Hermes Home 与安装包只包含本机配置，不提交密钥、会话数据库或生成运行时。
- 模型输出仍需核对，特别是实时行情、交易判断和收益相关内容。
