import type { MarketFuturesMembers, MarketFuturesPositionSeries } from './backend';

export const futuresOpeningScopeNote = '增仓口径：分别累加前20会员多单/空单持仓增减中的正值，减仓不抵扣；净值 = 多单正增仓合计 − 空单正增仓合计。不是逐笔新开仓量，也不是全市场净开仓或资金流入。';

export type FuturesOpeningStats = {
	longAdded: number | null;
	shortAdded: number | null;
	netAdded: number | null;
	reason: string;
};

export type FuturesOpeningAggregate = FuturesOpeningStats & { tradeDate: string };

// Published aggregate changes already net additions against reductions. They
// cannot be used to reconstruct the sum of members' positive changes.
export function buildFuturesOpeningStats(
	series: MarketFuturesPositionSeries | null | undefined,
	detail: MarketFuturesMembers | null | undefined,
): FuturesOpeningStats {
	const unavailable = (reason: string): FuturesOpeningStats => ({ longAdded: null, shortAdded: null, netAdded: null, reason });
	const latest = series?.rows.at(-1);
	if (!latest || !detail?.members.length) return unavailable('会员明细未获取，无法计算正增仓合计');
	if (detail.contract_code !== series?.contract_code || detail.trade_date !== latest.trade_date) {
		return unavailable('会员明细与当前合约或数据日期不一致');
	}
	const ranks = new Set(detail.members.map((member) => member.rank));
	if (detail.members.length !== 20 || ranks.size !== 20 || detail.members.some((member) =>
		member.contract !== detail.contract_code || !Number.isInteger(member.rank) || member.rank < 1 || member.rank > 20)) {
		return unavailable('前20会员排名不完整，暂不显示合计');
	}
	const sumPositive = (key: 'long_change' | 'short_change') => {
		let total = 0;
		for (const member of detail.members) {
			const value = member[key];
			if (typeof value !== 'number' || !Number.isSafeInteger(value)) return null;
			total += Math.max(0, value);
		}
		return Number.isSafeInteger(total) ? total : null;
	};
	const longAdded = sumPositive('long_change');
	const shortAdded = sumPositive('short_change');
	return {
		longAdded,
		shortAdded,
		netAdded: longAdded == null || shortAdded == null ? null : longAdded - shortAdded,
		reason: longAdded == null || shortAdded == null ? '部分会员增减字段缺失，相关合计显示 --，不按 0 补齐' : '',
	};
}

export function formatFuturesOpeningHands(value: number | null, signed = false): string {
	if (value == null || !Number.isFinite(value)) return '--';
	return `${signed && value > 0 ? '+' : ''}${value.toLocaleString('zh-CN')} 手`;
}

export function buildFuturesOpeningAggregate(
	entries: Array<{ series: MarketFuturesPositionSeries; members: MarketFuturesMembers | null }>,
): FuturesOpeningAggregate {
	const unavailable = (reason: string): FuturesOpeningAggregate => ({ longAdded: null, shortAdded: null, netAdded: null, tradeDate: '', reason });
	if (entries.length !== 4) return unavailable('四个股指期货品种未全部获取');
	const dates = new Set(entries.map((entry) => entry.series.rows.at(-1)?.trade_date).filter(Boolean));
	if (dates.size !== 1) return unavailable('四个品种的最新交易日不一致');
	const stats = entries.map((entry) => buildFuturesOpeningStats(entry.series, entry.members));
	if (stats.some((stat) => stat.netAdded == null)) return unavailable(`至少一个品种的前20会员明细不可用：${stats.find((stat) => stat.reason)?.reason || '未知原因'}`);
	const longAdded = stats.reduce((sum, stat) => sum + (stat.longAdded || 0), 0);
	const shortAdded = stats.reduce((sum, stat) => sum + (stat.shortAdded || 0), 0);
	return { longAdded, shortAdded, netAdded: longAdded - shortAdded, tradeDate: [...dates][0] || '', reason: '' };
}

export function buildFuturesNamedOpeningAggregate(
	entries: Array<{ series: MarketFuturesPositionSeries; members: MarketFuturesMembers | null }>,
	keyword: string,
): FuturesOpeningAggregate {
	const unavailable = (reason: string): FuturesOpeningAggregate => ({ longAdded: null, shortAdded: null, netAdded: null, tradeDate: '', reason });
	if (!keyword.trim()) return unavailable('会员筛选条件为空');
	if (entries.length !== 4) return unavailable('四个股指期货品种未全部获取');
	const dates = new Set(entries.map((entry) => entry.series.rows.at(-1)?.trade_date).filter(Boolean));
	if (dates.size !== 1) return unavailable('四个品种的最新交易日不一致');
	const stats = entries.map((entry) => buildFuturesOpeningStats(entry.series, entry.members));
	if (stats.some((stat) => stat.netAdded == null)) return unavailable(`至少一个品种的前20会员明细不可用：${stats.find((stat) => stat.reason)?.reason || '未知原因'}`);
	let longAdded = 0;
	let shortAdded = 0;
	for (const entry of entries) {
		for (const member of entry.members!.members) {
			if (member.long_name.includes(keyword) && member.long_change! > 0) longAdded += member.long_change!;
			if (member.short_name.includes(keyword) && member.short_change! > 0) shortAdded += member.short_change!;
		}
	}
	return { longAdded, shortAdded, netAdded: longAdded - shortAdded, tradeDate: [...dates][0] || '', reason: '' };
}
