// @vitest-environment jsdom
import { useState } from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Quote, StockDirectoryEntry } from '../lib/backend';
import { portfolioPlansStorageKey, readPortfolioDraft, writePortfolioDraft, type PortfolioDraft } from '../lib/portfolio-draft';
import { loadPortfolioQuotes } from '../lib/use-portfolio-quotes';
import { PortfolioSetupForm } from './PortfolioSetupForm';

const market = vi.hoisted(() => ({ quotes: {} as Record<string, Quote> }));
vi.mock('../lib/use-portfolio-quotes', () => ({
	usePortfolioQuotes: () => ({ quotes: market.quotes, loading: false, error: '', refresh: () => {} }),
	loadPortfolioQuotes: vi.fn(),
}));

const directory = Array.from({ length: 11 }, (_, index) => ({
	symbol: `6000${String(index).padStart(2, '0')}.SH`, code: `6000${String(index).padStart(2, '0')}`,
	name: index === 4 ? '瑞康医药' : `样本${index}`, instrument_type: 'stock',
})) as StockDirectoryEntry[];
const config = { backendUrl: 'http://test.invalid', token: '' };
const legacy: PortfolioDraft = {
	profile: 'balanced', totalAssets: 100000,
	holdings: directory.slice(0, 4).map((stock) => ({ symbol: stock.symbol, name: stock.name, weight: 25, costPrice: '10', quantity: '' })),
};

function Harness({ draft = legacy, onSubmit = vi.fn() }: { draft?: PortfolioDraft; onSubmit?: ReturnType<typeof vi.fn> }) {
	const [current, setCurrent] = useState(draft);
	return <PortfolioSetupForm draft={current} config={config} directory={directory} actionLabel="开始巡检" busyLabel="巡检中"
		onChange={(next) => { writePortfolioDraft(next); setCurrent(readPortfolioDraft()); }} onSubmit={onSubmit} />;
}

beforeEach(() => {
	window.localStorage.clear();
	market.quotes = Object.fromEntries(directory.map((stock) => [stock.symbol, { symbol: stock.symbol, price: 10 } as Quote]));
	vi.mocked(loadPortfolioQuotes).mockReset().mockImplementation(async () => market.quotes);
});
afterEach(cleanup);

