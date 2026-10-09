import { describe, expect, it } from 'vitest';
import { emptyNotifications, notificationDraft, notificationUpdate } from './notifications';

describe('notification credential updates', () => {
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
