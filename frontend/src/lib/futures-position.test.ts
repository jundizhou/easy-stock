import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import type { MarketFuturesConsensus, MarketFuturesMembers, MarketFuturesPositionSeries } from './backend';
import { buildFuturesOpeningAggregate, buildFuturesOpeningStats, formatFuturesOpeningHands } from './futures-position';
import { buildMarketModulePrompt } from './market-overview';
import { FuturesPositionView } from '../components/market/MarketDataViews';

function fixture(): { series: MarketFuturesPositionSeries; members: MarketFuturesMembers } {
	const meta = { source: 'cffex:futures-position', fetched_at: '2026-09-04T18:00:00+08:00', latency_ms: 10, stale: false };
	return {
		series: {
			variety: 'IF', variety_name: '沪深300股指期货', contract_code: 'IF2609', index_code: '000300.SH',
			meta: { ...meta, source: 'eastmoney:futures-position' },
			rows: [{ trade_date: '2026-09-04', long_position: 10000, short_position: 11000, net_position: -1000, long_change: 1600, short_change: 1100 }],
		},
		members: {
			contract_code: 'IF2609', trade_date: '2026-09-04', meta,
			members: Array.from({ length: 20 }, (_, i) => ({
				contract: 'IF2609', rank: i + 1, long_name: `多方会员${i + 1}`, short_name: `空方会员${i + 1}`,
				long_position: 5000, short_position: 6000,
				long_change: [1200, -400, 800][i] ?? 0, short_change: [600, 1000, -500][i] ?? 0,
			})),
		},
	};
}

function consensusFixture(): MarketFuturesConsensus {
	const meta = { source: 'cffex:futures-consensus', fetched_at: '2026-09-04T18:00:00+08:00', latency_ms: 10, stale: false };
	return {
		trade_date: '2026-09-04',
		varieties: ['IF', 'IH', 'IC', 'IM'].map((variety) => ({
			variety, trade_date: '2026-09-04', contract_count: 3,
			long_position: 10000, short_position: 11000, net_short_position: 1000, net_long_position: -1000,
			long_change: 100, short_change: 250, net_short_change: 150, net_long_change: -150, citic_net_short_change: 80, citic_net_long_change: -80,
		})),
		top20_net_short_position: 4000, top20_net_long_position: -4000, top20_net_short_change: 600, top20_net_long_change: -600, citic_net_short_change: 320, citic_net_long_change: -320,
		meta,
	};
}

describe('futures member positive position additions', () => {
	it('sums positive member changes independently without netting reductions or using aggregate changes', () => {
		const { series, members } = fixture();
		expect(buildFuturesOpeningStats(series, members)).toEqual({ longAdded: 2000, shortAdded: 1600, netAdded: 400, reason: '' });
	});

	it('supports a negative net value and real zero additions', () => {
		const { series, members } = fixture();
		for (const member of members.members) { member.long_change = -10; member.short_change = -20; }
		expect(buildFuturesOpeningStats(series, members)).toMatchObject({ longAdded: 0, shortAdded: 0, netAdded: 0 });
		members.members[0].short_change = 900;
		expect(buildFuturesOpeningStats(series, members)).toMatchObject({ longAdded: 0, shortAdded: 900, netAdded: -900 });
	});

	it.each([null, undefined, Number.NaN, Number.POSITIVE_INFINITY, 1.5])('does not replace a missing or invalid member change (%s) with zero', (value) => {
		const { series, members } = fixture();
		members.members[0].long_change = value;
		const result = buildFuturesOpeningStats(series, members);
		expect(result).toMatchObject({ longAdded: null, shortAdded: 1600, netAdded: null });
		expect(result.reason).toContain('字段缺失');
	});

	it('keeps the long side available if only short-side changes are missing', () => {
		const { series, members } = fixture();
		members.members[0].short_change = null;
		expect(buildFuturesOpeningStats(series, members)).toMatchObject({ longAdded: 2000, shortAdded: null, netAdded: null });
	});

	it('does not infer positive additions from trend aggregates when member data is unavailable', () => {
		const { series, members } = fixture();
		for (const detail of [null, { ...members, members: [] }]) {
			expect(buildFuturesOpeningStats(series, detail)).toMatchObject({ longAdded: null, shortAdded: null, netAdded: null });
		}
		expect(buildFuturesOpeningStats(null, members).netAdded).toBeNull();
	});

	it.each(['date', 'contract', 'row-contract', 'missing-rank', 'duplicate-rank', 'extra-rank', 'invalid-rank'])('rejects mismatched or incomplete rankings: %s', (problem) => {
		const { series, members } = fixture();
		if (problem === 'date') members.trade_date = '2026-09-03';
		if (problem === 'contract') members.contract_code = 'IF2610';
		if (problem === 'row-contract') members.members[0].contract = 'IH2609';
		if (problem === 'missing-rank') members.members.pop();
		if (problem === 'duplicate-rank') members.members[0].rank = 2;
		if (problem === 'extra-rank') members.members.push({ ...members.members[0], rank: 21 });
		if (problem === 'invalid-rank') members.members[0].rank = 0;
		expect(buildFuturesOpeningStats(series, members)).toMatchObject({ longAdded: null, shortAdded: null, netAdded: null });
	});

	it('formats unavailable, zero and signed values distinctly', () => {
		expect(formatFuturesOpeningHands(null)).toBe('--');
		expect(formatFuturesOpeningHands(0)).toBe('0 手');
		expect(formatFuturesOpeningHands(2000)).toBe('2,000 手');
		expect(formatFuturesOpeningHands(400, true)).toBe('+400 手');
		expect(formatFuturesOpeningHands(-400, true)).toBe('-400 手');
	});
});

