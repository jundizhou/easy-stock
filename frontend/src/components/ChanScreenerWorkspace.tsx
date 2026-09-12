import {
	CircleAlert,
	Filter,
	ListChecks,
	LoaderCircle,
	RefreshCw,
	ScanSearch,
	Target,
	TrendingDown,
	TrendingUp,
	Layers3,
	Activity,
	Zap,
} from 'lucide-react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
	BackendConfig,
	ChanPyAnalyze,
	ChanPyScreenItem,
	ChanPyScreenResult,
	ChanPyStatus,
	HotStockRankData,
	requestJSON,
} from '../lib/backend';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
};

type PoolKind = 'custom' | 'watchlist' | 'hot';

type PoolStock = { symbol: string; name: string };

// 买卖点类型筛选项。value 与 chan.py 的 BSP_TYPE 对应。
const BS_TYPE_OPTIONS: Array<{ value: string; label: string; hint: string }> = [
	{ value: '1', label: '一类', hint: '趋势转折的第一类买卖点' },
	{ value: '1p', label: '类一类', hint: '盘整背驰形成的类一买卖点' },
	{ value: '2', label: '二类', hint: '回抽不新低的第二类买卖点' },
	{ value: '2s', label: '类二类', hint: '次级别回抽形成的类二买卖点' },
	{ value: '3a', label: '三类a', hint: '中枢离开后的三类买卖点' },
	{ value: '3b', label: '三类b', hint: '中枢震荡中的三类买卖点' },
];

const DEFAULT_BS_TYPES = ['1', '2', '3a', '3b'];

function describeError(error: unknown): string {
	if (error instanceof Error) return error.message;
	return String(error);
}

