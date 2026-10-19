import type { SectorFlowRank, SectorFlowSeries, SectorPriceSeries, SourceMeta } from './backend';

export type SectorFlowDimension = 'concept' | 'industry';
/** 'ratio' 按当前资金口径（当日 / 5日 / 10日）的净流入占比排序，'main' | 'five' | 'ten' 按金额排序。 */
export type SectorFlowSortKey = 'main' | 'ratio' | 'change' | 'five' | 'ten';
export type SectorFlowSortDirection = 'desc' | 'asc';
/** 列表资金口径：当日 / 5日 / 10日，由榜单上方的滑动按钮切换。 */
export type SectorFlowPeriod = 'main' | 'five' | 'ten';

/** 后端一次最多接受 12 个板块代码，分时曲线数量上限与之保持一致。 */
export const MAX_SECTOR_FLOW_SERIES = 12;

export const SECTOR_FLOW_SELECTION_STORAGE_KEY = 'easy-stock.sector-flow.selection';

export const SECTOR_FLOW_DIMENSIONS: Array<{ id: SectorFlowDimension; label: string; hint: string }> = [
	{ id: 'concept', label: '概念资金流', hint: '题材概念板块' },
	{ id: 'industry', label: '行业资金流', hint: '东财行业板块' },
];

/** 列表只保留一列资金，口径在 当日 / 5日 / 10日 之间切换，避免三列金额把列宽挤散。 */
export const SECTOR_FLOW_PERIODS: Array<{ id: SectorFlowPeriod; label: string; hint: string }> = [
	{ id: 'main', label: '当日', hint: '当日主力净流入' },
	{ id: 'five', label: '5日', hint: '最近 5 个交易日累计主力净流入' },
	{ id: 'ten', label: '10日', hint: '最近 10 个交易日累计主力净流入' },
];

/** 曲线区口径：资金曲线（东财分时资金，亿元）与板块价格分时（东财分时行情，%）。两者量纲不同，用切换而不是同图叠加。 */
export type SectorFlowChartMode = 'fund' | 'price';

export const SECTOR_FLOW_CHART_MODES: Array<{ id: SectorFlowChartMode; label: string; hint: string }> = [
	{ id: 'fund', label: '资金曲线', hint: '当日累计主力净流入（亿元）' },
	{ id: 'price', label: '价格分时', hint: '板块分时涨跌幅（相对昨收，%）' },
];

// 12 条曲线足以覆盖主线对比，同时保证每条线的颜色可区分。
export const SECTOR_FLOW_SERIES_COLORS = [
	'#e0533d',
	'#2f7fe0',
	'#e39b1f',
	'#2aa37a',
	'#8a5bd6',
	'#c94f9c',
	'#12a0b5',
	'#7a8b2f',
	'#d1622c',
	'#4b5fd0',
	'#0f9d58',
	'#b03a48',
];

export function sectorFlowSortValue(item: SectorFlowRank, key: SectorFlowSortKey, period: SectorFlowPeriod = 'main'): number {
	switch (key) {
		case 'change':
			return item.change_percent;
		case 'ratio':
			return sectorFlowRatioValue(item, period);
		case 'five':
			return item.five_day_main_net_inflow;
		case 'ten':
			return item.ten_day_main_net_inflow;
		default:
			return item.main_net_inflow;
	}
}

/** 列表资金口径对应的主力净流入：当日 / 5 日累计 / 10 日累计。 */
export function sectorFlowNetValue(item: SectorFlowRank, period: SectorFlowPeriod): number {
	switch (period) {
		case 'five':
			return item.five_day_main_net_inflow;
		case 'ten':
			return item.ten_day_main_net_inflow;
		default:
			return item.main_net_inflow;
	}
}

/** 列表资金口径对应的主力净占比：多日口径来自东财 f165/f175。 */
export function sectorFlowRatioValue(item: SectorFlowRank, period: SectorFlowPeriod): number {
	switch (period) {
		case 'five':
			return item.five_day_main_net_inflow_ratio;
		case 'ten':
			return item.ten_day_main_net_inflow_ratio;
		default:
			return item.main_net_inflow_ratio;
	}
}

/**
 * 按当前排序键、方向与筛选条件整理榜单。
 *
 * 方向默认从大到小（净流入榜），切到升序即为净流出榜；同值时用当日主力净流入、
 * 再退化为板块代码保证顺序稳定，避免同一份数据在多次渲染间跳动。
 * period 只影响按占比排序（'ratio'）：占比随资金口径切换。
 */
