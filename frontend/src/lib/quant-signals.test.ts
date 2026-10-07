import { describe, expect, it } from 'vitest';
import { deriveOosGate, type ClipTrackPayload } from './quant-signals';

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
