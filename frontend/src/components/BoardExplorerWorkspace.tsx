import { useCallback, useEffect, useMemo, useState } from 'react';
import { Layers3, LoaderCircle, RefreshCw, Search } from 'lucide-react';
import { requestJSON, type BackendConfig } from '../lib/backend';
import { useStockNameMap } from '../lib/stock-directory';
import {
	boardsForSymbol,
	fetchBoards,
	groupBoardsByCategory,
	normalizeSymbol,
	type StockDBBoard,
} from '../lib/sidecars';
import { logRuntimeEvent } from '../lib/runtime-log';

export type BoardFocus = { name: string; hint?: string };

/** 成分股实时行情快照（来自 /api/v1/quotes/realtime，失败静默降级为纯代码按钮）。 */
export type BoardSymbolQuote = { symbol: string; name: string; price: number; changePercent: number };

/** 板块热度（成分股行情聚合；无行情数据返回 null）。 */
export function aggregateBoardHeat(quotes: BoardSymbolQuote[]): { avgChangePercent: number; upCount: number; downCount: number } | null {
	if (!quotes.length) return null;
	let up = 0;
	let down = 0;
	let total = 0;
	for (const quote of quotes) {
		if (!Number.isFinite(quote.changePercent)) continue;
		total += quote.changePercent;
		if (quote.changePercent > 0) up += 1;
		else if (quote.changePercent < 0) down += 1;
	}
	if (!quotes.length) return null;
	return { avgChangePercent: total / quotes.length, upCount: up, downCount: down };
}

/**
 * 跨工作台定位解析：优先全库精确同名（stockdb 概念兜底 chips 与本页同源必命中），
 * 其次包含关系模糊命中；均未命中回退为类目内搜索框填词。
 */
export function resolveBoardFocus(boards: StockDBBoard[], focus: BoardFocus): { category: string; code: string; query: string } | null {
	const keyword = focus.name.trim();
	if (!keyword || !boards.length) return null;
	const target = boards.find(board => board.name === keyword)
		|| boards.find(board => board.name.includes(keyword) || keyword.includes(board.name));
	if (target) return { category: target.category, code: target.code, query: '' };
	return { category: focus.hint || '概念', code: '', query: keyword };
}

function formatChangePercent(value: number): string {
	if (!Number.isFinite(value)) return '--';
	return `${value > 0 ? '+' : ''}${value.toFixed(2)}%`;
}

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
	/** 跨工作台联动：按板块名定位（如连板雷达概念 chip 下钻）；消费后回调清空。 */
	focusBoard?: BoardFocus | null;
	onFocusConsumed?: () => void;
	/** 点击成分股跳个股 AI 分析（symbol 为 6 位代码）。 */
	onOpenStock?: (symbol: string) => void;
};