export function visibleSectorFlowRanks(
	ranks: SectorFlowRank[],
	options: { query?: string; selectedOnly?: boolean; selection?: string[]; sortKey?: SectorFlowSortKey; direction?: SectorFlowSortDirection; period?: SectorFlowPeriod } = {},
): SectorFlowRank[] {
	const query = (options.query || '').trim().toLowerCase();
	const selectedOnly = Boolean(options.selectedOnly);
	const selection = options.selection || [];
	const sortKey = options.sortKey || 'main';
	const period = options.period || 'main';
	const sign = options.direction === 'asc' ? -1 : 1;
	const filtered = ranks.filter((item) => {
		if (selectedOnly && !selection.includes(item.code)) return false;
		if (!query) return true;
		return `${item.name}${item.code}`.toLowerCase().includes(query);
	});
	return [...filtered].sort((left, right) => {
		const delta = sectorFlowSortValue(right, sortKey, period) - sectorFlowSortValue(left, sortKey, period);
		if (delta !== 0) return delta * sign;
		const tie = right.main_net_inflow - left.main_net_inflow;
		if (tie !== 0) return tie * sign;
		return left.code.localeCompare(right.code);
	});
}

/**
 * 切换某个板块的分时曲线。达到上限时保持原样并通过 limited 告知调用方，
 * 由界面提示用户先移除一个板块。
 */
export function toggleSectorFlowSelection(
	selection: string[],
	code: string,
	max: number = MAX_SECTOR_FLOW_SERIES,
): { selection: string[]; limited: boolean } {
	if (selection.includes(code)) {
		return { selection: selection.filter((item) => item !== code), limited: false };
	}
	if (selection.length >= max) {
		return { selection, limited: true };
	}
	return { selection: [...selection, code], limited: false };
}

export function sectorFlowSeriesColor(index: number): string {
	const normalized = ((index % SECTOR_FLOW_SERIES_COLORS.length) + SECTOR_FLOW_SERIES_COLORS.length) % SECTOR_FLOW_SERIES_COLORS.length;
	return SECTOR_FLOW_SERIES_COLORS[normalized];
}

export function sectorFlowTone(value: number): 'up' | 'down' | 'flat' {
	if (value > 0) return 'up';
	if (value < 0) return 'down';
	return 'flat';
}

/** 曲线右端标签用的板块名，过长时截断避免互相重叠。 */
export function shortSectorFlowName(name: string, limit = 10): string {
	return name.length > limit ? `${name.slice(0, limit)}…` : name;
}

/**
 * 末端标签的像素宽度估算：中日韩字符按 12px、其余按 7px 计，
 * 用来给曲线留出恰好够用的右侧标签区，而不是固定留一大块空白。
 */
export function estimateSectorFlowLabelWidth(text: string): number {
	let width = 0;
	for (const char of text) width += /[\u2e80-\u9fff\uff00-\uffef]/.test(char) ? 12 : 7;
	return width;
}

/** 纵轴左侧留白：容纳「-1,279亿」这类刻度文字 + 刻度短线。 */
export const SECTOR_FLOW_AXIS_GUTTER = 76;

/** 没有曲线时的占位纵轴量级（±40亿）：空框架也有一套完整刻度，而不是只有刻度线没有数字。 */
export const SECTOR_FLOW_EMPTY_AXIS_EXTENT = 4_000_000_000;

/** 价格分时空框架的占位量级（±2%）：与资金曲线同构，只是纵轴换成涨跌幅。 */
export const SECTOR_PRICE_EMPTY_AXIS_EXTENT = 2;

/**
 * 曲线图上的一条曲线：把资金序列与价格序列统一成「时间 → 数值」，
 * 让同一个曲线组件既能画资金曲线，也能画板块价格分时。
 */
export type SectorFlowCurve = {
	code: string;
	name: string;
	trade_date: string;
	points: Array<{ time: string; value: number }>;
};

/** 资金曲线：数值取当日累计主力净流入（主力 = 超大单 + 大单）。 */
export function sectorFlowFundCurves(series: SectorFlowSeries[]): SectorFlowCurve[] {
	return series.map((item) => ({
		code: item.code,
		name: item.name,
		trade_date: item.trade_date,
		points: item.points.map((point) => ({ time: point.time, value: point.main })),
	}));
}

/** 板块价格分时：数值取相对昨收的涨跌幅，不同点位高度的板块才能叠在同一个纵轴上。 */
export function sectorFlowPriceCurves(series: SectorPriceSeries[]): SectorFlowCurve[] {
	return series.map((item) => ({
		code: item.code,
		name: item.name,
		trade_date: item.trade_date,
		points: item.points.map((point) => ({ time: point.time, value: point.change_percent })),
	}));
}

/**
 * 曲线右侧标签区宽度：按最长标签实际需要给（外加 26px 连线与间距），
 * 夹在 104–176px 之间；没有曲线时与左侧纵轴留白等宽，空框架左右对称。
 */
export function sectorFlowLabelGutter(labels: string[]): number {
	if (!labels.length) return SECTOR_FLOW_AXIS_GUTTER;
	const widest = Math.max(...labels.map((label) => estimateSectorFlowLabelWidth(label)));
	return Math.min(176, Math.max(104, Math.ceil(widest) + 26));
}

export function formatSectorFlowAmount(value: number): string {
	if (!Number.isFinite(value)) return '--';
	return `${value > 0 ? '+' : ''}${(value / 100_000_000).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}亿`;
}

export function formatSectorFlowPercent(value: number): string {
	if (!Number.isFinite(value)) return '--';
	return `${value > 0 ? '+' : ''}${value.toFixed(2)}%`;
}

