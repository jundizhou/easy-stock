import { describe, expect, it } from 'vitest';
import { aggregateBoardHeat, resolveBoardFocus, type BoardFocus, type BoardSymbolQuote } from './BoardExplorerWorkspace';
import type { StockDBBoard } from '../lib/sidecars';

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

const boards = [
	board({ code: '801780.SL', name: '银行', category: '申万一级' }),
	board({ code: '309265.TI', name: '人工智能', category: '概念' }),
	board({ code: '885001.TI', name: '机器人概念', category: '概念' }),
];

describe('resolveBoardFocus', () => {
	it('精确同名命中：返回板块类目与代码，query 置空', () => {
		const focus: BoardFocus = { name: '人工智能' };
		expect(resolveBoardFocus(boards, focus)).toEqual({
			category: '概念',
			code: '309265.TI',
			query: '',
		});
	});

	it('精确同名优先于包含关系：银行精确命中申万一级而非模糊候选', () => {
		const withOverlap = [
			...boards,
			board({ code: '999999.TI', name: '银行ETF联动', category: '概念' }),
		];
		const resolved = resolveBoardFocus(withOverlap, { name: '银行' });
		expect(resolved).toEqual({ category: '申万一级', code: '801780.SL', query: '' });
	});

	it('无精确同名时包含关系命中（关键词包含板块名方向）', () => {
		const resolved = resolveBoardFocus(boards, { name: '人工智能算力产业链' });
		expect(resolved).toEqual({ category: '概念', code: '309265.TI', query: '' });
	});

	it('包含关系命中（板块名包含关键词方向）', () => {
		const resolved = resolveBoardFocus(boards, { name: '机器人' });
		expect(resolved).toEqual({ category: '概念', code: '885001.TI', query: '' });
	});

	it('均未命中回退为类目内搜索：hint 缺省时 category=概念 且 query 填词', () => {
		const resolved = resolveBoardFocus(boards, { name: '低空经济' });
		expect(resolved).toEqual({ category: '概念', code: '', query: '低空经济' });
	});

	it('均未命中且提供 hint 时回退 category=hint', () => {
		const resolved = resolveBoardFocus(boards, { name: '低空经济', hint: '申万二级' });
		expect(resolved).toEqual({ category: '申万二级', code: '', query: '低空经济' });
	});

	it('板块列表为空返回 null（不消费 focus）', () => {
		expect(resolveBoardFocus([], { name: '银行' })).toBeNull();
	});

	it('关键词 trim 后为空返回 null', () => {
		expect(resolveBoardFocus(boards, { name: '   ' })).toBeNull();
	});

	it('关键词前后空白被 trim 后参与精确匹配', () => {
		const resolved = resolveBoardFocus(boards, { name: '  人工智能  ' });
		expect(resolved).toEqual({ category: '概念', code: '309265.TI', query: '' });
	});
});

describe('aggregateBoardHeat', () => {
	const quote = (changePercent: number): BoardSymbolQuote => ({ symbol: '600519', name: '贵州茅台', price: 100, changePercent });

	it('空行情返回 null', () => {
		expect(aggregateBoardHeat([])).toBeNull();
	});

	it('聚合均值与涨跌家数', () => {
		const heat = aggregateBoardHeat([quote(2.0), quote(-1.0), quote(0.5), quote(0)]);
		expect(heat).toEqual({ avgChangePercent: 0.375, upCount: 2, downCount: 1 });
	});

	it('平盘不计入涨跌家数但计入均值', () => {
		const heat = aggregateBoardHeat([quote(1.0), quote(0)]);
		expect(heat!.upCount).toBe(1);
		expect(heat!.downCount).toBe(0);
		expect(heat!.avgChangePercent).toBeCloseTo(0.5);
	});
});
