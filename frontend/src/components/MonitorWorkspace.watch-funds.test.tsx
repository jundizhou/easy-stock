// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { requestJSON } from '../lib/backend';
import { MonitorWorkspace } from './MonitorWorkspace';

vi.mock('../lib/backend', () => ({ requestJSON: vi.fn() }));

const config = { backendUrl: 'http://localhost', token: '' };
const meta = { source: 'fixture', fetched_at: '2026-11-02T09:32:44Z', available_fields: ['net_inflow', 'net_inflow_ratio', 'main_net_inflow', 'main_net_inflow_ratio', 'retail_net_inflow', 'retail_net_inflow_ratio'] };
let watchSymbol: string;
let flow: Record<string, unknown>;

beforeEach(() => {
	watchSymbol = '688512';
	flow = { symbol: '688512.SH', code: '688512', net_inflow: -2565957.97, net_inflow_ratio: -1.60449, main_net_inflow: -731771.94, main_net_inflow_ratio: -0.457577, retail_net_inflow: -3952597.66, retail_net_inflow_ratio: -2.47156, meta };
	vi.stubGlobal('EventSource', class { close() {} });
	vi.mocked(requestJSON).mockImplementation(async (_config, path) => {
		if (path.includes('/watchlist')) return { data: [{ symbol: watchSymbol, name: '慧智微', have_quote: true, last: 10.83, change_percent: 2.17 }] } as never;
		if (path.includes('/market/flows')) return { data: [flow], meta } as never;
		if (path.includes('/overview')) return { data: { engine: {}, events_today: 0 } } as never;
		if (path.includes('/radar')) return { data: null } as never;
		return { items: [] } as never;
	});
});

afterEach(() => {
	cleanup();
	vi.clearAllMocks();
	vi.unstubAllGlobals();
});

it.each(['688512', '688512.SH', 'sh688512'])('shows fund flows for a watchlist entry stored as %s', async (symbol) => {
	watchSymbol = symbol;
	render(<MonitorWorkspace config={config} refreshKey={0} onOpenMarketIndexes={() => {}} />);
	fireEvent.click(screen.getByRole('button', { name: '关注仓' }));
	await screen.findByText('-256.6万');
	expect(screen.getByText('-1.60%')).toBeTruthy();
	expect(screen.getByText('-73.2万')).toBeTruthy();
	expect(screen.getByText('-0.46%')).toBeTruthy();
	expect(screen.getByText('-395.3万')).toBeTruthy();
	expect(screen.getByText('-2.47%')).toBeTruthy();
});

it('matches a code-only response while keeping unavailable fields distinct from real zero', async () => {
	delete flow.symbol;
	flow.main_net_inflow = 0;
	flow.retail_net_inflow = 0;
	flow.meta = { ...meta, available_fields: ['main_net_inflow'] };
	const { container } = render(<MonitorWorkspace config={config} refreshKey={0} onOpenMarketIndexes={() => {}} />);
	fireEvent.click(screen.getByRole('button', { name: '关注仓' }));
	await waitFor(() => expect(container.querySelector('[data-flow-field="main_net_inflow"]')?.textContent).toBe('0.00亿'));
	expect(container.querySelector('[data-flow-field="retail_net_inflow"]')?.textContent).toBe('--');
	expect(container.querySelector('[data-flow-field="net_inflow"]')?.textContent).toBe('--');
});
