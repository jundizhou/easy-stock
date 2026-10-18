import { describe, expect, it } from 'vitest';
import type { SectorFlowRank, SectorFlowSeries, SourceMeta } from './backend';
import {
	MAX_SECTOR_FLOW_SERIES,
	SECTOR_FLOW_SELECTION_STORAGE_KEY,
	buildSectorFlowPrompt,
	formatSectorFlowAmount,
	formatSectorFlowPercent,
	readSectorFlowSelection,
	sectorFlowSeriesColor,
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
		ten_day_main_net_inflow: 0,
		meta,
		...overrides,
	};
}

const ranks: SectorFlowRank[] = [
	rank({ code: 'BK0917', name: '半导体概念', main_net_inflow: 3_981_310_464, main_net_inflow_ratio: 11.72, change_percent: -0.18, five_day_main_net_inflow: -26_905_739_776, ten_day_main_net_inflow: -99_373_526_272 }),
	rank({ code: 'BK0490', name: '军工', main_net_inflow: 3_723_490_560, main_net_inflow_ratio: 9.2, change_percent: 2.5, five_day_main_net_inflow: -100_000, ten_day_main_net_inflow: -300_000 }),
	rank({ code: 'BK1137', name: '存储芯片', main_net_inflow: 3_636_000_000, main_net_inflow_ratio: -3.21, change_percent: -0.16, five_day_main_net_inflow: 800_000_000, ten_day_main_net_inflow: 1_500_000_000 }),
];

describe('sector flow ranking', () => {
	it('sorts by main net inflow by default and supports every sort key', () => {
		expect(visibleSectorFlowRanks(ranks).map((item) => item.code)).toEqual(['BK0917', 'BK0490', 'BK1137']);
		expect(visibleSectorFlowRanks(ranks, { sortKey: 'change' }).map((item) => item.name)).toEqual(['军工', '存储芯片', '半导体概念']);
		expect(visibleSectorFlowRanks(ranks, { sortKey: 'five' }).map((item) => item.name)).toEqual(['存储芯片', '军工', '半导体概念']);
		expect(visibleSectorFlowRanks(ranks, { sortKey: 'ten' }).map((item) => item.name)).toEqual(['存储芯片', '军工', '半导体概念']);
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
});

describe('sector flow selection', () => {
	it('adds and removes boards', () => {
		const added = toggleSectorFlowSelection(['BK0917'], 'BK0490');
		expect(added).toEqual({ selection: ['BK0917', 'BK0490'], limited: false });
		expect(toggleSectorFlowSelection(added.selection, 'BK0917').selection).toEqual(['BK0490']);
	});

	it('reports the limit instead of silently dropping a board', () => {
		const full = Array.from({ length: MAX_SECTOR_FLOW_SERIES }, (_, index) => `BK${String(index).padStart(4, '0')}`);
		expect(toggleSectorFlowSelection(full, 'BK9999')).toEqual({ selection: full, limited: true });
		expect(toggleSectorFlowSelection(full, full[0]).selection).toHaveLength(MAX_SECTOR_FLOW_SERIES - 1);
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

	it('keeps series colors unique within the limit and truncates long names', () => {
		const colors = Array.from({ length: MAX_SECTOR_FLOW_SERIES }, (_, index) => sectorFlowSeriesColor(index));
		expect(new Set(colors).size).toBe(MAX_SECTOR_FLOW_SERIES);
		expect(sectorFlowSeriesColor(MAX_SECTOR_FLOW_SERIES)).toBe(colors[0]);
		expect(shortSectorFlowName('光通信模块')).toBe('光通信模块');
		expect(shortSectorFlowName('汽车一体化压铸产业概念')).toBe('汽车一体化压铸产业概…');
	});
});

describe('sector flow selection persistence', () => {
	it('reads valid entries, drops junk and caps the list', () => {
		const storage = { getItem: (key: string) => (key === SECTOR_FLOW_SELECTION_STORAGE_KEY ? JSON.stringify(['BK0917', 42, '', 'BK0490']) : null) };
		expect(readSectorFlowSelection(storage)).toEqual(['BK0917', 'BK0490']);
		expect(readSectorFlowSelection({ getItem: () => '{not json' })).toEqual([]);
		expect(readSectorFlowSelection({ getItem: () => '{"a":1}' })).toEqual([]);
		expect(readSectorFlowSelection(null)).toEqual([]);
		const many = { getItem: () => JSON.stringify(Array.from({ length: 40 }, (_, index) => `BK${index}`)) };
		expect(readSectorFlowSelection(many)).toHaveLength(MAX_SECTOR_FLOW_SERIES);
	});

	it('writes selections and tolerates unavailable storage', () => {
		const writes: Array<[string, string]> = [];
		writeSectorFlowSelection({ setItem: (key: string, value: string) => writes.push([key, value]) }, ['BK0917']);
		expect(writes).toEqual([[SECTOR_FLOW_SELECTION_STORAGE_KEY, '["BK0917"]']]);
		expect(() => writeSectorFlowSelection({ setItem: () => { throw new Error('quota'); } }, ['BK0917'])).not.toThrow();
		expect(() => writeSectorFlowSelection(null, ['BK0917'])).not.toThrow();
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
