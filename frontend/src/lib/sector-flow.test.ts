import { describe, expect, it } from 'vitest';
import type { SectorFlowRank, SectorFlowSeries, SectorPriceSeries, SourceMeta } from './backend';
import {
	SECTOR_FLOW_AXIS_GUTTER,
	SECTOR_FLOW_EMPTY_AXIS_EXTENT,
	SECTOR_FLOW_SELECTION_STORAGE_KEY,
	SECTOR_PRICE_EMPTY_AXIS_EXTENT,
	buildSectorFlowPrompt,
	defaultSectorFlowSelection,
	estimateSectorFlowLabelWidth,
	formatSectorFlowAmount,
	formatSectorFlowAxis,
	formatSectorFlowPercent,
	formatSectorFlowPriceAxis,
	nearestSectorFlowCurve,
	readSectorFlowSelection,
	sectorFlowAxisScale,
	sectorFlowFundCurves,
	sectorFlowLabelGutter,
	sectorFlowPriceCurves,
	sectorFlowSeriesColor,
	sectorFlowNetValue,
	sectorFlowRatioValue,
	sectorFlowTone,
	shortSectorFlowName,
	toggleSectorFlowSelection,
	visibleSectorFlowRanks,
	writeSectorFlowSelection,
} from './sector-flow';

const meta: SourceMeta = { source: 'eastmoney:bkzj', fetched_at: '2026-10-16T15:01:00+08:00', latency_ms: 320, stale: false };

function rank(overrides: Partial<SectorFlowRank> & { code: string; name: string }): SectorFlowRank {
	return {
		change_percent: 0,
		main_net_inflow: 0,
		main_net_inflow_ratio: 0,
		super_large_net_inflow: 0,
		large_net_inflow: 0,
		medium_net_inflow: 0,
		small_net_inflow: 0,
		five_day_main_net_inflow: 0,
		five_day_main_net_inflow_ratio: 0,
		ten_day_main_net_inflow: 0,
		ten_day_main_net_inflow_ratio: 0,
		meta,
		...overrides,
	};
}

const ranks: SectorFlowRank[] = [
	rank({ code: 'BK0917', name: '半导体概念', main_net_inflow: 3_981_310_464, main_net_inflow_ratio: 11.72, change_percent: -0.18, five_day_main_net_inflow: -26_905_739_776, five_day_main_net_inflow_ratio: -12.34, ten_day_main_net_inflow: -99_373_526_272, ten_day_main_net_inflow_ratio: 25 }),
	rank({ code: 'BK0490', name: '军工', main_net_inflow: 3_723_490_560, main_net_inflow_ratio: 9.2, change_percent: 2.5, five_day_main_net_inflow: -100_000, five_day_main_net_inflow_ratio: -0.56, ten_day_main_net_inflow: -300_000, ten_day_main_net_inflow_ratio: 1 }),
	rank({ code: 'BK1137', name: '存储芯片', main_net_inflow: 3_636_000_000, main_net_inflow_ratio: -3.21, change_percent: -0.16, five_day_main_net_inflow: 800_000_000, five_day_main_net_inflow_ratio: 4.5, ten_day_main_net_inflow: 1_500_000_000, ten_day_main_net_inflow_ratio: 6.25 }),
];

