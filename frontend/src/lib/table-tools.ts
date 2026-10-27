// 全站数据表的列工具：点表头排序、拖表头右缘调列宽、拖表头换列序。
// 偏好按「每张表」独立记住（localStorage），刷新后仍生效。
//
// 设计取舍：直接作用在已渲染的 DOM 上，而不要求每张表改写成受控组件。
// 站内三十多张手写表，逐个抽象成本高、回归面大。这里只做三类不破坏 React 的操作：
//   ① 读写 th / td 上的 data-* 标记（JSX 里没声明这些属性，React 不会覆盖它们）；
//   ② 在**同一个父节点内**重排已有节点（React 提交更新依赖 fiber 上绑定的宿主
//      节点引用，与节点在 DOM 里的物理位置无关，所以重排不会让某列的值串到
//      别的表头下面；位置被 React 复位后，本模块会在下一帧再摆正）；
//   ③ 只写 <col> 的 width 与 th 上的状态属性。
// 全程不插入、不删除任何宿主节点，避免 React 重渲染时节点被清掉造成闪烁。
// 每次应用都先比对目标顺序，已经正确就原地返回——保证幂等，不会自激循环。

import { useCallback, useState, type SetStateAction } from 'react';

export type SortDir = 'asc' | 'desc';
export type SortState = { col: number; dir: SortDir } | null;

export type TablePrefs = {
	/** 显示位置 → 逻辑列序号。缺省即自然顺序。 */
	order?: number[];
	/** 逻辑列序号 → 像素宽度。 */
	widths?: Record<number, number>;
	/** 当前排序；null 表示原始顺序。 */
	sort?: SortState;
	/** 建这份偏好时的表头文字签名，用来判断表结构是否已变。 */
	labels?: string[];
};

const STORAGE_PREFIX = 'easy-stock:table-prefs:v1:';
/** 边界热区半宽。左右两侧各算一次，实际可抓范围因此是它的两倍；
 *  6px 单侧太窄，鼠标要「正好压在线」才抓得住。 */
const EDGE_ZONE = 8;
const MIN_COL_WIDTH = 48;

/** 不支持排序的表头：里面是图标按钮，点它是要做事，不是要排序。 */
const UNSORTABLE = new Set(['操作', '#', '']);

/** 已由 React 按原始字段排序的列表继续使用原回调，共享持久化方式。 */
export function useTableSort<State extends { key: string; direction: SortDir }>(id: string, initial: State) {
	const storage = `${STORAGE_PREFIX}sort|${typeof window === 'undefined' ? '' : window.location.hash.replace(/[?&].*$/, '')}|${id}`;
	const [state, setState] = useState<State>(() => {
		try {
			const saved = JSON.parse(window.localStorage.getItem(storage) || 'null') as State | null;
			if (saved && typeof saved.key === 'string' && (saved.direction === 'asc' || saved.direction === 'desc')) return { ...initial, ...saved };
		} catch { /* 不可读时保持页面默认排序。 */ }
		return initial;
	});
	const update = useCallback((next: SetStateAction<State>) => {
		setState((current) => {
			const value = typeof next === 'function' ? next(current) : next;
			try { window.localStorage.setItem(storage, JSON.stringify(value)); } catch { /* 当前页面仍可排序。 */ }
			return value;
		});
	}, [storage]);
	return [state, update] as const;
}

/* ─────────────── 纯函数（可单测） ─────────────── */

export function parseSortValue(text: string): number | string | null {
	const t = text.replace(/[\s\u00a0]+/g, '');
	if (!t || t === '—' || t === '-' || t === '--' || t === '–' || t === '…') return null;
	const num = t.replace(/[+,%]/g, '').replace(/(万|亿)$/, '');
	if (/^-?\d+(\.\d+)?$/.test(num)) {
		const base = parseFloat(num);
		if (/亿$/.test(t)) return base * 1e8;
		if (/万$/.test(t)) return base * 1e4;
		return base;
	}
	const date = t.match(/^(\d{4})[/-](\d{1,2})[/-](\d{1,2})/);
	if (date) return Date.UTC(Number(date[1]), Number(date[2]) - 1, Number(date[3]));
	const clock = t.match(/^(\d{1,2}):(\d{2})(?::(\d{2}))?/);
	if (clock) return Number(clock[1]) * 3600 + Number(clock[2]) * 60 + Number(clock[3] || 0);
	return t;
}

export function compareSortValues(a: number | string | null, b: number | string | null, direction: SortDir = 'asc'): number {
	// 空值（「—」）恒沉底，不论升序降序——否则降序时一列破折号会霸占表头。
	if (a === null && b === null) return 0;
	if (a === null) return 1;
	if (b === null) return -1;
	const factor = direction === 'desc' ? -1 : 1;
	if (typeof a === 'number' && typeof b === 'number') return (a - b) * factor;
	return String(a).localeCompare(String(b), 'zh-Hans-CN', { numeric: true }) * factor;
}

export function reorderByDisplay<T>(items: T[], order: number[] | undefined): T[] {
	if (!order || order.length !== items.length) return items;
	const seen = new Set<number>();
	const out: T[] = [];
	for (const index of order) {
		if (!Number.isInteger(index) || index < 0 || index >= items.length || seen.has(index)) return items;
		seen.add(index);
		out.push(items[index]);
	}
	return out;
}

/** 把「第 from 个显示位拖到第 to 个显示位」翻译成新的显示顺序。 */
export function moveInOrder(order: number[], from: number, to: number): number[] {
	const next = order.slice();
	const [moved] = next.splice(from, 1);
	next.splice(to, 0, moved);
	return next;
}

/** 按显示顺序判断表头文字是否已排好——用来跳过无谓的 DOM 写入。 */
export function isOrderApplied(current: number[], target: number[]): boolean {
	return current.length === target.length && current.every((value, index) => value === target[index]);
}

/* ─────────────── DOM 适配 ─────────────── */

function headerRow(table: HTMLTableElement): HTMLTableRowElement | null {
	return table.tHead?.rows[0] ?? null;
}

