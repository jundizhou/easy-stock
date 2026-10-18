import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import { PortfolioSetupForm } from './PortfolioSetupForm';

vi.mock('../lib/use-portfolio-quotes', () => ({
 usePortfolioQuotes: () => ({quotes:{'510300.SH':{symbol:'510300.SH',price:1.237,change_percent:.05,trade_time:'2026-10-09T10:00:00+08:00'},'688002.SH':{symbol:'688002.SH',price:45.02,change_percent:.0328,trade_time:'2026-10-09T10:00:00+08:00'}},loading:false,error:'',refresh:()=>{}}),
 loadPortfolioQuotes:vi.fn(),
}));

describe('ETF portfolio editing',()=>{
 it('keeps three-decimal quotes, fund units and raw valuation arithmetic',()=>{
  const html=renderToStaticMarkup(<PortfolioSetupForm draft={{profile:'balanced',totalAssets:10000,holdings:[{symbol:'510300.SH',name:'样本ETF',instrumentType:'etf',weight:50,quantity:'',costPrice:'1.236'}]}} directory={[]} actionLabel="分析" busyLabel="分析中" onChange={()=>{}} onSubmit={()=>{}} />);
  expect(html).toContain('1.237 元');expect(html).toContain('份');expect(html).not.toContain('1.24 元');expect(html).toContain('+0.05%');
 });
});
describe('PortfolioSetupForm', () => {
	it('shows the holding share as a read-only market-value calculation', () => {
		const markup = renderToStaticMarkup(<PortfolioSetupForm
			draft={{
				profile: 'balanced',
				totalAssets: 500000,
				holdings: [
					{ symbol: '688002.SH', name: '睿创微纳', weight: 1, quantity: '10000', costPrice: '45.02' },
				],
			}}
			directory={[]}
			actionLabel="开始 AI 巡检"
			busyLabel="巡检进行中"
			onChange={() => {}}
			onSubmit={() => {}}
		/>);

		expect(markup).toContain('持仓占比');
		expect(markup).toContain('90.04%');
		expect(markup).not.toContain('按数量 × 最新价格 ÷ 总资产');
		expect(markup).not.toContain('type="range"');
		expect(markup).not.toContain('仓位百分比');
	});

	it('blocks quantities whose market value exceeds total assets', () => {
		const markup = renderToStaticMarkup(<PortfolioSetupForm
			draft={{
				profile: 'balanced',
				totalAssets: 500000,
				holdings: [
					{ symbol: '688002.SH', name: '睿创微纳', weight: 1, quantity: '20000', costPrice: '45.02' },
				],
			}}
			directory={[]}
			actionLabel="开始 AI 巡检"
			busyLabel="巡检进行中"
			onChange={() => {}}
			onSubmit={() => {}}
		/>);

		expect(markup).toContain('持仓数量对应的市值超过总资产');
		expect(markup).toContain('aria-invalid="true"');
		expect(markup).toContain('max="11106"');
		expect(markup).toMatch(/<button type="button" disabled="">.*开始 AI 巡检/);
	});
});
