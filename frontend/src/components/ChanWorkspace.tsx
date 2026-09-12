import {
	Activity,
	BarChart3,
	CircleAlert,
	Flame,
	Info,
	Gauge,
	Layers3,
	LineChart,
	LoaderCircle,
	RefreshCw,
	Settings2,
	Sigma,
	TrendingDown,
	TrendingUp,
	Wallet,
	Zap,
} from 'lucide-react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
	BackendConfig,
	ChanAnalysis,
	ChanBacktest,
	ChanDivergence,
	ChanPivot,
	ChanSignal,
	ChanSignalMeta,
	ChanStatus,
	ChanStroke,
	requestJSON,
} from '../lib/backend';

type Props = {
	config: BackendConfig | null;
	// symbol 为 6 位代码（600519），组件内部按后端约定补全后缀。
	symbol: string;
	name?: string;
};

// 缠论默认的四个信号，与后端 analyze.py 的 DEFAULT_SIGNALS 保持一致。
// 用户可在界面上勾选替换，因此这里只作为初始值。
const DEFAULT_SIGNAL_CHOICES: Array<{ name: string; label: string; params: Record<string, unknown> }> = [
	{ name: 'cxt_bi_status_V230101', label: '笔表里关系', params: { di: 1 } },
	{ name: 'zdy_macd_bc_V230422', label: 'MACD面积背驰', params: { di: 1, th: 3 } },
	{ name: 'zdy_dif_V230527', label: 'DIF远离', params: { n: 10, t: 10 } },
	{ name: 'jcc_fen_shou_xian_V20221113', label: '分手线形态', params: { di: 1 } },
];

// 回测可选信号：挑几个语义清晰、适合做多空择时的形态信号。
const BACKTEST_CHOICES: Array<{ name: string; label: string; params: Record<string, unknown> }> = [
	{ name: 'cxt_bi_status_V230101', label: '笔表里关系（笔方向择时）', params: { di: 1 } },
	{ name: 'zdy_dif_V230527', label: 'DIF远离', params: { n: 10, t: 10 } },
	{ name: 'jcc_fen_shou_xian_V20221113', label: '分手线形态', params: { di: 1 } },
];

