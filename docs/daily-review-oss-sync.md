# OSS 多作者每日复盘同步方案

## 目标

- 远程配置多位大 V，每位作者每天最多发布一篇复盘。
- 客户端首次启动时立即刷新远程作者清单，并只获取本地缺少的作者文章。
- 工作日 15:00 后每 30 分钟刷新一次作者清单。
- 某位作者的当日文章已写入 SQLite 后，不再重复请求该作者的文章对象。
- 其他作者即使更晚发布，也会在下一轮检查中补同步。
- OSS Bucket 已配置为公共读，发布时不再修改对象 ACL。

## 数据流

```text
管理端粘贴微信 / 雪球 / 淘股吧文章链接
                    ↓
直接请求公开页面并解析作者、标题、正文
                    ↓
更新 authors.json（仅新作者或资料变化时）
                    ↓
上传 {author_id}/YYYY-MM-DD.json
                    ↓
客户端刷新 authors.json
                    ↓
逐作者检查 SQLite，仅下载缺失作者的当日文章
```

## OSS 对象规范

```text
oss://easy-stock-fs/reviews/daily/authors.json
oss://easy-stock-fs/reviews/daily/{author_id}/YYYY-MM-DD.json
```

公开地址：

```text
https://easy-stock-fs.oss-cn-beijing.aliyuncs.com/reviews/daily/authors.json
https://easy-stock-fs.oss-cn-beijing.aliyuncs.com/reviews/daily/{author_id}/YYYY-MM-DD.json
```

### 作者清单

```json
{
  "schema_version": 1,
  "updated_at": "2026-08-11T16:00:00Z",
  "authors": [
    {
      "id": "wechat-0123456789abcdef",
      "name": "复盘作者",
      "platform": "wechat",
      "enabled": true
    }
  ]
}
```

作者 ID 根据“平台 + 作者名称”稳定生成。管理端首次发布某作者时自动加入清单。

### 作者每日文章

```json
{
  "schema_version": 1,
  "trade_date": "2026-08-11",
  "id": "official-20260811-wechat-0123456789abcdef",
  "external_id": "daily:2026-08-11:wechat-0123456789abcdef",
  "author_id": "wechat-0123456789abcdef",
  "author_name": "复盘作者",
  "platform": "wechat",
  "title": "8月11日复盘",
  "digest": "摘要",
  "content_text": "纯文本正文",
  "content_sha256": "正文 SHA-256",
  "source_url": "https://原文地址",
  "published_at": "2026-08-11T16:00:00+08:00",
  "related_stocks": [],
  "related_themes": []
}
```

同一作者的日期对象不可覆盖。客户端会校验协议版本、日期、作者 ID、外部 ID 和正文 SHA-256。

## 客户端调度

1. 本机 Go 后端启动时立即读取 `authors.json`。
2. 对清单内每位启用作者查询 SQLite：
   - 已存在 `daily:日期:author_id`：不请求该作者文章。
   - 不存在：请求作者日期对象。
3. 工作日 15:00 后每 30 分钟再次刷新 `authors.json`。
4. 清单支持 ETag；未变化时可返回 `304`。
5. 即使当前作者均已同步，仍刷新小型作者清单，以发现当天晚新增的作者。
6. 新作者出现后，只请求新作者的当日文章，其他作者不会重复下载。

客户端地址可覆盖：

```text
A_STOCK_DAILY_REVIEW_BASE_URL=https://your-domain.example/reviews/daily
```

## 管理端链接解析

独立管理后台位于：

```text
/Users/jundi/PycharmProjects/easy-stock-ai-backend
```

它是通过端口运行的 Flask 动态网站，默认地址为 `http://127.0.0.1:20100`。启动方式：

```bash
cd /Users/jundi/PycharmProjects/easy-stock-ai-backend
./start.sh
```

管理端支持：

- 微信公众号具体文章链接。
- 雪球具体文章链接。
- 淘股吧具体文章链接。

解析使用普通 HTTP 请求，不打开浏览器、不复用登录态。如果平台对公开请求返回风控页或要求登录，管理端会明确报错，管理员仍可手工粘贴正文后发布。

管理端使用 SQLite 保存本机发布审计记录，并通过本机 `ossutil` 发布对象。Bucket 已是公共读，上传命令不包含对象 ACL 参数。

## 安全和运维

- Bucket 已为公共读，文章和作者清单中不能写入私密信息。
- 管理端仍必须配置强密码并部署在 HTTPS 后。
- `ossutil` 建议使用仅允许目标 Bucket/前缀写入的 RAM 用户。
- 作者清单使用 `no-cache`，作者日期文章使用一年不可变缓存。
- 建议配置 OSS 流量告警和防盗链。
