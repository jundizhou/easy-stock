# 微信公众号订阅服务

桌面安装包已经内置固定版本的 `tmwgsicp/wechat-download-api`。App 启动时会在本机回环地址自动启动服务并把地址注入后端，用户不需要安装 Docker、填写 API 地址或配置 Token，只需在“系统设置 → 大V复盘自动化 → 微信公众号”中扫码登录。

登录凭据保存在桌面 App 的用户数据目录，升级安装包不会覆盖，通常约 4 天过期；过期后回到设置页重新扫码即可。

## 开发与独立部署

本目录中的 Docker Compose 只用于开发调试或把服务独立部署到其他机器：

1. 复制环境变量：`cp .env.example .env`
2. 执行：`docker compose up -d`
3. 浏览器访问 `http://127.0.0.1:5000/login.html` 扫码。

如需让非桌面后端连接独立服务，可设置环境变量 `A_STOCK_WECHAT_API_URL=http://127.0.0.1:5000`。

## 第三方许可证

安装包内置的源码固定在提交 `043c2f9828401220a00b7b125686b334581745e0`，遵循 `AGPL-3.0-only`。完整上游源码、`LICENSE` 和源码版本说明会一并放入安装包的 `resources/wechat-download-api` 目录。