export function ChanWorkspace({ config, symbol, name }: Props) {
	const [status, setStatus] = useState<ChanStatus | null>(null);
	const [state, setState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
	const [error, setError] = useState('');
	const [analysis, setAnalysis] = useState<ChanAnalysis | null>(null);

	const [period, setPeriod] = useState('day');
	const [limit, setLimit] = useState(400);
	const [withChart, setWithChart] = useState(true);
	const [backtestSignal, setBacktestSignal] = useState('');
	const [selectedSignals, setSelectedSignals] = useState<string[]>(DEFAULT_SIGNAL_CHOICES.map((item) => item.name));
	const [showSignalConfig, setShowSignalConfig] = useState(false);
	const [catalog, setCatalog] = useState<ChanSignalMeta[]>([]);
	const [catalogState, setCatalogState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
	const [catalogFilter, setCatalogFilter] = useState('');

	// 探针：服务不可用时直接给出原因，避免用户白等一次 90s 的分析。
	useEffect(() => {
		if (!config) return;
		let cancelled = false;
		void (async () => {
			try {
				const payload = await requestJSON<ChanStatus>(config, '/api/v1/stocks/chan-status');
				if (!cancelled) setStatus(payload);
			} catch (probeError) {
				if (!cancelled) setStatus({ available: false, reason: describeError(probeError) });
			}
		})();
		return () => { cancelled = true; };
	}, [config]);

	const runAnalysis = useCallback(async () => {
		if (!config || !symbol) return;
		setState('loading');
		setError('');
		try {
			const signals = DEFAULT_SIGNAL_CHOICES.filter((item) => selectedSignals.includes(item.name))
				.map((item) => ({ name: item.name, label: item.label, params: item.params }));
			const payload = await requestJSON<{ data: ChanAnalysis }>(config, '/api/v1/stocks/chan-analysis', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					symbol,
					period,
					limit,
					chart: withChart,
					// 服务端只负责返回路径；图由下方的「打开缠论图」入口跳转。
					chart_dir: withChart ? CHART_OUT_DIR : '',
					signals: signals.length > 0 ? signals : undefined,
					backtest: backtestSignal || undefined,
					backtest_params: backtestSignal
						? BACKTEST_CHOICES.find((item) => item.name === backtestSignal)?.params
						: undefined,
				}),
			});
			setAnalysis(payload.data);
			setState('ready');
		} catch (requestError) {
			setError(describeError(requestError));
			setState('error');
		}
	}, [config, symbol, period, limit, withChart, backtestSignal, selectedSignals]);

	const loadCatalog = useCallback(async () => {
		if (!config || catalogState === 'loading') return;
		setCatalogState('loading');
		try {
			const payload = await requestJSON<{ count: number; signals: ChanSignalMeta[] }>(config, '/api/v1/stocks/chan-signals');
			setCatalog(payload.signals || []);
			setCatalogState('ready');
		} catch {
			setCatalogState('error');
		}
	}, [config, catalogState]);

	// 首次进入且服务可用时自动跑一次，避免用户面对空白面板。
	useEffect(() => {
		if (status?.available && symbol && state === 'idle') void runAnalysis();
	}, [status?.available, symbol, state, runAnalysis]);

	// 切换标的时重置，让 effect 重新触发分析。
	useEffect(() => {
		setAnalysis(null);
		setState('idle');
	}, [symbol]);

	const filteredCatalog = useMemo(() => {
		const needle = catalogFilter.trim().toLowerCase();
		if (!needle) return catalog;
		return catalog.filter((item) =>
			item.name.toLowerCase().includes(needle)
			|| (item.category || '').toLowerCase().includes(needle)
			|| (item.namespace || '').toLowerCase().includes(needle));
	}, [catalog, catalogFilter]);

	if (!symbol) {
		return <section className="chan-panel chan-panel-empty"><Info size={20} /><strong>先选择一只股票</strong><span>缠论结构分析需要标的代码</span></section>;
	}

	if (status && !status.available) {
		return (
			<section className="chan-panel chan-panel-empty">
				<CircleAlert size={20} />
				<strong>缠论分析服务未就绪</strong>
				<span>{status.reason || '未配置 czsc 运行环境'}</span>
				<small>需要在本机安装 czsc 并设置 A_STOCK_CZSC_PYTHON / A_STOCK_CZSC_SCRIPT</small>
			</section>
		);
	}

	return (
		<div className="chan-workspace">
			<header className="chan-toolbar">
				<div className="chan-toolbar-title">
					<span className="chan-toolbar-icon"><Sigma size={17} /></span>
					<div>
						<strong>缠论结构分析</strong>
						<small>{name ? `${name} · ` : ''}{symbol} · czsc 引擎</small>
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
							<option value="200">200</option>
							<option value="400">400</option>
							<option value="800">800</option>
							<option value="1200">1200</option>
						</select>
					</label>
					<label className="chan-check"><input type="checkbox" checked={withChart} onChange={(event) => setWithChart(event.target.checked)} />生成缠论图</label>
					<label>回测信号
						<select value={backtestSignal} onChange={(event) => setBacktestSignal(event.target.value)}>
							<option value="">不回测</option>
							{BACKTEST_CHOICES.map((item) => <option value={item.name} key={item.name}>{item.label}</option>)}
						</select>
					</label>
					<button type="button" className="chan-btn ghost" onClick={() => { setShowSignalConfig((value) => !value); if (!showSignalConfig && catalogState === 'idle') void loadCatalog(); }}>
						<Settings2 size={14} />信号配置
					</button>
					<button type="button" className="chan-btn primary" onClick={() => void runAnalysis()} disabled={state === 'loading'}>
						<RefreshCw className={state === 'loading' ? 'spin' : ''} size={14} />{state === 'loading' ? '分析中' : '重新分析'}
					</button>
				</div>
			</header>

			{showSignalConfig && (
				<div className="chan-signal-config">
					<header>
						<div><strong>选择要计算的缠论信号</strong><small>共 {catalogState === 'ready' ? catalog.length : '--'} 个可用信号，已选 {selectedSignals.length} 个</small></div>
						<input value={catalogFilter} onChange={(event) => setCatalogFilter(event.target.value)} placeholder="筛选名称 / 分类 / 命名空间" />
					</header>
					{catalogState === 'loading' && <div className="chan-inline-loading"><LoaderCircle className="spin" size={16} />正在读取信号目录</div>}
					{catalogState === 'error' && <div className="chan-inline-error">信号目录读取失败，仍可使用默认四个信号</div>}
					<div className="chan-signal-grid">
						{DEFAULT_SIGNAL_CHOICES.map((item) => (
							<label className="chan-signal-item default" key={item.name}>
								<input type="checkbox" checked={selectedSignals.includes(item.name)} onChange={(event) => {
									setSelectedSignals((current) => event.target.checked
										? [...current, item.name]
										: current.filter((value) => value !== item.name));
								}} />
								<span><strong>{item.label}</strong><small>{item.name}</small></span>
							</label>
						))}
						{catalogState === 'ready' && filteredCatalog
							.filter((item) => !DEFAULT_SIGNAL_CHOICES.some((preset) => preset.name === item.name))
							.slice(0, 60)
							.map((item) => (
								<label className="chan-signal-item" key={item.name}>
									<input type="checkbox" checked={selectedSignals.includes(item.name)} onChange={(event) => {
										setSelectedSignals((current) => event.target.checked
											? [...current, item.name]
											: current.filter((value) => value !== item.name));
									}} />
									<span><strong>{item.name}</strong><small>{[item.category, item.namespace, item.param_keys?.join('/')].filter(Boolean).join(' · ')}</small></span>
								</label>
							))}
					</div>
					<small className="chan-signal-note">提示：勾选自定义信号后会替换默认组合；带参数的信号将使用 czsc 的默认参数值。</small>
				</div>
			)}

			{state === 'loading' && (
				<div className="chan-loading">
					<LoaderCircle className="spin" size={26} />
					<div><strong>正在计算缠论结构</strong><span>分型 / 笔 / 中枢识别，信号计算{backtestSignal ? '与权重回测（较慢，约 5—15 秒）' : ''}…</span></div>
				</div>
			)}

			{state === 'error' && (
				<div className="chan-error">
					<CircleAlert size={20} />
					<div><strong>缠论分析未完成</strong><span>{error}</span></div>
					<button type="button" className="chan-btn ghost" onClick={() => void runAnalysis()}><RefreshCw size={14} />重试</button>
				</div>
			)}

			{state === 'ready' && analysis && <ChanResult analysis={analysis} name={name} />}
		</div>
	);
}