export function ChanScreenerWorkspace({ config, refreshKey }: Props) {
	const [status, setStatus] = useState<ChanPyStatus | null>(null);
	const [pool, setPool] = useState<PoolKind>('custom');
	const [customSymbols, setCustomSymbols] = useState('600519.SH\n000001.SZ\n300750.SZ');
	const [poolStocks, setPoolStocks] = useState<PoolStock[]>([]);
	const [poolState, setPoolState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
	const [poolError, setPoolError] = useState('');

	const [period, setPeriod] = useState('day');
	const [limit, setLimit] = useState(500);
	const [side, setSide] = useState<'buy' | 'sell' | 'any'>('buy');
	const [bsTypes, setBsTypes] = useState<string[]>(DEFAULT_BS_TYPES);
	const [zsState, setZsState] = useState<'' | 'above' | 'inside' | 'below'>('');
	const [biDirection, setBiDirection] = useState<'' | 'up' | 'down'>('');
	const [recentBars, setRecentBars] = useState(15);
	const [minScore, setMinScore] = useState(0);
	const [onlyMatched, setOnlyMatched] = useState(true);

	const [state, setState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
	const [error, setError] = useState('');
	const [result, setResult] = useState<ChanPyScreenResult | null>(null);
	const [detailSymbol, setDetailSymbol] = useState('');
	const [detail, setDetail] = useState<ChanPyAnalyze | null>(null);
	const [detailState, setDetailState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
	const [detailError, setDetailError] = useState('');

	// 服务探针：不可用时直接给原因，避免白等一次批量扫描。
	useEffect(() => {
		if (!config) return;
		let cancelled = false;
		void (async () => {
			try {
				const payload = await requestJSON<ChanPyStatus>(config, '/api/v1/stocks/chan-screen-status');
				if (!cancelled) setStatus(payload);
			} catch (probeError) {
				if (!cancelled) setStatus({ available: false, reason: describeError(probeError) });
			}
		})();
		return () => { cancelled = true; };
	}, [config]);

	// 股票池切换时加载对应候选（自选股来自日报配置，热门股来自热点榜聚合）。
	useEffect(() => {
		if (!config || pool === 'custom') {
			setPoolStocks([]);
			setPoolState(pool === 'custom' ? 'ready' : 'idle');
			return;
		}
		let cancelled = false;
		setPoolState('loading');
		setPoolError('');
		void (async () => {
			try {
				if (pool === 'watchlist') {
					const payload = await requestJSON<{ data: { watchlist: string[] } }>(config, '/api/v1/daily-analysis/config');
					const stocks = (payload.data?.watchlist || []).map((code) => ({ symbol: code, name: '' }));
					if (!cancelled) { setPoolStocks(stocks); setPoolState('ready'); }
					return;
				}
				const payload = await requestJSON<{ data: HotStockRankData }>(config, '/api/v1/stocks/hot-ranks');
				const stocks = (payload.data?.stocks || []).slice(0, 80).map((entry) => ({ symbol: entry.symbol, name: entry.name }));
				if (!cancelled) { setPoolStocks(stocks); setPoolState('ready'); }
			} catch (loadError) {
				if (!cancelled) {
					setPoolStocks([]);
					setPoolState('error');
					setPoolError(describeError(loadError));
				}
			}
		})();
		return () => { cancelled = true; };
	}, [config, pool, refreshKey]);

	const symbols = useMemo(() => {
		if (pool === 'custom') {
			return customSymbols
				.split(/[\s,，;；]+/)
				.map((item) => item.trim())
				.filter(Boolean);
		}
		return poolStocks.map((item) => item.symbol);
	}, [pool, customSymbols, poolStocks]);

	const nameBySymbol = useMemo(() => {
		const map: Record<string, string> = {};
		for (const item of poolStocks) {
			if (item.symbol && item.name) map[item.symbol] = item.name;
		}
		return map;
	}, [poolStocks]);

	const runScreen = useCallback(async () => {
		if (!config) return;
		if (symbols.length === 0) {
			setError('股票池为空，请先填写代码或选择股票池');
			setState('error');
			return;
		}
		setState('loading');
		setError('');
		setDetail(null);
		setDetailSymbol('');
		try {
			const payload = await requestJSON<{ data: ChanPyScreenResult }>(config, '/api/v1/stocks/chan-screen', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					symbols,
					names: Object.keys(nameBySymbol).length > 0 ? nameBySymbol : undefined,
					period,
					limit,
					filters: {
						side,
						bs_types: bsTypes,
						zs_state: zsState || undefined,
						bi_direction: biDirection || undefined,
						bsp_recent_bars: recentBars,
						min_score: minScore > 0 ? minScore : undefined,
					},
				}),
			});
			setResult(payload.data);
			setState('ready');
		} catch (requestError) {
			setError(describeError(requestError));
			setState('error');
		}
	}, [config, symbols, nameBySymbol, period, limit, side, bsTypes, zsState, biDirection, recentBars, minScore]);

	const openDetail = useCallback(async (symbol: string) => {
		if (!config) return;
		setDetailSymbol(symbol);
		setDetail(null);
		setDetailState('loading');
		setDetailError('');
		try {
			const payload = await requestJSON<{ data: ChanPyAnalyze }>(
				config,
				`/api/v1/stocks/chanpy-analysis?symbol=${encodeURIComponent(symbol)}&period=${period}&limit=${limit}`,
			);
			setDetail(payload.data);
			setDetailState('ready');
		} catch (detailRequestError) {
			setDetailError(describeError(detailRequestError));
			setDetailState('error');
		}
	}, [config, period, limit]);

	const rows = useMemo(() => {
		if (!result) return [];
		return onlyMatched ? result.results.filter((item) => item.matched) : result.results;
	}, [result, onlyMatched]);

	if (!status) {
		return <section className="cs-panel cs-empty"><LoaderCircle className="spin" size={20} /><strong>正在检查 chan.py 引擎</strong></section>;
	}

	if (!status.available) {
		return (
			<section className="cs-panel cs-empty">
				<CircleAlert size={20} />
				<strong>缠论选股引擎未就绪</strong>
				<span>{status.reason || '未找到 chan.py 运行环境'}</span>
				<small>需要在本机部署 chanpy-service（chan.py 引擎 + 服务脚本），并保证 Python ≥3.11</small>
			</section>
		);
	}

	return (
		<div className="cs-workspace">
			<header className="chan-toolbar">
				<div className="chan-toolbar-title">
					<span className="chan-toolbar-icon"><ScanSearch size={17} /></span>
					<div>
						<strong>缠论选股</strong>
						<small>chan.py 引擎 · 笔 / 中枢 / 一二三类买卖点形态筛选</small>
					</div>
				</div>
				<div className="chan-toolbar-controls">
					<label>周期
						<select value={period} onChange={(event) => setPeriod(event.target.value)}>
							<option value="day">日线</option>
							<option value="week">周线</option>
							<option value="60min">60分钟</option>
							<option value="30min">30分钟</option>
						</select>
					</label>
					<label>K线数
						<select value={String(limit)} onChange={(event) => setLimit(Number(event.target.value))}>
							<option value="300">300</option>
							<option value="500">500</option>
							<option value="800">800</option>
						</select>
					</label>
					<button type="button" className="chan-btn primary" onClick={() => void runScreen()} disabled={state === 'loading'}>
						<RefreshCw className={state === 'loading' ? 'spin' : ''} size={14} />
						{state === 'loading' ? `扫描中（${symbols.length} 只）` : `开始选股（${symbols.length} 只）`}
					</button>
				</div>
			</header>

			<section className="chan-panel">
				<header>
					<div><span>股票池</span><h3>选择要扫描的候选股票</h3></div>
					<ListChecks size={18} />
				</header>
				<div className="cs-pool">
					<div className="cs-pool-tabs">
						<button type="button" className={pool === 'custom' ? 'active' : ''} onClick={() => setPool('custom')}>自定义代码</button>
						<button type="button" className={pool === 'watchlist' ? 'active' : ''} onClick={() => setPool('watchlist')}>自选股（日报配置）</button>
						<button type="button" className={pool === 'hot' ? 'active' : ''} onClick={() => setPool('hot')}>热门股票榜</button>
						{pool !== 'custom' && <span className="cs-pool-count">{poolState === 'loading' ? '加载中…' : `${poolStocks.length} 只候选`}</span>}
					</div>
					{pool === 'custom' && (
						<textarea
							className="cs-pool-input"
							value={customSymbols}
							onChange={(event) => setCustomSymbols(event.target.value)}
							rows={3}
							placeholder={'每行一个代码，支持 600519 / 600519.SH / sh600519\n单次最多 200 只'}
						/>
					)}
					{pool !== 'custom' && poolState === 'error' && <div className="chan-inline-error">股票池加载失败：{poolError}</div>}
				</div>
			</section>

			<section className="chan-panel">
				<header>
					<div><span>选股条件</span><h3>形态学买卖点与结构位置过滤</h3></div>
					<Filter size={18} />
				</header>
				<div className="cs-conditions">
					<div className="cs-condition-group">
						<span className="cs-condition-label">方向</span>
						<div className="cs-segmented">
							<button type="button" className={side === 'buy' ? 'active' : ''} onClick={() => setSide('buy')}>只看买点</button>
							<button type="button" className={side === 'sell' ? 'active' : ''} onClick={() => setSide('sell')}>只看卖点</button>
							<button type="button" className={side === 'any' ? 'active' : ''} onClick={() => setSide('any')}>都看</button>
						</div>
					</div>
					<div className="cs-condition-group">
						<span className="cs-condition-label">买卖点类型</span>
						<div className="cs-bs-types">
							{BS_TYPE_OPTIONS.map((option) => (
								<label className="cs-bs-type" key={option.value} title={option.hint}>
									<input
										type="checkbox"
										checked={bsTypes.includes(option.value)}
										onChange={(event) => setBsTypes((current) => event.target.checked
											? [...current, option.value]
											: current.filter((value) => value !== option.value))}
									/>
									<span>{option.label}</span>
								</label>
							))}
						</div>
					</div>
					<div className="cs-condition-group">
						<span className="cs-condition-label">时效</span>
						<label className="cs-inline-select">买卖点距今
							<select value={String(recentBars)} onChange={(event) => setRecentBars(Number(event.target.value))}>
								<option value="5">5 根K线内</option>
								<option value="10">10 根K线内</option>
								<option value="15">15 根K线内</option>
								<option value="30">30 根K线内</option>
								<option value="60">60 根K线内</option>
							</select>
						</label>
					</div>
					<div className="cs-condition-group">
						<span className="cs-condition-label">结构</span>
						<label className="cs-inline-select">中枢位置
							<select value={zsState} onChange={(event) => setZsState(event.target.value as typeof zsState)}>
								<option value="">不限</option>
								<option value="above">中枢上方</option>
								<option value="inside">中枢内部</option>
								<option value="below">中枢下方</option>
							</select>
						</label>
						<label className="cs-inline-select">当前笔
							<select value={biDirection} onChange={(event) => setBiDirection(event.target.value as typeof biDirection)}>
								<option value="">不限</option>
								<option value="up">向上</option>
								<option value="down">向下</option>
							</select>
						</label>
					</div>
					<div className="cs-condition-group">
						<span className="cs-condition-label">评分</span>
						<label className="cs-inline-select cs-score-slider">
							最低 {minScore} 分
							<input type="range" min={0} max={90} step={5} value={minScore} onChange={(event) => setMinScore(Number(event.target.value))} />
						</label>
					</div>
				</div>
			</section>

			{state === 'loading' && (
				<div className="chan-loading">
					<LoaderCircle className="spin" size={26} />
					<div><strong>正在扫描 {symbols.length} 只股票</strong><span>逐只拉取K线并计算笔 / 中枢 / 买卖点，约需 10—60 秒</span></div>
				</div>
			)}

			{state === 'error' && (
				<div className="chan-error">
					<CircleAlert size={20} />
					<div><strong>选股未完成</strong><span>{error}</span></div>
					<button type="button" className="chan-btn ghost" onClick={() => void runScreen()}><RefreshCw size={14} />重试</button>
				</div>
			)}

			{state === 'ready' && result && (
				<>
					<section className="chan-panel">
						<header>
							<div><span>扫描结果</span><h3>
								命中 <em className="cs-hit-count">{result.matched}</em> / {result.scanned} 只
								{result.failed > 0 && <span className="cs-failed-count">（{result.failed} 只失败）</span>}
							</h3></div>
							<label className="chan-check"><input type="checkbox" checked={onlyMatched} onChange={(event) => setOnlyMatched(event.target.checked)} />只看命中</label>
						</header>
						<div className="cs-meta-row">
							<span>{result.period === 'day' ? '日线' : result.period} · {result.engine}</span>
							<span>耗时 {(result.elapsed_ms / 1000).toFixed(1)}s</span>
							{result.errors.length > 0 && <span title={result.errors.map((item) => `${item.symbol}: ${item.error}`).join('\n')}>{result.errors.length} 只失败（悬停查看）</span>}
						</div>
						{rows.length === 0
							? <div className="chan-empty">没有符合条件的股票，可放宽时效 / 评分 / 类型条件</div>
							: <div className="cs-table-wrap">
								<table className="cs-table">
									<thead>
										<tr>
											<th>代码 / 名称</th>
											<th>现价</th>
											<th>评分</th>
											<th>命中</th>
											<th>中枢位置</th>
											<th>当前笔</th>
											<th>命中原因</th>
										</tr>
									</thead>
									<tbody>
										{rows.map((item) => <ScreenRow item={item} key={item.symbol} active={item.symbol === detailSymbol} onOpen={() => void openDetail(item.symbol)} />)}
									</tbody>
								</table>
							</div>}
					</section>

					<DetailSection
						symbol={detailSymbol}
						state={detailState}
						error={detailError}
						detail={detail}
					/>
				</>
			)}
		</div>
	);
}