describe('sector flow ranking', () => {
	it('sorts by main net inflow by default and supports every sort key', () => {
		expect(visibleSectorFlowRanks(ranks).map((item) => item.code)).toEqual(['BK0917', 'BK0490', 'BK1137']);
		expect(visibleSectorFlowRanks(ranks, { sortKey: 'change' }).map((item) => item.name)).toEqual(['军工', '存储芯片', '半导体概念']);
		expect(visibleSectorFlowRanks(ranks, { sortKey: 'five' }).map((item) => item.name)).toEqual(['存储芯片', '军工', '半导体概念']);
		expect(visibleSectorFlowRanks(ranks, { sortKey: 'ten' }).map((item) => item.name)).toEqual(['存储芯片', '军工', '半导体概念']);
		// 净流入占比列也能排序，且跟随资金口径（当日 / 5日 / 10日）取对应占比。
		expect(visibleSectorFlowRanks(ranks, { sortKey: 'ratio' }).map((item) => item.name)).toEqual(['半导体概念', '军工', '存储芯片']);
		expect(visibleSectorFlowRanks(ranks, { sortKey: 'ratio', period: 'five' }).map((item) => item.name)).toEqual(['存储芯片', '军工', '半导体概念']);
		expect(visibleSectorFlowRanks(ranks, { sortKey: 'ratio', period: 'ten' }).map((item) => item.name)).toEqual(['半导体概念', '存储芯片', '军工']);
		expect(visibleSectorFlowRanks(ranks, { sortKey: 'ratio', period: 'five', direction: 'asc' }).map((item) => item.name)).toEqual(['半导体概念', '军工', '存储芯片']);
	});

	it('picks the net inflow and ratio that match the selected period', () => {
		const item = ranks[0];
		expect(sectorFlowNetValue(item, 'main')).toBe(item.main_net_inflow);
		expect(sectorFlowNetValue(item, 'five')).toBe(item.five_day_main_net_inflow);
		expect(sectorFlowNetValue(item, 'ten')).toBe(item.ten_day_main_net_inflow);
		expect(sectorFlowRatioValue(item, 'main')).toBe(item.main_net_inflow_ratio);
		expect(sectorFlowRatioValue(item, 'five')).toBe(item.five_day_main_net_inflow_ratio);
		expect(sectorFlowRatioValue(item, 'ten')).toBe(item.ten_day_main_net_inflow_ratio);
		expect(sectorFlowRatioValue(ranks[2], 'five')).toBe(4.5);
	});

	it('filters by keyword and by selected codes without mutating the input', () => {
		const filtered = visibleSectorFlowRanks(ranks, { query: '芯片' });
		expect(filtered.map((item) => item.code)).toEqual(['BK1137']);
		expect(visibleSectorFlowRanks(ranks, { selectedOnly: true, selection: ['BK0490', 'BK1137'] }).map((item) => item.code)).toEqual(['BK0490', 'BK1137']);
		expect(visibleSectorFlowRanks(ranks, { query: 'BK0490' }).map((item) => item.name)).toEqual(['军工']);
		expect(ranks.map((item) => item.code)).toEqual(['BK0917', 'BK0490', 'BK1137']);
	});

	it('keeps ordering stable for equal values', () => {
		const ties = [rank({ code: 'BK0002', name: '乙', main_net_inflow: 100 }), rank({ code: 'BK0001', name: '甲', main_net_inflow: 100 })];
		expect(visibleSectorFlowRanks(ties).map((item) => item.code)).toEqual(['BK0001', 'BK0002']);
	});

	it('flips to the outflow board in ascending order', () => {
		expect(visibleSectorFlowRanks(ranks, { direction: 'asc' }).map((item) => item.code)).toEqual(['BK1137', 'BK0490', 'BK0917']);
		expect(visibleSectorFlowRanks(ranks, { sortKey: 'five', direction: 'asc' }).map((item) => item.name)).toEqual(['半导体概念', '军工', '存储芯片']);
		expect(visibleSectorFlowRanks(ranks, { sortKey: 'change', direction: 'asc' }).map((item) => item.name)).toEqual(['半导体概念', '存储芯片', '军工']);
		expect(visibleSectorFlowRanks(ranks, { query: '芯片', direction: 'asc' }).map((item) => item.code)).toEqual(['BK1137']);
	});
});

describe('sector flow selection', () => {
	it('defaults to the three largest inflows and outflows regardless of input order', () => {
		const items = [0, -20, 10, -40, 40, -10, 30, -30, 20, Number.NaN].map((value, index) => rank({
			code: `BK${index}`, name: `板块${index}`, main_net_inflow: value,
			// 默认曲线始终按当日资金选择，不受涨幅、多日资金排名影响。
			change_percent: -value, five_day_main_net_inflow: -value,
		}));
		expect(defaultSectorFlowSelection(items)).toEqual(['BK4', 'BK6', 'BK8', 'BK3', 'BK7', 'BK1']);
		expect(items[0].main_net_inflow).toBe(0);
	});

	it('uses only available inflows and outflows without filling with zero or opposite-side boards', () => {
		expect(defaultSectorFlowSelection([])).toEqual([]);
		expect(defaultSectorFlowSelection([
			rank({ code: 'BK0003', name: '零值' }),
			rank({ code: 'BK0002', name: '乙', main_net_inflow: 100 }),
			rank({ code: 'BK0001', name: '甲', main_net_inflow: 100 }),
			rank({ code: 'BK0004', name: '流出', main_net_inflow: -200 }),
		])).toEqual(['BK0001', 'BK0002', 'BK0004']);
		expect(defaultSectorFlowSelection(ranks)).toEqual(['BK0917', 'BK0490', 'BK1137']);
	});

	it('adds and removes boards without a cap', () => {
		const added = toggleSectorFlowSelection(['BK0917'], 'BK0490');
		expect(added).toEqual(['BK0917', 'BK0490']);
		expect(toggleSectorFlowSelection(added, 'BK0917')).toEqual(['BK0490']);
		// 选中数量不设上限：第 13 个、第 40 个板块照样加进去。
		const many = Array.from({ length: 40 }, (_, index) => `BK${String(index).padStart(4, '0')}`);
		expect(toggleSectorFlowSelection(many, 'BK9999')).toHaveLength(41);
		expect(toggleSectorFlowSelection(many, 'BK9999')).toContain('BK9999');
	});
});