// 缠论图的服务端输出目录。放在 easy-stock-env 下，便于后续通过本地文件入口打开。
const CHART_OUT_DIR = 'K:/easy-stock-env/czsc-service/out';

function ChanResult({ analysis, name }: { analysis: ChanAnalysis; name?: string }) {
	const { structure, summary } = analysis;
	const { counts } = structure;
	const currentBi = structure.current_bi;
	const zsPosition = structure.zs_position;
	const latestDivergence = structure.divergence.at(-1);

	return (
		<div className="chan-result">
			<section className={`chan-verdict ${summary.tone}`}>
				<div className="chan-verdict-score">
					<span>缠论评分</span>
					<strong>{summary.score.toFixed(1)}</strong>
					<em>{summary.stance}</em>
				</div>
				<div className="chan-verdict-body">
					<strong>{summary.conclusion || '结构信号不足'}</strong>
					<ul>{summary.reasons.map((reason) => <li key={reason}>{reason}</li>)}</ul>
				</div>
				<div className="chan-verdict-meta">
					<span>{name ? `${name} · ` : ''}{analysis.symbol}</span>
					<small>{analysis.freq} · {analysis.range.start} ~ {analysis.range.end}</small>
					<small>{counts.bars} 根K线 · 计算 {analysis.elapsed_ms} ms</small>
					{analysis.source && <small>数据源 {analysis.source}</small>}
				</div>
			</section>

			<section className="chan-kpis">
				<ChanKPI icon={<Activity size={16} />} label="分型" value={String(counts.fx)} detail="顶/底分型总数" />
				<ChanKPI icon={<TrendingUp size={16} />} label="笔" value={String(counts.bi)} detail="已确认走势段" />
				<ChanKPI icon={<Layers3 size={16} />} label="中枢" value={String(counts.zs)} detail={zsPosition ? zsPosition.state : '尚未形成'} />
				<ChanKPI icon={<Gauge size={16} />} label="现价" value={structure.last_close.toFixed(2)} detail={currentBi ? `${currentBi.direction}笔推进 ${(currentBi.progress * 100).toFixed(0)}%` : '暂无进行中的笔'} />
				<ChanKPI icon={<Flame size={16} />} label="背驰" value={String(structure.divergence.length)} detail={latestDivergence ? `${latestDivergence.kind} ${latestDivergence.time}` : '区间内未识别到'} />
			</section>

			<div className="chan-grid">
				<section className="chan-panel">
					<header><div><span>结构概览</span><h3>当前笔与中枢位置</h3></div><LineChart size={18} /></header>
					<div className="chan-structure">
						{currentBi ? (
							<div className={`chan-current-bi ${currentBi.direction === '向上' ? 'up' : 'down'}`}>
								<div><span>当前笔</span><strong>{currentBi.direction}</strong>{!currentBi.sure && <em>未确认</em>}</div>
								<div className="chan-bi-track">
									<i style={{ width: `${Math.min(Math.max(currentBi.progress, 0), 1) * 100}%` }} />
								</div>
								<div className="chan-bi-ends">
									<span>{currentBi.start.time}<strong>{currentBi.start.price.toFixed(2)}</strong></span>
									<span>{currentBi.end.time}<strong>{currentBi.end.price.toFixed(2)}</strong></span>
								</div>
								<small>{currentBi.bars} 根K线 · 力度 {currentBi.power.toFixed(1)} · 斜率 {currentBi.slope.toFixed(2)}</small>
							</div>
						) : <div className="chan-empty">尚未形成有效笔</div>}
						{zsPosition && (
							<div className="chan-zs-position">
								<div><span>中枢位置</span><strong className={zsPosition.state === '中枢上方' ? 'up' : zsPosition.state === '中枢下方' ? 'down' : ''}>{zsPosition.state}</strong></div>
								<PivotBar pivot={zsPosition.zone} lastClose={structure.last_close} />
								<small>{zsPosition.note}</small>
							</div>
						)}
					</div>
				</section>

				<section className="chan-panel">
					<header><div><span>背驰信号</span><h3>力度衰竭检测</h3></div><TrendingDown size={18} /></header>
					{structure.divergence.length === 0
						? <div className="chan-empty">区间内未识别到背驰</div>
						: <div className="chan-divergence-list">
							{[...structure.divergence].reverse().map((item, index) => <DivergenceRow item={item} key={`${item.kind}-${item.time}-${index}`} />)}
						</div>}
				</section>
			</div>

			<section className="chan-panel">
				<header><div><span>缠论信号</span><h3>形态与择时信号取值</h3></div><Zap size={18} /></header>
				<div className="chan-signals">
					{analysis.signals.map((signal) => <SignalRow signal={signal} key={signal.name} />)}
				</div>
			</section>

			{analysis.backtest && <BacktestPanel backtest={analysis.backtest} />}

			<div className="chan-grid">
				<section className="chan-panel">
					<header><div><span>笔列表</span><h3>最近走势段</h3></div><BarChart3 size={18} /></header>
					<div className="chan-bi-table">
						<header><span>方向</span><span>区间</span><span>幅度</span><span>力度</span></header>
						{[...structure.bi].reverse().slice(0, 12).map((bi, index) => <BiRow bi={bi} key={`${bi.start.time}-${index}`} />)}
					</div>
				</section>

				<section className="chan-panel">
					<header><div><span>中枢列表</span><h3>价格震荡区间</h3></div><Layers3 size={18} /></header>
					{structure.zs.length === 0
						? <div className="chan-empty">尚未形成中枢</div>
						: <div className="chan-zs-list">{[...structure.zs].reverse().map((pivot) => <PivotRow pivot={pivot} key={`${pivot.start}-${pivot.zg}`} />)}</div>}
				</section>
			</div>

			{analysis.chart?.ok && analysis.chart.path && (
				<div className="chan-chart-note">
					<Info size={14} />
					<span>缠论图已生成（{(analysis.chart.size / 1024).toFixed(0)} KB），含分型 / 笔 / 中枢标注</span>
				</div>
			)}
			{analysis.chart && !analysis.chart.ok && (
				<div className="chan-chart-note error"><CircleAlert size={14} /><span>缠论图生成失败：{analysis.chart.error}</span></div>
			)}
		</div>
	);
}

