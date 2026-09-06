# 开发版本与推送规则

## 默认目标

- 用户未特别指定版本时，需求默认在开源版开发。
- 开源版远程为 `upstream`：`git@github.com:jundizhou/easy-stock.git`。
- “提交代码”“推送代码”等未注明版本的请求，也按开源版处理。

## 商业版切换条件

只有用户明确提到“商业版”“商业仓库”“commercial”或明确指定 `origin` 时，才允许在商业版开发或推送。

- 商业版远程为 `origin`：`git@github.com:jundizhou/easy-stock-commercial.git`。
- 商业版工作树：`/Users/jundi/GolandProjects/a-stock-ai-worktrees/commercial-auth`。
- 不得因为当前目录、上一次任务或 git tracking 配置而自动选择商业版。

## 开始任务前检查

1. 查看当前工作树、当前分支和两个远程地址。
2. 根据用户是否明确指定版本，确认目标远程。
3. 默认从 `upstream/main` 创建或切换开源开发分支。
4. 商业版专属改动不得回流开源版；公共能力应优先在开源版完成，再由商业版合并。

## 提交与推送

- 默认使用 `codex/<topic>` 分支并推送到 `upstream`。
- 推送前确认远程、目标分支、提交内容和工作树均与目标版本一致。
- 未得到用户明确的商业版指示，不得执行 `git push origin ...`。
- 未经用户要求，不直接推送或合并目标仓库的 `main`；先推送功能分支并提供 Pull Request 链接。

## 版本边界

- 开源版可以包含通用行情、分析、Skill、Hermes 和桌面能力。
- 商业账号、商业凭据、商业服务地址、商业授权校验和商业发行配置属于商业版，不能写入开源版。
- 不确定是否属于商业专属时，按公共能力设计，但不得把商业密钥、服务地址或账号逻辑带入开源版。
