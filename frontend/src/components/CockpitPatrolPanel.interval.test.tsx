// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { requestJSON } from '../lib/backend';
import type { PatrolSetting, PatrolStatus } from '../lib/cockpit-patrol';
import { useCockpitPatrol } from '../lib/use-cockpit-patrol';
import { CockpitPatrolPanel } from './CockpitPatrolPanel';

vi.mock('../lib/backend', async (importOriginal) => ({
	...await importOriginal<typeof import('../lib/backend')>(), requestJSON: vi.fn(),
}));
vi.mock('../lib/use-cockpit-live-facts', () => ({
	useCockpitLiveFacts: () => ({ data: null, error: '', loading: false, reload: vi.fn() }),
}));

const config = { backendUrl: 'http://fixture.invalid', token: '' };
let serverStatus: PatrolStatus;
let saveError: Error | null;
let saveGate: ReturnType<typeof deferred<void>> | null;
let delayedStatus: { at: number; gate: ReturnType<typeof deferred<{ data: PatrolStatus }>> } | null;
let statusReads: number;

function deferred<T>() {
	let resolve!: (value: T) => void;
	const promise = new Promise<T>((done) => { resolve = done; });
	return { promise, resolve };
}

beforeEach(() => {
	vi.clearAllMocks();
	serverStatus = { phase: 'intraday', session_date: '2026-11-03', trading_day: true, running: false, ai_available: true, auto_patrol_enabled: false, interval_minutes: 30 };
	saveError = null;
	saveGate = null;
	delayedStatus = null;
	statusReads = 0;
	vi.mocked(requestJSON).mockImplementation(async (_config, path, init) => {
		if (path === '/api/v1/cockpit/status') {
			statusReads += 1;
			if (delayedStatus?.at === statusReads) return delayedStatus.gate.promise as never;
			return { data: { ...serverStatus } } as never;
		}
		if (path.startsWith('/api/v1/cockpit/briefs?')) return { data: [] } as never;
		if (path === '/api/v1/cockpit/auto-patrol') {
			expect(init?.method).toBe('PUT');
			const body = JSON.parse(String(init?.body));
			if (saveGate) await saveGate.promise;
			if (saveError) throw saveError;
			if ('interval_minutes' in body) serverStatus.interval_minutes = body.interval_minutes;
			if ('enabled' in body) serverStatus.auto_patrol_enabled = body.enabled;
			serverStatus.next_run_at = serverStatus.auto_patrol_enabled ? '2026-11-03T10:10:00+08:00' : undefined;
			serverStatus.next_run_label = serverStatus.auto_patrol_enabled ? '盘中巡查 10:10' : undefined;
			return { data: { ...serverStatus } } as never;
		}
		if (path === '/api/v1/cockpit/briefs' && init?.method === 'POST') {
			const { phase } = JSON.parse(String(init.body));
			return { data: { id: 'manual-fixture', phase, status: 'ready', facts: { sources: [] }, session_date: '2026-11-03', started_at: '2026-11-03T10:00:00+08:00' } } as never;
		}
		throw new Error(`未声明的测试请求：${path}`);
	});
});
afterEach(cleanup);

const showPanel = () => render(<CockpitPatrolPanel config={config} refreshKey={0} />);
function intervalButton() { return screen.getByRole('button', { name: '自动巡查设置' }) as HTMLButtonElement; }
async function loadedPanel() {
	const view = showPanel();
	await waitFor(() => expect(intervalButton().disabled).toBe(false));
	return view;
}
async function chooseSetting(setting: PatrolSetting) {
	fireEvent.click(intervalButton());
	fireEvent.click(within(screen.getByRole('listbox', { name: '自动巡查设置' })).getByRole('option', { name: setting === 'off' ? '巡查关闭' : `${setting} 分钟/次` }));
}
function settingsBodies() {
	return vi.mocked(requestJSON).mock.calls.filter((call) => call[1] === '/api/v1/cockpit/auto-patrol').map((call) => JSON.parse(String(call[2]?.body)));
}