export function formatSectorFlowAxis(value: number): string {
	return `${(value / 100_000_000).toLocaleString('zh-CN', { maximumFractionDigits: 1 })}亿`;
}

/** 价格分时纵轴刻度：直接是涨跌幅百分数，去掉多余的小数位（-2% / 0.5%）。 */
export function formatSectorFlowPriceAxis(value: number): string {
	if (!Number.isFinite(value)) return '--';
	return `${Number(value.toFixed(2))}%`;
}

/**
 * 纵轴取整刻度：按数据范围挑一个「好看」的步长（1 / 2 / 2.5 / 5 × 10ⁿ），
 * 标签落在 20亿、40亿 这类整数上，而不是均分算出来的 12 / 30 / 49 这种零头。
 * 返回的 min / max 已经外扩到刻度边界，曲线不会被切掉。
 */
export function sectorFlowAxisScale(min: number, max: number, steps = 4): { min: number; max: number; ticks: number[] } {
	const lower = Number.isFinite(min) ? min : 0;
	const upper = Number.isFinite(max) && max > lower ? max : lower + 1;
	const rough = (upper - lower) / Math.max(1, steps);
	const magnitude = 10 ** Math.floor(Math.log10(rough));
	const normalized = rough / magnitude;
	const step = (normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 2.5 ? 2.5 : normalized <= 5 ? 5 : 10) * magnitude;
	const start = Math.floor(lower / step) * step;
	const end = Math.ceil(upper / step) * step;
	const ticks: number[] = [];
	for (let value = start; value <= end + step / 2; value += step) ticks.push(value);
	return { min: start, max: end, ticks };
}

export function readSectorFlowSelection(storage: Pick<Storage, 'getItem'> | null | undefined, max: number = MAX_SECTOR_FLOW_SERIES): string[] {
	if (!storage) return [];
	try {
		const raw = storage.getItem(SECTOR_FLOW_SELECTION_STORAGE_KEY);
		if (!raw) return [];
		const parsed: unknown = JSON.parse(raw);
		if (!Array.isArray(parsed)) return [];
		return parsed.filter((item): item is string => typeof item === 'string' && item.length > 0).slice(0, max);
	} catch {
		return [];
	}
}

export function writeSectorFlowSelection(storage: Pick<Storage, 'setItem'> | null | undefined, selection: string[]): void {
	if (!storage) return;
	try {
		storage.setItem(SECTOR_FLOW_SELECTION_STORAGE_KEY, JSON.stringify(selection));
	} catch {
		// 隐私模式下 localStorage 可能不可写，选中状态保留在内存即可。
	}
}

/** 榜单与分时数据统一交给 AI 时使用的提示词，只包含带来源的时间序列事实。 */
export function buildSectorFlowPrompt(input: {
	dimension: SectorFlowDimension;
	ranks: SectorFlowRank[];
	series: SectorFlowSeries[];
	meta?: SourceMeta | null;
	time?: string;
}): string {
	const { dimension, ranks, series, meta } = input;
	const scope = dimension === 'concept' ? '概念板块' : '行业板块';
	const ranked = [...ranks].sort((left, right) => right.main_net_inflow - left.main_net_inflow);
	const inflow = ranked.slice(0, 8);
	const outflow = [...ranked].reverse().slice(0, 5);
	const describe = (item: SectorFlowRank) =>
		`${item.name} 当日 ${formatSectorFlowAmount(item.main_net_inflow)}（净占比 ${formatSectorFlowPercent(item.main_net_inflow_ratio)}，涨幅 ${formatSectorFlowPercent(item.change_percent)}），5日 ${formatSectorFlowAmount(item.five_day_main_net_inflow)}（净占比 ${formatSectorFlowPercent(item.five_day_main_net_inflow_ratio)}），10日 ${formatSectorFlowAmount(item.ten_day_main_net_inflow)}（净占比 ${formatSectorFlowPercent(item.ten_day_main_net_inflow_ratio)}）`;
	const curves = series.map((item) => {
		const last = item.points[item.points.length - 1];
		return `${item.name}（${item.trade_date || '未知日期'} 收盘累计 ${formatSectorFlowAmount(last ? last.main : item.latest_main)}）`;
	});
	return [
		`请基于东方财富板块资金流数据（${scope}${meta?.source ? `，来源 ${meta.source}` : ''}${meta?.fetched_at ? `，取数时间 ${meta.fetched_at}` : ''}）解读当前资金结构：`,
		`净流入前列：${inflow.length ? inflow.map(describe).join('；') : '暂无'}`,
		`净流出前列：${outflow.length ? outflow.map(describe).join('；') : '暂无'}`,
		curves.length ? `分时对比曲线：${curves.join('、')}` : '',
		'请指出当日资金与多日趋势（5日/10日）背离的板块、可能的资金主线与退潮方向，说明哪些结论还需要更多证据确认。不要编造数据，沿用给定时间点。',
	]
		.filter(Boolean)
		.join('\n');
}
