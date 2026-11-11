// @vitest-environment jsdom
import { useState } from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { requestJSON, type AppSettings } from '../lib/backend';
import { emptyNotifications, notificationSettingsChangedEvent } from '../lib/notifications';
import { SettingsDrawer } from './SettingsDrawer';
import { PortfolioSchedulePanel } from './PortfolioSchedulePanel';

// Full drawer interactions can exceed Vitest's default timeout on loaded runners.
vi.setConfig({ testTimeout: 30_000 });

vi.mock('../lib/backend', async (importOriginal) => ({ ...await importOriginal<typeof import('../lib/backend')>(), requestJSON: vi.fn() }));
vi.mock('./AgentSettingsPanel', () => ({ AgentSettingsPanel: () => null }));
vi.mock('./AppUpdatePanel', () => ({ AppUpdatePanel: () => null }));

const config = { backendUrl: 'http://localhost', token: '' };
const plan = { id: 'issue37', name: '测试方案', draft: { profile: 'balanced' as const, holdings: [] } };
const feishuWebhook = 'https://open.feishu.cn/open-apis/bot/v2/hook/issue37-test';
const dingtalkWebhook = 'https://oapi.dingtalk.com/robot/send?access_token=issue37-test';
let saved: AppSettings;
let loadError = false;
let saveError = false;

function Harness() {
	const [open, setOpen] = useState(true);
	return <>
		<button onClick={() => setOpen(true)}>打开设置</button>
		<PortfolioSchedulePanel config={config} plan={plan} disabled={false} onBusyChange={() => {}} onOpenSettings={() => setOpen(true)} />
		<SettingsDrawer config={config} open={open} initialSection="notifications" onClose={() => setOpen(false)} />
	</>;
}

beforeEach(() => {
	loadError = false; saveError = false;
	Element.prototype.scrollIntoView = vi.fn();
	const secret = { configured: false };
	saved = {
		agent_runtime: 'hermes', agent: { available: false, configured: false, api_key_configured: false },
		llm: { provider: 'openai', base_url: '', model: '', api_mode: 'chat_completions', response_timeout_seconds: 300, api_key: secret },
		llm_profiles: [], active_llm_profile_id: 'llm-default',
		credentials: { tushare_token: secret, ths_cookie: secret, xueqiu_cookie: secret, eastmoney_cookie: secret },
		review_automation: { profiles: [] }, notifications: emptyNotifications(),
	};
	vi.mocked(requestJSON).mockReset();
	vi.mocked(requestJSON).mockImplementation(async (_config, path, options) => {
		if (path === '/api/v1/settings/agent') return { data: { skills: [], mcp_servers: [], reasoning_effort: 'medium' } };
		if (path.startsWith('/api/v1/portfolio-schedules/')) return { data: null };
		if (path === '/api/v1/settings/notifications/test') return { data: { message: '测试通知已发送' } };
		if (options?.method === 'PUT') {
			if (saveError) throw new Error('保存失败，请重试');
			const body = JSON.parse(String(options.body));
			const patch = path === '/api/v1/settings' ? body.notifications : body;
			// Emulate the server's redacted response; endpoint validation is covered in Go.
			if (patch) {
				for (const channel of ['feishu', 'dingtalk'] as const) {
					const current = saved.notifications![channel];
					const next = patch[channel];
					if (next) saved.notifications![channel] = {
						enabled: next.enabled, keyword: next.keyword,
						webhook: { configured: !next.clear_webhook && (Boolean(next.webhook) || current.webhook.configured) },
						secret: { configured: !next.clear_secret && (Boolean(next.secret) || current.secret.configured) },
					};
				}
				if (patch.events) saved.notifications!.events = patch.events;
			}
		}
		if (path === '/api/v1/settings') {
			if (loadError) throw new Error('读取设置失败');
			return { data: structuredClone(saved) };
		}
		if (path === '/api/v1/settings/notifications') return { data: structuredClone(saved.notifications) };
		throw new Error(`Unexpected endpoint: ${path}`);
	});
});
afterEach(cleanup);

async function loadedDialog() {
	await screen.findByLabelText('飞书 Webhook', { exact: true });
	return within(screen.getByRole('dialog', { name: '系统设置' }));
}

