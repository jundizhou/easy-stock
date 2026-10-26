import type { NotificationChannelSettings, NotificationSettings } from './backend';

export const notificationSettingsChangedEvent = 'easy-stock:notification-settings-changed';

export type NotificationChannel = 'feishu' | 'dingtalk';
export type NotificationDraft = NotificationChannelSettings & { webhook_value: string; secret_value: string; clear_webhook: boolean; clear_secret: boolean };
export type NotificationsDraft = Record<NotificationChannel, NotificationDraft> & { events: NotificationSettings['events'] };

export function notificationsDraft(settings: NotificationSettings): NotificationsDraft {
	return { feishu: notificationDraft(settings.feishu), dingtalk: notificationDraft(settings.dingtalk), events: { ...settings.events } };
}

export function notificationsUpdate(draft: NotificationsDraft) {
	return { feishu: notificationUpdate(draft.feishu), dingtalk: notificationUpdate(draft.dingtalk), events: draft.events };
}

export function notificationDraft(settings: NotificationChannelSettings): NotificationDraft {
	return { ...settings, webhook_value: '', secret_value: '', clear_webhook: false, clear_secret: false };
}

export function notificationUpdate(draft: NotificationDraft) {
	return {
		enabled: draft.enabled && !draft.clear_webhook,
		webhook: draft.webhook_value.trim() || undefined,
		secret: draft.secret_value.trim() || undefined,
		keyword: draft.keyword.trim(),
		clear_webhook: draft.clear_webhook,
		clear_secret: draft.clear_secret,
	};
}

export function emptyNotifications(): NotificationSettings {
	const channel = () => ({ enabled: false, webhook: { configured: false }, secret: { configured: false }, keyword: '' });
	return { feishu: channel(), dingtalk: channel(), events: { stock_research: true, portfolio_inspection: true, task_failed: true } };
}