describe('adding portfolio holdings', () => {
	it('fills the quantity from the default 10% allocation when adding a holding', async () => {
		market.quotes[directory[0].symbol].price = 12.34;
		render(<Harness draft={{ profile: 'balanced', totalAssets: 100000, holdings: [] }} />);
		fireEvent.change(screen.getByLabelText('搜索持仓股票或 ETF'), { target: { value: directory[0].code } });
		fireEvent.click(screen.getByRole('button', { name: '添加持仓' }));
		await screen.findByText('1/10 只标的');
		expect((screen.getByLabelText('样本0持仓数量') as HTMLInputElement).value).toBe('810');
		expect((screen.getByLabelText('样本0持仓占比') as HTMLInputElement).value).toBe('10');
		expect(readPortfolioDraft().holdings[0]).toMatchObject({ weight: 10, quantity: '810', costPrice: '12.34' });
	});

	it('adds to a full legacy portfolio, persists it, and requires allocation adjustment before submitting', async () => {
		const onSubmit = vi.fn();
		render(<Harness onSubmit={onSubmit} />);
		expect(screen.getByText('总仓位 100%')).toBeTruthy();
		fireEvent.change(screen.getByLabelText('搜索持仓股票或 ETF'), { target: { value: '瑞康' } });
		fireEvent.click(screen.getByRole('option', { name: /瑞康医药/ }));
		await screen.findByText('5/10 只标的');
		expect(readPortfolioDraft().holdings).toHaveLength(5);
		expect(readPortfolioDraft().holdings.slice(0, 4)).toEqual(legacy.holdings);
		expect(screen.getByText(/持仓总市值超过总资产/)).toBeTruthy();
		expect((screen.getByRole('button', { name: '开始巡检' }) as HTMLButtonElement).disabled).toBe(true);
		fireEvent.change(screen.getByLabelText('样本0持仓数量'), { target: { value: '1000' } });
		expect(screen.getByText('总仓位 95%')).toBeTruthy();
		fireEvent.click(screen.getByRole('button', { name: '开始巡检' }));
		expect(onSubmit).toHaveBeenCalledWith(expect.arrayContaining([expect.objectContaining({ symbol: directory[0].symbol, weight_percent: 10 })]));
		expect(onSubmit.mock.calls[0][0].reduce((sum: number, holding: { weight_percent: number }) => sum + holding.weight_percent, 0)).toBe(95);
	});

	it('allows adding up to ten holdings with the plus button without silently dropping them', async () => {
		render(<Harness />);
		for (let index = 4; index < 10; index++) {
			fireEvent.change(screen.getByLabelText('搜索持仓股票或 ETF'), { target: { value: directory[index].code } });
			fireEvent.click(screen.getByRole('button', { name: '添加持仓' }));
			await screen.findByText(`${index + 1}/10 只标的`);
		}
		expect(JSON.parse(window.localStorage.getItem(portfolioPlansStorageKey)!).plans[0].draft.holdings).toHaveLength(10);
		expect((screen.getByLabelText('搜索持仓股票或 ETF') as HTMLInputElement).disabled).toBe(true);
		expect((screen.getByRole('button', { name: '添加持仓' }) as HTMLButtonElement).disabled).toBe(true);
		expect(loadPortfolioQuotes).toHaveBeenCalledTimes(6);
	});

	it('keeps the new holding when quote lookup fails and shows the missing-cost message beside search', async () => {
		vi.mocked(loadPortfolioQuotes).mockRejectedValue(new Error('offline'));
		render(<Harness />);
		fireEvent.change(screen.getByLabelText('搜索持仓股票或 ETF'), { target: { value: '瑞康' } });
		fireEvent.keyDown(screen.getByLabelText('搜索持仓股票或 ETF'), { key: 'Enter' });
		await screen.findByText('5/10 只标的');
		expect(screen.getByText('未取得添加时的有效价格，请手动填写持仓成本').closest('.portfolio-holdings-section')).toBeTruthy();
		expect(screen.getByText('总仓位 —')).toBeTruthy();
		expect((screen.getByRole('button', { name: '开始巡检' }) as HTMLButtonElement).disabled).toBe(true);
	});

	it('uses the latest draft when quantity changes while the add request is pending', async () => {
		let resolve!: (quotes: Record<string, Quote>) => void;
		vi.mocked(loadPortfolioQuotes).mockImplementation(() => new Promise((done) => { resolve = done; }));
		render(<Harness />);
		fireEvent.change(screen.getByLabelText('搜索持仓股票或 ETF'), { target: { value: '瑞康' } });
		fireEvent.click(screen.getByRole('option', { name: /瑞康医药/ }));
		fireEvent.change(screen.getByLabelText('样本0持仓数量'), { target: { value: '2000' } });
		resolve(market.quotes);
		await waitFor(() => expect(readPortfolioDraft().holdings).toHaveLength(5));
		expect(readPortfolioDraft().holdings[0].quantity).toBe('2000');
		expect(readPortfolioDraft().holdings[4].weight).toBe(5);
		expect(readPortfolioDraft().holdings[4].quantity).toBe('500');
	});
});

