# 下载与安装

easy-stock 提供 Windows 和 macOS 桌面安装包，已包含所需运行时，无需另行安装 Node.js、Go、Python、Hermes 或 Codex。

## 下载渠道

| 入口 | 用途 |
| --- | --- |
| [GitHub 最新稳定版](https://github.com/jundizhou/easy-stock/releases/latest) | 下载桌面安装包、查看更新说明和 SHA256 校验文件 |
| [Gitee 源码镜像](https://gitee.com/zhoujundi/easy-stock) | 浏览、克隆源码，查看中文文档 |
| [Gitee 发行版](https://gitee.com/zhoujundi/easy-stock/releases) | 查看镜像发行说明，按其中的链接下载对应版本安装包 |

桌面安装包和自动更新统一由 GitHub Releases 分发。Gitee 普通仓库的单个附件上限为 100M，目前桌面安装包超过该限制，因此 Gitee 发行页提供对应的官方安装包链接。页面中的“源码 ZIP / TAR.GZ”是源码归档，不是桌面应用安装包。镜像说明若尚未跟上新版本，以 GitHub 最新稳定版页面为准。

## 选择安装包

| 电脑 | 安装文件 | 如何确认 |
| --- | --- | --- |
| Windows x64 | 文件名以 `windows-x64-setup.exe` 结尾 | 在“设置 → 系统 → 系统信息”查看系统类型 |
| macOS Apple 芯片 | 文件名以 `macos-arm64.dmg` 结尾 | “关于本机”中的芯片为 Apple M 系列 |
| macOS Intel 芯片 | 文件名以 `macos-x64.dmg` 结尾 | “关于本机”中的处理器为 Intel |

ZIP、`blockmap` 和 `latest*.yml` 是更新配套文件。手动安装请选择上表中的 EXE 或 DMG。当前不提供 Windows ARM 或 Linux 桌面安装包。

## 安装与升级

1. 从发行页下载适合电脑系统和芯片的安装包。
2. 升级前退出 easy-stock，保留原有应用数据目录。
3. Windows 运行 EXE 安装包；macOS 打开 DMG 后将应用拖入“应用程序”，升级时覆盖原应用。
4. 启动后按 [使用帮助](./user-guide.md) 配置模型连接与所需的数据源登录。

Windows 可通过应用内“检查更新”下载并安装更新；macOS 请下载对应架构的 DMG 手动覆盖安装。

v1.5.2 及更早版本需要从 GitHub 发行页手动下载并覆盖安装一次，才能切换到 GitHub 更新源。

## 校验文件完整性

下载与安装包处于**同一发行版本**的 `SHA256SUMS.txt`。以下命令中的文件名请替换为实际下载的文件名，再与校验文件中的对应值比较。

Windows PowerShell：

```powershell
Get-FileHash -Algorithm SHA256 .\easy-stock-vX.Y.Z-windows-x64-setup.exe
```

macOS 终端：

```sh
shasum -a 256 ./easy-stock-vX.Y.Z-macos-arm64.dmg
```

