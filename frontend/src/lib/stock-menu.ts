// 个股右键菜单：在任意列表的个股行上点右键，弹出统一的操作项。
//
// 为什么走全局委托而不是每张表改 JSX：站内出现个股的列表有十几处，逐处加
// onContextMenu 会散落十几份重复代码，且各表的「当前行是哪只股」取法不同。
//
// 认标的的顺序（可靠性从高到低）：
//   ① 行上显式的 data-symbol / data-name —— 列表自己知道自己渲染的是哪只股，
//      这是唯一不会出错的方式，站内个股列表都应标注；
//   ② 兜底：逐格找「整格就是一个代码」的单元格，再从同格的名称节点取名。
//      早期只靠 ②，遇到「身份/阶段/领导力」这类多列并排的表会把分数当成
//      名称（显示成「43」），所以现在把 ① 作为主路径。
//
// 与 App 的连接：菜单需要「跳转 + 预填 prompt」这类能力，通过绑定一个
// handler 注入（见 bindStockMenu），避免 lib 反向依赖组件层。

import { isWatched, markWatched, unmarkWatched } from './watchlist-registry';

export type StockMenuTarget = {
	symbol: string;
	name: string;
	/** 这一行是否已经在本系统的关注仓里；决定首项是「加入关注」还是「删除关注」。
	 *  由全局镜像 + 行内标记共同判断，见 readStockFromRow。 */
	inWatchlist?: boolean;
};

export type StockMenuHandlers = {
	/** 加入关注仓；返回提示文案。 */
	addWatch: (target: StockMenuTarget) => Promise<string>;
	/** 从关注仓移除；返回提示文案。 */
	removeWatch: (target: StockMenuTarget) => Promise<string>;
	/** 进入 AI 对话并预填该股分析 prompt。 */
	askAI: (target: StockMenuTarget) => void;
	/** 跳到题材库并定位到该股。 */
	openThemeLibrary: (target: StockMenuTarget) => void;
};

const MENU_ID = 'stock-context-menu';

/** 整格就是一个代码：必须从头到尾都对上。
 *  不能用「行文本里搜 6 位数字」——行文本是各单元格拼起来的，
 *  名称后面的数字会和代码粘成一串（「华阳智能3015026…」），
 *  搜出来的是半个代码或干脆搜不到。 */
const CODE_CELL_RE = /^\s*(\d{6})(?:\.(?:SZ|SH|BJ))?\s*$/i;
const CODE_ANY_RE = /(?:^|[^\d])(\d{6})(?:\.(?:SZ|SH|BJ))?(?=$|[^\d])/i;
/** 纯数字（含正负号、小数点、百分号）不是股票名称，是分数或涨跌幅。 */
const NUMERIC_RE = /^[\d.+\-%\s]+$/;

let handlers: StockMenuHandlers | null = null;
let menuEl: HTMLDivElement | null = null;
let current: StockMenuTarget | null = null;
let noticeTimer = 0;

export function bindStockMenu(next: StockMenuHandlers) {
	handlers = next;
}

/** 名称是否可信：非空、不是纯数字、不是代码本身。 */
function saneName(value: string | undefined | null): string {
	const text = (value || '').replace(/\s+/g, ' ').trim();
	if (!text || text.length > 16) return '';
	if (NUMERIC_RE.test(text)) return '';
	if (CODE_ANY_RE.test(text)) return '';
	return text;
}

/** 在一行里找名称节点：优先明确的名称类名，其次 strong。 */
function nameFrom(scope: Element): string {
	const named = scope.querySelector<HTMLElement>('.trading-symbol-name, .monitor-abn-rank-name, .stock-name');
	const fromNamed = saneName(named?.textContent);
	if (fromNamed) return fromNamed;
	for (const node of scope.querySelectorAll<HTMLElement>('strong')) {
		const value = saneName(node.textContent);
		if (value) return value;
	}
	return '';
}

/** 兜底：逐格找「整格就是一个代码」的单元格，再从同格 / 同行取名。 */
function pickByCells(row: HTMLElement): StockMenuTarget | null {
	const candidates = [...row.querySelectorAll<HTMLElement>('.trading-symbol-code, .trading-symbol-cell, small, .mono')];
	const cells: Element[] = row instanceof HTMLTableRowElement ? [...row.cells] : [...row.children];
	const pool = [...candidates, ...cells];
	for (const node of pool) {
		const text = (node.textContent || '').trim();
		const match = text.match(CODE_CELL_RE) || text.match(CODE_ANY_RE);
		if (!match) continue;
		const symbol = match[1];
		const scope = node.closest('td, th') ?? node;
		// 名称只在「装着这个代码的那一格」里找；同一格里没有就退到整行找。
		const name = nameFrom(scope) || nameFrom(row);
		return { symbol, name, inWatchlist: row.dataset.stockContext === 'watchlist' };
	}
	return null;
}