/** 表头文字，**按逻辑列号排列**（不是 DOM 里的显示顺序）。
 *
 *  这个区别是「刷新后记忆失效」的根因：列序被拖动后，如果按显示顺序取
 *  文字，签名就变成「结论~标的~…」；重新加载时表格回到自然列序，签名又变
 *  回「标的~结论~…」，两者对不上，偏好就被当成别的表丢掉了。
 *  按逻辑列号取，签名与显示顺序无关，拖完刷新仍然认得出是同一张表。 */
function headerLabels(table: HTMLTableElement): string[] {
	const row = headerRow(table);
	if (!row) return [];
	const cells = [...row.cells];
	const labels = new Array<string>(cells.length).fill('');
	cells.forEach((cell, domIndex) => {
		const tagged = Number((cell as HTMLTableCellElement).dataset.ttCol);
		const logical = Number.isInteger(tagged) ? tagged : domIndex;
		if (logical >= 0 && logical < cells.length) {
			labels[logical] = (cell.textContent || '').replace(/\s+/g, ' ').trim();
		}
	});
	return labels;
}

function isEligible(table: HTMLTableElement): boolean {
	if (table.closest('[data-tt-skip]')) return false;
	const row = headerRow(table);
	if (!row || row.cells.length < 2) return false;
	if (table.tBodies.length === 0) return false;
	// 合并表头无法一一对应数据列，保留该表原来的交互。
	if ([...row.cells].some((cell) => cell.colSpan > 1 || cell.rowSpan > 1)) return false;
	return true;
}

function storageKey(table: HTMLTableElement, labels: string[]): string {
	const page = window.location.hash.replace(/[?&].*$/, '') || '#cockpit';
	const cls = (table.className || 'table').trim().replace(/\s+/g, '.');
	// 原监控/交易表保留已有偏好键；新接入的报告表按位置区分，避免同名表互相覆盖。
	const legacy = table.closest('.trading-table-wrap, .trading-universe-table, .monitor-abn-rows-wrap, .stock-table-wrap, .market-overview-main');
	const path: string[] = [];
	if (!legacy) {
		let node: Element | null = table;
		while (node && node !== document.body) {
			if (node.id) { path.unshift(`#${node.id}`); break; }
			const parent: Element | null = node.parentElement;
			const index = parent ? [...parent.children].filter((child) => child.tagName === node!.tagName).indexOf(node) : 0;
			path.unshift(`${node.tagName}:${index}`);
			node = parent;
		}
	}
	return `${STORAGE_PREFIX}${page}|${cls}|${labels.join('~')}${path.length ? `|${path.join('/')}` : ''}`;
}

function readPrefs(key: string, labels: string[]): TablePrefs {
	try {
		const raw = window.localStorage.getItem(key);
		if (!raw) return {};
		const parsed = JSON.parse(raw) as TablePrefs;
		// 表头文字变了，说明这已不是同一张表：旧列序/列宽一律作废。
		if (parsed.labels && parsed.labels.join('~') !== labels.join('~')) return {};
		return parsed;
	} catch {
		return {};
	}
}

function writePrefs(key: string, prefs: TablePrefs) {
	try {
		window.localStorage.setItem(key, JSON.stringify(prefs));
	} catch {
		// 隐私模式下不可写：本次会话内仍生效，只是不跨刷新。
	}
}

/** 给每一格打上「逻辑列号」。
 *
 *  按**自身 DOM 位置**打标签，且只写一次。理由：React 新建出来的行总是按
 *  JSX 的自然顺序渲染，此刻位置就等于逻辑列号；而被拖动过的旧节点标签已经
 *  写好，不会再被覆盖。两者都成立，所以标签始终稳定。
 *  标签写完由 applyColumnOrder 按标签摆放，因此「拖过列序 + React 重建行」
 *  时新行也能一次摆到位。 */
function tagColumns(table: HTMLTableElement): number {
	const row = headerRow(table);
	if (!row) return 0;
	const width = row.cells.length;
	for (let i = 0; i < width; i++) {
		const th = row.cells[i];
		if (th.dataset.ttCol === undefined) th.dataset.ttCol = String(i);
	}
	for (const body of table.tBodies) {
		for (const tr of body.rows) {
			// 只有一个跨列单元格的展开行不参与列重排。
			if (tr.cells.length !== width) continue;
			for (let i = 0; i < tr.cells.length; i++) {
				const cell = tr.cells[i];
				if (cell.dataset.ttCol === undefined) cell.dataset.ttCol = String(i);
			}
		}
	}
	return width;
}

function currentDisplayOrder(table: HTMLTableElement): number[] {
	const row = headerRow(table);
	if (!row) return [];
	return [...row.cells].map((cell) => Number((cell as HTMLTableCellElement).dataset.ttCol));
}

/** 只在顺序确实不对时才动 DOM——保证幂等，不会与 MutationObserver 互相触发。 */
function applyColumnOrder(table: HTMLTableElement, order: number[]): boolean {
	const head = headerRow(table);
	if (!head) return false;
	let changed = false;
	if (!isOrderApplied(currentDisplayOrder(table), order)) {
		const byLogical = new Map<number, HTMLTableCellElement>();
		for (const cell of head.cells) byLogical.set(Number((cell as HTMLTableCellElement).dataset.ttCol), cell as HTMLTableCellElement);
		const holder = document.createDocumentFragment();
		for (const logical of order) {
			const cell = byLogical.get(logical);
			if (cell) holder.appendChild(cell);
		}
		head.appendChild(holder);
		changed = true;
	}

	// 表头可能已恢复列序，正文仍会在请求完成或轮询后新增；逐行判断才能同步。
	const width = order.length;
	for (const body of table.tBodies) {
		for (const tr of body.rows) {
			if (tr.cells.length !== width) continue;
			if (isOrderApplied([...tr.cells].map((cell) => Number(cell.dataset.ttCol)), order)) continue;
			const cells = new Map<number, HTMLTableCellElement>();
			let complete = true;
			for (const cell of tr.cells) {
				const logical = Number(cell.dataset.ttCol);
				if (!Number.isInteger(logical)) { complete = false; break; }
				cells.set(logical, cell as HTMLTableCellElement);
			}
			if (!complete || cells.size !== width) continue;
			const rowHolder = document.createDocumentFragment();
			for (const logical of order) {
				const cell = cells.get(logical);
				if (cell) rowHolder.appendChild(cell);
			}
			tr.appendChild(rowHolder);
			changed = true;
		}
	}
	return changed;
}

