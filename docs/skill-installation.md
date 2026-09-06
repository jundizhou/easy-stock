# Skill 添加与启用说明

本文说明如何在 easy-stock 的开发环境、Windows release 包和 macOS 版本中添加本机 Skill。

## 1. Skill 的目录结构

应用只会扫描 Hermes Home 下名为 `SKILL.md` 的文件。推荐使用下面的结构：

```text
hermes-home/
└── skills/
    └── trading/                 # 分类名称，可改成 research、finance 等
        └── my-new-skill/
            ├── SKILL.md         # 必需
            ├── references/      # 可选：参考资料
            ├── scripts/         # 可选：脚本
            └── assets/          # 可选：模板或资源
```

`SKILL.md` 至少需要包含 `name` 和 `description`：

```md
---
name: my-new-skill
description: 用户询问某类内容时使用这个 Skill。
---

# 使用说明

1. 收集必要信息。
2. 按照本 Skill 的规则处理。
3. 输出结论，并标明需要进一步核验的内容。
```

`name` 在所有 Skill 中应保持唯一。文件名必须是大写的 `SKILL.md`。

## 2. 开发模式

本项目的开发配置使用：

```text
/Users/jundi/GolandProjects/a-stock-ai/.runtime/hermes-home/skills/
```

例如，新增 Skill 的完整路径为：

```text
.runtime/hermes-home/skills/trading/my-new-skill/SKILL.md
```

如果使用其他工作目录，请以环境变量 `A_STOCK_HERMES_HOME` 指定的目录为准：

```bash
export A_STOCK_HERMES_HOME=/path/to/hermes-home
```

## 3. Windows release 版本

release 包中的程序文件位于安装目录，但用户配置和 Skill 保存在 Electron 的用户数据目录中。默认路径为：

```text
%APPDATA%\easy-stock\hermes-home\skills\trading\my-new-skill\SKILL.md
```

在资源管理器地址栏中输入下面的路径，可以直接打开用户数据目录：

```text
%APPDATA%\easy-stock
```

也可以在 PowerShell 中创建目录：

```powershell
$skillDir = Join-Path $env:APPDATA 'easy-stock\hermes-home\skills\trading\my-new-skill'
New-Item -ItemType Directory -Force -Path $skillDir | Out-Null
notepad (Join-Path $skillDir 'SKILL.md')
```

如果启动参数或环境变量设置了 `A_STOCK_USER_DATA_DIR`，应改用该目录：

```text
<A_STOCK_USER_DATA_DIR>\hermes-home\skills\trading\my-new-skill\SKILL.md
```

不要把 Skill 放在 release 包的 `resources` 目录中。升级或重新安装时该目录可能被替换，而且 release 包校验会拒绝携带本地 `hermes-home`。

## 4. macOS 版本

默认用户数据目录为：

```text
~/Library/Application Support/easy-stock/hermes-home/skills/trading/my-new-skill/SKILL.md
```

在 Finder 中选择“前往 → 前往文件夹…”，输入：

```text
~/Library/Application Support/easy-stock
```

也可以在终端中创建：

```bash
skill_dir="$HOME/Library/Application Support/easy-stock/hermes-home/skills/trading/my-new-skill"
mkdir -p "$skill_dir"
open -a TextEdit "$skill_dir/SKILL.md"
```

如果设置了 `A_STOCK_USER_DATA_DIR`，实际位置为：

```text
<A_STOCK_USER_DATA_DIR>/hermes-home/skills/trading/my-new-skill/SKILL.md
```

不要修改 `.app/Contents/Resources` 下的文件来保存个人 Skill。DMG 和 ZIP 内的程序资源属于安装包内容，个人数据应放在用户数据目录中。

## 5. 在设置中启用

保存 `SKILL.md` 后：

1. 重新打开“设置 → Skill 与 MCP”。
2. 在 Skills 列表中找到新 Skill。
3. 勾选右侧复选框。
4. 点击“保存 Skill/MCP”。

设置页负责启用和停用已经发现的 Skill，本身不负责创建 Skill。如果列表没有更新，请完全退出并重新启动应用，再重新打开设置页。

从当前版本开始，设置页还支持：

- **导入目录**：选择包含一个或多个 `SKILL.md` 的本地目录；
- **导入 ZIP**：选择 Skill 压缩包；
- **从 GitHub 安装**：输入 `https://github.com/用户名/仓库`，应用下载仓库归档并自动查找其中的 Skill。

设置页还提供市场目录入口：

- **A 股精选 Skill**：市场总览、个股研究和盘后复盘可直接一键安装；

- **SkillHub（中国）**：适合发现中文 Skill；
- **SkillsMP、skills.sh（海外）**：适合发现跨平台和社区 Skill。

市场目录负责发现和跳转，安装仍通过 GitHub 仓库或 ZIP 导入完成。这样可以在保留来源可追踪性的同时，避免直接执行市场网页中的内容。

如果仓库包含多个 Skill，建议直接输入某个目录地址，例如：

```text
https://github.com/openai/skills/tree/main/skills/.curated/pdf
```

应用会只安装这个目录，不会把整个仓库的重复 Skill 一起导入。

第三方 Skill 安装后默认保留为启用状态。使用前请检查其 `SKILL.md` 内容；应用不会自动执行 Skill 中的脚本。

## 6. 检查是否生效

确认以下条件：

- 路径位于正确的 `hermes-home/skills` 目录下；
- 文件名为 `SKILL.md`，大小写正确；
- 文件首行是 `---`，并且存在结束的 `---`；
- `name` 没有与其他 Skill 重复；
- 设置页已勾选并保存。

当前项目内置的 Skill 位于：

```text
hermes-home/skills/trading/a-stock-short-term-masters/SKILL.md
```