function ChanKPI({ icon, label, value, detail }: { icon: React.ReactNode; label: string; value: string; detail: string }) {
	return <article className="chan-kpi"><div>{icon}<span>{label}</span></div><strong>{value}</strong><small>{detail}</small></article>;
}

function PivotBar({ pivot, lastClose }: { pivot: ChanPivot; lastClose: number }) {
	// 用中枢区间与现价一起构造比例尺，保证现价始终落在可视范围内。
	const low = Math.min(pivot.zd, lastClose);
	const high = Math.max(pivot.zg, lastClose);
	const span = Math.max(high - low, (high || 1) * 0.02);
	const position = (value: number) => `${Math.min(Math.max(((value - low) / span) * 100, 0), 100)}%`;
	return (
		<div className="chan-pivot-bar">
			<div className="chan-pivot-zone" style={{ left: position(pivot.zd), right: `calc(100% - ${position(pivot.zg)})` }} />
			<i className="chan-pivot-price" style={{ left: position(lastClose) }} title={`现价 ${lastClose.toFixed(2)}`} />
			<span className="chan-pivot-label low" style={{ left: position(pivot.zd) }}>{pivot.zd.toFixed(2)}</span>
			<span className="chan-pivot-label high" style={{ left: position(pivot.zg) }}>{pivot.zg.toFixed(2)}</span>
		</div>
	);
}

