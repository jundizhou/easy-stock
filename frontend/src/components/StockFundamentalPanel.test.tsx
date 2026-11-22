// @vitest-environment jsdom
import { render, within } from '@testing-library/react';
import { it, expect } from 'vitest';
import { StockFundamentalPanel } from './StockFundamentalPanel';
import type { StockAIFundamental } from '../lib/backend';

it('区分港股财报中的未提供、真实零、亏损及不适用口径', () => {
	const fundamental: StockAIFundamental = {
		available: true, report_name: '三季报', report_date: '2026-09-30', score: 50, quality: '中性',
		revenue: 100, revenue_yoy: null, net_profit: -100_000_000, net_profit_yoy: 0,
		roe: 0, gross_margin: null, eps: 0, debt_ratio: null, operating_cash_flow_per_share: null,
		currency: 'USD', summary: '', source: 'eastmoney:hkf10-financials',
	};
	const view = render(<StockFundamentalPanel analysis={{ symbol: '00005.HK', fundamental }} />);
	const metric = (name: string) => within(view.getByText(name).closest('article')!);
	expect(metric('毛利率').getByText('未提供')).toBeTruthy();
	expect(metric('ROE').getByText('0.0%')).toBeTruthy();
	expect(metric('营运收入').getByText('同比未提供')).toBeTruthy();
	expect(metric('股东应占溢利').getByText('−1.00亿')).toBeTruthy();
	expect(metric('股东应占溢利').getByText('同比 0.0%')).toBeTruthy();
	expect(metric('扣非净利润及一次性损益占比').getByText('不适用')).toBeTruthy();
});