describe('统一自动巡查设置', () => {
	it.each(['pre_open', 'intraday', 'post_close'] as const)('%s 顶部将关闭项放在五档频率之前，沿用系统下拉并标明午休', async (phase) => {
		serverStatus.phase = phase;
		await loadedPanel();
		expect(intervalButton().closest('.patrol-frequency')?.getAttribute('title')).toContain('11:30–13:00');
		fireEvent.click(intervalButton());
		const options = within(screen.getByRole('listbox', { name: '自动巡查设置' })).getAllByRole('option');
		expect(options.map((option) => option.textContent)).toEqual(['巡查关闭', '5 分钟/次', '10 分钟/次', '15 分钟/次', '30 分钟/次', '60 分钟/次']);
		expect(options.find((option) => option.getAttribute('aria-selected') === 'true')?.textContent).toBe('巡查关闭');
		expect(screen.queryByRole('switch')).toBeNull();
		expect(document.querySelector('.patrol-auto-control')).toBeNull();
		expect(settingsBodies()).toEqual([]);
	});

	it.each([5, 10, 15, 30, 60] as const)('选择 %i 分钟一次请求同时开启自动巡查和保存频率', async (minutes) => {
		await loadedPanel();
		await chooseSetting(minutes);
		await waitFor(() => expect(intervalButton().disabled).toBe(false));
		expect(intervalButton().textContent).toContain(`${minutes} 分钟/次`);
		expect(settingsBodies()).toEqual([{ enabled: true, interval_minutes: minutes }]);
		expect(serverStatus.auto_patrol_enabled).toBe(true);
		expect(screen.getByText(/会话 2026-11-03/).textContent).toContain('下次 盘中巡查 10:10');
		expect(vi.mocked(requestJSON).mock.calls.some((call) => call[1] === '/api/v1/cockpit/briefs' && call[2]?.method === 'POST')).toBe(false);
	});

	it('开启保存完成前禁用选择并保留已确认的关闭状态', async () => {
		saveGate = deferred<void>();
		await loadedPanel();
		await chooseSetting(10);
		expect(intervalButton().disabled).toBe(true);
		expect(intervalButton().textContent).toContain('巡查关闭');
		await act(async () => { saveGate!.resolve(); });
		await waitFor(() => expect(intervalButton().disabled).toBe(false));
		expect(intervalButton().textContent).toContain('10 分钟/次');
	});

	it('开启失败显示原因，保持关闭状态和原频率', async () => {
		saveError = new Error('测试保存失败');
		await loadedPanel();
		await chooseSetting(15);
		await screen.findByText('测试保存失败');
		expect(intervalButton().textContent).toContain('巡查关闭');
		expect(intervalButton().disabled).toBe(false);
		expect(serverStatus.auto_patrol_enabled).toBe(false);
		expect(serverStatus.interval_minutes).toBe(30);
	});

	it('自动巡查开启时修改频率即时更新下次时间，并保持开关开启', async () => {
		serverStatus.auto_patrol_enabled = true;
		serverStatus.next_run_at = '2026-11-03T10:40:00+08:00';
		serverStatus.next_run_label = '盘中巡查 10:40';
		await loadedPanel();
		await chooseSetting(5);
		await waitFor(() => expect(screen.getByText(/会话 2026-11-03/).textContent).toContain('下次 盘中巡查 10:10'));
		expect(serverStatus.auto_patrol_enabled).toBe(true);
		expect(intervalButton().textContent).toContain('5 分钟/次');
	});

	it('选择巡查关闭只关闭计划、保留频率并隐藏下次计划', async () => {
		serverStatus.auto_patrol_enabled = true;
		serverStatus.interval_minutes = 15;
		serverStatus.next_run_label = '盘中巡查 10:40';
		await loadedPanel();
		await chooseSetting('off');
		await waitFor(() => expect(intervalButton().textContent).toContain('巡查关闭'));
		expect(settingsBodies()).toEqual([{ enabled: false }]);
		expect(serverStatus.interval_minutes).toBe(15);
		expect(serverStatus.auto_patrol_enabled).toBe(false);
		expect(screen.getByText(/会话 2026-11-03/).textContent).not.toContain('下次');
	});

	it('关闭请求完成前保留开启频率，失败后保持原状态', async () => {
		serverStatus.auto_patrol_enabled = true;
		saveGate = deferred<void>();
		saveError = new Error('关闭保存失败');
		await loadedPanel();
		await chooseSetting('off');
		expect(intervalButton().disabled).toBe(true);
		expect(intervalButton().textContent).toContain('30 分钟/次');
		await act(async () => { saveGate!.resolve(); });
		await screen.findByText('关闭保存失败');
		expect(intervalButton().disabled).toBe(false);
		expect(intervalButton().textContent).toContain('30 分钟/次');
		expect(serverStatus.auto_patrol_enabled).toBe(true);
		expect(settingsBodies()).toEqual([{ enabled: false }]);
	});

	it('页面重新打开时读取后端保留的开启状态和频率', async () => {
		const first = await loadedPanel();
		await chooseSetting(60);
		await waitFor(() => expect(intervalButton().textContent).toContain('60 分钟/次'));
		first.unmount();
		await loadedPanel();
		expect(intervalButton().textContent).toContain('60 分钟/次');
		expect(settingsBodies()).toHaveLength(1);
	});

	it('页面重新打开时恢复关闭选项，不将保留频率误认为开启', async () => {
		serverStatus.auto_patrol_enabled = true;
		const first = await loadedPanel();
		await chooseSetting('off');
		await waitFor(() => expect(intervalButton().textContent).toContain('巡查关闭'));
		first.unmount();
		await loadedPanel();
		expect(intervalButton().textContent).toContain('巡查关闭');
		expect(settingsBodies()).toEqual([{ enabled: false }]);
	});

	it('状态读取完成前显示 -- 并禁用控件', async () => {
		const gate = deferred<{ data: PatrolStatus }>();
		delayedStatus = { at: 1, gate };
		showPanel();
		expect(intervalButton().textContent).toContain('--');
		expect(intervalButton().disabled).toBe(true);
		await act(async () => { gate.resolve({ data: { ...serverStatus } }); });
		await waitFor(() => expect(intervalButton().textContent).toContain('巡查关闭'));
	});

	it('开启但没有频率字段时显示 --，仍可主动关闭', async () => {
		serverStatus.auto_patrol_enabled = true;
		delete serverStatus.interval_minutes;
		await loadedPanel();
		expect(intervalButton().textContent).toContain('--');
		await chooseSetting('off');
		await waitFor(() => expect(intervalButton().textContent).toContain('巡查关闭'));
	});

	it('没有开关字段时显示 --，不将未知状态伪装为关闭或开启', async () => {
		delete serverStatus.auto_patrol_enabled;
		await loadedPanel();
		expect(intervalButton().textContent).toContain('--');
		fireEvent.click(intervalButton());
		expect(within(screen.getByRole('listbox')).getAllByRole('option').every((option) => option.getAttribute('aria-selected') === 'false')).toBe(true);
	});

	it('选择频率开启后仍如实显示 AI 未配置', async () => {
		serverStatus.ai_available = false;
		await loadedPanel();
		await chooseSetting(10);
		await waitFor(() => expect(screen.getByText(/会话 2026-11-03/).textContent).toContain('AI 未配置'));
		expect(intervalButton().textContent).toContain('10 分钟/次');
		expect(screen.queryByText('AI 已连接')).toBeNull();
	});

	it('手动巡查继续使用原接口，不发送频率设置', async () => {
		await loadedPanel();
		fireEvent.click(screen.getByRole('button', { name: '立即盘中巡查' }));
		await waitFor(() => expect(vi.mocked(requestJSON).mock.calls.filter((call) => call[1] === '/api/v1/cockpit/briefs')).toHaveLength(1));
		const manual = vi.mocked(requestJSON).mock.calls.find((call) => call[1] === '/api/v1/cockpit/briefs')!;
		expect(JSON.parse(String(manual[2]?.body))).toEqual({ phase: 'intraday' });
		expect(settingsBodies()).toEqual([]);
	});

	it.each([5, 'off'] as const)('保存 %s 后，旧状态慢响应不能覆盖新开关和频率', async (setting) => {
		serverStatus.auto_patrol_enabled = setting === 'off';
		const { result } = renderHook(() => useCockpitPatrol(config, 0));
		await waitFor(() => expect(result.current.status?.interval_minutes).toBe(30));
		const gate = deferred<{ data: PatrolStatus }>();
		delayedStatus = { at: 2, gate };
		const oldStatus = { ...serverStatus };
		let oldLoad!: Promise<void>;
		act(() => { oldLoad = result.current.reload(); });
		await act(async () => { await result.current.changeSetting(setting); });
		const expected = { auto_patrol_enabled: setting !== 'off', interval_minutes: setting === 'off' ? 30 : setting };
		expect(result.current.status).toMatchObject(expected);
		await act(async () => { gate.resolve({ data: oldStatus }); await oldLoad; });
		expect(result.current.status).toMatchObject(expected);
	});

	it('保存过程中忽略重复提交，完成后同时采用后端确认的开关和频率', async () => {
		saveGate = deferred<void>();
		const { result } = renderHook(() => useCockpitPatrol(config, 0));
		await waitFor(() => expect(result.current.status?.auto_patrol_enabled).toBe(false));
		let pending!: Promise<void>;
		act(() => { pending = result.current.changeSetting(5); });
		await act(async () => { await result.current.changeSetting(60); });
		expect(settingsBodies()).toEqual([{ enabled: true, interval_minutes: 5 }]);
		expect(result.current.status).toMatchObject({ auto_patrol_enabled: false, interval_minutes: 30 });
		await act(async () => { saveGate!.resolve(); await pending; });
		expect(result.current.status).toMatchObject({ auto_patrol_enabled: true, interval_minutes: 5 });
		expect(result.current.savingSetting).toBe(false);
	});
});