describe('sector flow formatting', () => {
	it('formats amounts in hundred million with sign', () => {
		expect(formatSectorFlowAmount(3_981_310_464)).toBe('+39.81亿');
		expect(formatSectorFlowAmount(-26_905_739_776)).toBe('-269.06亿');
		expect(formatSectorFlowAmount(0)).toBe('0.00亿');
		expect(formatSectorFlowAmount(Number.NaN)).toBe('--');
	});

	it('formats percents and tones', () => {
		expect(formatSectorFlowPercent(11.72)).toBe('+11.72%');
		expect(formatSectorFlowPercent(-0.18)).toBe('-0.18%');
		expect(sectorFlowTone(1)).toBe('up');
		expect(sectorFlowTone(-1)).toBe('down');
		expect(sectorFlowTone(0)).toBe('flat');
	});

	it('keeps series colors distinct beyond the 12-colour palette and truncates long names', () => {
		const colors = Array.from({ length: 40 }, (_, index) => sectorFlowSeriesColor(index));
		// 前 12 条用品牌色且互不重复；第 13 条起按色相生成，不再回到第 1 条的颜色。
		expect(new Set(colors.slice(0, 12)).size).toBe(12);
		expect(sectorFlowSeriesColor(12)).not.toBe(colors[0]);
		expect(new Set(colors).size).toBeGreaterThanOrEqual(30);
		expect(shortSectorFlowName('光通信模块')).toBe('光通信模块');
		expect(shortSectorFlowName('汽车一体化压铸产业概念')).toBe('汽车一体化压铸产业概…');
	});

	it('labels the axis with rounded values and the 亿 unit', () => {
		expect(formatSectorFlowAxis(2_000_000_000)).toBe('20亿');
		expect(formatSectorFlowAxis(-3_000_000_000)).toBe('-30亿');
		expect(formatSectorFlowAxis(0)).toBe('0亿');
	});

	it('sizes the right gutter from the actual end labels', () => {
		// 没有曲线时右侧不留标签区，与左侧纵轴留白等宽，空框架左右对称。
		expect(sectorFlowLabelGutter([])).toBe(SECTOR_FLOW_AXIS_GUTTER);
		// 一个中文板块名 + 金额大约 120px，加连线与间距后落在 104–176 之间。
		const gutter = sectorFlowLabelGutter(['半导体概念 +39.81亿']);
		expect(gutter).toBeGreaterThan(104);
		expect(gutter).toBeLessThan(176);
		expect(estimateSectorFlowLabelWidth('半导体概念')).toBe(60);
		expect(estimateSectorFlowLabelWidth('+3.75%')).toBe(42);
		// 超长标签最多也只占到 176px，不会把曲线区挤没。
		expect(sectorFlowLabelGutter(['汽车一体化压铸产业概念 +1,279.23亿'])).toBe(176);
	});

	it('picks the curve under the pointer for hover highlighting', () => {
		const curves = [
			{ code: 'BK0917', name: '半导体概念', trade_date: '2026-10-16', points: [{ time: '09:31', value: 10 }, { time: '09:32', value: 12 }] },
			{ code: 'BK0490', name: '军工', trade_date: '2026-10-16', points: [{ time: '09:31', value: -10 }] },
		];
		// 纵轴换算：数值越大越靠上（y 越小）。
		const positionY = (value: number) => 100 - value * 2;
		expect(nearestSectorFlowCurve(curves, '09:31', 82, positionY)).toBe('BK0917');
		expect(nearestSectorFlowCurve(curves, '09:31', 118, positionY)).toBe('BK0490');
		// 离所有曲线都超过 16px 的空白处不高亮。
		expect(nearestSectorFlowCurve(curves, '09:31', 100, positionY)).toBeNull();
		// 该分钟没有数据的曲线不参与命中（军工缺 09:32）。
		expect(nearestSectorFlowCurve(curves, '09:32', 118, positionY)).toBeNull();
	});

	it('gives the empty frame a labelled placeholder axis', () => {
		// 没有曲线时也有一套完整的纵轴数字：±40亿，20亿一档。
		const empty = sectorFlowAxisScale(-SECTOR_FLOW_EMPTY_AXIS_EXTENT, SECTOR_FLOW_EMPTY_AXIS_EXTENT);
		expect(empty.ticks.map(formatSectorFlowAxis)).toEqual(['-40亿', '-20亿', '0亿', '20亿', '40亿']);
	});

	it('snaps the axis to round steps that still cover the data', () => {
		// 半导体概念当日峰值约 79 亿：刻度取 20 亿一档，曲线顶端仍然在轴内。
		const up = sectorFlowAxisScale(0, 7_945_318_234);
		expect(up.min).toBe(0);
		expect(up.max).toBe(8_000_000_000);
		expect(up.ticks).toEqual([0, 2_000_000_000, 4_000_000_000, 6_000_000_000, 8_000_000_000]);
		// 净流出为主的一天：轴向下外扩到整数刻度，0 线仍在轴上。
		const down = sectorFlowAxisScale(-2_690_573_976, 0);
		expect(down.min).toBe(-3_000_000_000);
		expect(down.max).toBe(0);
		expect(down.ticks.at(-1)).toBe(0);
		// 没有数据时给一套对称空轴，0 线落在中间。
		const empty = sectorFlowAxisScale(-1, 1);
		expect(empty.min).toBe(-1);
		expect(empty.max).toBe(1);
		expect(empty.ticks).toContain(0);
	});
});