function DivergenceRow({ item }: { item: ChanDivergence }) {
	return (
		<article className={`chan-divergence ${item.kind === '顶背驰' ? 'up' : 'down'}`}>
			<div><strong>{item.kind}</strong><em>{item.time}</em></div>
			<span>{item.price.toFixed(2)}</span>
			<div className="chan-decay">
				<i><b style={{ width: `${Math.min(item.decay * 100, 100)}%` }} /></i>
				<small>力度衰减 {(item.decay * 100).toFixed(0)}%（{item.prev_slope.toFixed(2)} → {item.slope.toFixed(2)}）</small>
			</div>
		</article>
	);
}

function SignalRow({ signal }: { signal: ChanSignal }) {
	const bias = !signal.ok ? 'unknown' : signal.value?.startsWith('向上') ? 'up' : signal.value?.startsWith('向下') ? 'down' : 'flat';
	return (
		<article className={`chan-signal ${bias}`}>
			<div><strong>{signal.label}</strong><small>{signal.name}</small></div>
			<em>{signal.ok ? (signal.value || '--') : '调用失败'}</em>
			<small className="chan-signal-glossary">{signal.ok ? (signal.glossary || '无明确结构信号') : signal.error}</small>
		</article>
	);
}

function BacktestPanel({ backtest }: { backtest: ChanBacktest }) {
	if (!backtest.ok) {
		return (
			<section className="chan-panel">
				<header><div><span>权重回测</span><h3>信号历史绩效</h3></div><Wallet size={18} /></header>
				<div className="chan-empty">{backtest.error || '回测未产生有效结果'}</div>
			</section>
		);
	}
	// 指标键可能是中文（年化收益）也可能是英文（annual_return），统一做展示映射。
	const metrics: Array<[string, string, 'percent' | 'number' | 'ratio']> = [
		['total_return', '绝对收益', 'percent'],
		['annual_return', '年化收益', 'percent'],
		['max_drawdown', '最大回撤', 'percent'],
		['sharpe_ratio', '夏普比率', 'number'],
		['calmar_ratio', '卡玛比率', 'number'],
		['daily_win_rate', '日胜率', 'percent'],
		['trade_count', '交易次数', 'number'],
		['pnl_ratio', '单笔盈亏比', 'number'],
	];
	return (
		<section className="chan-panel chan-backtest">
			<header>
				<div><span>权重回测</span><h3>{backtest.signal} 历史绩效</h3></div>
				<Wallet size={18} />
			</header>
			<p className="chan-backtest-note">
				逐根推进取信号（point-in-time）：信号为「向上」满仓做多、「向下」满仓做空，含万二手续费。用于评估该形态信号的择时有效性，不代表未来收益。
			</p>
			<div className="chan-backtest-metrics">
				{metrics.map(([key, label, kind]) => {
					const value = pickMetric(backtest.stats, key, label);
					if (value === undefined) return null;
					return <article key={key}><span>{label}</span><strong className={metricTone(key, value)}>{formatMetric(value, kind)}</strong></article>;
				})}
			</div>
		</section>
	);
}