/** 从一行里认出标的。 */
export function readStockFromRow(row: HTMLElement): StockMenuTarget | null {
	// 关注仓判定以全局镜像为准：同一只票在雷达/异动/行情总览右键时，
	// 行上不会有 watchlist 标记，但它确实在关注仓里，必须显示「删除关注」。
	// 行级 data-stock-context 只作为镜像尚未就绪时的补充（首次加载竞态）。
	const marked = row.dataset.stockContext === 'watchlist'
		|| Boolean(row.closest('[data-stock-context="watchlist"]'));
	// ① 显式标注：行本身或行内第一个带 data-symbol 的节点。
	const holder = row.dataset.symbol ? row : row.querySelector<HTMLElement>('[data-symbol]');
	const explicit = (holder?.dataset.symbol || '').trim();
	if (explicit) {
		const name = saneName(holder?.dataset.name) || saneName(row.dataset.name) || nameFrom(row);
		const inWatchlist = marked || isWatched(explicit);
		return { symbol: explicit, name, inWatchlist };
	}
	// ② 兜底。
	const found = pickByCells(row);
	if (!found) return null;
	return { ...found, inWatchlist: marked || isWatched(found.symbol) };
}

function closeMenu() {
	if (menuEl) {
		menuEl.remove();
		menuEl = null;
	}
	current = null;
}

function showNotice(text: string) {
	let node = document.getElementById('stock-menu-notice');
	if (!node) {
		node = document.createElement('div');
		node.id = 'stock-menu-notice';
		node.className = 'stock-menu-notice';
		node.setAttribute('role', 'status');
		document.body.appendChild(node);
	}
	node.textContent = text;
	node.classList.add('is-open');
	window.clearTimeout(noticeTimer);
	noticeTimer = window.setTimeout(() => node?.classList.remove('is-open'), 2600);
}

type MenuItem = { action: 'addWatch' | 'removeWatch' | 'askAI' | 'openThemeLibrary'; label: string; hint: string; icon: string };

/** 菜单项按上下文变化：已经在关注仓里的股票不该再出现「加入关注」。 */
function itemsFor(target: StockMenuTarget): MenuItem[] {
	const watchItem: MenuItem = target.inWatchlist
		? { action: 'removeWatch', label: '删除关注', hint: '从关注仓移除该股', icon: 'trash' }
		: { action: 'addWatch', label: '加入关注', hint: '加到关注仓，持续跟踪', icon: 'plus' };
	return [
		watchItem,
		{ action: 'askAI', label: 'AI 分析', hint: '进入 AI 对话分析该股', icon: 'spark' },
		{ action: 'openThemeLibrary', label: '题材库', hint: '查看相关题材与标的', icon: 'book' },
	];
}

const ICONS: Record<string, string> = {
	plus: 'M8 3v10M3 8h10',
	trash: 'M3 4h10M6 4V3h4v1M5 4l.5 9h5L11 4',
	spark: 'M8 2l1.6 4.4L14 8l-4.4 1.6L8 14l-1.6-4.4L2 8l4.4-1.6z',
	book: 'M3 3h4a1.5 1.5 0 011.5 1.5V13A1.2 1.2 0 007.3 12H3zM13 3H9.5A1.5 1.5 0 008 4.5V13a1.2 1.2 0 011.2-1H13z',
};

function svgIcon(name: string): SVGSVGElement {
	const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
	svg.setAttribute('viewBox', '0 0 16 16');
	svg.setAttribute('width', '15');
	svg.setAttribute('height', '15');
	svg.setAttribute('fill', 'none');
	svg.setAttribute('stroke', 'currentColor');
	svg.setAttribute('stroke-width', '1.5');
	svg.setAttribute('stroke-linecap', 'round');
	svg.setAttribute('stroke-linejoin', 'round');
	svg.setAttribute('aria-hidden', 'true');
	const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
	path.setAttribute('d', ICONS[name] || ICONS.plus);
	svg.appendChild(path);
	return svg;
}

