// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MarketOverviewWorkspace } from './MarketOverviewWorkspace';
import { FundFlowView } from './market/MarketDataViews';
import { requestJSON, type KLine, type MarketFundFlow, type SourceMeta } from '../lib/backend';
import { StockKLineModal } from './StockKLineModal';

vi.mock('../lib/backend', async (importOriginal) => ({
	...await importOriginal<typeof import('../lib/backend')>(), requestJSON: vi.fn(),
}));

const config = { backendUrl: 'http://fixture.invalid', token: '' };
const meta: SourceMeta = {
	source: 'sina:stock-money-flow', fetched_at: '2026-11-02T15:05:00+08:00', latency_ms: 1, stale: false,
	available_fields: ['price', 'change_percent', 'net_inflow', 'main_net_inflow'],
};
function row(code: string, name: string, net: number, main = net): MarketFundFlow {
	return {
		dimension: 'stock', code, symbol: `${code}.SH`, name, price: 10, change_percent: 1,
		inflow: 0, outflow: 0, net_inflow: net, net_inflow_ratio: 0,
		main_inflow: 0, main_outflow: 0, main_net_inflow: main, main_net_inflow_ratio: 0,
		retail_inflow: 0, retail_outflow: 0, retail_net_inflow: 0, retail_net_inflow_ratio: 0,
		super_large_net_inflow: 0, super_large_net_inflow_ratio: 0, large_net_inflow: 0, large_net_inflow_ratio: 0,
		medium_net_inflow: 0, medium_net_inflow_ratio: 0, small_net_inflow: 0, small_net_inflow_ratio: 0,
		leader_price: 0, leader_change_percent: 0, leader_net_inflow_ratio: 0, meta,
	};
}
const inflows = [row('600001', '测试流入较小', 100), row('600002', '测试流入较大', 900), row('600003', '测试反向记录', -10)];
const outflows = [row('600004', '测试流出较小', -100), row('600005', '测试流出较大', -900), row('600006', '测试反向正值', 10)];
type Payload = { data: MarketFundFlow[]; meta: SourceMeta };
const dimensions = ['stock', 'industry', 'theme'] as const;
function payloadFor(dimension: typeof dimensions[number], direction: 'inflow' | 'outflow'): Payload {
	const items = direction === 'outflow' ? outflows : inflows;
	if (dimension === 'stock') return { data: items, meta };
	const sectorMeta = { ...meta, source: `sina:${dimension}-money-flow`, available_fields: ['price', 'change_percent', 'net_inflow'] };
	return { data: items.map((item) => ({ ...item, dimension, code: `BK${item.code}`, symbol: undefined, meta: sectorMeta })), meta: sectorMeta };
}
function deferred<T = Payload>() {
	let resolve!: (payload: T) => void;
	let reject!: (error: Error) => void;
	const promise = new Promise<T>((finish, fail) => { resolve = finish; reject = fail; });
	return { promise, resolve, reject };
}
function tableNames(container: HTMLElement) {
	return [...container.querySelectorAll('.market-data-table article')].map((item) => item.getAttribute('data-name') || item.firstElementChild?.querySelector('strong')?.textContent);
}
function openWorkspace() {
	return render(<MarketOverviewWorkspace config={config} refreshKey={0} onAskAI={vi.fn()} />);
}

beforeEach(() => {
	window.history.replaceState(null, '', '#market/stock-flow');
	window.localStorage.clear();
	vi.mocked(requestJSON).mockImplementation(async (_config, path) => {
		const params = new URL(path, config.backendUrl).searchParams;
		return payloadFor(params.get('dimension') as typeof dimensions[number], params.get('sort') === 'outflow' ? 'outflow' : 'inflow') as never;
	});
});

