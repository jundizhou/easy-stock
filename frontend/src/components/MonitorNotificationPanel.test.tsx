// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { requestJSON } from '../lib/backend';
import { emptyNotifications, notificationSettingsChangedEvent } from '../lib/notifications';
import { MonitorNotificationPanel } from './MonitorNotificationPanel';

vi.mock('../lib/backend', () => ({ requestJSON: vi.fn() }));

const config = { backendUrl: 'http://localhost', token: '' };
let robots: ReturnType<typeof emptyNotifications>;
let notifyEnabled: boolean;
let failSave: boolean;
const preferences = () => ({ notify_enabled: notifyEnabled, settings: { enabled: false, quote_interval_sec: 3, snapshot_interval_sec: 15, quiet_hours: '23:00-08:30', digest_seconds: 300 } });

beforeEach(() => {
	robots = emptyNotifications();
	robots.feishu = { ...robots.feishu, enabled: true, webhook: { configured: true } };
	notifyEnabled = false; failSave = false;
	vi.mocked(requestJSON).mockImplementation(async (_config, path, options) => {
		if (path === '/api/v1/settings/notifications') return { data: robots } as never;
		if (options?.method === 'PUT') {
			if (failSave) throw new Error('save failed');
			notifyEnabled = JSON.parse(options.body as string).notify_enabled;
		}
		return { data: preferences() } as never;
	});
});

afterEach(() => { cleanup(); vi.clearAllMocks(); });

it('enables monitor notifications without changing the robot or polling configuration', async () => {
	render(<MonitorNotificationPanel config={config} refreshKey={0} />);
	const toggle = screen.getByRole('switch', { name: '钉钉/飞书机器人通知' });
	await waitFor(() => expect((toggle as HTMLButtonElement).disabled).toBe(false));
	fireEvent.click(toggle);
	await waitFor(() => expect(toggle.getAttribute('aria-checked')).toBe('true'));
	const update = vi.mocked(requestJSON).mock.calls.find(([, , options]) => options?.method === 'PUT');
	expect(update?.[1]).toBe('/api/v1/monitor/settings');
	expect(JSON.parse(update?.[2]?.body as string)).toEqual({ notify_enabled: true });
	expect(screen.getAllByRole('switch')).toHaveLength(1);
	expect(screen.queryByRole('button')).toBeNull();
});

it('explains why notification enablement is unavailable and refreshes after robot settings are saved', async () => {
	robots.feishu.enabled = false;
	render(<MonitorNotificationPanel config={config} refreshKey={0} />);
	const toggle = screen.getByRole('switch');
	await waitFor(() => expect(toggle.getAttribute('title')).toContain('请在系统设置中配置并启用'));
	expect((toggle as HTMLButtonElement).disabled).toBe(true);
	robots.feishu.enabled = true;
	act(() => { window.dispatchEvent(new Event(notificationSettingsChangedEvent)); });
	await waitFor(() => expect((toggle as HTMLButtonElement).disabled).toBe(false));
});

it('keeps the last saved switch state if persistence fails', async () => {
	failSave = true;
	render(<MonitorNotificationPanel config={config} refreshKey={0} />);
	const toggle = screen.getByRole('switch');
	await waitFor(() => expect((toggle as HTMLButtonElement).disabled).toBe(false));
	fireEvent.click(toggle);
	await screen.findByRole('alert');
	expect(toggle.getAttribute('aria-checked')).toBe('false');
});
