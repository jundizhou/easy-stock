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

大V复盘的作者归纳、跨作者总结和 JSON 纠错会启用一次性任务沙箱。沙箱仅开放 Python 结构化计算与只读 Web 检索，遇到工具授权请求时只在当前临时会话内自动允许，因此无需等待人工确认；普通侧边栏对话仍沿用交互式授权。

## 配置与密钥

桌面模式下，Electron 为后端注入：

- `A_STOCK_HERMES_RUNTIME_ROOT`：安装包内的 `resources/hermes-runtime`；
- `A_STOCK_HERMES_HOME`：Electron `userData/hermes-home`；
- `A_STOCK_HERMES_WORKDIR`：Electron `userData/hermes-workspace`；
- `A_STOCK_SETTINGS_PATH` 和 `A_STOCK_REVIEW_DB`：Electron `userData` 内的应用数据。

保存模型设置时：

- 服务商、模型、Base URL、协议写入 `hermes-home/config.yaml`；
- “模型响应等待时间”写入当前 Hermes Provider 的 `stale_timeout_seconds`，并同步写入 `HERMES_API_CALL_STALE_TIMEOUT` 兼容自定义 Provider，默认 300 秒，可在设置中调整为 30–3600 秒；
- 模型 API Key 只写入 `hermes-home/.env` 的 `MODEL_API_KEY`；
- 应用通用 `settings.json` 不保存模型 API Key；
- Hermes 文件权限设置为仅当前用户可读写。

个股研究的一次性调用由 Go 监控首个有效响应与连续无输出的等待时间。实际思考和正文会刷新无输出计时，等待提示不会刷新。调用使用临时 Hermes 配置，把其内部总耗时上限放在 Go 阶段截止时间之后，并停用重复的 Responses 首响应/空闲监控，避免正在正常生成的请求被 300 秒总耗时限制切断。此配置不修改用户保存的模型设置。

深度研究每个模型阶段最多 8 分钟，整个任务最多 24 分钟；快速和标准档每个阶段最多 3 分钟。阶段截止时间涵盖运行时与应用层的所有重试，每阶段最多尝试两次；运行时已经重试后，应用不再叠加重试。中断只保留量化资料和已经完整完成的研究阶段，半截正文不会标为研究成功。

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

运行时固定使用 Hermes GitHub 标签 `v0.22.5` 与 Python 3.14。准备脚本校验源码归档的 SHA-256，保留上游源码及资源文件，并从上游 `uv.lock` 安装带摘要的依赖；同时包含应用支持的 Anthropic 依赖。构建使用上游锁文件中的依赖覆盖，不通过 PyPI 分发包版本推断 GitHub 版本。

可用 `HERMES_AGENT_ARCHIVE=/path/to/archive.tar.gz` 指定离线归档，仍会验证相同摘要。`HERMES_RUNTIME_SOURCE=/path/to/hermes-runtime` 可以复制已准备好的同版本 Runtime；旧版或来源不符的 Runtime 会被拒绝。构建在临时目录完成验证后才替换现有 Runtime。

源码携带上游规定的安装标记。发布校验检查来源摘要、关键源码文件、版本标记、目标平台，以及包内 Python 实际导入的模块路径。CI 还会解压 macOS 更新 ZIP 和 Windows 安装器进行同样的检查；macOS DMG 在挂载后检查。任一校验失败都会阻止上传发布产物。

AI 对话将 `reasoning.delta` 和 `message.complete.payload.reasoning` 单独保存到消息的 `reasoning` 字段，在正文上方的「思考过程」中展示。生成时默认展开，完成后默认折叠，本地历史刷新后仍可查看；模型未返回 reasoning 时不生成思考内容。0.21.3 的 `thinking.delta` 用于等待状态，`reasoning.available` 在当前上游实现中还会携带助手正文预览，因此这两类事件显示为进度提示，不混入思考正文。历史消息若此前没有保存 reasoning，不会自动回填。

macOS 准备脚本会复制 uv 托管的基础 Python，并把 Runtime 内的符号链接实体化；Windows 准备脚本会复制完整的 uv 托管 Python 到 `hermes-runtime/python`，再把构建用 venv 的 `site-packages` 合并进去并删除带有构建机绝对路径的 venv。两个平台都会拒绝指向 Runtime 目录之外的链接，安装后的应用不依赖 GitHub Actions runner 或开发机上的 Python。

发布包生成前会调用包内 Python 导入 `hermes_cli`、`tui_gateway` 和微信公众号服务入口，并用本地模拟模型运行真实 Hermes 会话与思考参数测试。

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
- 自动工具授权只用于大V复盘的一次性沙箱：每次请求使用独立 Home、工作目录和临时目录，结束后删除；不加载用户 MCP、技能、记忆或浏览器登录态，也不开放终端和本机文件工具。
- 沙箱中的 Python 不能读取或修改临时目录之外的文件，不能启动子进程、加载本机原生库或直接访问外网；公开资料检索只能经 Hermes 的只读 Web 工具完成。模型 API Key 仅供 Hermes 网关请求模型，代码执行子进程不会继承它。
- 模型输出仍需核对，特别是实时行情、交易判断和收益相关内容。

## 可选 Parallel Search MCP

普通 Codex 对话可以在「系统设置 → Skill 与 MCP」点击「添加 Parallel 搜索」，再保存设置，启用免费网页搜索与原文抓取。预设使用 [Parallel Search MCP](https://docs.parallel.ai/integrations/mcp/search-mcp) 的 Streamable HTTP 端点，无需账号或 API Key；匿名搜索使用 Fast 模式。它只创建可编辑的草稿，不自动保存，也不覆盖已有 MCP 配置。已有相同服务（包括停用或 OAuth 配置）时，不会重复添加。

MCP 设置由 Hermes 与 Codex 共享：启用后两者的普通对话都可加载这些工具。Hermes 已有原生免密钥网页检索，此预设不更改其原生服务商、默认检索或沙箱工具策略。需要停用时取消该 MCP 卡片的「启用」并保存；删除卡片并保存可移除。工具授权仍沿用当前会话策略：Codex 的 MCP 工具调用会显示服务名称与参数，支持仅本次允许、会话内允许或拒绝；不会自动授予永久权限。禁用工具的任务不会加载该服务。

数据来自第三方公开网页，检索与抓取均按需发起，没有固定更新频率；网页发布时间和原文可能滞后，不能替代行情数据或交易所公告。保留并核对结果中的来源 URL、日期及原文，区分原文事实与模型判断。免费匿名服务有用量限制；连接失败、超时或额度不足时由工具报告错误，不会替用户改用其他服务。需要更高额度可自行编辑请求头添加认证，使用前请阅读 [服务条款](https://parallel.ai/terms-of-service)。此预设不设置 session_id 等工具参数，也不注入全局会话标识。
