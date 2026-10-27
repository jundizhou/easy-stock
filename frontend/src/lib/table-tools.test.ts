import { afterEach, describe, expect, it, vi } from 'vitest';
import {
	applyColumnOrderForTest,
	applyColumnWidthsForTest,
	compareSortValues,
	isOrderApplied,
	moveInOrder,
	parseSortValue,
	reorderByDisplay,
	resetTablePrefsForTest,
} from './table-tools';

describe('parseSortValue', () => {
	it('把带符号与百分号的数字解析成数值', () => {
		expect(parseSortValue('+1.58%')).toBe(1.58);
		expect(parseSortValue('-0.98%')).toBe(-0.98);
		expect(parseSortValue('12.86')).toBe(12.86);
	});

	it('把「万 / 亿」换算成同一个量级，便于跨行比较', () => {
		expect(parseSortValue('162.42亿')).toBeCloseTo(16242000000, 0);
		expect(parseSortValue('8700万')).toBeCloseTo(87000000, 0);
	});

	it('把日期与时刻解析成可比较的数值', () => {
		expect(parseSortValue('2026/10/25')).toBe(Date.UTC(2026, 9, 25));
		expect(parseSortValue('14:56:43')).toBe(14 * 3600 + 56 * 60 + 43);
	});

	it('破折号与空白视为空值，不参与比较', () => {
		expect(parseSortValue('—')).toBeNull();
		expect(parseSortValue('  ')).toBeNull();
		expect(parseSortValue('--')).toBeNull();
	});

	it('非数值文本原样返回，走字符串比较', () => {
		expect(parseSortValue('已触发')).toBe('已触发');
	});
});

describe('compareSortValues', () => {
	it('数值按大小比较', () => {
		expect(compareSortValues(1, 2)).toBeLessThan(0);
		expect(compareSortValues(2, 1)).toBeGreaterThan(0);
	});

	it('空值恒沉底，升序降序都排在有值项之后', () => {
		expect(compareSortValues(null, 5)).toBeGreaterThan(0);
		expect(compareSortValues(5, null)).toBeLessThan(0);
		expect(compareSortValues(null, null)).toBe(0);
		expect(compareSortValues(null, 5, 'desc')).toBeGreaterThan(0);
		expect(compareSortValues(5, null, 'desc')).toBeLessThan(0);
	});

	it('降序按真实数值比较，缺失仍在末尾，零不是缺失', () => {
		const values = [null, -12, 0, 10, 2];
		expect(values.slice().sort((a, b) => compareSortValues(a, b, 'desc'))).toEqual([10, 2, 0, -12, null]);
		expect(values.slice().sort((a, b) => compareSortValues(a, b, 'asc'))).toEqual([-12, 0, 2, 10, null]);
	});

	it('名称中包含数字时按自然顺序排', () => {
		expect(compareSortValues('模型2', '模型10')).toBeLessThan(0);
	});
});

describe('reorderByDisplay', () => {
	it('按显示顺序重排', () => {
		expect(reorderByDisplay(['a', 'b', 'c'], [2, 0, 1])).toEqual(['c', 'a', 'b']);
	});

	it('顺序长度不匹配时原样返回，避免把表排乱', () => {
		expect(reorderByDisplay(['a', 'b', 'c'], [1, 0])).toEqual(['a', 'b', 'c']);
	});

	it('顺序里有重复或越界时原样返回', () => {
		expect(reorderByDisplay(['a', 'b'], [0, 0])).toEqual(['a', 'b']);
		expect(reorderByDisplay(['a', 'b'], [0, 5])).toEqual(['a', 'b']);
	});
});

describe('moveInOrder', () => {
	it('把第 0 位拖到第 2 位', () => {
		expect(moveInOrder([0, 1, 2, 3], 0, 2)).toEqual([1, 2, 0, 3]);
	});

	it('把末位拖到首位', () => {
		expect(moveInOrder([0, 1, 2], 2, 0)).toEqual([2, 0, 1]);
	});

	it('不改变原数组', () => {
		const order = [0, 1, 2];
		moveInOrder(order, 0, 2);
		expect(order).toEqual([0, 1, 2]);
	});
});

