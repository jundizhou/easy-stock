import { requestJSON, type BackendConfig } from './backend';

/** 本机 sidecar 服务地址（来源：GET /api/v1/runtime/sidecars）。 */
export type SidecarEndpoints = {
	quant_url: string;
	stockdb_url: string;
};

/** stockdb 板块表的行值结构（表名：板块*）。 */
export type StockDBBoard = {
	code: string;
	name: string;
	source: string;
	type: string;
	group: string;
	category: string;
	symbols: string[];
};

export const BOARD_TABLE = '板块*';

/** 板块类目固定顺序：申万体系在前，概念在后。 */
export const BOARD_CATEGORY_ORDER = ['申万一级', '申万二级', '申万三级', '概念'] as const;

export function fetchSidecars(config: BackendConfig, init: RequestInit = {}): Promise<SidecarEndpoints> {
	return requestJSON<SidecarEndpoints>(config, '/api/v1/runtime/sidecars', init);
}

/**
 * 解析 stockdb `?cmd=get&t=板块*&json=1` 的响应。
 * 响应为 [key, value] 数组的数组；value 缺字段或 symbols 非数组时跳过该行。
 */
export function parseBoardRows(raw: unknown): StockDBBoard[] {
	if (!Array.isArray(raw)) return [];
	const boards: StockDBBoard[] = [];
	for (const row of raw) {
		if (!Array.isArray(row) || row.length < 2 || typeof row[1] !== 'object' || row[1] === null) continue;
		const value = row[1] as Record<string, unknown>;
		const code = typeof value.code === 'string' ? value.code : '';
		const name = typeof value.name === 'string' ? value.name : '';
		const category = typeof value.category === 'string' ? value.category : '';
		if (!code || !name || !category) continue;
		const symbols = Array.isArray(value.symbols)
			? value.symbols.filter((item): item is string => typeof item === 'string')
			: [];
		boards.push({
			code,
			name,
			category,
			source: typeof value.source === 'string' ? value.source : '',
			type: typeof value.type === 'string' ? value.type : '',
			group: typeof value.group === 'string' ? value.group : '',
			symbols,
		});
	}
	return boards;
}

export function fetchBoards(config: BackendConfig, init: RequestInit = {}): Promise<StockDBBoard[]> {
	return requestJSON<unknown>(config, `/api/v1/stockdb/query?t=${encodeURIComponent(BOARD_TABLE)}`, init)
		.then(raw => parseBoardRows(raw));
}

/** 按固定顺序归组板块（未知类目排在最后，组内按名称排序）。 */
export function groupBoardsByCategory(boards: StockDBBoard[]): Array<[string, StockDBBoard[]]> {
	const buckets = new Map<string, StockDBBoard[]>();
	for (const board of boards) {
		const bucket = buckets.get(board.category);
		if (bucket) bucket.push(board);
		else buckets.set(board.category, [board]);
	}
	const known = BOARD_CATEGORY_ORDER.filter(category => buckets.has(category));
	const unknown = [...buckets.keys()].filter(category => !(BOARD_CATEGORY_ORDER as readonly string[]).includes(category));
	return [...known, ...unknown].map((category) => {
		const items = buckets.get(category) || [];
		items.sort((a, b) => a.name.localeCompare(b.name, 'zh-Hans-CN'));
		return [category, items] as [string, StockDBBoard[]];
	});
}

/** 反查：一只股票代码所属的全部板块。 */
export function boardsForSymbol(boards: StockDBBoard[], symbol: string): StockDBBoard[] {
	const normalized = normalizeSymbol(symbol);
	if (!normalized) return [];
	return boards.filter(board => board.symbols.includes(normalized));
}

/** 归一化输入：去掉交易所后缀与空白，保留 6 位代码。 */
export function normalizeSymbol(input: string): string {
	const trimmed = input.trim().toUpperCase();
	const match = trimmed.match(/^(\d{6})/);
	return match ? match[1] : '';
}
