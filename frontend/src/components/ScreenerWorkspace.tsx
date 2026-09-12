import {
	CircleAlert,
	LoaderCircle,
	RefreshCw,
	Sparkles,
	Target,
	ListFilter,
	Zap,
} from 'lucide-react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
	BackendConfig,
	ScreenerHit,
	ScreenerResult,
	ScreenerStrategy,
	StockAIAnalysis,
	requestJSON,
} from '../lib/backend';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
	onOpenStockAnalysis: (analysis: StockAIAnalysis) => void;
};

const DEFAULT_SELECTED = ['vol_surge_up', 'macd_golden', 'ma_bull_align'];

function describeError(error: unknown): string {
	if (error instanceof Error) return error.message;
	return String(error);
}

/** 策略选股板块：内置策略目录 + 全市场两级筛选（快照条件 + K线指标）。 */
export function ScreenerWorkspace({ config, onOpenStockAnalysis }: Props) {
	const [catalog, setCatalog] = useState<ScreenerStrategy[]>([]);
	const [catalogState, setCatalogState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
	const [catalogError, setCatalogError] = useState('');
	const [selected, setSelected] = useState<string[]>(DEFAULT_SELECTED);

	const [excludeST, setExcludeST] = useState(true);
	const [excludeNew, setExcludeNew] = useState(true);
	const [minAmountYi, setMinAmountYi] = useState(1);
	const [universeLimit, setUniverseLimit] = useState(300);

	const [state, setState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
	const [error, setError] = useState('');
	const [result, setResult] = useState<ScreenerResult | null>(null);
	const [onlyMulti, setOnlyMulti] = useState(false);
	const [analyzingSymbol, setAnalyzingSymbol] = useState('');

	useEffect(() => {
		if (!config || catalogState !== 'idle') return;
		setCatalogState('loading');
		// StrictMode 双挂载下第一次 effect 的 cleanup 不可取消此请求：
		// 第二次挂载会被 loading 态早退，取消会让目录永远为空。
		void (async () => {
			try {
				const payload = await requestJSON<{ data: ScreenerStrategy[] }>(config, '/api/v1/screener/strategies');
				setCatalog(payload.data ?? []);
				setCatalogState('ready');
			} catch (loadError) {
				setCatalogState('error');
				setCatalogError(describeError(loadError));
			}
		})();
	}, [config, catalogState]);

	const categories = useMemo(() => {
		const map = new Map<string, ScreenerStrategy[]>();
		for (const strategy of catalog) {
			const list = map.get(strategy.category) ?? [];
			list.push(strategy);
			map.set(strategy.category, list);
		}
		return [...map.entries()];
	}, [catalog]);

	const run = useCallback(async () => {
		if (!config) return;
		if (selected.length === 0) {
			setError('至少选择一个策略');
			setState('error');
			return;
		}
		setState('loading');
		setError('');
		try {
			const payload = await requestJSON<{ data: ScreenerResult }>(config, '/api/v1/screener/run', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					strategy_ids: selected,
					options: { exclude_st: excludeST, exclude_new: excludeNew, min_amount_yi: minAmountYi, kline_universe_limit: universeLimit },
				}),
			});
			setResult(payload.data);
			setState('ready');
		} catch (runError) {
			setError(describeError(runError));
			setState('error');
		}
	}, [config, selected, excludeST, excludeNew, minAmountYi, universeLimit]);

	const rows = useMemo(() => {
		if (!result) return [];
		return onlyMulti ? result.hits.filter((hit) => hit.strategies.length >= 2) : result.hits;
	}, [result, onlyMulti]);

	const nameOf = useCallback((id: string) => catalog.find((strategy) => strategy.id === id)?.name ?? id, [catalog]);

	const openStockAnalysis = useCallback(async (symbol: string) => {
		if (!config || analyzingSymbol) return;
		setAnalyzingSymbol(symbol);
		try {
			const payload = await requestJSON<{ data: StockAIAnalysis }>(config, '/api/v1/stocks/ai-analysis', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ symbol }),
			});
			onOpenStockAnalysis(payload.data);
		} catch (analysisError) {
			setError(analysisError instanceof Error ? analysisError.message : '个股分析失败');
		} finally {
			setAnalyzingSymbol('');
		}
	}, [config, analyzingSymbol, onOpenStockAnalysis]);

	const toggle = (id: string) => {
		setSelected((current) => current.includes(id)
			? current.filter((item) => item !== id)
			: [...current, id]);
	};

	return (
		<div className="scr-workspace">
			<header className="chan-toolbar">
				<div className="chan-toolbar-title">
					<span className="chan-toolbar-icon"><ListFilter size={17} /></span>
					<div>
						<strong>策略选股</strong>
						<small>全市场两级筛选 · 快照条件毫秒级 + K线指标精选 · {catalog.length} 个内置策略</small>
					</div>
				</div>
				<div className="chan-toolbar-controls">
					<button type="button" className="chan-btn primary" onClick={() => void run()} disabled={state === 'loading'}>
						<RefreshCw className={state === 'loading' ? 'spin' : ''} size={14} />
						{state === 'loading' ? '扫描全市场…' : `运行选股（${selected.length} 策略）`}
					</button>
				</div>
			</header>

			{catalogState === 'loading' && <div className="chan-loading"><LoaderCircle className="spin" size={22} /><div><strong>正在加载策略目录</strong></div></div>}
			{catalogState === 'error' && (
				<div className="chan-error"><CircleAlert size={18} /><div><strong>策略目录加载失败</strong><span>{catalogError}</span></div></div>
			)}

			{catalogState === 'ready' && (
				<section className="chan-panel">
					<header>
						<div><span>策略目录</span><h3>勾选要运行的选股策略</h3></div>
						<ListFilter size={18} />
					</header>
					<div className="scr-catalog">
						{categories.map(([category, strategies]) => (
							<div className="scr-category" key={category}>
								<div className="scr-category-title">{category}</div>
								<div className="scr-strategy-grid">
									{strategies.map((strategy) => (
										<label className={`scr-strategy ${selected.includes(strategy.id) ? 'active' : ''}`} key={strategy.id} title={strategy.description}>
											<input
												type="checkbox"
												checked={selected.includes(strategy.id)}
												onChange={() => toggle(strategy.id)}
											/>
											<span>
												<strong>{strategy.name}{strategy.kind === 'kline' && <em className="scr-kind-tag">K线</em>}</strong>
												<small>{strategy.description}</small>
											</span>
										</label>
									))}
								</div>
							</div>
						))}
					</div>
					<div className="scr-options">
						<label className="chan-check"><input type="checkbox" checked={excludeST} onChange={(event) => setExcludeST(event.target.checked)} />排除 ST/退市</label>
						<label className="chan-check"><input type="checkbox" checked={excludeNew} onChange={(event) => setExcludeNew(event.target.checked)} />排除次新股（&lt;60 交易日）</label>
						<label className="scr-inline">最小成交额
							<select value={String(minAmountYi)} onChange={(event) => setMinAmountYi(Number(event.target.value))}>
								<option value="0">不限</option>
								<option value="0.5">5000 万</option>
								<option value="1">1 亿</option>
								<option value="2">2 亿</option>
								<option value="5">5 亿</option>
							</select>
						</label>
						<label className="scr-inline">K线池上限
							<select value={String(universeLimit)} onChange={(event) => setUniverseLimit(Number(event.target.value))}>
								<option value="150">150 只（最快）</option>
								<option value="300">300 只</option>
								<option value="600">600 只（最全）</option>
							</select>
						</label>
						<small className="scr-option-note">K线策略只对计算池逐只计算：快照策略命中股优先，不足部分按成交额补足</small>
					</div>
				</section>
			)}

			{state === 'loading' && (
				<div className="chan-loading">
					<LoaderCircle className="spin" size={26} />
					<div><strong>正在扫描全市场</strong><span>快照筛选 + 逐只K线指标计算，通常 15—60 秒</span></div>
				</div>
			)}

			{state === 'error' && (
				<div className="chan-error">
					<CircleAlert size={20} />
					<div><strong>选股未完成</strong><span>{error}</span></div>
					<button type="button" className="chan-btn ghost" onClick={() => void run()}><RefreshCw size={14} />重试</button>
				</div>
			)}

			{state === 'ready' && result && (
				<section className="chan-panel">
					<header>
						<div><span>选股结果</span>
							<h3>命中 <em className="scr-hit-count">{result.matched}</em> / {result.scanned} 只
								{result.kline_count > 0 && <span className="scr-kline-meta">（K线计算 {result.kline_count} 只 · {result.kline_failed} 失败 · 总耗时 {(result.elapsed_ms / 1000).toFixed(1)}s）</span>}
							</h3>
						</div>
						<label className="chan-check"><input type="checkbox" checked={onlyMulti} onChange={(event) => setOnlyMulti(event.target.checked)} />只看多策略共振</label>
					</header>
					{result.warnings?.map((warning) => <p className="scr-warning" key={warning}><CircleAlert size={13} /> {warning}</p>)}
					{rows.length === 0
						? <div className="chan-empty">没有命中的股票，可放宽条件或增加策略</div>
						: <div className="scr-table-wrap">
							<table className="scr-table">
								<thead>
									<tr>
										<th>#</th>
										<th>代码 / 名称</th>
										<th>现价</th>
										<th>涨幅</th>
										<th>换手</th>
										<th>量比</th>
										<th>流通市值</th>
										<th>命中策略与详情</th>
									</tr>
								</thead>
								<tbody>
									{rows.slice(0, 200).map((hit, index) => (
										<ScreenerRow hit={hit} index={index} nameOf={nameOf} key={hit.symbol} onOpen={() => void openStockAnalysis(hit.symbol)} />
									))}
								</tbody>
							</table>
							{rows.length > 200 && <p className="scr-footnote">仅展示前 200 条，共 {rows.length} 条命中</p>}
						</div>}
				</section>
			)}

			{state === 'idle' && (
				<section className="scr-intro">
					<Target size={20} />
					<div>
						<strong>选择策略后点击「运行选股」</strong>
						<span>快照策略（量价 / 资金 / 动量 / 规模）毫秒级过滤全市场；K线策略（均线 / MACD / RSI / KDJ / 布林 / 形态）对精选池逐只计算指标</span>
					</div>
					<Sparkles size={16} />
					<Zap size={16} />
				</section>
			)}
		</div>
	);
}

