// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { requestJSON } from '../lib/backend';
import { MonitorWorkspace } from './MonitorWorkspace';

vi.mock('../lib/backend', () => ({ requestJSON: vi.fn() }));
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it.each(['detail', 'list'])('在实际展开行展示港股阈值口径（%s）', async (source) => {
	const note = '偏离阈值按 A 股口径估算，仅供观察';
	const item = { symbol: '00700.HK', name: '腾讯控股', board: 'HK', price: 344, level: 'normal', monitored: true, windows: [], signals: [], threshold_note: source === 'list' ? note : undefined };
	vi.stubGlobal('EventSource', class { close() {} });
	vi.mocked(requestJSON).mockImplementation(async (_config, path) => {
		if (path.includes('/abnormal/calc')) return { data: { symbol: item.symbol, benchmark_label: '恒生指数', threshold_note: source === 'detail' ? note : undefined } } as never;
		if (path.includes('/abnormal?')) return { data: { items: [item], total: 1 } } as never;
		if (path.includes('/watchlist')) return { data: [item] } as never;
		if (path.includes('/overview')) return { data: { engine: {}, events_today: 0 } } as never;
		return { data: null } as never;
	});
	render(<MonitorWorkspace config={{ backendUrl: 'http://localhost', token: '' }} refreshKey={0} portfolioRefreshKey={0}
		onOpenSettings={vi.fn()} onOpenNotificationSettings={vi.fn()} onOpenStockAnalysis={vi.fn()} onOpenMarketIndexes={vi.fn()} />);
	fireEvent.click(screen.getByRole('button', { name: '异动预警' }));
	const row = await screen.findByTitle('点击展开该股的异动明细');
	expect(screen.queryByText(note)).toBeNull();
	fireEvent.click(row);
	await screen.findByText('基准 恒生指数');
	expect(screen.getByText(note).closest('tr')?.className).toContain('monitor-abn-detail-row');
});