function buildMenu(target: StockMenuTarget): HTMLDivElement {
	const menu = document.createElement('div');
	menu.id = MENU_ID;
	menu.className = 'stock-context-menu';
	menu.setAttribute('role', 'menu');

	const head = document.createElement('div');
	head.className = 'stock-context-head';
	const title = document.createElement('strong');
	// 名称取不到时只显示代码，不留一个空格子——总比显示一串分数好。
	title.textContent = target.name || target.symbol;
	title.title = target.name ? `${target.name} ${target.symbol}` : target.symbol;
	const code = document.createElement('small');
	code.textContent = target.symbol;
	head.append(title, code);
	menu.appendChild(head);

	const items = itemsFor(target);
	for (const item of items) {
		const button = document.createElement('button');
		button.type = 'button';
		button.className = `stock-context-item${item.action === 'removeWatch' ? ' is-danger' : ''}`;
		button.setAttribute('role', 'menuitem');
		button.dataset.action = item.action;
		const icon = document.createElement('span');
		icon.className = 'stock-context-icon';
		icon.appendChild(svgIcon(item.icon));
		const body = document.createElement('span');
		body.className = 'stock-context-copy';
		const label = document.createElement('span');
		label.textContent = item.label;
		const hint = document.createElement('em');
		hint.textContent = item.hint;
		body.append(label, hint);
		button.append(icon, body);
		button.addEventListener('click', (event) => {
			event.stopPropagation();
			const active = current;
			closeMenu();
			if (!active || !handlers) return;
			if (item.action === 'addWatch' || item.action === 'removeWatch') {
				// 点击瞬间就乐观更新镜像：写接口要等后端补完行情快照才返回（数秒），
				// 期间用户若切页再右键同一只票，会看到过期首项。失败则回滚。
				// lib/monitor 里的回写负责把镜像与后端清单对齐，两者互补。
				const adding = item.action === 'addWatch';
				if (adding) markWatched(active.symbol, active.name);
				else unmarkWatched(active.symbol);
				const run = adding ? handlers.addWatch : handlers.removeWatch;
				void run(active)
					.then((message) => showNotice(message))
					.catch((error: unknown) => {
						// 回滚乐观更新：以服务端为准。
						if (adding) unmarkWatched(active.symbol);
						else markWatched(active.symbol, active.name);
						showNotice(error instanceof Error ? error.message : '操作失败');
					});
				return;
			}
			if (item.action === 'askAI') handlers.askAI(active);
			else handlers.openThemeLibrary(active);
		});
		menu.appendChild(button);
	}
	return menu;
}

function place(menu: HTMLDivElement, x: number, y: number) {
	menu.style.visibility = 'hidden';
	document.body.appendChild(menu);
	// 必须先登记到 menuEl：closeMenu 依赖它判断「有没有开着的菜单」，
	// 漏了这一步会导致 Esc / 点外部都关不掉。
	menuEl = menu;
	const rect = menu.getBoundingClientRect();
	const left = Math.max(8, Math.min(x, window.innerWidth - rect.width - 8));
	const top = Math.max(8, Math.min(y, window.innerHeight - rect.height - 8));
	menu.style.left = `${left}px`;
	menu.style.top = `${top}px`;
	menu.style.visibility = 'visible';
}

function onContextMenu(event: MouseEvent) {
	if (!handlers) return;
	const element = event.target instanceof Element ? event.target : null;
	if (!element) return;
	// 行级容器：表格行、异动左栏行、连板个股卡、行情总览的列表项。
	const row = element.closest<HTMLElement>('tr, .monitor-abn-watchlist-row, .limit-stock-chip, [data-stock-row]');
	if (!row) return;
	// 表头不算数据行；输入框/按钮上的右键留给浏览器原生菜单。
	if (row.closest('thead')) return;
	if (element.closest('input, textarea, [contenteditable="true"]')) return;
	const target = readStockFromRow(row);
	if (!target) return;
	event.preventDefault();
	closeMenu();
	current = target;
	const menu = buildMenu(target);
	place(menu, event.clientX, event.clientY);
}

export function installStockMenu() {
	if (typeof window === 'undefined' || installed) return;
	installed = true;
	document.addEventListener('contextmenu', onContextMenu, true);
	document.addEventListener('pointerdown', (event) => {
		if (!menuEl) return;
		if (event.target instanceof Node && menuEl.contains(event.target)) return;
		closeMenu();
	}, true);
	document.addEventListener('keydown', (event) => {
		if (event.key === 'Escape') closeMenu();
	}, true);
	window.addEventListener('blur', closeMenu);
	window.addEventListener('resize', closeMenu);
	document.addEventListener('scroll', () => { if (menuEl) closeMenu(); }, true);
}

let installed = false;