/** 列宽的落点：有 colgroup 就写 <col>，否则写表头 <th>。
 *
 *  两条路都不插入节点，避免与 React 的子节点对账打架：
 *  · 有 colgroup：<col> 的 width 在 fixed 布局下优先级最高，能压过 JSX 里
 *    写死的百分比（例如触发记录表的 12%/20%/42%）。
 *  · 没有 colgroup：table-layout: fixed 下，列宽来自首行单元格，所以写 th
 *    的 width 就够，不需要为它补一个 colgroup。
 *  表头单元格与 <col> 都不带 React 声明的 style（除少数表），React 重渲染
 *  不会把它清掉；万一被清，MutationObserver 下一帧会补回来。 */
function writeColumnWidth(table: HTMLTableElement, display: number, px: number | null) {
	const head = headerRow(table);
	const group = table.querySelector('colgroup');
	const cols = group && group.children.length === (head?.cells.length ?? -1) ? ([...group.children] as HTMLElement[]) : null;
	const target = (cols ? cols[display] : head?.cells[display]) as HTMLElement | undefined;
	if (!target) return;
	// 第一次覆盖前把 React 写进去的原始宽度（如 '15%'）记下来：
	// 覆盖后就再也读不回来了，resetTablePrefs 只能靠这份备份还原设计值。
	if (target.dataset.ttW0 === undefined) target.dataset.ttW0 = target.style.width;
	if (px === null) target.style.removeProperty('width');
	else target.style.width = `${px}px`;
}

/** 读回某一列当前被我们写死的像素宽度；读不到（没存过）返回 null。
 *  必须与 writeColumnWidth 落在同一处，否则总和算错、表宽就钉不准。
 *  只认 'px' 结尾：JSX 里写死的百分比会被 parseFloat 解析成数值（'15%' → 15），
 *  一旦某个百分比 ≥ MIN_COL_WIDTH（'60%' → 60）就会被误当成像素宽度，
 *  于是 syncTableWidth 把表宽钉成一个荒谬的小值、各列比例全乱。 */
function readColumnWidth(table: HTMLTableElement, display: number): number | null {
	const head = headerRow(table);
	const group = table.querySelector('colgroup');
	const cols = group && group.children.length === (head?.cells.length ?? -1) ? ([...group.children] as HTMLElement[]) : null;
	const target = (cols ? cols[display] : head?.cells[display]) as HTMLElement | undefined;
	if (!target) return null;
	const raw = target.style.width;
	if (!raw.endsWith('px')) return null;
	const px = parseFloat(raw);
	return Number.isFinite(px) && px >= MIN_COL_WIDTH ? px : null;
}

/** 把表格自身的宽度钉成「各列宽度之和」。
 *
 *  这是「拉宽时边界不跟手」的根因修法：只要表还是 width:100%（或留空），
 *  browser 在 table-layout:fixed 下会把容器多出来的空间再摊回各列——
 *  于是拖动第 3 列时，第 4、5 列也在动，边界从光标下溜走，永远抓不准。
 *  把表宽钉成各列之和后，拖第 3 列只有它自己变宽，边界与光标 1:1 同步；
 *  超出容器的部分交给容器的 overflow 去横向滚动。 */
function syncTableWidth(table: HTMLTableElement, count: number) {
	let total = 0;
	for (let display = 0; display < count; display++) {
		const px = readColumnWidth(table, display);
		if (px === null) return; // 还有列没写死（纯自动布局），不动表宽
		total += px;
	}
	if (total <= 0) return;
	if (table.dataset.ttW0 === undefined) table.dataset.ttW0 = table.style.width;
	table.style.width = `${total}px`;
	table.dataset.ttLayout = 'custom';
}

/** 把没存过宽度的列按当前实际宽度量下来写死。
 *  只在「这张表已存过部分列宽」时调用：混着「写死的 px」与「自动分配」
 *  会让 fixed 布局把余量只摊给后者，列宽变得不可预期。 */
function freezeUnsetColumns(table: HTMLTableElement, order: number[], widths: Record<number, number>) {
	const head = headerRow(table);
	if (!head) return;
	order.forEach((logical, display) => {
		if (typeof widths[logical] === 'number') return;
		if (readColumnWidth(table, display) !== null) return;
		const th = head.cells[display];
		if (!th) return;
		const measured = Math.round(th.getBoundingClientRect().width);
		if (measured >= MIN_COL_WIDTH) writeColumnWidth(table, display, measured);
	});
}

/** 第一次拖列宽前把所有列的实际宽度量下来写死。
 *  不这么做的话，把某一列改成 200px 会让浏览器按剩余空间重算其它列，
 *  明明只动了一列，整张表却一起跳。 */
function freezeColumnWidths(table: HTMLTableElement, width: number) {
	const head = headerRow(table);
	if (!head) return;
	for (let display = 0; display < width; display++) {
		const th = head.cells[display];
		if (!th) continue;
		const measured = Math.round(th.getBoundingClientRect().width);
		if (measured >= MIN_COL_WIDTH) writeColumnWidth(table, display, measured);
	}
	table.style.tableLayout = 'fixed';
	syncTableWidth(table, width);
}

function applyColumnWidths(table: HTMLTableElement, order: number[], widths: Record<number, number>) {
	if (order.length === 0) return;
	const saved = Object.keys(widths).length > 0;
	// 这张表从来没被调过列宽：一个字节都不要动。
	// 原来这里会对每一列调 writeColumnWidth(px = null)，而那是 removeProperty('width')——
	// 等于把 JSX 里 <col style={{ width: '15%' }}> 的设计列宽全擦掉，
	// table-layout: fixed 下直接退化成全列等宽（实测 8 列全 134px）。
	// 用户没表达过偏好时，页面原本的排版就是唯一正确答案。
	if (!saved) return;
	order.forEach((logical, displayIndex) => {
		const px = widths[logical];
		// 只写存过的那几列；没存的列不去清它，交给 freezeUnsetColumns 按实际宽度量下来。
		if (typeof px === 'number' && px >= MIN_COL_WIDTH) writeColumnWidth(table, displayIndex, px);
	});
	// 存过列宽的表必须是 fixed，否则那些 px 只是建议值，被内容一挤就失效。
	// 没有存过列宽的保持原样：自动布局对长短不一的内容更省空间。
	if (saved && table.style.tableLayout !== 'fixed') table.style.tableLayout = 'fixed';
	if (saved) {
		// 只存了部分列宽时，把其余列也按当前实际宽度量下来：
		// px 与 auto 混在同一条 fixed 表里，余量只会摊给 auto 那几列。
		freezeUnsetColumns(table, order, widths);
		syncTableWidth(table, order.length);
	}
}