describe('futures opening statistics presentation', () => {
	it('sums the four varieties only when all four share the same trade date', () => {
		const { series, members } = fixture();
		const entries = ['IF', 'IH', 'IC', 'IM'].map((variety) => ({ series: { ...series, variety }, members: { ...members, members: members.members.map((member) => ({ ...member })) } }));
		const aggregate = buildFuturesOpeningAggregate(entries);
		expect(aggregate).toMatchObject({ longAdded: 8000, shortAdded: 6400, netAdded: 1600, tradeDate: '2026-09-04', reason: '' });
		entries[3].series.rows = [{ ...entries[3].series.rows[0], trade_date: '2026-09-03' }];
		expect(buildFuturesOpeningAggregate(entries).netAdded).toBeNull();
	});

	it('does not show a four-variety sum when one member detail request failed', () => {
		const { series, members } = fixture();
		const entries = ['IF', 'IH', 'IC'].map((variety) => ({ series: { ...series, variety }, members }));
		expect(buildFuturesOpeningAggregate(entries)).toMatchObject({ netAdded: null, reason: '四个股指期货品种未全部获取' });
	});

	function render(members: MarketFuturesMembers | null, series = fixture().series, variety = 'IF', consensus: MarketFuturesConsensus | null = consensusFixture()) {
		return renderToStaticMarkup(createElement(FuturesPositionView, { series, members, consensus, variety, onVariety: () => {}, meta: series.meta }));
	}

	it('shows consensus cards and scope instead of positive-additions statistics', () => {
		const markup = render(fixture().members);
		for (const text of ['前20多空单净值', '当日多空单变化', '中信期货多空单变化', '-1,000 手', '-150 手', '-80 手', '2026-09-04', '+ 多单 · − 空单', '四大期指多空单共识', '主力合约持仓趋势参考']) expect(markup).toContain(text);
		expect(markup).not.toContain('当日总新开多单（增仓口径）');
	});

	it('shows unavailable statistics and a reason instead of zeros on member failure', () => {
		const markup = render(null, fixture().series, 'IF', null);
		expect(markup).toContain('共识统计未获取');
		expect(markup.match(/>--<\/strong>/g)).toHaveLength(4);
	});

	it('identifies cached member evidence independently from fresh trend data', () => {
		const { members } = fixture();
		members.meta.stale = true;
		members.meta.fallback_reason = '会员明细刷新失败，返回最近成功快照';
		const markup = render(members);
		expect(markup).toContain('缓存快照');
		expect(markup).toContain(members.meta.fallback_reason);
	});

	it('does not show an IF statistic under an IH selection', () => {
		expect(render(fixture().members, fixture().series, 'IH')).not.toContain('IF2609');
	});

	it('sends the same qualified values and source to AI', () => {
		const { series, members } = fixture();
		members.meta.stale = true;
		const prompt = buildMarketModulePrompt('futures-position', { futures: series, futuresConsensus: consensusFixture() }, '2026-09-05');
		expect(prompt).toContain('全部合约前20会员多空单净值 -4,000 手');
		expect(prompt).toContain('当日多空单变化 -600 手');
		expect(prompt).toContain('中信期货(代客)当日多空单变化 -320 手');
		expect(prompt).toContain('eastmoney:futures-position');
		const missing = buildMarketModulePrompt('futures-position', { futures: series }, '2026-09-05');
		expect(missing).not.toContain('总新开多单');
		expect(missing).toContain('机构多空单”是股指期货前20会员持仓口径');
	});
});
