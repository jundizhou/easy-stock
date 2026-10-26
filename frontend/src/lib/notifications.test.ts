import { describe, expect, it } from 'vitest';
import { emptyNotifications, notificationDraft, notificationUpdate } from './notifications';

describe('notification credential updates', () => {
 it('preserves disabled robots even when they have no webhook', () => {
  expect(emptyNotifications().feishu.enabled).toBe(false);
  expect(emptyNotifications().dingtalk.enabled).toBe(false);
  const draft = notificationDraft({ ...emptyNotifications().feishu, enabled: false });
  expect(draft.enabled).toBe(false);
  expect(notificationUpdate(draft).enabled).toBe(false);
  draft.webhook_value = 'https://open.feishu.cn/open-apis/bot/v2/hook/example';
  expect(notificationUpdate(draft).enabled).toBe(false);
  draft.enabled = true;
  expect(notificationUpdate(draft).enabled).toBe(true);
 });
 it('keeps an explicit enable request so the server can report a missing webhook', () => {
  const draft = notificationDraft(emptyNotifications().feishu);
  draft.enabled = true;
  expect(notificationUpdate(draft).enabled).toBe(true);
  draft.clear_webhook = true;
  expect(notificationUpdate(draft).enabled).toBe(false);
 });
 it('preserves a configured robot that was explicitly disabled', () => {
  const draft = notificationDraft({ ...emptyNotifications().dingtalk, enabled: false, webhook: { configured: true } });
  expect(draft.enabled).toBe(false);
  expect(notificationUpdate(draft).enabled).toBe(false);
 });
	it('preserves saved credentials when a masked configuration is loaded and saved', () => {
		const draft = notificationDraft({ ...emptyNotifications().feishu, enabled: true, webhook: { configured: true }, secret: { configured: true } });
		const body = JSON.parse(JSON.stringify(notificationUpdate(draft)));
		expect(body).not.toHaveProperty('webhook');
		expect(body).not.toHaveProperty('secret');
		expect(body).toMatchObject({ enabled: true, clear_webhook: false, clear_secret: false });
	});
	it('sends explicit clearing flags and trims replacements without sending masks', () => {
		const draft = notificationDraft(emptyNotifications().dingtalk);
		draft.webhook_value = ' https://oapi.dingtalk.com/robot/send?access_token=new-token ';
		draft.clear_secret = true;
		expect(notificationUpdate(draft)).toMatchObject({ webhook: 'https://oapi.dingtalk.com/robot/send?access_token=new-token', clear_secret: true });
	});
});