type RowGroup = { key: string; rows: HTMLTableRowElement[]; origin: number };

/** 给主行记一个「原始次序」。取消排序时要靠它把行恢复原状——
 *  只靠当前 DOM 顺序是恢复不了的：那正是被排过的顺序。 */
function tagRowOrigin(table: HTMLTableElement, width: number) {
	for (const body of table.tBodies) {
		let max = -1;
		for (const tr of body.rows) {
			const value = Number(tr.dataset.ttOrig);
			if (Number.isInteger(value) && value > max) max = value;
		}
		for (const tr of body.rows) {
			if (tr.cells.length !== width) continue;
			if (tr.dataset.ttOrig === undefined) tr.dataset.ttOrig = String(++max);
		}
	}
}

/** 把 tbody 里的行切成「主行 + 紧随其后的展开行」若干组：展开行必须跟着主行走。 */
function rowGroupsOf(body: HTMLTableSectionElement, width: number): RowGroup[] {
	const groups: RowGroup[] = [];
	for (const tr of body.rows) {
		const isDetail = tr.cells.length !== width || [...tr.cells].some((cell) => cell.colSpan > 1);
		if (isDetail && groups.length > 0) {
			groups[groups.length - 1].rows.push(tr);
			continue;
		}
		const origin = Number(tr.dataset.ttOrig);
		groups.push({
			key: tr.dataset.ttRow || [...tr.cells].map((cell) => cell.dataset.ttCol ?? '').join('|') + '#' + (tr.textContent || '').slice(0, 24),
			rows: [tr],
			origin: Number.isInteger(origin) ? origin : groups.length,
		});
	}
	return groups;
}

/** 把带 data-tt-follow 的展开行挪到它所属主行之后。
 *
 *  为什么需要它：本模块会重排 tbody 的行（排序），而 React 插入展开行时用的是
 *  它自己**数据数组**的顺序——那正是被我们重排前的顺序。于是点第 4 行展开，
 *  内容却出现在第 34 行下面（实测偏移 30 行），正对用户反馈的
 *  「点击的个股却没有在这个个股下展开，内容显示到其他个股下面去了」。
 *  展开行自己声明属于谁（data-tt-follow=<主行 data-tt-row-id>），这里按声明归位，
 *  所以无论表被排过什么序，展开内容都跟着它那一行走。
 *  只搬带标记的行，纯 DOM 操作、不增删节点，React 重渲染后下一帧会再纠正一次。 */
function attachFollowRows(table: HTMLTableElement, width: number) {
	for (const body of table.tBodies) {
		const anchors = new Map<string, HTMLTableRowElement>();
		for (const tr of body.rows) {
			const id = tr.dataset.ttRowId;
			if (tr.cells.length === width && id) anchors.set(id, tr);
		}
		if (anchors.size === 0) continue;
		for (const tr of [...body.rows]) {
			const follow = tr.dataset.ttFollow;
			if (!follow) continue;
			const anchor = anchors.get(follow);
			if (!anchor) continue;
			if (anchor.nextSibling === tr) continue; // 已经就位
			anchor.after(tr);
		}
	}
}

function sortValueOf(row: HTMLTableRowElement, col: number): number | string | null {
	for (const cell of row.cells) {
		if (cell.dataset.ttCol !== String(col)) continue;
		const explicit = cell.dataset.ttValue;
		if (typeof explicit === 'string') return parseSortValue(explicit);
		// 合并列（序号 + 名称 + 代码）按名称排序：序号只是索引，
		// 拿整格文本排会变成按序号排，那就失去意义了。
		const name = cell.querySelector('.trading-symbol-name')?.textContent;
		if (name && name.trim()) return parseSortValue(name);
		return parseSortValue(cell.textContent || '');
	}
	return null;
}

function applySort(table: HTMLTableElement, sort: SortState, width: number) {
	// 表头状态先落地：箭头要立刻反映点击结果，哪怕数据整列不可比。
	const head = headerRow(table);
	if (head) {
		for (const cell of head.cells) {
			const th = cell as HTMLTableCellElement;
			const active = sort && Number(th.dataset.ttCol) === sort.col;
			if (active) th.dataset.ttSort = sort.dir;
			else if (th.dataset.ttSort) delete th.dataset.ttSort;
		}
	}
	for (const body of table.tBodies) {
		const groups = rowGroupsOf(body, width);
		if (groups.length < 2) continue;
		let ordered = groups;
		if (sort && Number.isInteger(sort.col)) {
			const col = sort.col;
			const decorated = groups.map((group) => ({ group, value: sortValueOf(group.rows[0], col) }));
			// 整列都没有可比值（全是「—」）时保持原序，免得点一下只换个箭头。
			if (decorated.some((entry) => entry.value !== null)) {
				decorated.sort((a, b) => compareSortValues(a.value, b.value, sort.dir));
				ordered = decorated.map((entry) => entry.group);
			}
		} else {
			// 取消排序：按原始次序还原，而不是留在「最后一次排序」的顺序里。
			ordered = groups.slice().sort((a, b) => a.origin - b.origin);
		}
		const changed = ordered.some((group, index) => group.rows[0] !== groups[index]?.rows[0]);
		if (!changed) continue;
		const holder = document.createDocumentFragment();
		for (const group of ordered) {
			for (const row of group.rows) holder.appendChild(row);
		}
		body.appendChild(holder);
	}
	renumberSequence(table, width);
}

/** 排序后重排序号。
 *
 *  序号是随行渲染的（index + 1），行被搬动后数字会变成 7、1、2… 看着像坏了。
 *  这里按显示顺序把序号改写成 1、2、3…。只改文本，不动节点，也不影响
 *  组件下一次渲染（React 会用回它自己的 index，随后这里再纠正）。 */
