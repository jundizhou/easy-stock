import { describe, expect, it } from 'vitest';
import { analysisTone, calculatePositionSizing, formatCompactAmount, normalizeAnalysisSymbol, resolveStockDirectorySymbol, searchStockDirectory, signedPercent } from './stock-analysis';

const stockDirectory = [
	{ symbol: '000001.SZ', code: '000001', name: '平安银行' },
	{ symbol: '600519.SH', code: '600519', name: '贵州茅台' },
	{ symbol: '600664.SH', code: '600664', name: '哈药股份' },
];

const hkDirectory = [
	{ symbol: '00700.HK', code: '00700', name: '腾讯控股' },
	{ symbol: '09988.HK', code: '09988', name: '阿里巴巴' },
];

describe('stock analysis helpers', () => {
	it('normalizes analysis symbols', () => {
		expect(normalizeAnalysisSymbol(' 600519.sh ')).toBe('600519.SH');
	});

	it('fuzzy searches the stock directory by code or name', () => {
		expect(searchStockDirectory(stockDirectory, '6005').map((item) => item.name)).toEqual(['贵州茅台']);
		expect(searchStockDirectory(stockDirectory, '茅台').map((item) => item.code)).toEqual(['600519']);
		expect(searchStockDirectory(stockDirectory, '平银').map((item) => item.symbol)).toEqual(['000001.SZ']);
	});

	it('searches Hong Kong stocks by code or Chinese name', () => {
		expect(searchStockDirectory(hkDirectory, '00700').map((item) => item.name)).toEqual(['腾讯控股']);
		expect(searchStockDirectory(hkDirectory, '腾讯').map((item) => item.symbol)).toEqual(['00700.HK']);
		expect(searchStockDirectory(hkDirectory, '阿里').map((item) => item.code)).toEqual(['09988']);
	});

	it('resolves exact names and selected display values to symbols', () => {
		expect(resolveStockDirectorySymbol('贵州茅台', stockDirectory)).toBe('600519.SH');
		expect(resolveStockDirectorySymbol('贵州茅台 · 600519', stockDirectory)).toBe('600519');
		expect(resolveStockDirectorySymbol('600664.sh', [])).toBe('600664.SH');
	});

	it('resolves Hong Kong codes and names to .HK symbols', () => {
		expect(resolveStockDirectorySymbol('00700.HK', [])).toBe('00700.HK');
		expect(resolveStockDirectorySymbol('hk00700', [])).toBe('00700.HK');
		expect(resolveStockDirectorySymbol('00700', [])).toBe('00700.HK');
		expect(resolveStockDirectorySymbol('腾讯控股', hkDirectory)).toBe('00700.HK');
		expect(resolveStockDirectorySymbol('阿里巴巴 · 09988', hkDirectory)).toBe('09988.HK');
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

it('港股按证券每手股数向下取整', () => {
	const input = { accountCapital: 200000, riskPercent: 0.8, entryPrice: 20, stopPrice: 19, maxPositionPercent: 30 };
	expect(calculatePositionSizing({ ...input, boardLot: 500 }).shares).toBe(1500);
	expect(calculatePositionSizing({ ...input, boardLot: 0 }).shares).toBe(0);
});