// pickMetric 依次尝试英文键与中文键，兼容不同 wbt 版本的字段命名。
function pickMetric(stats: Record<string, number | string>, ...keys: string[]): number | string | undefined {
	for (const key of keys) {
		if (stats[key] !== undefined) return stats[key];
	}
	return undefined;
}

function formatMetric(value: number | string, kind: 'percent' | 'number' | 'ratio'): string {
	if (typeof value === 'string') return value;
	if (kind === 'percent') return `${(value * 100).toFixed(2)}%`;
	if (kind === 'number' && Number.isInteger(value)) return String(value);
	return value.toFixed(2);
}

function metricTone(key: string, value: number | string): string {
	if (typeof value !== 'number') return '';
	if (key === 'max_drawdown') return value > 0.3 ? 'negative' : value > 0.15 ? 'warn' : 'positive';
	if (key === 'trade_count') return '';
	return value > 0 ? 'positive' : value < 0 ? 'negative' : '';
}

function BiRow({ bi }: { bi: ChanStroke }) {
	const change = bi.start.price ? ((bi.end.price - bi.start.price) / bi.start.price) * 100 : 0;
	return (
		<div className={`chan-bi-row ${bi.direction === '向上' ? 'up' : 'down'}`}>
			<span>{bi.direction}{!bi.is_sure && <em>?</em>}</span>
			<span>{bi.start.time} → {bi.end.time}</span>
			<span>{change >= 0 ? '+' : ''}{change.toFixed(2)}%</span>
			<span>{bi.power.toFixed(1)}</span>
		</div>
	);
}

function PivotRow({ pivot }: { pivot: ChanPivot }) {
	const amplitude = typeof pivot.amplitude === 'number' && pivot.amplitude ? pivot.amplitude : undefined;
	return (
		<article className="chan-zs-row">
			<div><strong>{pivot.zd.toFixed(2)} ~ {pivot.zg.toFixed(2)}</strong><em>{pivot.start} → {pivot.end}</em></div>
			<small>波动 {pivot.dd.toFixed(2)} ~ {pivot.gg.toFixed(2)}{amplitude ? ` · 振幅 ${amplitude.toFixed(2)}` : ''}</small>
		</article>
	);
}

function describeError(error: unknown): string {
	if (error instanceof Error) return error.message;
	return String(error);
}