function renumberSequence(table: HTMLTableElement, width: number) {
	const body = table.tBodies[0];
	if (!body) return;
	let counter = 0;
	for (const tr of body.rows) {
		if (tr.cells.length !== width) continue;
		counter += 1;
		const seq = tr.querySelector<HTMLElement>('.trading-symbol-seq');
		if (!seq) continue;
		const next = String(counter);
		if (seq.textContent !== next) seq.textContent = next;
	}
}

/* 有列头的网格列表：保留原 DOM 与 CSS，只在用户调整后写列宽/显示顺序。 */
function gridHead(grid: HTMLElement): HTMLElement | null {
	return grid.querySelector<HTMLElement>(':scope > [data-tt-head]') || grid.firstElementChild as HTMLElement | null;
}

function columnHeaders(surface: HTMLElement): HTMLElement[] {
	if (surface instanceof HTMLTableElement) return [...(headerRow(surface)?.cells || [])];
	return [...(gridHead(surface)?.children || [])] as HTMLElement[];
}

function gridRows(grid: HTMLElement): HTMLElement[] {
	return [...grid.querySelectorAll<HTMLElement>(':scope > article, :scope > [data-tt-row], :scope > [data-tt-body] > [data-tt-row]')];
}

function gridState(grid: HTMLElement) {
	const cells = columnHeaders(grid);
	const width = cells.length;
	const rows = gridRows(grid);
	for (const row of [gridHead(grid), ...rows]) {
		if (!row || row.children.length !== width) continue;
		[...row.children].forEach((cell, index) => { (cell as HTMLElement).dataset.ttCol = String(index); });
	}
	const labels = grid.dataset.ttLabels?.split('|') || cells.map((cell) => (cell.textContent || '').replace(/\s+/g, ' ').trim());
	const key = `${STORAGE_PREFIX}${window.location.hash.replace(/[?&].*$/, '')}|grid:${grid.dataset.ttGrid}|${labels.join('~')}`;
	const prefs = readPrefs(key, labels);
	const natural = [...Array(width).keys()];
	const order = reorderByDisplay(natural, prefs.order);
	return { width, labels, key, prefs, order };
}

const gridWidths = new WeakMap<HTMLElement, Record<number, number>>();
const gridStyles = new WeakMap<HTMLElement, string>();

function rememberGridStyle(element: HTMLElement) {
	if (!gridStyles.has(element)) gridStyles.set(element, element.style.cssText);
}

function gridDisplayHeaders(grid: HTMLElement) {
	return columnHeaders(grid).sort((a, b) => Number(a.style.order || a.dataset.ttCol) - Number(b.style.order || b.dataset.ttCol));
}

function measureGrid(grid: HTMLElement): Record<number, number> {
	return Object.fromEntries(columnHeaders(grid).map((cell) => [Number(cell.dataset.ttCol), Math.max(MIN_COL_WIDTH, Math.round(cell.getBoundingClientRect().width))]));
}

function layoutGrid(grid: HTMLElement, order: number[], widths: Record<number, number>) {
	const head = gridHead(grid);
	if (!head || !order.length) return;
	const style = window.getComputedStyle(head);
	const gap = parseFloat(style.columnGap) || 0;
	const total = order.reduce((sum, logical) => sum + widths[logical], 0) + gap * (order.length - 1) + (parseFloat(style.paddingLeft) || 0) + (parseFloat(style.paddingRight) || 0);
	const tracks = order.map((logical) => `${widths[logical]}px`).join(' ');
	grid.dataset.ttLayout = 'custom';
	for (const row of [head, ...gridRows(grid)]) {
		if (row.children.length !== order.length) continue;
		rememberGridStyle(row);
		row.style.gridTemplateColumns = tracks;
		row.style.width = `${total}px`;
		row.style.boxSizing = 'border-box';
		row.style.minWidth = '0';
		[...row.children].forEach((cell, logical) => {
			const element = cell as HTMLElement;
			rememberGridStyle(element);
			element.style.order = String(order.indexOf(logical));
		});
	}
	gridWidths.set(grid, widths);
}

function applyGrid(grid: HTMLElement) {
	if (grid.closest('[data-tt-skip]')) return;
	const state = gridState(grid);
	if (state.width < 2) return;
	const external = grid.dataset.ttSortMode === 'react';
	const cells = columnHeaders(grid);
	cells.forEach((cell, logical) => {
		const button = cell.matches('button') ? cell : cell.querySelector('button');
		cell.dataset.ttSortable = UNSORTABLE.has(state.labels[logical]) || Boolean(button?.hasAttribute('disabled')) ? 'no' : 'yes';
		if (external) return;
		const sort = state.prefs.sort;
		if (sort?.col === logical) cell.dataset.ttSort = sort.dir;
		else delete cell.dataset.ttSort;
		cell.setAttribute('aria-sort', sort?.col === logical ? sort.dir === 'asc' ? 'ascending' : 'descending' : 'none');
	});
	const customized = state.prefs.order || (state.prefs.widths && Object.keys(state.prefs.widths).length);
	if (customized) layoutGrid(grid, state.order, { ...measureGrid(grid), ...state.prefs.widths });
	grid.dataset.ttKey = state.key;
	if (external) return;
	const rows = gridRows(grid);
	let origin = Math.max(-1, ...rows.map((row) => Number(row.dataset.ttOrig) || 0));
	for (const row of rows) if (row.dataset.ttOrig === undefined) row.dataset.ttOrig = String(++origin);
	const sort = state.prefs.sort;
	const value = (row: HTMLElement) => {
		const cell = row.children[sort?.col ?? 0] as HTMLElement | undefined;
		return parseSortValue(cell?.dataset.ttValue ?? cell?.textContent ?? '');
	};
	const ordered = [...rows].sort((a, b) => sort ? compareSortValues(value(a), value(b), sort.dir) : Number(a.dataset.ttOrig) - Number(b.dataset.ttOrig));
	if (ordered.some((row, index) => row !== rows[index])) for (const row of ordered) row.parentElement?.appendChild(row);
}

