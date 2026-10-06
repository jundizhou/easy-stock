import { describe, expect, it } from 'vitest';
import {
	boardsForSymbol,
	groupBoardsByCategory,
	parseBoardRows,
	normalizeSymbol,
	type StockDBBoard,
} from './sidecars';

const board = (overrides: Partial<StockDBBoard>): StockDBBoard => ({
	code: '309265.TI',
	name: '测试板块',
	source: 'ths',
	type: 'concept',
	group: '特色指数列表',
	category: '概念',
	symbols: ['000061', '000062'],
	...overrides,
});

describe('parseBoardRows', () => {
	it('解析 stockdb [key, value] 行并跳过残缺行', () => {
		const raw = [
			['板块:概念_测试:309265.TI', board({})],
			['残缺', '不是对象'],
			['缺字段', { code: '', name: 'x', category: '概念' }],
			[42],
			'顶层垃圾',
		];
		const boards = parseBoardRows(raw);
		expect(boards).toHaveLength(1);
		expect(boards[0]).toMatchObject({ code: '309265.TI', category: '概念', symbols: ['000061', '000062'] });
	});

	it('symbols 非数组时降级为空数组', () => {
		const boards = parseBoardRows([['k', { code: '1.TI', name: 'x', category: '概念', symbols: 'bad' }]]);
		expect(boards[0].symbols).toEqual([]);
	});

	it('非数组响应返回空列表', () => {
		expect(parseBoardRows(null)).toEqual([]);
		expect(parseBoardRows({ error: 'x' })).toEqual([]);
	});
});

describe('groupBoardsByCategory', () => {
	it('按申万一级→二级→三级→概念固定顺序归组并组内按名称排序', () => {
		const groups = groupBoardsByCategory([
			board({ code: 'c1', name: '乙', category: '概念' }),
			board({ code: 'c2', name: '甲', category: '概念' }),
			board({ code: 's1', name: '农林牧渔', category: '申万一级' }),
		]);
		expect(groups.map(([category]) => category)).toEqual(['申万一级', '概念']);
		expect(groups[1][1].map(item => item.name)).toEqual(['甲', '乙']);
	});
});

describe('boardsForSymbol / normalizeSymbol', () => {
	it('反查归属板块并容忍交易所后缀', () => {
		const boards = [
			board({ code: 'b1', name: '银行', category: '申万一级', symbols: ['600036'] }),
			board({ code: 'b2', name: '白酒', category: '概念', symbols: ['000858'] }),
		];
		expect(boardsForSymbol(boards, '600036.SH').map(item => item.name)).toEqual(['银行']);
		expect(boardsForSymbol(boards, '')).toEqual([]);
	});

	it('normalizeSymbol 只保留 6 位代码', () => {
		expect(normalizeSymbol(' 000061.SZ ')).toBe('000061');
		expect(normalizeSymbol('平安银行')).toBe('');
	});
});