describe('isOrderApplied', () => {
	it('顺序一致时判定为已应用，用于跳过无谓的 DOM 写入', () => {
		expect(isOrderApplied([0, 1, 2], [0, 1, 2])).toBe(true);
		expect(isOrderApplied([1, 0, 2], [0, 1, 2])).toBe(false);
		expect(isOrderApplied([0, 1], [0, 1, 2])).toBe(false);
	});
});

/* ─────────────── 异步正文与已恢复表头的列序同步 ─────────────── */

type OrderCell = { dataset: { ttCol: string } };
type OrderFragment = { children: OrderCell[]; appendChild: (cell: OrderCell) => void };

function orderRow(columns: number[]) {
	return {
		cells: columns.map((logical) => ({ dataset: { ttCol: String(logical) } })),
		writes: 0,
		appendChild(fragment: OrderFragment) {
			this.cells = this.cells.filter((cell) => !fragment.children.includes(cell)).concat(fragment.children);
			this.writes++;
		},
	};
}

function orderTable(head: ReturnType<typeof orderRow>, rows: ReturnType<typeof orderRow>[]) {
	vi.stubGlobal('document', {
		createDocumentFragment: (): OrderFragment => ({
			children: [],
			appendChild(cell) { this.children.push(cell); },
		}),
	});
	return { tHead: { rows: [head] }, tBodies: [{ rows }] } as unknown as HTMLTableElement;
}

const rowOrder = (row: ReturnType<typeof orderRow>) => row.cells.map((cell) => Number(cell.dataset.ttCol));

describe('applyColumnOrder 异步行回归', () => {
	afterEach(() => vi.unstubAllGlobals());
	const natural = [0, 1, 2, 3, 4, 5, 6, 7, 8];
	const saved = [0, 1, 2, 3, 4, 5, 7, 6, 8];

	it('表头已记住今日操作换位后，仍纠正稍后加载的正文', () => {
		const head = orderRow(saved);
		const row = orderRow(natural);
		const cells = row.cells.slice();
		expect(applyColumnOrderForTest(orderTable(head, [row]), saved)).toBe(true);
		expect(rowOrder(head)).toEqual(saved);
		expect(rowOrder(row)).toEqual(saved);
		expect(row.cells[6]).toBe(cells[7]);
		expect(row.cells[7]).toBe(cells[6]);
		expect(head.writes).toBe(0);
	});

	it('整页刷新先恢复空表表头，收到异步账户行后再恢复正文', () => {
		const head = orderRow(natural);
		const rows: ReturnType<typeof orderRow>[] = [];
		const table = orderTable(head, rows);
		expect(applyColumnOrderForTest(table, saved)).toBe(true);
		expect(rowOrder(head)).toEqual(saved);
		rows.push(orderRow(natural));
		expect(applyColumnOrderForTest(table, saved)).toBe(true);
		expect(rowOrder(rows[0])).toEqual(saved);
		expect(head.writes).toBe(1);
	});

	it('轮询新增行只重排新行，原行、展开行和不完整行不动', () => {
		const head = orderRow(saved);
		const existing = orderRow(saved);
		const added = orderRow(natural);
		const detail = orderRow([0]);
		const incomplete = orderRow([0, 1, 2, 3, 4, 5, 6, 6, 8]);
		const rows = [existing, detail, added, incomplete];
		expect(applyColumnOrderForTest(orderTable(head, rows), saved)).toBe(true);
		expect(rowOrder(added)).toEqual(saved);
		expect([existing.writes, detail.writes, incomplete.writes]).toEqual([0, 0, 0]);
		expect(rows).toEqual([existing, detail, added, incomplete]);
	});

	it('表头和所有正文均对齐时不写DOM，避免观察器反复触发', () => {
		const head = orderRow(saved);
		const row = orderRow(natural);
		const table = orderTable(head, [row]);
		applyColumnOrderForTest(table, saved);
		row.writes = 0;
		expect(applyColumnOrderForTest(table, saved)).toBe(false);
		expect([head.writes, row.writes]).toEqual([0, 0]);
	});

	it('可同时恢复表头与正文，也能将列序重置回自然顺序', () => {
		const head = orderRow(natural);
		const row = orderRow(natural);
		const table = orderTable(head, [row]);
		applyColumnOrderForTest(table, saved);
		expect([rowOrder(head), rowOrder(row)]).toEqual([saved, saved]);
		applyColumnOrderForTest(table, natural);
		expect([rowOrder(head), rowOrder(row)]).toEqual([natural, natural]);
	});
});