function applySurface(surface: HTMLElement) {
	if (surface instanceof HTMLTableElement) applyOne(surface);
	else applyGrid(surface);
}

function surfaceState(surface: HTMLElement) {
	return surface instanceof HTMLTableElement ? readState(surface) : gridState(surface);
}

function displayHeaders(surface: HTMLElement) {
	return surface instanceof HTMLTableElement ? columnHeaders(surface) : gridDisplayHeaders(surface);
}

function freezeSurface(surface: HTMLElement) {
	if (surface instanceof HTMLTableElement) freezeColumnWidths(surface, tagColumns(surface));
	else layoutGrid(surface, gridState(surface).order, measureGrid(surface));
}

function resizeSurface(surface: HTMLElement, display: number, px: number) {
	if (surface instanceof HTMLTableElement) {
		writeColumnWidth(surface, display, px);
		syncTableWidth(surface, tagColumns(surface));
		if (surface.parentElement) surface.parentElement.dataset.ttOverflow = 'yes';
	} else {
		const logical = Number(displayHeaders(surface)[display]?.dataset.ttCol);
		layoutGrid(surface, gridState(surface).order, { ...gridWidths.get(surface), [logical]: px });
	}
}

/* ─────────────── 调度 ─────────────── */

let applying = false;
let frame = 0;

function applyOne(table: HTMLTableElement) {
	if (!isEligible(table)) return;
	// 先打逻辑列号，再读表头文字——签名必须按逻辑列排列，不能按显示顺序。
	const width = tagColumns(table);
	if (width === 0) return;
	tagRowOrigin(table, width);
	const labels = headerLabels(table);
	const key = storageKey(table, labels);
	const prefs = readPrefs(key, labels);
	let order = prefs.order && prefs.order.length === width ? prefs.order : [...Array(width).keys()];
	if (prefs.order && prefs.order.length !== width) order = [...Array(width).keys()];
	applyColumnOrder(table, order);
	applyColumnWidths(table, order, prefs.widths || {});
	if (prefs.widths && Object.keys(prefs.widths).length && table.parentElement) table.parentElement.dataset.ttOverflow = 'yes';
	// 展开行先归位，再排序：排序是按「主行 + 其后展开行」成组搬的，
	// 若展开行此刻还错位在别处，它会被分到错误的那一组里跟着走。
	attachFollowRows(table, width);
	applySort(table, prefs.sort ?? null, width);
	table.dataset.ttKey = key;
	const head = headerRow(table);
	if (head) {
		for (const cell of head.cells) {
			const th = cell as HTMLTableCellElement;
			th.dataset.ttSortable = UNSORTABLE.has((th.textContent || '').replace(/\s+/g, ' ').trim()) ? 'no' : 'yes';
		}
	}
}

function applyAll() {
	if (applying) return;
	applying = true;
	try {
		for (const table of document.querySelectorAll<HTMLTableElement>('table')) {
			try {
				applyOne(table);
			} catch (error) {
				console.warn('[table-tools] 应用失败', error);
			}
		}
		for (const grid of document.querySelectorAll<HTMLElement>('[data-tt-grid]')) {
			try { applyGrid(grid); } catch (error) { console.warn('[table-tools] 应用失败', error); }
		}
	} finally {
		applying = false;
	}
}

function scheduleApply() {
	if (frame) return;
	frame = window.requestAnimationFrame(() => {
		frame = 0;
		applyAll();
	});
}

/* ─────────────── 交互 ─────────────── */

type Pending = {
	th: HTMLElement;
	table: HTMLElement;
	col: number;
	display: number;
	startX: number;
	startY: number;
	mode: 'resize' | 'reorder';
	dragging: boolean;
	startWidth: number;
	sortable: boolean;
};

let pending: Pending | null = null;

/** 判定指针是否落在某列的「边界热区」上。
 *
 *  两个要点，都是「抓不准边界」的根因：
 *  · **左右各算一次**——左边界与右边界都能抓。原来只算右边界，于是每一列
 *    的左侧一律抓不到，用户永远要往右偏一点才命中。
 *  · 取**相邻两列的中点**为界，而不是只看「离本列右缘多远」：相邻列之间
 *    本来就只有一条缝，把两侧合起来算，命中带才是连续且对称的，不会出现
 *    在两列交接处反而抓不住的空档。
 *  最后一列的右缘也允许抓（表格外框），方便把整张表加宽。 */
function edgeTarget(table: HTMLElement, clientX: number, clientY: number):
	{ display: number; th: HTMLElement } | null {
	const cells = displayHeaders(table);
	for (let i = 0; i < cells.length; i++) {
		const rect = cells[i].getBoundingClientRect();
		if (clientY < rect.top || clientY > rect.bottom) continue;
		// 左缘：只有不是第一列时才可抓（第一列左缘贴着表格框，拖动无意义，
		// 且会与容器滚动条冲突）。右缘：任意列都可抓。
		if (i > 0 && Math.abs(clientX - rect.left) <= EDGE_ZONE) return { display: i - 1, th: cells[i - 1] };
		if (Math.abs(clientX - rect.right) <= EDGE_ZONE) return { display: i, th: cells[i] };
	}
	return null;
}

/** 悬停时把整列的高度参考线画出来，让人看得见边界在哪。
 *  只画一条 line，进出表头都在同一处更新，不产生节点堆积。 */
let hoverLine: HTMLElement | null = null;
let hoverTable: HTMLElement | null = null;
/** 指针最后停留的屏幕坐标：容器滚动时靠它重算提示位置。 */
let hoverPoint: { x: number; y: number } | null = null;

function clearHoverCue() {
	if (hoverLine) {
		hoverLine.remove();
		hoverLine = null;
	}
	hoverTable = null;
	hoverPoint = null;
	document.body.classList.remove('tt-col-edge');
}