type KLinePayload = { data: KLine[] };
function testKLine(symbol = '600002.SH', close = 12.34, time = '2026-11-02T15:00:00+08:00'): KLine {
	return { symbol, time, open: close - 1, high: close + 1, low: close - 2, close, volume: 10000, amount: 120000, change_percent: 1.5, meta };
}
function mockKLineRequests(load: (path: string, init?: RequestInit) => Promise<KLinePayload> = async (path) => ({ data: [testKLine(new URL(path, config.backendUrl).searchParams.get('symbol')!)] })) {
	vi.mocked(requestJSON).mockImplementation((_config, path, init) => {
		if (path.startsWith('/api/v1/quotes/kline?')) return load(path, init) as never;
		const params = new URL(path, config.backendUrl).searchParams;
		return Promise.resolve(payloadFor(params.get('dimension') as typeof dimensions[number], params.get('sort') === 'outflow' ? 'outflow' : 'inflow')) as never;
	});
}
function kLineCalls() {
	return vi.mocked(requestJSON).mock.calls.filter((call) => call[1].startsWith('/api/v1/quotes/kline?'));
}

describe('个股资金 K 线弹窗', () => {
	it.each(['inflow', 'outflow'] as const)('%s 点击个股名称请求该股真实接口，默认日K，关闭后保留榜单', async (direction) => {
		mockKLineRequests();
		const { container } = openWorkspace();
		await screen.findByText('榜首净流入');
		if (direction === 'outflow') {
			fireEvent.click(screen.getByRole('button', { name: '资金流出' }));
			await screen.findByText('榜首净流出');
		}
		const name = direction === 'outflow' ? '测试流出较大' : '测试流入较大';
		const symbol = direction === 'outflow' ? '600005.SH' : '600002.SH';
		const before = tableNames(container);
		const stockRow = screen.getByRole('button', { name: `查看${name} K线` });
		fireEvent.click(stockRow.querySelector('strong')!);
		const dialog = await screen.findByRole('dialog', { name: `${name} K线` });
		await within(dialog).findByRole('img', { name: '日K价格、涨跌幅和成交量图' });
		expect(within(dialog).getByRole('tab', { name: '日K' }).getAttribute('aria-selected')).toBe('true');
		expect(within(dialog).getByText('最新').parentElement?.querySelector('strong')?.textContent).toBe('12.34');
		expect(within(dialog).getByText('个股资金 · 日K')).toBeTruthy();
		expect(within(dialog).getByText(`资金${direction === 'outflow' ? '流出' : '流入'}榜 · 新浪总资金 / 主力 / 散户`)).toBeTruthy();
		expect(kLineCalls()).toHaveLength(1);
		expect(kLineCalls()[0][0]).toBe(config);
		expect(kLineCalls()[0][1]).toBe(`/api/v1/quotes/kline?symbol=${symbol}&period=day&limit=120`);
		fireEvent.click(within(dialog).getByRole('button', { name: '关闭 K 线弹窗' }));
		expect(screen.queryByRole('dialog')).toBeNull();
		expect(tableNames(container)).toEqual(before);
	});

	it('五个周期按对应接口加载，分时只保留最新交易日，返回已加载周期使用本次弹窗缓存', async () => {
		mockKLineRequests(async () => ({ data: [testKLine('600002.SH', 11, '2026-10-30T15:00:00+08:00'), testKLine()] }));
		openWorkspace();
		fireEvent.click(await screen.findByRole('button', { name: '查看测试流入较大 K线' }));
		await screen.findByRole('img', { name: '日K价格、涨跌幅和成交量图' });
		for (const [label, period, limit] of [['分时', '1', 240], ['5日', '5', 240], ['周K', 'week', 80], ['月K', 'month', 60]] as const) {
			fireEvent.click(screen.getByRole('tab', { name: label }));
			await screen.findByRole('img', { name: `${label}价格、涨跌幅和成交量图` });
			expect(kLineCalls().at(-1)?.[1]).toBe(`/api/v1/quotes/kline?symbol=600002.SH&period=${period}&limit=${limit}`);
			if (label === '分时') expect(screen.getByText('分时 · 1 个数据点')).toBeTruthy();
			if (label === '5日') expect(screen.getByText('5日 · 2 个数据点')).toBeTruthy();
		}
		fireEvent.click(screen.getByRole('tab', { name: '日K' }));
		await screen.findByRole('img', { name: '日K价格、涨跌幅和成交量图' });
		expect(kLineCalls()).toHaveLength(5);
	});

	it.each(['Enter', ' '])('右键保留原行为，键盘 %s 可打开，Esc 可关闭', async (key) => {
		mockKLineRequests();
		openWorkspace();
		const stockRow = await screen.findByRole('button', { name: '查看测试流入较大 K线' });
		fireEvent.contextMenu(stockRow);
		expect(screen.queryByRole('dialog')).toBeNull();
		expect(stockRow.getAttribute('data-stock-row')).toBe('fund-flow');
		expect(stockRow.getAttribute('data-symbol')).toBe('600002.SH');
		stockRow.focus();
		fireEvent.keyDown(stockRow, { key });
		await screen.findByRole('img', { name: '日K价格、涨跌幅和成交量图' });
		fireEvent.keyDown(window, { key: 'Escape' });
		expect(screen.queryByRole('dialog')).toBeNull();
	});

	it.each(['success', 'error'] as const)('快速切周期后旧请求迟到 %s 不覆盖当前周K', async (outcome) => {
		const pending = deferred<KLinePayload>();
		mockKLineRequests((path) => new URL(path, config.backendUrl).searchParams.get('period') === 'day' ? pending.promise : Promise.resolve({ data: [testKLine('600002.SH', 22.22)] }));
		openWorkspace();
		fireEvent.click(await screen.findByRole('button', { name: '查看测试流入较大 K线' }));
		await screen.findByText('正在加载日K数据…');
		const oldSignal = kLineCalls()[0][2]?.signal;
		fireEvent.click(screen.getByRole('tab', { name: '周K' }));
		await screen.findByRole('img', { name: '周K价格、涨跌幅和成交量图' });
		expect(oldSignal?.aborted).toBe(true);
		await act(async () => {
			if (outcome === 'success') pending.resolve({ data: [testKLine('600002.SH', 99.99)] });
			else pending.reject(new Error('测试旧日K错误'));
		});
		expect(screen.getByRole('tab', { name: '周K' }).getAttribute('aria-selected')).toBe('true');
		expect(screen.getByRole('dialog').querySelector('.kline-modal-quote strong')?.textContent).toBe('22.22');
		expect(screen.queryByText('99.99')).toBeNull();
		expect(screen.queryByText('测试旧日K错误')).toBeNull();
	});

	it('关闭取消请求，再打开另一只个股回到日K，旧个股响应不串入', async () => {
		const pending = deferred<KLinePayload>();
		mockKLineRequests((path) => path.includes('symbol=600002.SH') ? pending.promise : Promise.resolve({ data: [testKLine('600001.SH', 22.22)] }));
		openWorkspace();
		fireEvent.click(await screen.findByRole('button', { name: '查看测试流入较大 K线' }));
		fireEvent.click(screen.getByRole('tab', { name: '月K' }));
		const oldSignals = kLineCalls().map((call) => call[2]?.signal);
		fireEvent.click(screen.getByRole('button', { name: '关闭 K 线弹窗' }));
		expect(oldSignals.every((signal) => signal?.aborted)).toBe(true);
		fireEvent.click(screen.getByRole('button', { name: '查看测试流入较小 K线' }));
		await screen.findByRole('img', { name: '日K价格、涨跌幅和成交量图' });
		await act(async () => pending.resolve({ data: [testKLine('600002.SH', 99.99)] }));
		expect(screen.getByRole('dialog', { name: '测试流入较小 K线' })).toBeTruthy();
		expect(screen.getByRole('dialog').querySelector('.kline-modal-quote strong')?.textContent).toBe('22.22');
		expect(screen.queryByText('99.99')).toBeNull();
		expect(kLineCalls().at(-1)?.[1]).toBe('/api/v1/quotes/kline?symbol=600001.SH&period=day&limit=120');
	});

	it('点击弹窗内部保持打开，点击遮罩关闭，切换资金方向也关闭并取消请求', async () => {
		const pending = deferred<KLinePayload>();
		mockKLineRequests(() => pending.promise);
		const { container } = openWorkspace();
		fireEvent.click(await screen.findByRole('button', { name: '查看测试流入较大 K线' }));
		fireEvent.mouseDown(screen.getByRole('dialog'));
		expect(screen.getByRole('dialog')).toBeTruthy();
		fireEvent.mouseDown(container.querySelector('.kline-modal-overlay')!);
		expect(screen.queryByRole('dialog')).toBeNull();
		fireEvent.click(screen.getByRole('button', { name: '查看测试流入较大 K线' }));
		const signal = kLineCalls().at(-1)?.[2]?.signal;
		fireEvent.click(screen.getByRole('button', { name: '资金流出' }));
		await screen.findByText('榜首净流出');
		expect(screen.queryByRole('dialog')).toBeNull();
		expect(signal?.aborted).toBe(true);
		await act(async () => pending.resolve({ data: [testKLine()] }));
		expect(screen.queryByRole('dialog')).toBeNull();
	});

	it.each(['error', 'empty'] as const)('行情 %s 如实显示，不绘制虚构K线', async (outcome) => {
		mockKLineRequests(() => outcome === 'error' ? Promise.reject(new Error('测试行情源不可用')) : Promise.resolve({ data: [] }));
		openWorkspace();
		fireEvent.click(await screen.findByRole('button', { name: '查看测试流入较大 K线' }));
		await screen.findByText(outcome === 'error' ? '测试行情源不可用' : '暂无日K数据。');
		expect(screen.queryByRole('img', { name: '日K价格、涨跌幅和成交量图' })).toBeNull();
		expect(within(screen.getByRole('dialog')).getByText('10.00')).toBeTruthy();
		expect(screen.getByText('0 根日K')).toBeTruthy();
	});

	it('缺少连接和价格字段时显示错误及占位，不把资金源默认零当成行情', async () => {
		render(<FundFlowView config={null} items={inflows} dimension="stock" meta={{ ...meta, available_fields: ['net_inflow'] }} />);
		fireEvent.click(screen.getByRole('button', { name: '查看测试流入较大 K线' }));
		await screen.findByText('后端尚未连接');
		expect([...document.querySelectorAll('.kline-modal-quote strong')].map((node) => node.textContent)).toEqual(Array(8).fill('--'));
		expect(requestJSON).not.toHaveBeenCalled();
	});

	it('没有个股代码的记录及行业题材行不触发个股行情请求', () => {
		const { container, rerender } = render(<FundFlowView config={config} items={[{ ...inflows[0], symbol: undefined }]} dimension="stock" meta={meta} />);
		fireEvent.click(container.querySelector('.market-data-table article')!);
		expect(container.querySelector('.market-data-table article')?.hasAttribute('tabindex')).toBe(false);
		for (const dimension of ['industry', 'theme'] as const) {
			rerender(<FundFlowView config={config} items={payloadFor(dimension, 'inflow').data} dimension={dimension} meta={meta} />);
			fireEvent.click(container.querySelector('.market-data-table article')!);
		}
		expect(screen.queryByRole('dialog')).toBeNull();
		expect(requestJSON).not.toHaveBeenCalled();
	});

	it('共用弹窗保留连板梯队标题、说明、八项行情和五个周期', () => {
		render(<StockKLineModal stock={{ symbol: '600002.SH', name: '测试连板股', price: 10, change_percent: 1 }} contextLabel="连板梯队" description="3板 · 测试题材" lines={[testKLine()]} state="ready" error="" periodKey="day" onPeriodChange={vi.fn()} onClose={vi.fn()} />);
		expect(screen.getByText('连板梯队 · 日K')).toBeTruthy();
		expect(screen.getByText('3板 · 测试题材')).toBeTruthy();
		expect(screen.getByRole('dialog', { name: '测试连板股 K线' })).toBeTruthy();
		expect(screen.getAllByRole('tab').map((tab) => tab.textContent)).toEqual(['分时', '5日', '日K', '周K', '月K']);
		expect([...document.querySelectorAll('.kline-modal-quote > div > span')].map((node) => node.textContent)).toEqual(['最新', '涨跌幅', '开盘', '最高', '最低', '成交量', '成交额', '换手率']);
	});
});
afterEach(() => { cleanup(); vi.resetAllMocks(); });