/* ─────────────── 列宽：不许动没存过偏好的表 ─────────────── */

function fakeStyle() {
	const store: Record<string, string> = {};
	return {
		get width() { return store.width ?? ''; },
		set width(value: string) { store.width = value; },
		removeProperty(name: string) { if (name === 'width') delete store.width; },
	};
}

/** 够 applyColumnWidths 用的最小表桩。
 *  列宽从 <col style={{ width }}> 开始，与 TSX 里的写法一致。 */
function fakeTable(widths: (string | null)[]) {
	const cols = widths.map((width) => {
		const el = { dataset: {} as Record<string, string>, style: fakeStyle() };
		if (width !== null) el.style.width = width;
		return el;
	});
	const ths = widths.map(() => {
		const el = { dataset: {} as Record<string, string>, style: fakeStyle(), getBoundingClientRect: () => ({ width: 120 }) };
		return el;
	});
	return {
		cols,
		ths,
		dataset: {} as Record<string, string>,
		style: fakeStyle(),
		// isEligible 会先问这两句；tBodies 为空即判定「不是数据表」，applyOne 直接返回。
		closest: () => null,
		tBodies: [] as unknown[],
		tHead: { rows: [{ cells: ths }] },
		querySelector: (selector: string) => (selector === 'colgroup' ? { children: cols } : null),
	};
}

const asTable = (fake: ReturnType<typeof fakeTable>) => fake as unknown as HTMLTableElement;

describe('applyColumnWidths', () => {
	it('用户从没存过列宽时，一个字节都不动（回归：曾把 JSX 的百分比列宽擦成全列等宽）', () => {
		const table = fakeTable(['15%', '9%', '12%']);
		applyColumnWidthsForTest(asTable(table), [0, 1, 2], {});
		expect(table.cols.map((col) => col.style.width)).toEqual(['15%', '9%', '12%']);
	});

	it('存过列宽时只写存了的那几列，没存的列按实际宽度量下来', () => {
		const table = fakeTable(['15%', '9%', '12%']);
		applyColumnWidthsForTest(asTable(table), [0, 1, 2], { 0: 200 });
		expect(table.cols[0].style.width).toBe('200px');
		// 第 2 列必须走「量实际宽度」这条路。若 readColumnWidth 用 parseFloat 读 '9%'，
		// 会得到 9（< MIN_COL_WIDTH，恰好躲过）或对 '60%' 得到 60 而被当成像素。
		expect(table.cols[1].style.width).toBe('120px');
		expect(table.style.width).toBe('440px');
		expect(table.style.width === '200px').toBe(false);
	});

	it('百分比不会被误读成像素宽度', () => {
		const table = fakeTable(['60%', '20%']);
		applyColumnWidthsForTest(asTable(table), [0, 1], { 0: 200 });
		// '60%' → parseFloat 得 60，误读就会把第 1 列钉成 60px 并让表宽算成 540px。
		expect(table.cols[1].style.width).toBe('120px');
		expect(table.style.width).toBe('320px');
	});
});

describe('resetTablePrefs', () => {
	it('重置后还原 JSX 里写死的设计列宽，而不是留空', () => {
		const table = fakeTable(['15%', '9%']);
		applyColumnWidthsForTest(asTable(table), [0, 1], { 0: 200, 1: 300 });
		expect(table.cols[0].style.width).toBe('200px');
		resetTablePrefsForTest(asTable(table));
		expect(table.cols.map((col) => col.style.width)).toEqual(['15%', '9%']);
		expect(table.style.width).toBe('');
		expect(table.dataset.ttLayout).toBeUndefined();
	});
});