function showHoverCue(table: HTMLElement, display: number) {
	document.body.classList.add('tt-col-edge');
	if (!hoverLine) {
		hoverLine = document.createElement('div');
		hoverLine.className = 'tt-col-edge-line';
		document.body.appendChild(hoverLine);
	}
	hoverTable = table;
	const th = displayHeaders(table)[display];
	if (!th) return;
	const rect = th.getBoundingClientRect();
	// 参考线从表头顶一直画到表格底部，跨过正文——比只在表头画一小段更容易对齐。
	const tableRect = table.getBoundingClientRect();
	hoverLine.style.left = `${Math.round(rect.right)}px`;
	hoverLine.style.top = `${Math.round(rect.top)}px`;
	hoverLine.style.height = `${Math.round(Math.max(rect.height, tableRect.bottom - rect.top))}px`;
}

function hitHeader(node: EventTarget | null): { th: HTMLElement; table: HTMLElement } | null {
	if (!(node instanceof Element)) return null;
	const grid = node.closest<HTMLElement>('[data-tt-grid]');
	if (grid && !grid.closest('[data-tt-skip]')) {
		const th = columnHeaders(grid).find((cell) => cell.contains(node));
		if (th) return { th, table: grid };
	}
	if (node.closest('button, a, input, select, [role="button"]')) return null;
	const th = node.closest('th');
	if (!th) return null;
	const table = th.closest('table');
	if (!(table instanceof HTMLTableElement) || !isEligible(table)) return null;
	return { th: th as HTMLTableCellElement, table };
}

function readState(table: HTMLTableElement) {
	const width = tagColumns(table);
	const labels = headerLabels(table);
	const key = storageKey(table, labels);
	const prefs = readPrefs(key, labels);
	const order = prefs.order && prefs.order.length === width ? prefs.order : [...Array(width).keys()];
	return { labels, width, key, prefs, order };
}

function onPointerDown(event: PointerEvent) {
	if (event.button !== 0) return;
	const hit = hitHeader(event.target);
	if (!hit) return;
	const { th, table } = hit;
	// 按下即接管：先把悬停提示收干净，免得拖动期间留着一条不动的参考线。
	clearHoverCue();
	const col = Number(th.dataset.ttCol);
	const display = displayHeaders(table).indexOf(th);
	if (!Number.isInteger(col) || display < 0) return;
	const rect = th.getBoundingClientRect();
	// 先按「边界热区」判，命中即为调宽；否则才是换列序/排序。
	// 用统一的 edgeTarget 而不是「离右缘 6px」的单边判定，命中率与手感一致。
	const edge = edgeTarget(table, event.clientX, event.clientY);
	const nearEdge = rect.width > 40 && edge !== null;
	pending = {
		th, table,
		col: nearEdge && edge ? Number(edge.th.dataset.ttCol) ?? col : col,
		display: nearEdge && edge ? edge.display : display,
		startX: event.clientX,
		startY: event.clientY,
		mode: nearEdge ? 'resize' : 'reorder',
		dragging: false,
		startWidth: rect.width,
		sortable: th.dataset.ttSortable !== 'no',
	};
	if (nearEdge) {
		// 记下真正要调宽的那一列，拖动量以它为基准（与光标下的 th 可能不同）。
		const target = edge?.th;
		if (target) pending.startWidth = target.getBoundingClientRect().width;
		event.preventDefault();
		clearHoverCue();
		document.body.classList.add('tt-col-resizing');
	}
}

function onPointerMove(event: PointerEvent) {
	if (!pending) { updateHoverCue(event); return; }
	const dx = event.clientX - pending.startX;
	const dy = event.clientY - pending.startY;
	if (!pending.dragging && Math.abs(dx) + Math.abs(dy) < 4) return;
	if (!pending.dragging && pending.mode === 'resize') {
		// 第一次真正拖动时才冻结其余列宽：没动过列宽的表保持原有布局方式。
		freezeSurface(pending.table);
	}
	pending.dragging = true;
	if (pending.mode === 'resize') {
		const next = Math.max(MIN_COL_WIDTH, Math.round(pending.startWidth + dx));
		resizeSurface(pending.table, pending.display, next);
		return;
	}
	document.body.classList.add('tt-col-dragging');
	if (pending.th.dataset.ttDrag !== 'active') pending.th.dataset.ttDrag = 'active';
}

/** 没按下时：跟着指针更新边界提示（光标 + 参考线）。 */
function updateHoverCue(event: PointerEvent) {
	hoverPoint = { x: event.clientX, y: event.clientY };
	const hit = hitHeader(event.target);
	if (!hit) { clearHoverCue(); return; }
	const edge = edgeTarget(hit.table, event.clientX, event.clientY);
	if (!edge) { clearHoverCue(); return; }
	showHoverCue(hit.table, edge.display);
}

/** 参考线是 fixed 定位，容器一滚旧坐标就失效。此时指针没动，
 *  所以只能按「指针当前落在哪」重新找一次表头再摆线。
 *  hoverLine 自带 pointer-events:none，不会被 elementFromPoint 命中。 */
function refreshHoverCue() {
	if (!hoverLine || !hoverPoint) return;
	// 表已被 React 换掉（切页/刷新数据）：旧线对应的节点早不在文档里，直接收掉。
	if (!hoverTable?.isConnected) { clearHoverCue(); return; }
	const hit = hitHeader(document.elementFromPoint(hoverPoint.x, hoverPoint.y));
	if (!hit) { clearHoverCue(); return; }
	const edge = edgeTarget(hit.table, hoverPoint.x, hoverPoint.y);
	if (!edge) { clearHoverCue(); return; }
	showHoverCue(hit.table, edge.display);
}