export function BoardExplorerWorkspace({ config, refreshKey, focusBoard, onFocusConsumed, onOpenStock }: Props) {
	const [boards, setBoards] = useState<StockDBBoard[]>([]);
	const [state, setState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
	const [error, setError] = useState('');
	// 就绪后再刷新保留内容不卸载（避免闪烁），另用独立标志给按钮反馈。
	const [refreshing, setRefreshing] = useState(false);
	const [category, setCategory] = useState('');
	const [query, setQuery] = useState('');
	const [selectedCode, setSelectedCode] = useState('');
	const [quotes, setQuotes] = useState<BoardSymbolQuote[] | null>(null);
	const [symbolQuery, setSymbolQuery] = useState('');
	const [symbolMatches, setSymbolMatches] = useState<StockDBBoard[] | null>(null);

	const load = useCallback(() => {
		if (!config) return;
		const abort = new AbortController();
		setState(previous => (previous === 'ready' ? previous : 'loading'));
		setError('');
		setRefreshing(true);
		fetchBoards(config, { signal: abort.signal })
			.then(payload => {
				setRefreshing(false);
				if (abort.signal.aborted) return;
				setBoards(payload);
				setState('ready');
			})
			.catch((reason) => {
				setRefreshing(false);
				if (abort.signal.aborted) return;
				setError(reason instanceof Error ? reason.message : '板块数据加载失败');
				setState('error');
				logRuntimeEvent('warn', 'board-explorer', { event: 'boards_unavailable' });
			});
		return () => abort.abort();
	}, [config]);

	useEffect(() => { const cleanup = load(); return cleanup; }, [load, refreshKey]);

	// 跨工作台定位：解析逻辑见 resolveBoardFocus（纯函数，含单测）。
	useEffect(() => {
		if (!focusBoard?.name || !boards.length) return;
		const resolved = resolveBoardFocus(boards, focusBoard);
		if (resolved) {
			setCategory(resolved.category);
			setSelectedCode(resolved.code);
			setQuery(resolved.query);
		}
		onFocusConsumed?.();
	}, [focusBoard, boards, onFocusConsumed]);

	const groups = useMemo(() => groupBoardsByCategory(boards), [boards]);
	const activeCategory = category || groups[0]?.[0] || '';
	const activeBoards = useMemo(() => {
		const bucket = groups.find(([name]) => name === activeCategory)?.[1] || [];
		const keyword = query.trim().toLowerCase();
		if (!keyword) return bucket;
		return bucket.filter(board => board.name.toLowerCase().includes(keyword) || board.code.toLowerCase().includes(keyword));
	}, [groups, activeCategory, query]);
	const selectedBoard = activeBoards.find(board => board.code === selectedCode) || activeBoards[0] || null;

	// 成分股实时行情：选中板块变化时拉一次（≤60 只截断，失败静默——行情列是增强不是依赖）。
	useEffect(() => {
		if (!config || !selectedBoard || !selectedBoard.symbols.length) { setQuotes(null); return; }
		const symbols = selectedBoard.symbols.slice(0, 60);
		const abort = new AbortController();
		const timer = setTimeout(() => {
			requestJSON<{ data?: Array<{ symbol?: string; name?: string; price?: number; change_percent?: number }> }>(
				config,
				`/api/v1/quotes/realtime?symbols=${encodeURIComponent(symbols.join(','))}`,
				{ signal: abort.signal },
			).then(payload => {
				if (abort.signal.aborted) return;
				const rows = (payload.data || [])
					.filter(item => typeof item.symbol === 'string' && item.symbol.length >= 6)
					.map(item => {
						// 后端格式 000552.SZ / 600519.SH：代码在前，取前 6 位。
						const digits = item.symbol!.match(/\d{6}/);
						return {
							symbol: digits ? digits[0] : item.symbol!.slice(0, 6),
							name: typeof item.name === 'string' ? item.name : '',
							price: Number(item.price) || 0,
							changePercent: Number(item.change_percent) || 0,
						};
					});
				setQuotes(rows);
			}).catch(() => {
				if (!abort.signal.aborted) setQuotes(null);
			});
		}, 120);
		return () => { abort.abort(); clearTimeout(timer); };
	}, [config, selectedBoard]);

	const quoteBySymbol = useMemo(() => new Map((quotes || []).map(quote => [quote.symbol, quote])), [quotes]);
	// 股名兜底：行情接口失败 / 只覆盖前 60 只时，用全库名录补齐，成分股不再是一堵代码墙。
	const nameMap = useStockNameMap(config);
	// 展示顺序：有行情时按涨跌幅降序（热股置顶），无行情保持库序。
	const displaySymbols = useMemo(() => {
		if (!selectedBoard) return [];
		const symbols = selectedBoard.symbols;
		if (!quotes || !quotes.length) return symbols;
		return [...symbols].sort((a, b) => (quoteBySymbol.get(b)?.changePercent ?? -Infinity) - (quoteBySymbol.get(a)?.changePercent ?? -Infinity));
	}, [selectedBoard, quotes, quoteBySymbol]);
	// 板块热度：只用 activeBoards 内、已被行情覆盖的成分股聚合（最多取前 60 只与行情请求一致）。
	const heatByBoard = useMemo(() => {
		const map = new Map<string, { avgChangePercent: number; upCount: number; downCount: number }>();
		if (!quotes || !quotes.length) return map;
		const covered = new Set(quotes.map(quote => quote.symbol));
		for (const board of activeBoards) {
			const sample = board.symbols.filter(symbol => covered.has(symbol));
			const heat = aggregateBoardHeat(sample.map(symbol => quoteBySymbol.get(symbol)!).filter(Boolean));
			if (heat) map.set(board.code, heat);
		}
		return map;
	}, [activeBoards, quotes, quoteBySymbol]);

	const lookupSymbol = useCallback(() => {
		const normalized = normalizeSymbol(symbolQuery);
		if (!normalized || !boards.length) { setSymbolMatches(null); return; }
		setSymbolMatches(boardsForSymbol(boards, normalized));
	}, [boards, symbolQuery]);

	if (!config) return <div className="board-explorer-empty"><Layers3 size={22} /><span>等待后端配置…</span></div>;

	return (
		<section className="board-explorer" aria-label="板块图谱">
			<header className="board-explorer-heading">
				<div>
					<h2>板块图谱</h2>
					<p>Free-StockDB 板块双向映射（概念 {groups.find(([name]) => name === '概念')?.[1].length ?? 0} · 申万 {groups.filter(([name]) => name.startsWith('申万')).reduce((total, [, items]) => total + items.length, 0)}）· 来源 /api/v1/stockdb/query?t=板块*</p>
				</div>
				<div className="board-symbol-lookup">
					<Search size={15} aria-hidden="true" />
					<input
						value={symbolQuery}
						onChange={(event) => setSymbolQuery(event.target.value)}
						onKeyDown={(event) => { if (event.key === 'Enter') lookupSymbol(); }}
						placeholder="输入 6 位代码反查所属板块"
						aria-label="按股票反查板块"
					/>
					<button type="button" onClick={lookupSymbol}>反查</button>
				</div>
				<button type="button" className="icon-button" onClick={load} disabled={refreshing} aria-label="刷新板块数据"><RefreshCw size={16} className={refreshing ? 'spin' : undefined} aria-hidden="true" /></button>
			</header>

			{symbolMatches && (
				<div className="board-symbol-result" role="status">
					<strong>{normalizeSymbol(symbolQuery) || '——'}</strong>
					{symbolMatches.length
						? symbolMatches.map(board => (
							<button type="button" key={board.code} onClick={() => { setCategory(board.category); setSelectedCode(board.code); setQuery(''); }}>
								{board.name}<small>{board.category}</small>
							</button>
						))
						: <span>未找到归属板块</span>}
				</div>
			)}

			{state === 'error' && <p className="load-notice">{error} <button type="button" onClick={load}>重试</button></p>}
			{state === 'loading' && !boards.length && (
				<div className="chart-placeholder"><LoaderCircle className="spin" size={20} /><span>加载板块索引（约 1MB，首次稍慢）…</span></div>
			)}

			{state === 'ready' && (
				<div className="board-explorer-layout">
					<aside className="board-rail" aria-label="板块类目">
						{groups.map(([name, items]) => (
							<button
								type="button"
								key={name}
								className={name === activeCategory ? 'active' : ''}
								onClick={() => { setCategory(name); setSelectedCode(''); }}
							>
								{name}<span>{items.length}</span>
							</button>
						))}
					</aside>
					<aside className="board-list" aria-label="板块列表">
						<label className="board-search">
							<Search size={14} aria-hidden="true" />
							<input value={query} onChange={(event) => { setQuery(event.target.value); setSelectedCode(''); }} placeholder={`在${activeCategory}中搜索名称或代码`} />
						</label>
						<div className="board-list-scroll">
							{activeBoards.map(board => {
								const heat = heatByBoard.get(board.code);
								return (
									<button
										type="button"
										key={board.code}
										className={selectedBoard?.code === board.code ? 'active' : ''}
										onClick={() => setSelectedCode(board.code)}
									>
										<strong>{board.name}</strong>
										<small>{board.code} · {board.symbols.length}只{heat ? ` · 均值 ${formatChangePercent(heat.avgChangePercent)}` : ''}</small>
										{heat && <span className={`board-heat ${heat.avgChangePercent >= 0 ? 'up' : 'down'}`}>{heat.upCount}/{heat.downCount}</span>}
									</button>
								);
							})}
							{!activeBoards.length && <p className="load-notice">没有匹配板块</p>}
						</div>
					</aside>
					<section className="board-detail" aria-label="板块成分">
						{selectedBoard ? (
							<>
								<header className="board-detail-heading">
									<div>
										<h3>{selectedBoard.name}</h3>
										<p>{selectedBoard.category} · {selectedBoard.code} · 来源 {selectedBoard.source || '--'} · {selectedBoard.group || '--'}</p>
									</div>
									<span className="board-count">{selectedBoard.symbols.length} 只成分</span>
								</header>
								<div className="board-symbols">
									{displaySymbols.map(symbol => {
										const quote = quoteBySymbol.get(symbol);
										const name = (quote && quote.name) || nameMap.get(symbol) || '';
										return (
											<button
												type="button"
												className="board-symbol"
												key={symbol}
												title={quote ? `${name || symbol} ${quote.price} ${formatChangePercent(quote.changePercent)}` : name ? `${name} ${symbol}` : symbol}
												onClick={(event) => { event.stopPropagation(); onOpenStock?.(symbol); }}
											>
												<span className="board-symbol-code">{symbol}</span>
												{name && <span className="board-symbol-name">{name}</span>}
												{quote && <span className={`board-symbol-change ${quote.changePercent >= 0 ? 'up' : 'down'}`}>{formatChangePercent(quote.changePercent)}</span>}
											</button>
										);
									})}
								</div>
							</>
						) : <p className="load-notice">从左侧选择板块查看成分股。</p>}
					</section>
				</div>
			)}
		</section>
	);
}
