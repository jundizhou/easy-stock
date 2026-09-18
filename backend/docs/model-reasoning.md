# 模型思考能力

思考选项由后端统一提供，聊天与个股分析使用同一个控件。Hermes 的通用等级枚举不是模型能力声明。

## 能力来源

1. 同步模型时优先解析接口明确给出的档位：OpenRouter 使用 Hermes 自带的能力解析器；Anthropic 读取 `capabilities.effort` 与自适应思考声明，并与 Hermes 实际能够原样输出的档位取交集。单独的布尔值或 `supported_parameters` 不能证明支持哪些档位。
2. 接口未提供档位时，复用应用内置 Hermes 的服务商适配与模型能力函数。UI 的“来源：Hermes”表示运行时规则，不表示官方模型列表返回了这些信息。
3. 对 Hermes 尚未覆盖或与国内接口不同的能力，按官方地址、协议和明确模型 ID 补充：国内 GLM、部分 GPT 型号和百炼千问混合思考开关。
4. 未确认的模型或协议显示“暂不支持调节”，使用运行时默认行为；不意味着模型没有推理能力。

模型列表响应包含每个模型的 `reasoning`，设置页面显示选项及来源。接口能力保存在 Hermes 用户目录的 `model-capabilities.json`，按 Base URL、协议及模型区分，有效期 30 天；重新同步替换该路由的记录。缓存不含密钥。

当前文档规则（核对日期：2026-09-18）：

| 模型及官方接口 | 选项 |
| --- | --- |
| 智谱 GLM-5.3、GLM-5.3-Flash | low、high、max；不能关闭 |
| 智谱 GLM-5.3-FlashX | 关闭、low、high、max |
| 智谱 GLM-5.2 | 关闭、high、max（不重复展示映射到相同效果的兼容值） |
| 智谱 GLM-5.1、5、5-Turbo、5V-Turbo、4.7、4.6、4.5 | 开关 |
| OpenAI GPT-5 | minimal、low、medium、high |
| OpenAI GPT-5.1 | none、low、medium、high |
| OpenAI GPT-5.2、GPT-5.5 | none、low、medium、high、xhigh |

文档补充匹配明确列出的官方日期快照，不将其扩展到 Pro、未知版本或代理别名。智谱补充仅适用于官方 Chat Completions 路由。

其他能力复用内置 Hermes（当前 0.21.3）：

| 服务商 | 实现 |
| --- | --- |
| DeepSeek | 使用原生模型别名归一化及 provider 参数转换，展示独立有效档位 |
| Kimi | 使用逐模型能力函数，区分 K2 与 K3，并复用思考开关/强度互斥规则 |
| Z.AI | 使用原生 provider；国内智谱接口采用上表文档补充 |
| GPT | 已列出的新 Responses 型号复用 Hermes 能力函数；文档已核对型号采用上表补充 |
| 千问百炼 | 明确列出的混合思考模型发送 `enable_thinking` 布尔值；纯思考和未知型号不虚构关闭选项 |

这不是整个系列所有型号、服务地区与 API 协议的支持承诺。模型列表没有统一的跨厂商能力字段，Hermes 升级也可能改变原生规则。

来源：

- https://help.aliyun.com/zh/model-studio/deep-thinking
- https://docs.bigmodel.cn/cn/guide/capabilities/thinking
- https://platform.claude.com/docs/en/api/models/list
- https://developers.openai.com/api/docs/models/gpt-5
- https://developers.openai.com/api/docs/models/gpt-5.1
- https://developers.openai.com/api/docs/models/gpt-5.2
- https://developers.openai.com/api/docs/models/gpt-5.5

## 保存与执行

`GET /api/v1/settings/agent` 返回有效选项、当前选择与 `reasoning_context`。保存时校验模型能力；旧上下文返回 409，避免模型切换期间将旧页面的操作写到新模型上。已有无效选项和切换后不兼容的选项回退到已确认的默认档位。模型接口未声明默认值时，明确选择一个已支持的档位（优先 high），不宣称这是服务商默认值。

`agent.easy_stock_reasoning_effort` 保存应用选择；`agent.reasoning_effort` 传给 Hermes。Go 内嵌的 `reasoning_launcher.py` 有两个用途：同步时批量读取本地 Hermes 能力；启动网关时，通过 provider 扩展点连接原生参数转换。没有 SDK 请求拦截，也不修改 Hermes 安装文件。

应用的命名连接在 Hermes 中解析为 `custom`，因此桥接只在网关子进程内注册适配器，按当前模型与服务地址匹配，其余连接交回原来的 custom provider。DeepSeek 别名按 Hermes 的有效模型进行能力翻译。OpenRouter 使用同步得到的能力快照，避免请求时再次读取不同的目录结果。

GLM、DeepSeek、Kimi 和 GPT Chat 的参数转换复用原生 provider；Responses 复用原生 transport 的能力钩子。仅补充百炼布尔开关，以及 OpenAI Responses 关闭时显式传入 `reasoning.effort=none`，防止省略字段后服务端继续默认思考。Anthropic 由 Hermes 的 Messages adapter 处理。

聊天与隔离分析都使用相同桥接，隔离分析读取自己的配置快照。能力发现只读取已有模型元数据和本地代码，不发送模型推理请求或密钥。运行时无法读取时回退到文档补充及未知能力。

## 验证

后端：在 `backend` 运行 `go test ./internal/hermes ./internal/httpapi`。

仓库根目录运行内置 Hermes 的请求构建集成测试（不联网、不调用付费模型）：

```sh
desktop/resources/hermes-runtime/venv/bin/python -B backend/internal/hermes/reasoning_launcher_test.py
```

设置 `HERMES_TEST_PYTHON` 为内置 Python 的绝对路径后运行后端测试，还会验证 Go 到 Python 的真实能力发现链路。Hermes 升级时必须重跑这些测试，验证扩展点与能力函数仍兼容。

前端：在 `frontend` 运行 `npm run build` 和 `npm test -- --run`。