function ScreenRow({ item, active, onOpen }: { item: ChanPyScreenItem; active: boolean; onOpen: () => void }) {
	const tone = item.score >= 60 ? 'up' : item.score <= 40 ? 'down' : 'flat';
	return (
		<tr className={`cs-row ${item.matched ? 'matched' : ''} ${active ? 'active' : ''}`} onClick={onOpen}>
			<td>
				<div className="cs-cell-symbol"><strong>{item.symbol}</strong>{item.name && <span>{item.name}</span>}</div>
			</td>
			<td>{item.last_close.toFixed(2)}</td>
			<td>
				<div className={`cs-score ${tone}`}>
					<i><b style={{ width: `${item.score}%` }} /></i>
					<span>{item.score.toFixed(0)}</span>
				</div>
			</td>
			<td>
				{item.matched
					? <div className="cs-tags">{item.last_bsp?.labels.map((label) => <em className={`cs-tag ${item.last_bsp?.is_buy ? 'buy' : 'sell'}`} key={label}>{label}</em>)}</div>
					: <span className="cs-miss">—</span>}
			</td>
			<td><ZsStateBadge state={item.zs_state} /></td>
			<td>
				{item.bi_direction === 'up'
					? <span className="cs-direction up"><TrendingUp size={13} />向上</span>
					: item.bi_direction === 'down'
						? <span className="cs-direction down"><TrendingDown size={13} />向下</span>
						: <span className="cs-miss">—</span>}
			</td>
			<td className="cs-reason-cell">{item.match_reasons.join('；') || '—'}</td>
		</tr>
	);
}