function globalSaveBody() {
	const calls = vi.mocked(requestJSON).mock.calls.filter(([, path, options]) => path === '/api/v1/settings' && options?.method === 'PUT');
	return JSON.parse(String(calls.at(-1)?.[2]?.body));
}

describe('model setup guidance', () => {
	it('opens a spotlight on the provider controls and can be dismissed and restarted', async () => {
		const onClose = vi.fn();
		render(<SettingsDrawer config={config} open initialSection="llm" onClose={onClose} />);
		await screen.findByRole('region', { name: '大模型配置引导' });
		expect(within(screen.getByRole('dialog', { name: '系统设置' })).getByRole('region', { name: '大模型配置引导' })).toBeTruthy();
		const section = screen.getByLabelText('大模型和运行时配置');
		expect(section.closest('details')!.open).toBe(true);
		expect(document.activeElement).toBe(screen.getByLabelText('服务商'));
		expect(document.querySelector('.llm-setup-spotlight')?.getAttribute('data-target')).toBe('provider');
		const providerTarget = document.querySelector('[data-llm-setup-target="provider"]');
		expect(vi.mocked(Element.prototype.scrollIntoView).mock.contexts).not.toContain(providerTarget);
		fireEvent.click(screen.getByRole('button', { name: '稍后配置' }));
		expect(screen.queryByRole('region', { name: '大模型配置引导' })).toBeNull();
		expect(onClose).not.toHaveBeenCalled();
		fireEvent.click(screen.getByRole('button', { name: '开始配置引导' }));
		await screen.findByRole('region', { name: '大模型配置引导' });
		fireEvent.keyDown(window, { key: 'Escape' });
		expect(screen.queryByRole('region', { name: '大模型配置引导' })).toBeNull();
		expect(onClose).not.toHaveBeenCalled();
	});

	it('hides setup instructions for an existing model', async () => {
		saved.agent.configured = true;
		render(<SettingsDrawer config={config} open initialSection="llm" onClose={() => {}} />);
		await screen.findByLabelText('服务商');
		expect(screen.queryByRole('region', { name: '大模型配置引导' })).toBeNull();
	});

	it('requires the key and model before saving, and preserves the guide and inputs on save failure', async () => {
		const onClose = vi.fn();
		render(<SettingsDrawer config={config} open initialSection="llm" onClose={onClose} />);
		const guide = within(await screen.findByRole('region', { name: '大模型配置引导' }));
		fireEvent.click(guide.getByRole('button', { name: '下一步' }));
		expect(document.querySelector('.llm-setup-spotlight')?.getAttribute('data-target')).toBe('key');
		expect((guide.getByRole('button', { name: '下一步' }) as HTMLButtonElement).disabled).toBe(true);
		fireEvent.change(screen.getByLabelText('模型 API Key', { exact: true }), { target: { value: 'fake-tour-key' } });
		fireEvent.click(guide.getByRole('button', { name: '下一步' }));
		expect(document.querySelector('.llm-setup-spotlight')?.getAttribute('data-target')).toBe('model');
		expect((guide.getByRole('button', { name: '下一步' }) as HTMLButtonElement).disabled).toBe(true);
		fireEvent.change(screen.getByPlaceholderText('例如 gpt-5.5 或 deepseek-v4-pro'), { target: { value: 'tour-model' } });
		fireEvent.click(guide.getByRole('button', { name: '下一步' }));
		expect(document.querySelector('.llm-setup-spotlight')?.getAttribute('data-target')).toBe('save');
		expect(vi.mocked(requestJSON).mock.calls.some(([, , options]) => options?.method === 'PUT')).toBe(false);
		saveError = true;
		fireEvent.click(guide.getByRole('button', { name: '保存并开始使用' }));
		await waitFor(() => expect(guide.getByRole('alert').textContent).toContain('保存失败'));
		expect(onClose).not.toHaveBeenCalled();
		expect((screen.getByLabelText('模型 API Key', { exact: true }) as HTMLInputElement).value).toBe('fake-tour-key');
		saveError = false;
		fireEvent.click(guide.getByRole('button', { name: '保存并开始使用' }));
		await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
		expect(globalSaveBody().llm_profiles[0]).toMatchObject({ model: 'tour-model', api_key: 'fake-tour-key' });
	});

	it('allows a keyless custom endpoint and keeps keyboard navigation within the current step', async () => {
		render(<SettingsDrawer config={config} open initialSection="llm" onClose={() => {}} />);
		const guide = within(await screen.findByRole('region', { name: '大模型配置引导' }));
		fireEvent.change(screen.getByLabelText('服务商'), { target: { value: 'custom' } });
		expect((guide.getByRole('button', { name: '下一步' }) as HTMLButtonElement).disabled).toBe(true);
		fireEvent.change(screen.getByLabelText('API Base URL'), { target: { value: 'http://localhost:11434/v1' } });
		fireEvent.click(guide.getByRole('button', { name: '下一步' }));
		expect((guide.getByRole('button', { name: '下一步' }) as HTMLButtonElement).disabled).toBe(false);
		expect(document.activeElement).toBe(screen.getByLabelText('模型 API Key', { exact: true }));
		fireEvent.keyDown(window, { key: 'Tab', shiftKey: true });
		expect(document.activeElement).toBe(guide.getByRole('button', { name: '下一步' }));
		fireEvent.keyDown(window, { key: 'Tab' });
		expect(document.activeElement).toBe(screen.getByLabelText('模型 API Key', { exact: true }));
	});
});