describe('资金方向切换', () => {
	it('分别请求真实榜单，更新统计并按净流出额从大到小展示负值', async () => {
		const { container } = openWorkspace();
		await waitFor(() => expect(tableNames(container)).toEqual(['测试流入较大', '测试流入较小']));
		expect(screen.getByText('榜首净流入')).toBeTruthy();
		expect(screen.getByRole('button', { name: '资金流入' }).getAttribute('aria-pressed')).toBe('true');
		fireEvent.click(screen.getByRole('button', { name: '资金流入' }));
		expect(requestJSON).toHaveBeenCalledTimes(1);
		fireEvent.click(screen.getByRole('button', { name: '资金流出' }));
		await waitFor(() => expect(tableNames(container)).toEqual(['测试流出较大', '测试流出较小']));
		expect(vi.mocked(requestJSON).mock.calls.map((call) => call[1])).toEqual([
			'/api/v1/market/flows?dimension=stock&sort=inflow&limit=100',
			'/api/v1/market/flows?dimension=stock&sort=outflow&limit=100',
		]);
		expect(screen.getByText('净流出项目')).toBeTruthy();
		expect(screen.getByText('榜首净流出').parentElement?.querySelector('strong')?.textContent).toBe('-900');
		expect(screen.getByText('净流出项目').parentElement?.textContent).toContain('共 2 项');
		expect(screen.queryByText('测试反向正值')).toBeNull();
		fireEvent.change(screen.getByPlaceholderText('搜索名称、代码或领涨标的'), { target: { value: '600004' } });
		expect(tableNames(container)).toEqual(['测试流出较小']);
	});

	it.each(dimensions.flatMap((dimension) => (['success', 'error'] as const).map((outcome) => ({ dimension, outcome }))))('$dimension 快速切回流入后忽略旧流出请求的 $outcome 结果', async ({ dimension, outcome }) => {
		window.history.replaceState(null, '', `#market/${dimension}-flow`);
		const pending = deferred();
		vi.mocked(requestJSON).mockImplementation((_config, path) => (
			path.includes('sort=outflow') ? pending.promise : Promise.resolve(payloadFor(dimension, 'inflow'))
		) as never);
		const { container } = openWorkspace();
		await waitFor(() => expect(tableNames(container)).toHaveLength(2));
		fireEvent.click(screen.getByRole('button', { name: '资金流出' }));
		await waitFor(() => expect(requestJSON).toHaveBeenCalledTimes(2));
		expect(screen.getByLabelText('行情数据加载中')).toBeTruthy();
		expect(tableNames(container)).toEqual([]);
		expect(screen.getByRole('button', { name: '交给 AI 解读' }).hasAttribute('disabled')).toBe(true);
		fireEvent.click(screen.getByRole('button', { name: '资金流入' }));
		await waitFor(() => expect(tableNames(container)).toEqual(['测试流入较大', '测试流入较小']));
		await act(async () => {
			if (outcome === 'success') pending.resolve(payloadFor(dimension, 'outflow'));
			else pending.reject(new Error('隔离测试旧请求失败'));
		});
		expect(tableNames(container)).toEqual(['测试流入较大', '测试流入较小']);
		expect(screen.getByText('榜首净流入')).toBeTruthy();
		expect(screen.queryByText('数据加载失败')).toBeNull();
	});

	it.each(dimensions)('%s 新方向失败时清空旧证据，保留开关供用户切回', async (dimension) => {
		window.history.replaceState(null, '', `#market/${dimension}-flow`);
		vi.mocked(requestJSON).mockImplementation((_config, path) => (
			path.includes('sort=outflow') ? Promise.reject(new Error('隔离测试数据源不可用')) : Promise.resolve(payloadFor(dimension, 'inflow'))
		) as never);
		const { container } = openWorkspace();
		await waitFor(() => expect(tableNames(container)).toHaveLength(2));
		fireEvent.click(screen.getByRole('button', { name: '资金流出' }));
		await screen.findByText('数据加载失败');
		expect(tableNames(container)).toEqual([]);
		expect(container.querySelector('.market-source-notice')).toBeNull();
		expect(screen.getByRole('button', { name: '交给 AI 解读' }).hasAttribute('disabled')).toBe(true);
		fireEvent.click(screen.getByRole('button', { name: '资金流入' }));
		await waitFor(() => expect(tableNames(container)).toHaveLength(2));
	});

	it.each(dimensions)('%s 备用源只有主力资金时按主力净流出排序，缺失字段显示占位', (dimension) => {
		const fallbackMeta = { ...meta, source: 'eastmoney:fund-flow', available_fields: ['main_net_inflow'] };
		const { container } = render(<FundFlowView items={[row('600007', '测试主力流出较小', 0, -100), row('600008', '测试主力流出较大', 0, -900)]} dimension={dimension} direction="outflow" meta={fallbackMeta} />);
		expect(tableNames(container)).toEqual(['测试主力流出较大', '测试主力流出较小']);
		expect(screen.getByText('榜首净流出').parentElement?.textContent).toContain('测试主力流出较大');
		expect(container.querySelector('.market-data-table article')?.children[3].textContent).toBe('--');
		expect(screen.getByText('东方财富分单资金 · 主力资金口径')).toBeTruthy();
	});

	it.each(dimensions)('%s 没有负值时如实显示空状态，保留切换开关', (dimension) => {
		render(<FundFlowView items={inflows.filter((item) => item.net_inflow > 0)} dimension={dimension} direction="outflow" meta={meta} onDirectionChange={vi.fn()} />);
		const label = dimension === 'stock' ? '个股' : dimension === 'industry' ? '行业' : '题材';
		expect(screen.getByText(`当前数据源暂无净流出${label}，请稍后刷新。`)).toBeTruthy();
		expect(screen.getByText('榜首净流出').parentElement?.textContent).toContain('--');
		expect(screen.getByRole('button', { name: '资金流入' })).toBeTruthy();
	});

	it.each(['industry', 'theme'] as const)('%s 使用相同开关，分别请求并排序真实双向榜单', async (dimension) => {
		window.history.replaceState(null, '', `#market/${dimension}-flow`);
		const { container } = openWorkspace();
		await waitFor(() => expect(tableNames(container)).toEqual(['测试流入较大', '测试流入较小']));
		const label = dimension === 'industry' ? '行业' : '题材';
		expect(screen.getByRole('group', { name: `${label}资金方向` })).toBeTruthy();
		fireEvent.click(screen.getByRole('button', { name: '资金流出' }));
		await waitFor(() => expect(tableNames(container)).toEqual(['测试流出较大', '测试流出较小']));
		expect(screen.getByText('榜首净流出').parentElement?.textContent).toContain('测试流出较大');
		expect(screen.getByText('新浪板块资金与领涨标的')).toBeTruthy();
		expect(vi.mocked(requestJSON).mock.calls.map((call) => call[1])).toEqual([
			`/api/v1/market/flows?dimension=${dimension}&sort=inflow&limit=100`,
			`/api/v1/market/flows?dimension=${dimension}&sort=outflow&limit=100`,
		]);
	});

	it('行业、题材、个股分别保留自己的方向，返回行业后仍为流出', async () => {
		window.history.replaceState(null, '', '#market/industry-flow');
		const { container } = openWorkspace();
		await waitFor(() => expect(tableNames(container)).toHaveLength(2));
		fireEvent.click(screen.getByRole('button', { name: '资金流出' }));
		await screen.findByText('榜首净流出');
		fireEvent.click(screen.getByRole('button', { name: /^题材概念/ }));
		await screen.findByText('榜首净流入');
		expect(screen.getByRole('button', { name: '资金流入' }).getAttribute('aria-pressed')).toBe('true');
		fireEvent.click(screen.getByRole('button', { name: '资金流出' }));
		await screen.findByText('榜首净流出');
		fireEvent.click(screen.getByRole('button', { name: /^个股资金流入流出/ }));
		await screen.findByText('榜首净流入');
		fireEvent.click(screen.getByRole('button', { name: /^行业资金/ }));
		await screen.findByText('榜首净流出');
		expect(screen.getByRole('group', { name: '行业资金方向' }).getAttribute('data-direction')).toBe('outflow');
		expect(tableNames(container)).toEqual(['测试流出较大', '测试流出较小']);
	});

	it.each(['industry', 'theme'] as const)('%s 流出榜无负值时禁用 AI 解读，不将流入证据混入流出', async (dimension) => {
		window.history.replaceState(null, '', `#market/${dimension}-flow`);
		const payload = payloadFor(dimension, 'inflow');
		vi.mocked(requestJSON).mockResolvedValue({ ...payload, data: payload.data.filter((item) => item.net_inflow > 0) });
		const { container } = openWorkspace();
		await screen.findByText('榜首净流入');
		fireEvent.click(screen.getByRole('button', { name: '资金流出' }));
		await screen.findByText(`当前数据源暂无净流出${dimension === 'industry' ? '行业' : '题材'}，请稍后刷新。`);
		expect(tableNames(container)).toEqual([]);
		expect(screen.getByRole('button', { name: '交给 AI 解读' }).hasAttribute('disabled')).toBe(true);
	});

	it('题材流出交给 AI 时只包含当前方向的真实记录', async () => {
		window.history.replaceState(null, '', '#market/theme-flow');
		const onAskAI = vi.fn();
		render(<MarketOverviewWorkspace config={config} refreshKey={0} onAskAI={onAskAI} />);
		await screen.findByText('榜首净流入');
		fireEvent.click(screen.getByRole('button', { name: '资金流出' }));
		await screen.findByText('榜首净流出');
		fireEvent.click(screen.getByRole('button', { name: '交给 AI 解读' }));
		expect(onAskAI).toHaveBeenCalledTimes(1);
		expect(onAskAI.mock.calls[0][0]).toContain('测试流出较大');
		expect(onAskAI.mock.calls[0][0]).not.toContain('测试反向正值');
	});

	it('行业流出请求迟到时不会覆盖已经打开的题材榜', async () => {
		window.history.replaceState(null, '', '#market/industry-flow');
		const pending = deferred();
		vi.mocked(requestJSON).mockImplementation((_config, path) => {
			const params = new URL(path, config.backendUrl).searchParams;
			const dimension = params.get('dimension') as typeof dimensions[number];
			return (dimension === 'industry' && params.get('sort') === 'outflow' ? pending.promise : Promise.resolve(payloadFor(dimension, 'inflow'))) as never;
		});
		const { container } = openWorkspace();
		await screen.findByText('榜首净流入');
		fireEvent.click(screen.getByRole('button', { name: '资金流出' }));
		await waitFor(() => expect(requestJSON).toHaveBeenCalledTimes(2));
		fireEvent.click(screen.getByRole('button', { name: /^题材概念/ }));
		await screen.findByText('榜首净流入');
		await act(async () => pending.resolve(payloadFor('industry', 'outflow')));
		expect(container.querySelector('.market-source-notice')?.textContent).toContain('sina:theme-money-flow');
		expect(tableNames(container)).toEqual(['测试流入较大', '测试流入较小']);
		expect(screen.queryByText('榜首净流出')).toBeNull();
	});
});