describe('linked holding allocation and quantity', () => {
	const draft: PortfolioDraft = {
		profile: 'balanced', totalAssets: 100000,
		holdings: [{ symbol: directory[0].symbol, name: directory[0].name, weight: 10, quantity: '', costPrice: '20' }],
	};
	const quantityInput = () => screen.getByLabelText('样本0持仓数量') as HTMLInputElement;
	const weightInput = () => screen.getByLabelText('样本0持仓占比') as HTMLInputElement;

	it('fills legacy estimates and uses the current price for edits in both directions and submission', () => {
		market.quotes[directory[0].symbol].price = 25;
		const onSubmit = vi.fn();
		render(<Harness draft={draft} onSubmit={onSubmit} />);
		expect(quantityInput().value).toBe('500');
		expect(weightInput().value).toBe('12.5');
		fireEvent.change(weightInput(), { target: { value: '20' } });
		expect(quantityInput().value).toBe('800');
		expect(readPortfolioDraft().holdings[0]).toMatchObject({ weight: 20, quantity: '800', costPrice: '20' });
		fireEvent.change(quantityInput(), { target: { value: '1000' } });
		expect(weightInput().value).toBe('25');
		expect(readPortfolioDraft().holdings[0]).toMatchObject({ weight: 25, quantity: '1000' });
		fireEvent.click(screen.getByRole('button', { name: '开始巡检' }));
		expect(onSubmit).toHaveBeenCalledWith([{ symbol: directory[0].symbol, name: directory[0].name, weight_percent: 25, cost_price: 20 }]);
	});

	it('keeps the entered allocation while typing and shows the actual whole-share allocation on blur', () => {
		market.quotes[directory[0].symbol].price = 33;
		render(<Harness draft={draft} />);
		fireEvent.change(weightInput(), { target: { value: '10.25' } });
		expect(weightInput().value).toBe('10.25');
		expect(quantityInput().value).toBe('310');
		fireEvent.blur(weightInput());
		expect(weightInput().value).toBe('10.23');
		expect(screen.getByText('总仓位 10.23%')).toBeTruthy();
	});

	it('keeps filled quantities fixed when assets, cost or quotes change and updates the allocation', () => {
		market.quotes[directory[0].symbol].price = 25;
		const view = render(<Harness draft={draft} />);
		fireEvent.change(weightInput(), { target: { value: '20' } });
		fireEvent.blur(weightInput());
		fireEvent.change(screen.getByLabelText('总资产（元）'), { target: { value: '200000' } });
		expect(quantityInput().value).toBe('800');
		expect(weightInput().value).toBe('10');
		fireEvent.change(screen.getByLabelText('样本0持仓成本'), { target: { value: '15' } });
		expect(quantityInput().value).toBe('800');
		market.quotes = { ...market.quotes, [directory[0].symbol]: { ...market.quotes[directory[0].symbol], price: 30 } };
		view.rerender(<Harness draft={draft} />);
		expect(quantityInput().value).toBe('800');
		expect(weightInput().value).toBe('12');
	});

	it('lets either field be cleared while editing without losing the holding or its quantity', () => {
		render(<Harness draft={draft} />);
		fireEvent.change(weightInput(), { target: { value: '' } });
		expect(weightInput().value).toBe('');
		fireEvent.blur(weightInput());
		expect(weightInput().value).toBe('5');
		fireEvent.change(quantityInput(), { target: { value: '' } });
		expect(quantityInput().value).toBe('');
		fireEvent.change(quantityInput(), { target: { value: '300' } });
		expect(weightInput().value).toBe('3');
		fireEvent.change(quantityInput(), { target: { value: '' } });
		fireEvent.blur(quantityInput());
		expect(quantityInput().value).toBe('300');
		expect(readPortfolioDraft().holdings).toHaveLength(1);
	});

	it('retains a zero allocation for correction and blocks submission', () => {
		render(<Harness draft={draft} />);
		fireEvent.change(weightInput(), { target: { value: '0' } });
		expect(quantityInput().value).toBe('0');
		expect(readPortfolioDraft().holdings).toHaveLength(1);
		expect((screen.getByRole('button', { name: '开始巡检' }) as HTMLButtonElement).disabled).toBe(true);
		fireEvent.change(quantityInput(), { target: { value: '100' } });
		expect(weightInput().value).toBe('1');
		expect((screen.getByRole('button', { name: '开始巡检' }) as HTMLButtonElement).disabled).toBe(false);
	});

	it('uses cost as the sizing fallback when quotes are missing and preserves the quantity budget', () => {
		market.quotes = {};
		render(<Harness draft={draft} />);
		fireEvent.change(weightInput(), { target: { value: '20' } });
		expect(quantityInput().value).toBe('1000');
		expect((screen.getByRole('button', { name: '开始巡检' }) as HTMLButtonElement).disabled).toBe(true);
		cleanup();
		market.quotes = Object.fromEntries(directory.map((stock) => [stock.symbol, { symbol: stock.symbol, price: 10 } as Quote]));
		render(<Harness draft={{ ...draft, holdings: [draft.holdings[0], { ...draft.holdings[0], symbol: directory[1].symbol, name: directory[1].name, quantity: '9000' }] }} />);
		fireEvent.change(quantityInput(), { target: { value: '2000' } });
		expect(quantityInput().value).toBe('1000');
		expect(weightInput().value).toBe('10');
		expect(screen.getByText('总仓位 100%')).toBeTruthy();
	});
});