function ScreenerRow({ hit, index, nameOf, onOpen }: { hit: ScreenerHit; index: number; nameOf: (id: string) => string; onOpen: () => void }) {
	return (
		<tr className="scr-row" onClick={onOpen} title={`${hit.name}（${hit.symbol}）→ AI 个股分析`}>
			<td className="scr-index">{index + 1}</td>
			<td><div className="scr-cell-symbol"><strong>{hit.name || hit.symbol}</strong><small>{hit.symbol}</small></div></td>
			<td className="scr-num">{hit.close.toFixed(2)}</td>
			<td className={`scr-num ${hit.change_percent >= 0 ? 'up' : 'down'}`}>{hit.change_percent >= 0 ? '+' : ''}{hit.change_percent.toFixed(2)}%</td>
			<td className="scr-num">{hit.turnover_rate > 0 ? `${hit.turnover_rate.toFixed(1)}%` : '--'}</td>
			<td className="scr-num">{hit.volume_ratio > 0 ? hit.volume_ratio.toFixed(1) : '--'}</td>
			<td className="scr-num">{hit.float_cap_yi > 0 ? `${hit.float_cap_yi.toFixed(0)} 亿` : '--'}</td>
			<td>
				<div className="scr-tags">
					{hit.strategies.map((id) => (
						<em className="scr-tag" key={id} title={[hit.details?.[id], formatIndicators(hit.indicators)].filter(Boolean).join(' · ')}>
							{nameOf(id)}{hit.details?.[id] ? <i>{hit.details[id]}</i> : null}
						</em>
					))}
				</div>
			</td>
		</tr>
	);
}

function formatIndicators(indicators?: Record<string, number>): string {
	if (!indicators) return '';
	const pieces = Object.entries(indicators)
		.filter(([key]) => key.startsWith('ma') || key === 'rsi14' || key === 'dif' || key === 'dea')
		.slice(0, 4)
		.map(([key, value]) => `${key.toUpperCase()} ${value}`);
	return pieces.length > 0 ? pieces.join(' / ') : '';
}
