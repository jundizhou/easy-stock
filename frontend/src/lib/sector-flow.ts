import type { SectorFlowRank, SectorFlowSeries, SourceMeta } from './backend';

export type SectorFlowDimension = 'concept' | 'industry';
export type SectorFlowSortKey = 'main' | 'change' | 'five' | 'ten';

/** 后端一次最多接受 12 个板块代码，分时曲线数量上限与之保持一致。 */
export const MAX_SECTOR_FLOW_SERIES = 12;

export const SECTOR_FLOW_SELECTION_STORAGE_KEY = 'easy-stock.sector-flow.selection';

export const SECTOR_FLOW_DIMENSIONS: Array<{ id: SectorFlowDimension; label: string; hint: string }> = [
	{ id: 'concept', label: '概念资金流', hint: '题材概念板块' },
	{ id: 'industry', label: '行业资金流', hint: '东财行业板块' },
];

export const SECTOR_FLOW_SORT_OPTIONS: Array<{ key: SectorFlowSortKey; label: string; hint: string }> = [
	{ key: 'main', label: '当日', hint: '按当日主力净流入排序' },
	{ key: 'five', label: '5日', hint: '按最近 5 个交易日累计主力净流入排序' },
	{ key: 'ten', label: '10日', hint: '按最近 10 个交易日累计主力净流入排序' },
	{ key: 'change', label: '涨幅', hint: '按当日涨跌幅排序' },
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

export function sectorFlowSortValue(item: SectorFlowRank, key: SectorFlowSortKey): number {
	switch (key) {
		case 'change':
			return item.change_percent;
		case 'five':
			return item.five_day_main_net_inflow;
		case 'ten':
			return item.ten_day_main_net_inflow;
		default:
			return item.main_net_inflow;
	}
}

/**
 * 按当前排序键与筛选条件整理榜单。
 *
 * 排序方向固定为从大到小（净流入、涨幅都由高到低），同值时用当日主力净流入、
 * 再退化为板块代码保证顺序稳定，避免同一份数据在多次渲染间跳动。
 */
export function visibleSectorFlowRanks(
	ranks: SectorFlowRank[],
	options: { query?: string; selectedOnly?: boolean; selection?: string[]; sortKey?: SectorFlowSortKey } = {},
): SectorFlowRank[] {
	const query = (options.query || '').trim().toLowerCase();
	const selectedOnly = Boolean(options.selectedOnly);
	const selection = options.selection || [];
	const sortKey = options.sortKey || 'main';
	const filtered = ranks.filter((item) => {
		if (selectedOnly && !selection.includes(item.code)) return false;
		if (!query) return true;
		return `${item.name}${item.code}`.toLowerCase().includes(query);
	});
	return [...filtered].sort((left, right) => {
		const delta = sectorFlowSortValue(right, sortKey) - sectorFlowSortValue(left, sortKey);
		if (delta !== 0) return delta;
		const tie = right.main_net_inflow - left.main_net_inflow;
		if (tie !== 0) return tie;
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

export function formatSectorFlowAmount(value: number): string {
	if (!Number.isFinite(value)) return '--';
	return `${value > 0 ? '+' : ''}${(value / 100_000_000).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}亿`;
}

export function formatSectorFlowPercent(value: number): string {
	if (!Number.isFinite(value)) return '--';
	return `${value > 0 ? '+' : ''}${value.toFixed(2)}%`;
}

export function formatSectorFlowAxis(value: number): string {
	return (value / 100_000_000).toLocaleString('zh-CN', { maximumFractionDigits: 0 });
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
		`${item.name} 当日 ${formatSectorFlowAmount(item.main_net_inflow)}（净占比 ${formatSectorFlowPercent(item.main_net_inflow_ratio)}，涨幅 ${formatSectorFlowPercent(item.change_percent)}，5日 ${formatSectorFlowAmount(item.five_day_main_net_inflow)}，10日 ${formatSectorFlowAmount(item.ten_day_main_net_inflow)}）`;
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