function ZsStateBadge({ state }: { state: string }) {
	if (!state) return <span className="cs-miss">—</span>;
	const map: Record<string, string> = { above: '中枢上方', inside: '中枢内部', below: '中枢下方' };
	return <span className={`cs-zs-state ${state}`}>{map[state] || state}</span>;
}

function DetailSection({ symbol, state, error, detail }: {
	symbol: string;
	state: 'idle' | 'loading' | 'ready' | 'error';
	error: string;
	detail: ChanPyAnalyze | null;
}) {
	if (!symbol || state === 'idle') return null;
	if (state === 'loading') {
		return <div className="chan-loading"><LoaderCircle className="spin" size={22} /><div><strong>正在分析 {symbol}</strong><span>计算该股完整缠论结构…</span></div></div>;
	}
	if (state === 'error') {
		return <div className="chan-error"><CircleAlert size={18} /><div><strong>个股结构分析失败</strong><span>{error}</span></div></div>;
	}
	if (!detail) return null;
	const { structure, summary } = detail;
	return (
		<section className="chan-panel">
			<header>
				<div><span>个股结构</span><h3>{detail.name ? `${detail.name} · ` : ''}{detail.symbol} · chan.py 分析</h3></div>
				<Target size={18} />
			</header>
			<div className="cs-detail">
				<div className={`chan-verdict ${summary.tone}`}>
					<div className="chan-verdict-score">
						<span>结构评分</span>
						<strong>{summary.score.toFixed(1)}</strong>
						<em>{summary.stance}</em>
					</div>
					<div className="chan-verdict-body">
						<strong>{summary.conclusion || '结构信号不足'}</strong>
						<ul>{summary.reasons.map((reason) => <li key={reason}>{reason}</li>)}</ul>
					</div>
					<div className="chan-verdict-meta">
						<span>{detail.range.bars} 根K线</span>
						<small>{detail.range.start} ~ {detail.range.end}</small>
						<small>耗时 {detail.elapsed_ms} ms</small>
					</div>
				</div>

				<div className="cs-detail-grid">
					<section className="cs-detail-block">
						<header><Zap size={14} /><strong>买卖点（{structure.bsp.length}）</strong></header>
						{structure.bsp.length === 0
							? <div className="cs-detail-empty">区间内无买卖点</div>
							: <div className="cs-bsp-list">
								{[...structure.bsp].reverse().slice(0, 10).map((bsp, index) => (
									<article className={`cs-bsp-row ${bsp.is_buy ? 'buy' : 'sell'}`} key={`${bsp.time}-${index}`}>
										<div>
											{bsp.labels.map((label) => <em key={label}>{label}</em>)}
											{!bsp.is_sure && <i>待确认</i>}
										</div>
										<span>{bsp.time}</span>
										<strong>{bsp.price.toFixed(2)}</strong>
									</article>
								))}
							</div>}
					</section>

					<section className="cs-detail-block">
						<header><Activity size={14} /><strong>最近笔（{structure.bi_count}）</strong></header>
						<div className="cs-bi-list">
							{[...structure.bi].reverse().slice(0, 8).map((bi, index) => (
								<article className={`cs-bi-row ${bi.direction}`} key={`${bi.end_time}-${index}`}>
									<span>{bi.direction === 'up' ? '向上' : '向下'}{!bi.is_sure && <i>?</i>}</span>
									<small>{bi.start_time} → {bi.end_time}</small>
									<strong className={bi.change_percent >= 0 ? 'up' : 'down'}>
										{bi.change_percent >= 0 ? '+' : ''}{bi.change_percent.toFixed(1)}%
									</strong>
								</article>
							))}
						</div>
					</section>

					<section className="cs-detail-block">
						<header><Layers3 size={14} /><strong>中枢（{structure.zs_count}）</strong></header>
						{structure.zs_position && (
							<div className="cs-detail-zs-pos">
								当前价 {structure.last_close.toFixed(2)} 位于
								<strong className={structure.zs_position.state === 'above' ? 'up' : structure.zs_position.state === 'below' ? 'down' : ''}>
									{structure.zs_position.state === 'above' ? '中枢上方' : structure.zs_position.state === 'below' ? '中枢下方' : '中枢内部'}
								</strong>
							</div>
						)}
						{structure.zs.length === 0
							? <div className="cs-detail-empty">尚未形成中枢</div>
							: <div className="cs-zs-list">
								{[...structure.zs].reverse().slice(0, 5).map((zs, index) => (
									<article key={`${zs.begin}-${index}`}>
										<strong>{zs.zd.toFixed(2)} ~ {zs.zg.toFixed(2)}</strong>
										<small>{zs.begin} → {zs.end} · {zs.bi_count} 笔</small>
									</article>
								))}
							</div>}
					</section>
				</div>
			</div>
		</section>
	);
}