describe('notification settings save flow (issue #37)', () => {
	it('saves tested robot drafts through the global button and refreshes mounted schedule channels', async () => {
		render(<Harness />);
		const dialog = await loadedDialog();
		fireEvent.click(screen.getByText('定时巡检', { exact: true }));
		expect((screen.getByRole('checkbox', { name: /^飞书\s*（需配置并启用）$/ }) as HTMLInputElement).disabled).toBe(true);
		fireEvent.change(dialog.getByLabelText('飞书 Webhook', { exact: true }), { target: { value: feishuWebhook } });
		fireEvent.change(dialog.getByLabelText('钉钉 Webhook', { exact: true }), { target: { value: dingtalkWebhook } });
		fireEvent.change(dialog.getAllByLabelText('安全关键词（可选）')[0], { target: { value: '研究' } });
		for (const checkbox of dialog.getAllByLabelText('启用通知')) fireEvent.click(checkbox);
		fireEvent.click(dialog.getByLabelText('同时提醒所选任务失败或未完成'));
		fireEvent.click(dialog.getAllByRole('button', { name: '测试发送' })[0]);
		await dialog.findByText('测试通知已发送');
		expect(saved.notifications!.feishu.webhook.configured).toBe(false);
		await waitFor(() => expect(dialog.getByRole('button', { name: '保存设置' }).matches(':disabled')).toBe(false));
		fireEvent.click(dialog.getByRole('button', { name: '保存设置' }));
		await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
		expect(globalSaveBody().notifications).toMatchObject({
			feishu: { enabled: true, webhook: feishuWebhook, keyword: '研究' },
			dingtalk: { enabled: true, webhook: dingtalkWebhook }, events: { task_failed: false },
		});
		expect(vi.mocked(requestJSON).mock.calls.some(([, path, options]) => path === '/api/v1/settings/notifications' && options?.method === 'PUT')).toBe(false);
		await waitFor(() => expect((screen.getByRole('checkbox', { name: '飞书' }) as HTMLInputElement).disabled).toBe(false));
		fireEvent.click(screen.getByRole('checkbox', { name: '飞书' }));
		expect((screen.getByRole('checkbox', { name: '飞书' }) as HTMLInputElement).checked).toBe(true);
		fireEvent.click(screen.getByRole('button', { name: '打开设置' }));
		const reopened = await loadedDialog();
		expect(reopened.getAllByText('已配置 Webhook')).toHaveLength(2);
		expect((reopened.getAllByLabelText('安全关键词（可选）')[0] as HTMLInputElement).value).toBe('研究');
		expect((reopened.getByLabelText('飞书 Webhook', { exact: true }) as HTMLInputElement).value).toBe('');
	});

	it.each([false, true])('preserves an explicitly disabled channel after reopening (configured=%s)', async (configured) => {
		saved.notifications!.feishu = { ...saved.notifications!.feishu, enabled: true, webhook: { configured } };
		render(<Harness />);
		const dialog = await loadedDialog();
		fireEvent.click(dialog.getAllByLabelText('启用通知')[0]);
		fireEvent.click(dialog.getByRole('button', { name: '保存设置' }));
		await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
		expect(globalSaveBody().notifications.feishu.enabled).toBe(false);
		fireEvent.click(screen.getByRole('button', { name: '打开设置' }));
		const reopened = await loadedDialog();
		expect((reopened.getAllByLabelText('启用通知')[0] as HTMLInputElement).checked).toBe(false);
	});

	it('keeps drafts and the drawer open when save fails, and retries without losing the webhook', async () => {
		saveError = true;
		const changed = vi.fn();
		window.addEventListener(notificationSettingsChangedEvent, changed);
		try {
			render(<Harness />);
			const dialog = await loadedDialog();
			fireEvent.change(dialog.getByLabelText('飞书 Webhook', { exact: true }), { target: { value: feishuWebhook } });
			fireEvent.click(dialog.getByRole('button', { name: '保存设置' }));
			await dialog.findByText('保存失败：保存失败，请重试');
			expect((dialog.getByLabelText('飞书 Webhook', { exact: true }) as HTMLInputElement).value).toBe(feishuWebhook);
			expect(changed).not.toHaveBeenCalled();
			saveError = false;
			fireEvent.click(dialog.getByRole('button', { name: '保存设置' }));
			await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
			expect(changed).toHaveBeenCalledOnce();
			expect(globalSaveBody().notifications.feishu.webhook).toBe(feishuWebhook);
		} finally { window.removeEventListener(notificationSettingsChangedEvent, changed); }
	});

	it('supports notification-only saving and preserves those credentials on the subsequent global save', async () => {
		render(<Harness />);
		const dialog = await loadedDialog();
		fireEvent.change(dialog.getByLabelText('飞书 Webhook', { exact: true }), { target: { value: feishuWebhook } });
		fireEvent.click(dialog.getAllByLabelText('启用通知')[0]);
		fireEvent.click(dialog.getByRole('button', { name: '仅保存通知配置' }));
		await dialog.findByText('已配置 Webhook');
		expect(dialog.getByLabelText('飞书 Webhook', { exact: true }).closest('details')!.open).toBe(true);
		expect(vi.mocked(requestJSON).mock.calls.filter(([, path, options]) => path === '/api/v1/settings' && options?.method === 'PUT')).toHaveLength(0);
		fireEvent.click(dialog.getByRole('button', { name: '保存设置' }));
		await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
		expect(globalSaveBody().notifications.feishu).not.toHaveProperty('webhook');
		expect(saved.notifications!.feishu.webhook.configured).toBe(true);
	});

	it('waits for test delivery before allowing either save button', async () => {
		let finishTest!: () => void;
		const pending = new Promise<void>((resolve) => { finishTest = resolve; });
		const original = vi.mocked(requestJSON).getMockImplementation()!;
		vi.mocked(requestJSON).mockImplementation(async (...args) => {
			if (args[1] === '/api/v1/settings/notifications/test') await pending;
			return original(...args);
		});
		render(<Harness />);
		const dialog = await loadedDialog();
		fireEvent.change(dialog.getByLabelText('飞书 Webhook', { exact: true }), { target: { value: feishuWebhook } });
		fireEvent.click(dialog.getAllByRole('button', { name: '测试发送' })[0]);
		expect((dialog.getByRole('button', { name: '保存设置' }) as HTMLButtonElement).disabled).toBe(true);
		expect((dialog.getByRole('button', { name: '仅保存通知配置' }) as HTMLButtonElement).disabled).toBe(true);
		await act(async () => { finishTest(); await pending; });
		await dialog.findByText('测试通知已发送');
		expect((dialog.getByRole('button', { name: '保存设置' }) as HTMLButtonElement).disabled).toBe(false);
	});

	it('blocks saving when the settings failed to load', async () => {
		loadError = true;
		render(<Harness />);
		await screen.findByText('读取设置失败');
		expect((screen.getByRole('button', { name: '保存设置' }) as HTMLButtonElement).disabled).toBe(true);
		expect(vi.mocked(requestJSON).mock.calls.some(([, , options]) => options?.method === 'PUT')).toBe(false);
	});
});
