import { describe, expect, it } from 'vitest';
import { sortSymbolsByChangePercent } from './quote-sort';

describe('sortSymbolsByChangePercent', () => {
	it('按涨跌幅降序排列', () => {
		const symbols = ['000901.SZ', '001208.SZ', '600519.SH'];
		const changes = { '000901.SZ': 7.87, '001208.SZ': -6.96, '600519.SH': 1.2 };
		expect(sortSymbolsByChangePercent(symbols, changes)).toEqual(['000901.SZ', '600519.SH', '001208.SZ']);
	});

	it('缺行情 / 停牌的排在最后，并按代码稳定排序', () => {
		const symbols = ['300750.SZ', '000001.SZ', '600519.SH'];
		const changes = { '300750.SZ': 3.5, '000001.SZ': undefined, '600519.SH': 2.1 };
		expect(sortSymbolsByChangePercent(symbols, changes)).toEqual(['300750.SZ', '600519.SH', '000001.SZ']);
	});

	it('多个缺失行情时按代码字典序，保证多次渲染顺序一致', () => {
		const symbols = ['600519.SH', '000001.SZ', '300750.SZ'];
		const changes: Record<string, number | undefined> = {};
		expect(sortSymbolsByChangePercent(symbols, changes)).toEqual(['000001.SZ', '300750.SZ', '600519.SH']);
	});

	it('NaN 与 0 分开处理：0 视为有效值参与排序', () => {
		const symbols = ['A', 'B', 'C'];
		const changes = { A: Number.NaN, B: 0, C: -1.5 };
		expect(sortSymbolsByChangePercent(symbols, changes)).toEqual(['B', 'C', 'A']);
	});

	it('不修改传入的数组', () => {
		const symbols = ['B', 'A'];
		sortSymbolsByChangePercent(symbols, { A: 1, B: 2 });
		expect(symbols).toEqual(['B', 'A']);
	});
});
