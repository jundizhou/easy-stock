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
	{ symbol: '600000.SH', code: '600000', name: '浦发银行', instrument_type: 'stock' },
] as StockDirectoryEntry[];
const config = { backendUrl: 'http://test.invalid', token: '' };
const draft: PortfolioDraft = {
	profile: 'balanced', totalAssets: 100000,
	holdings: [{ symbol: '600000.SH', name: '浦发银行', weight: 25, costPrice: '10', quantity: '1000' }],
};

function Harness() {
	const [current, setCurrent] = useState(draft);
	return <PortfolioSetupForm draft={current} config={config} directory={directory} actionLabel="开始巡检" busyLabel="巡检中"
		onChange={(next) => { writePortfolioDraft(next); setCurrent(readPortfolioDraft()); }} onSubmit={vi.fn()} />;
}

beforeEach(() => {
	window.localStorage.clear();
	market.quotes = {
		'600000.SH': { symbol: '600000.SH', price: 10 } as Quote,
		'00700.HK': { symbol: '00700.HK', price: 344 } as Quote,
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
