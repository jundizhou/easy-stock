import { describe, expect, it } from 'vitest';
import {
	MER_STALE_HINT,
	deriveOosGate,
	describeFreshness,
	nextYearOf,
	type ClipTrackPayload,
} from './quant-signals';

const track = (overrides: Partial<ClipTrackPayload> = {}): ClipTrackPayload => ({
	ok: true,
	track_dir: 'C:\\quant\\paper_forward\\shadow\\clip_n300',
	config: null,
	summary: null,
	periods: [],
	settled: [],
	nav: [],
	oos: {
		contaminated_start: '2024-01-01',
		contaminated_end: '2026-09-18',
		anchor_date: '2026-10-08',
		target_periods: 24,
		basis: 'governance/ADR-20261001',
	},
	...overrides,
});

describe('deriveOosGate', () => {
	it('空轨道：LOCKED，进度 0', () => {
		const gate = deriveOosGate(track());
		expect(gate).not.toBeNull();
		expect(gate!.state).toBe('LOCKED');
		expect(gate!.forwardSignals).toBe(0);
		expect(gate!.forwardSettled).toBe(0);
		expect(gate!.progress).toBe(0);
	});

	it('T0=2026-09-30 属 PRE_ANCHOR，不计入前向信号', () => {
		const gate = deriveOosGate(track({
			periods: [{ period: 0, signal_date: '2026-09-30', role: 'T0_BUILD', evidence_class: 'PRE_ANCHOR_DESCRIPTIVE', status: 'SIGNAL_ONLY', n_holdings: 300, file: 'signal_period_00.json' }],
		}));
		expect(gate!.forwardSignals).toBe(0);
		expect(gate!.state).toBe('LOCKED');
	});

	it('锚点后信号期计入 forwardSignals；无 settled 时仍 LOCKED', () => {
		const gate = deriveOosGate(track({
			periods: [
				{ period: 0, signal_date: '2026-09-30', role: 'T0_BUILD', evidence_class: 'PRE_ANCHOR_DESCRIPTIVE', status: 'SIGNAL_ONLY', n_holdings: 300, file: 'a' },
				{ period: 1, signal_date: '2026-10-08', role: 'FORWARD', evidence_class: 'FORWARD', status: 'SIGNAL_ONLY', n_holdings: 300, file: 'b' },
				{ period: 2, signal_date: '2026-11-06', role: 'FORWARD', evidence_class: 'FORWARD', status: 'SIGNAL_ONLY', n_holdings: 300, file: 'c' },
			],
		}));
		expect(gate!.forwardSignals).toBe(2);
		expect(gate!.forwardSettled).toBe(0);
		expect(gate!.state).toBe('LOCKED');
	});

	it('settled 文件覆盖的期计入 forwardSettled；达标时 OPEN 且进度封顶 1', () => {
		const periods = Array.from({ length: 24 }, (_, i) => ({
			period: i + 1,
			signal_date: `2027-${String((i % 12) + 1).padStart(2, '0')}-08`,
			role: 'FORWARD',
			evidence_class: 'FORWARD',
			status: 'SIGNAL_ONLY',
			n_holdings: 300,
			file: `p${i + 1}`,
		}));
		const settled = periods.map(p => ({ period: p.period, file: `period_${p.period}.json` }));
		const gate = deriveOosGate(track({ periods, settled }));
		expect(gate!.forwardSettled).toBe(24);
		expect(gate!.state).toBe('OPEN');
		expect(gate!.progress).toBe(1);
	});

	it('settled 只认锚点后的期（T0 期即使 settled 也不计入）', () => {
		const gate = deriveOosGate(track({
			periods: [{ period: 0, signal_date: '2026-09-30', role: 'T0_BUILD', evidence_class: 'PRE_ANCHOR_DESCRIPTIVE', status: 'SETTLED', n_holdings: 300, file: 'a' }],
			settled: [{ period: 0 }],
		}));
		expect(gate!.forwardSettled).toBe(0);
		expect(gate!.state).toBe('LOCKED');
	});

	it('轨道为 null 返回 null（调用方渲染占位）', () => {
		expect(deriveOosGate(null)).toBeNull();
	});
});

describe('nextYearOf', () => {
	it('受污染区间 2026-09-18 → 前向区间标注 2027（数值相加，非字符串拼接）', () => {
		expect(nextYearOf('2026-09-18')).toBe('2027');
	});

	it('不得出现 "20261" 这类拼接结果', () => {
		for (const input of ['2026-09-18', '2021-06-30', '2024-01-01']) {
			const value = nextYearOf(input);
			expect(value).not.toContain(input.slice(0, 4) + '1');
			expect(value).toMatch(/^\d{4}$/);
		}
	});

	it('空值/非法输入降级为占位符，不抛错', () => {
		expect(nextYearOf('')).toBe('—');
		expect(nextYearOf('abcd')).toBe('—');
	});
});

describe('describeFreshness', () => {
	const at = (iso: string) => new Date(`${iso}T12:00:00`);

	it('当日：fresh，文案「今日」', () => {
		const freshness = describeFreshness('2026-10-11', at('2026-10-11'));
		expect(freshness.days).toBe(0);
		expect(freshness.level).toBe('fresh');
		expect(freshness.label).toBe('今日');
		expect(freshness.stale).toBe(false);
	});

	it('3 天内 fresh / 4-7 天 aging / 8 天以上 stale', () => {
		expect(describeFreshness('2026-10-08', at('2026-10-11')).level).toBe('fresh');
		expect(describeFreshness('2026-10-05', at('2026-10-11')).level).toBe('aging');
		expect(describeFreshness('2026-10-03', at('2026-10-11')).level).toBe('stale');
	});

	it('停更 11 天（09-30 → 10-11）判为 stale 且给出天数', () => {
		const freshness = describeFreshness('2026-09-30', at('2026-10-11'));
		expect(freshness.days).toBe(11);
		expect(freshness.stale).toBe(true);
		expect(freshness.label).toBe('11 天前');
	});

	it('asof 缺失或不可解析：unknown，不误报陈旧也不谎称新鲜', () => {
		expect(describeFreshness(undefined).level).toBe('unknown');
		expect(describeFreshness('').stale).toBe(false);
		expect(describeFreshness('not-a-date', at('2026-10-11')).level).toBe('unknown');
	});

	it('时间倒流（asof 晚于 now）不产生负天数', () => {
		const freshness = describeFreshness('2026-10-20', at('2026-10-11'));
		expect(freshness.days).toBe(0);
		expect(freshness.stale).toBe(false);
	});

	it('带时分秒的 asof 也能正确取日期部分', () => {
		expect(describeFreshness('2026-10-11T09:30:00+08:00', at('2026-10-11')).days).toBe(0);
	});

	it('陈旧提示必须给出可照做的排查路径（不是只说「刷新无效」）', () => {
		expect(MER_STALE_HINT).toContain('verdict_latest.json');
		expect(MER_STALE_HINT).toContain('8766');
	});
});
