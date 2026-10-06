import { useCallback, useEffect, useMemo, useState } from 'react';
import { Layers3, LoaderCircle, RefreshCw, Search } from 'lucide-react';
import type { BackendConfig } from '../lib/backend';
import {
	boardsForSymbol,
	fetchBoards,
	groupBoardsByCategory,
	normalizeSymbol,
	type StockDBBoard,
} from '../lib/sidecars';
import { logRuntimeEvent } from '../lib/runtime-log';

export function BoardExplorerWorkspace({ config, refreshKey }: { config: BackendConfig | null; refreshKey: number }) {
	const [boards, setBoards] = useState<StockDBBoard[]>([]);
	const [state, setState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
	const [error, setError] = useState('');
	const [category, setCategory] = useState('');
	const [query, setQuery] = useState('');
	const [selectedCode, setSelectedCode] = useState('');
	const [symbolQuery, setSymbolQuery] = useState('');
	const [symbolMatches, setSymbolMatches] = useState<StockDBBoard[] | null>(null);

	const load = useCallback(() => {
		if (!config) return;
		const abort = new AbortController();
		setState(previous => (previous === 'ready' ? previous : 'loading'));
		setError('');
		fetchBoards(config, { signal: abort.signal })
			.then(payload => {
				if (abort.signal.aborted) return;
				setBoards(payload);
				setState('ready');
			})
			.catch((reason) => {
				if (abort.signal.aborted) return;
				setError(reason instanceof Error ? reason.message : '板块数据加载失败');
				setState('error');
				logRuntimeEvent('warn', 'board-explorer', { event: 'boards_unavailable' });
			});
		return () => abort.abort();
	}, [config]);

	useEffect(() => { const cleanup = load(); return cleanup; }, [load, refreshKey]);

	const groups = useMemo(() => groupBoardsByCategory(boards), [boards]);
	const activeCategory = category || groups[0]?.[0] || '';
	const activeBoards = useMemo(() => {
		const bucket = groups.find(([name]) => name === activeCategory)?.[1] || [];
		const keyword = query.trim().toLowerCase();
		if (!keyword) return bucket;
		return bucket.filter(board => board.name.toLowerCase().includes(keyword) || board.code.toLowerCase().includes(keyword));
	}, [groups, activeCategory, query]);
	const selectedBoard = activeBoards.find(board => board.code === selectedCode) || activeBoards[0] || null;

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
				<button type="button" className="icon-button" onClick={load} aria-label="刷新板块数据"><RefreshCw size={16} aria-hidden="true" /></button>
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
							{activeBoards.map(board => (
								<button
									type="button"
									key={board.code}
									className={selectedBoard?.code === board.code ? 'active' : ''}
									onClick={() => setSelectedCode(board.code)}
								>
									<strong>{board.name}</strong>
									<small>{board.code} · {board.symbols.length}只</small>
								</button>
							))}
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
									{selectedBoard.symbols.map(symbol => <span className="board-symbol" key={symbol}>{symbol}</span>)}
								</div>
							</>
						) : <p className="load-notice">从左侧选择板块查看成分股。</p>}
					</section>
				</div>
			)}
		</section>
	);
}