// 资金曲线与价格分时共用同一个曲线组件，这里固定两者的「时间 → 数值」映射口径。
describe('sector flow curves', () => {
	const fundSeries: SectorFlowSeries = {
		code: 'BK0917',
		name: '半导体概念',
		trade_date: '2026-10-16',
		points: [
			{ time: '09:31', main: 545_160_864, super_large: 480_172_992, large: 64_987_872, medium: -181_177_728, small: -363_358_256 },
			{ time: '15:00', main: 3_981_310_464, super_large: 830_138_880, large: 3_151_171_584, medium: -944_193_536, small: -3_068_276_736 },
		],
		latest_main: 3_981_310_464,
		meta,
	};

	const priceSeries: SectorPriceSeries = {
		code: 'BK0917',
		name: '半导体概念',
		trade_date: '2026-10-16',
		pre_close: 2419.01,
		points: [
			{ time: '09:30', price: 2468.24, average: 2468.238, change_percent: 2.035 },
			{ time: '15:00', price: 2414.76, average: 2439.12, change_percent: -0.176 },
		],
		latest_change_percent: -0.176,
		meta,
	};

	it('maps fund series to the cumulative main net inflow', () => {
		expect(sectorFlowFundCurves([fundSeries])).toEqual([{
			code: 'BK0917',
			name: '半导体概念',
			trade_date: '2026-10-16',
			points: [{ time: '09:31', value: 545_160_864 }, { time: '15:00', value: 3_981_310_464 }],
		}]);
	});

	it('maps price series to the change percent so boards share one axis', () => {
		expect(sectorFlowPriceCurves([priceSeries])).toEqual([{
			code: 'BK0917',
			name: '半导体概念',
			trade_date: '2026-10-16',
			points: [{ time: '09:30', value: 2.035 }, { time: '15:00', value: -0.176 }],
		}]);
	});

	it('labels the price axis in percent without trailing zeros', () => {
		expect(formatSectorFlowPriceAxis(-2)).toBe('-2%');
		expect(formatSectorFlowPriceAxis(0.5)).toBe('0.5%');
		expect(formatSectorFlowPriceAxis(0)).toBe('0%');
		expect(formatSectorFlowPriceAxis(Number.NaN)).toBe('--');
	});

	it('gives the price chart a percent placeholder axis', () => {
		const empty = sectorFlowAxisScale(-SECTOR_PRICE_EMPTY_AXIS_EXTENT, SECTOR_PRICE_EMPTY_AXIS_EXTENT);
		expect(empty.ticks.map(formatSectorFlowPriceAxis)).toEqual(['-2%', '-1%', '0%', '1%', '2%']);
	});
});

