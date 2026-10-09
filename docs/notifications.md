# 飞书与钉钉消息通知

在「系统设置 → 消息通知」中配置群自定义机器人。飞书与钉钉各有独立开关，可以同时启用；默认均关闭。配置完成后点击「保存通知配置」，无需重启应用，也无需配置 AI 模型。

## 配置机器人

- 飞书：在群设置中添加自定义机器人，复制 `https://open.feishu.cn/open-apis/bot/v2/hook/…` 格式的 Webhook。国际版 Lark 的 `open.larksuite.com` 地址也受支持。
- 钉钉：在群机器人管理中添加自定义机器人，复制 `https://oapi.dingtalk.com/robot/send?access_token=…` 格式的 Webhook。
- 开启机器人签名校验时，将平台提供的签名密钥填入对应渠道；钉钉加签密钥通常以 `SEC` 开头。
- 开启关键词校验时，填写机器人要求的安全关键词。每条通知会带上该关键词；没有配置时可留空。
- 开启 IP 白名单时，需要在平台侧允许当前网络的公网出口 IP。

「测试发送」会向所选机器人发送一条测试消息，使用当前输入和已保存凭据，不会保存草稿，也不要求先打开通知开关。测试成功后仍需保存配置。发送失败时页面显示 HTTP 或平台错误码，可据此检查签名、关键词和网络。

Webhook 含有机器人访问凭据，与签名密钥一样仅保存在权限为 `0600` 的本机设置文件中。界面和设置接口只显示是否已配置，不返回凭据原文。留空保留原值，输入新值替换；点击清除后保存才会删除凭据。清除 Webhook 会同时关闭该渠道。

## 通知范围

可以分别开启个股 AI 研究完成、持仓 AI 巡检完成，以及所选任务失败或未完成的提醒。消息包含任务标识和报告摘要，完整报告仍在应用中查看。持仓巡检生成新的个股研究时，也会按个股研究开关发送通知。量化速览、手动取消的任务不发送，历史报告读取和进度轮询不重复发送。

通知在应用运行期间发送。任务先保存结果，再进入独立的有界发送队列；机器人响应慢或发送失败不会更改研究结果。请求超时为 10 秒，失败记录到运行日志，不自动重试，以免重复推送。退出应用会取消尚未完成的发送，积压队列不会在重启后补发。

## 接口与实现参考

- `GET /api/v1/settings/notifications`：读取脱敏配置。
- `PUT /api/v1/settings/notifications`：单独更新通知配置，省略的字段保持原值；与模型配置互不依赖。
- `POST /api/v1/settings/notifications/test`：测试指定渠道，可传入未保存的配置。

以上接口沿用本机 API 鉴权。只接受官方机器人 HTTPS 地址，不跟随 HTTP 重定向。飞书使用秒级时间戳，以 `timestamp + "\n" + secret` 为 HMAC-SHA256 密钥签名空消息；钉钉使用毫秒级时间戳，以 `secret` 为密钥签名 `timestamp + "\n" + secret`，再将 Base64 签名写入 URL 查询参数。

接入方式参考以下开源项目的协议处理与发送结构，项目内实现使用 Go 标准库，无新增第三方依赖：

- [daily_stock_analysis：飞书发送器](https://github.com/ZhuLinsen/daily_stock_analysis/blob/main/src/notification_sender/feishu_sender.py)：交互卡片、签名、关键词及业务状态码检查。
- [daily_stock_analysis：钉钉发送器](https://github.com/ZhuLinsen/daily_stock_analysis/blob/main/src/notification_sender/dingtalk_sender.py)：Markdown、毫秒时间戳与 URL 加签。
- [Apprise：DingTalk](https://github.com/caronc/apprise/blob/master/apprise/plugins/dingtalk.py)：钉钉 HMAC-SHA256 加签与请求超时处理。
