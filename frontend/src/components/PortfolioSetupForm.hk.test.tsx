// @vitest-environment jsdom
import { useState } from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Quote, StockDirectoryEntry } from '../lib/backend';
import { readPortfolioDraft, writePortfolioDraft, type PortfolioDraft } from '../lib/portfolio-draft';
import { loadPortfolioQuotes } from '../lib/use-portfolio-quotes';
import { fetchMonitorStockSuggest } from '../lib/monitor';
import { PortfolioSetupForm } from './PortfolioSetupForm';

const market = vi.hoisted(() => ({ quotes: {} as Record<string, Quote> }));
vi.mock('../lib/use-portfolio-quotes', () => ({
	usePortfolioQuotes: () => ({ quotes: market.quotes, loading: false, error: '', refresh: () => {} }),
	loadPortfolioQuotes: vi.fn(),
}));
// 持仓表单用监控的 suggest 接口补港股候选：本地目录只有 A 股 + ETF。
vi.mock('../lib/monitor', () => ({ fetchMonitorStockSuggest: vi.fn() }));

const directory = [
	{ symbol: '600941.SH', code: '600941', name: '中国移动', instrument_type: 'stock' },
	{ symbol: '600000.SH', code: '600000', name: '浦发银行', instrument_type: 'stock' },
] as StockDirectoryEntry[];
const config = { backendUrl: 'http://test.invalid', token: '' };
const draft: PortfolioDraft = {
	profile: 'balanced', totalAssets: 100000,
	holdings: [{ symbol: '600000.SH', name: '浦发银行', weight: 25, costPrice: '10', quantity: '1000' }],
};

const submit = vi.fn();
function Harness() {
	const [current, setCurrent] = useState(draft);
	return <PortfolioSetupForm draft={current} config={config} directory={directory} actionLabel="开始巡检" busyLabel="巡检中"
		onChange={(next) => { writePortfolioDraft(next); setCurrent(readPortfolioDraft()); }} onSubmit={submit} />;
}

beforeEach(() => {
	window.localStorage.clear();
	submit.mockClear();
	market.quotes = {
		'600000.SH': { symbol: '600000.SH', price: 10 } as Quote,
		'00700.HK': { symbol: '00700.HK', name: '腾讯控股', currency: 'HKD', price: 344 } as Quote,
	};
	vi.mocked(loadPortfolioQuotes).mockReset().mockImplementation(async () => market.quotes);
	vi.mocked(fetchMonitorStockSuggest).mockReset();
});
afterEach(cleanup);

describe('portfolio holdings 港股', () => {
	it('通过远端建议把港股加入持仓，并保留规范代码', async () => {
		vi.mocked(fetchMonitorStockSuggest).mockResolvedValue({
			data: [{ symbol: '00700.HK', code: '00700', name: '腾讯控股' }],
		} as never);
		render(<Harness />);
		fireEvent.change(screen.getByLabelText('搜索持仓股票或 ETF'), { target: { value: '腾讯控股' } });
		const option = await screen.findByRole('option', { name: /腾讯控股/ });
		// 候选行要标明这是港股，否则与 A 股同名标的无法区分。
		expect(option.textContent).toContain('港股');
		fireEvent.click(option);
		await screen.findByText('2/10 只标的');
		expect(readPortfolioDraft().holdings.some((holding) => holding.symbol === '00700.HK')).toBe(true);
	});
});

it.each(['00700.HK', 'HK00700'])('直接输入 %s 回车会保存规范代码', async (code) => {
	render(<Harness />);
	const field = screen.getByLabelText('搜索持仓股票或 ETF');
	fireEvent.change(field, { target: { value: code } });
	fireEvent.keyDown(field, { key: 'Enter' });
	await screen.findByText('2/10 只标的');
	expect(readPortfolioDraft().holdings.at(-1)?.symbol).toBe('00700.HK');
});

it('切换代码后立即回车，保存新代码', async () => {
	vi.mocked(fetchMonitorStockSuggest).mockResolvedValueOnce({ data: [{ symbol: '00700.HK', code: '00700', name: '腾讯控股' }] } as never);
	market.quotes['09988.HK'] = { symbol: '09988.HK', name: '阿里巴巴', price: 100 } as Quote;
	render(<Harness />);
	const field = screen.getByLabelText('搜索持仓股票或 ETF');
	fireEvent.change(field, { target: { value: '00700' } });
	await screen.findByRole('option', { name: /腾讯控股/ });
	fireEvent.change(field, { target: { value: '09988' } });
	fireEvent.keyDown(field, { key: 'Enter' });
	await screen.findByText('2/10 只标的');
	expect(readPortfolioDraft().holdings.at(-1)?.symbol).toBe('09988.HK');
});

it('同名 A 股与港股均可选择', async () => {
	vi.mocked(fetchMonitorStockSuggest).mockResolvedValue({ data: [{ symbol: '00941.HK', code: '00941', name: '中国移动' }] } as never);
	render(<Harness />);
	fireEvent.change(screen.getByLabelText('搜索持仓股票或 ETF'), { target: { value: '中国移动' } });
	await screen.findByRole('option', { name: /00941/ });
	expect(screen.getByRole('option', { name: /600941/ })).toBeTruthy();
});

it.each([true, false])('混合持仓提交人民币权重，汇率可用=%s', async (available) => {
	if (available) market.quotes['00700.HK'].cny_conversion = { rate: 0.85, date: new Date().toISOString().slice(0,10) };
	render(<Harness />);
	const field = screen.getByLabelText('搜索持仓股票或 ETF');
	fireEvent.change(field, { target: { value: '00700' } });
	fireEvent.keyDown(field, { key: 'Enter' });
	await screen.findByText('2/10 只标的');
	fireEvent.change(screen.getByLabelText('腾讯控股持仓数量'), { target: { value: '100' } });
	const button = screen.getByRole('button', { name: '开始巡检' }) as HTMLButtonElement;
	expect(button.disabled).toBe(!available);
	fireEvent.click(button);
	if (available) {
		expect(submit).toHaveBeenCalledWith([
			expect.objectContaining({ symbol: '600000.SH', weight_percent: 10 }),
			expect.objectContaining({ symbol: '00700.HK', weight_percent: 29, cost_price: 344 }),
		]);
	} else { expect(submit).not.toHaveBeenCalled(); }
});
