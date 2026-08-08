import { describe, expect, it } from 'vitest';
import { analysisTone, calculatePositionSizing, formatCompactAmount, normalizeAnalysisSymbol, signedPercent } from './stock-analysis';

describe('stock analysis helpers', () => {
	it('normalizes analysis symbols', () => {
		expect(normalizeAnalysisSymbol(' 600519.sh ')).toBe('600519.SH');
	});

	it('derives analysis tone from the routed profile', () => {
		expect(analysisTone({ profile: { primary_type: 'trend_capacity' } as never, trend: { score: 76 } as never })).toBe('strong');
		expect(analysisTone({ profile: { primary_type: 'weak_risk' } as never, trend: { score: 55 } as never })).toBe('risk');
	});

	it('formats trading metrics', () => {
		expect(formatCompactAmount(1_230_000_000)).toBe('12.3亿');
		expect(signedPercent(3.25)).toBe('+3.3%');
	});

	it('calculates board-lot position sizing from account risk and position cap', () => {
		const result = calculatePositionSizing({ accountCapital: 200_000, riskPercent: 0.8, entryPrice: 20, stopPrice: 19, maxPositionPercent: 30 });
		expect(result.shares).toBe(1600);
		expect(result.positionValue).toBe(32_000);
		expect(result.maxLoss).toBe(1600);
		expect(result.positionPercent).toBe(16);
	});

	it('returns an empty sizing result for an invalid stop', () => {
		expect(calculatePositionSizing({ accountCapital: 200_000, riskPercent: 1, entryPrice: 20, stopPrice: 20.5, maxPositionPercent: 30 }).shares).toBe(0);
	});
});