function onPointerUp(event: PointerEvent) {
	const current = pending;
	pending = null;
	document.body.classList.remove('tt-col-resizing');
	document.body.classList.remove('tt-col-dragging');
	if (!current) return;
	const { th, table, col, display } = current;
	if (th.dataset.ttDrag) delete th.dataset.ttDrag;
	if (current.dragging || current.mode === 'resize') suppressClick = { table, until: Date.now() + 500 };

	if (!current.dragging) {
		// 在边界上单击（想调宽但没拖动）：什么都不做。
		// 原来这里会掉进排序分支——用户点边界想抓宽，反而把表排了一遍。
		if (current.mode === 'resize') return;
		if (!current.sortable) return;
		if (table.dataset.ttSortMode === 'react') return;
		const state = surfaceState(table);
		const same = state.prefs.sort && state.prefs.sort.col === col;
		const next: SortState = same && state.prefs.sort!.dir === 'asc'
			? { col, dir: 'desc' }
			: same && state.prefs.sort!.dir === 'desc'
				? null
				: { col, dir: 'asc' };
		writePrefs(state.key, { ...state.prefs, sort: next, labels: state.labels });
		applySurface(table);
		return;
	}

	if (current.mode === 'resize') {
		// 存**全部列**的宽度，而不是只存被拖的那一列：
		// 此时其余列已被 freezeColumnWidths 写死，只存一列的话，刷新后其余列
		// 会被浏览器重新分配，跟用户调好的比例对不上。
		const state = surfaceState(table);
		const saved: Record<number, number> = { ...(state.prefs.widths || {}) };
		displayHeaders(table).forEach((cell) => {
			const px = Math.round(cell.getBoundingClientRect().width);
			const logical = Number((cell as HTMLTableCellElement).dataset.ttCol);
			if (Number.isInteger(logical) && px >= MIN_COL_WIDTH) saved[logical] = px;
		});
		writePrefs(state.key, { ...state.prefs, widths: saved, labels: state.labels });
		return;
	}

	const rects = displayHeaders(table).map((cell) => cell.getBoundingClientRect());
	let target = rects.findIndex((rect) => event.clientX < rect.left + rect.width / 2);
	if (target < 0) target = rects.length - 1;
	if (target === display) return;
	const state = surfaceState(table);
	const nextOrder = moveInOrder(state.order, display, target);
	const measured = Object.fromEntries(columnHeaders(table).map((cell) => [Number(cell.dataset.ttCol), Math.max(MIN_COL_WIDTH, Math.round(cell.getBoundingClientRect().width))]));
	const widths = { ...measured, ...state.prefs.widths };
	writePrefs(state.key, { ...state.prefs, order: nextOrder, widths, labels: state.labels });
	applySurface(table);
}

let suppressClick: { table: HTMLElement; until: number } | null = null;

export function resetTablePrefs(table?: HTMLTableElement) {
	if (table) {
		const key = table.dataset.ttKey;
		if (key) writePrefs(key, {});
		// 还原列宽：不能直接 removeProperty('width')，否则连 JSX 里写死的百分比
		// 一起擦掉（就是这一版修的那个 bug）。有备份就用备份，没有才留空。
		const targets: HTMLElement[] = [table];
		const group = table.querySelector('colgroup');
		const head = headerRow(table);
		if (group) targets.push(...([...group.children] as HTMLElement[]));
		if (head) targets.push(...([...head.cells] as HTMLElement[]));
		for (const target of targets) {
			const original = target.dataset.ttW0;
			if (original === undefined) continue;
			if (original) target.style.width = original;
			else target.style.removeProperty('width');
			delete target.dataset.ttW0;
		}
		if (head) {
			for (const cell of head.cells) delete (cell as HTMLTableCellElement).dataset.ttSort;
		}
		table.style.removeProperty('table-layout');
		delete table.dataset.ttLayout;
		applyOne(table);
		return;
	}
	try {
		const keys = Object.keys(window.localStorage).filter((key) => key.startsWith(STORAGE_PREFIX));
		for (const key of keys) window.localStorage.removeItem(key);
	} catch { /* 不可写即无偏好可清 */ }
	// 整表重置：把已改过的内联宽度也还原回设计值。
	for (const target of document.querySelectorAll<HTMLElement>('[data-tt-w0]')) {
		const original = target.dataset.ttW0 || '';
		if (original) target.style.width = original;
		else target.style.removeProperty('width');
		delete target.dataset.ttW0;
	}
	for (const el of document.querySelectorAll<HTMLTableElement>('table')) {
		el.style.removeProperty('table-layout');
		delete el.dataset.ttLayout;
	}
	for (const grid of document.querySelectorAll<HTMLElement>('[data-tt-grid]')) {
		for (const row of [gridHead(grid), ...gridRows(grid)]) {
			if (!row) continue;
			for (const element of [row, ...row.children] as HTMLElement[]) {
				const original = gridStyles.get(element);
				if (original !== undefined) element.style.cssText = original;
				gridStyles.delete(element);
			}
		}
		gridWidths.delete(grid);
		delete grid.dataset.ttLayout;
	}
	scheduleApply();
}

/** 仅供单测：把列宽应用逻辑暴露出来，好在没有完整 DOM 的环境里验证
 *  「用户没存过列宽时一个字节都不动」这条不变量。生产代码不要调用。 */
export const applyColumnWidthsForTest = applyColumnWidths;
/** 仅供单测：校验表头先恢复、正文后加载时的列序同步。 */
export const applyColumnOrderForTest = applyColumnOrder;
/** 仅供单测：同上（重置路径必须还原 JSX 设计列宽，不能留空）。 */
export const resetTablePrefsForTest = resetTablePrefs;

export function installTableTools() {
	if (typeof window === 'undefined' || observer) return;
	document.addEventListener('pointerdown', onPointerDown, true);
	document.addEventListener('pointermove', onPointerMove, true);
	document.addEventListener('pointerup', onPointerUp, true);
	document.addEventListener('click', (event) => {
		const blocked = suppressClick;
		suppressClick = null;
		if (blocked && Date.now() < blocked.until && event.detail > 0 && event.target instanceof Node && blocked.table.contains(event.target)) {
			event.preventDefault();
			event.stopImmediatePropagation();
		}
	}, true);
	document.addEventListener('pointercancel', () => {
		if (pending) delete pending.th.dataset.ttDrag;
		pending = null;
		document.body.classList.remove('tt-col-resizing');
		document.body.classList.remove('tt-col-dragging');
	}, true);
	observer = new MutationObserver(() => scheduleApply());
	observer.observe(document.body, { childList: true, characterData: true, subtree: true });
	// scroll 不冒泡，但捕获阶段仍会经过 document，所以用 capture 监听即可。
	// 参考线是 fixed 定位，容器一滚它就与数据脱钩，必须重算。
	document.addEventListener('scroll', () => refreshHoverCue(), true);
	document.addEventListener('pointerleave', () => clearHoverCue());
	window.addEventListener('resize', scheduleApply);
	window.addEventListener('hashchange', scheduleApply);
	scheduleApply();
}

let observer: MutationObserver | null = null;
