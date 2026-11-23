// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { CockpitWorkspace } from './CockpitWorkspace';

vi.mock('./CockpitPatrolPanel', () => ({ CockpitLiveModules: () => null, CockpitPatrolPanel: () => null }));

afterEach(() => {
	cleanup();
	localStorage.clear();
	vi.unstubAllGlobals();
	vi.useRealTimers();
});

it('逐条关闭提醒，同步待处理数量和简报，当天重新打开仍生效', async () => {
	vi.useFakeTimers({ toFake: ['Date'] });
	vi.setSystemTime(new Date('2026-11-23T15:59:00Z'));
	const shanghai = { id: 'shanghai', secid: '1.000001', name: '上证指数', change_percent: -1.12 };
	vi.stubGlobal('fetch', async (url: URL) => {
		if (url.pathname.endsWith('/indexes')) return Response.json({ data: [
			shanghai,
			{ secid: '1.000300', name: '沪深300', change_percent: -1.13 },
		] });
		return Response.json({});
	});
	const props = { config: { backendUrl: 'http://localhost', token: '' }, refreshKey: 0, onNavigate: vi.fn(), onAskAI: vi.fn(), onRefresh: vi.fn() };
	const first = render(<CockpitWorkspace {...props} />);
	fireEvent.click(await screen.findByRole('button', { name: '关闭提醒：上证指数 下跌 1.12%' }));
	expect(props.onNavigate).not.toHaveBeenCalled();
	expect(screen.getByLabelText('今日态势').textContent).toContain('1项需要你处理');
	fireEvent.click(screen.getByRole('button', { name: '生成 AI 简报' }));
	expect(props.onAskAI.mock.lastCall?.[0]).toContain('需要处理：[中] 沪深300 下跌 1.13%');
	fireEvent.click(screen.getByRole('button', { name: /^中 沪深300/ }));
	expect(props.onNavigate).toHaveBeenCalledWith('market');

	first.unmount();
	shanghai.change_percent = -1.25;
	shanghai.secid = 'sh000001';
	const reopened = render(<CockpitWorkspace {...props} />);
	fireEvent.click(await screen.findByRole('button', { name: '关闭提醒：沪深300 下跌 1.13%' }));
	expect(screen.getByText('当前提醒已全部关闭')).toBeTruthy();
	expect(screen.getByLabelText('今日态势').textContent).toContain('当前没有需要处理的事项');

	vi.setSystemTime(new Date('2026-11-23T16:01:00Z'));
	reopened.rerender(<CockpitWorkspace {...props} refreshKey={1} />);
	expect(await screen.findByRole('button', { name: '关闭提醒：上证指数 下跌 1.25%' })).toBeTruthy();
	expect(screen.getAllByRole('button', { name: /^关闭提醒/ })).toHaveLength(2);
});