describe('sector flow selection persistence', () => {
	it('reads valid entries, drops junk and keeps the whole list', () => {
		const storage = { getItem: (key: string) => (key === `${SECTOR_FLOW_SELECTION_STORAGE_KEY}.concept` ? JSON.stringify(['BK0917', 42, '', 'BK0490']) : null) };
		expect(readSectorFlowSelection(storage, 'concept')).toEqual(['BK0917', 'BK0490']);
		expect(readSectorFlowSelection({ getItem: () => '{not json' }, 'concept')).toBeNull();
		expect(readSectorFlowSelection({ getItem: () => '{"a":1}' }, 'concept')).toBeNull();
		expect(readSectorFlowSelection(null, 'concept')).toBeNull();
		const many = { getItem: () => JSON.stringify(Array.from({ length: 40 }, (_, index) => `BK${index}`)) };
		expect(readSectorFlowSelection(many, 'concept')).toHaveLength(40);
	});

	it('writes selections and tolerates unavailable storage', () => {
		const writes: Array<[string, string]> = [];
		writeSectorFlowSelection({ setItem: (key: string, value: string) => writes.push([key, value]) }, 'concept', ['BK0917']);
		expect(writes).toEqual([[`${SECTOR_FLOW_SELECTION_STORAGE_KEY}.concept`, '["BK0917"]']]);
		expect(() => writeSectorFlowSelection({ setItem: () => { throw new Error('quota'); } }, 'concept', ['BK0917'])).not.toThrow();
		expect(() => writeSectorFlowSelection(null, 'concept', ['BK0917'])).not.toThrow();
	});

	it('keeps concept and industry independent and distinguishes manual clearing from first use', () => {
		const values = new Map<string, string>([[SECTOR_FLOW_SELECTION_STORAGE_KEY, '["BK0917"]']]);
		const storage = { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value); } };
		// 旧版共用列表没有维度信息，不能作为任何维度的手动选择。
		expect(readSectorFlowSelection(storage, 'concept')).toBeNull();
		expect(readSectorFlowSelection(storage, 'industry')).toBeNull();
		writeSectorFlowSelection(storage, 'concept', ['BK0917']);
		writeSectorFlowSelection(storage, 'industry', ['BK0475']);
		writeSectorFlowSelection(storage, 'concept', []);
		expect(readSectorFlowSelection(storage, 'concept')).toEqual([]);
		expect(readSectorFlowSelection(storage, 'industry')).toEqual(['BK0475']);
	});
});

describe('sector flow AI prompt', () => {
	const series: SectorFlowSeries[] = [{
		code: 'BK0917',
		name: '半导体概念',
		trade_date: '2026-10-16',
		points: [{ time: '09:31', main: 1_000, super_large: 900, large: 100, medium: -400, small: -600 }, { time: '15:00', main: 3_981_310_464, super_large: 830_138_880, large: 3_151_171_584, medium: -3_068_276_736, small: -944_193_536 }],
		latest_main: 3_981_310_464,
		meta,
	}];

	it('carries ranking, multi-day and curve evidence with the source', () => {
		const prompt = buildSectorFlowPrompt({ dimension: 'concept', ranks, series, meta });
		expect(prompt).toContain('概念板块');
		expect(prompt).toContain('eastmoney:bkzj');
		expect(prompt).toContain('半导体概念 当日 +39.81亿');
		expect(prompt).toContain('5日 -269.06亿');
		expect(prompt).toContain('10日 -993.74亿');
		expect(prompt).toContain('半导体概念（2026-10-16 收盘累计 +39.81亿）');
		expect(prompt).toContain('不要编造数据');
	});

	it('describes the industry dimension and empty selections', () => {
		const prompt = buildSectorFlowPrompt({ dimension: 'industry', ranks: [], series: [] });
		expect(prompt).toContain('行业板块');
		expect(prompt).toContain('净流入前列：暂无');
		expect(prompt).not.toContain('分时对比曲线');
	});
});
