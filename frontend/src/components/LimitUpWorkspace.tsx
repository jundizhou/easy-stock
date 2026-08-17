import {
	Activity,
	BadgeCent,
	ChevronDown,
	Database,
	Flame,
	GitBranch,
	History,
	Layers3,
	Landmark,
	LineChart,
	RefreshCw,
	ShieldAlert,
	TimerReset,
	TrendingUp,
	X,
} from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import {
	BackendConfig,
	KLine,
	LimitUpLadderData,
	LimitUpLadderDay,
	LimitUpLadderLevel,
	LimitUpLadderStock,
	MarketBillboardDetail,
	MarketBillboardItem,
	MarketBillboardSeat,
	MarketEmotionHistory,
	MarketEmotionIntraday,
	MarketEmotionPoint,
	requestJSON,
} from '../lib/backend';
import { KLineChart } from './KLineChart';
import { classifyBillboardSeat } from '../lib/billboard';
import { latestTradingDayKLines } from '../lib/kline';

type LoadState = 'idle' | 'loading' | 'ready' | 'error';
type BillboardState = 'idle' | 'loading' | 'ready' | 'empty' | 'error';

type KLinePeriodKey = 'intraday' | 'five-day' | 'day' | 'week' | 'month';

type KLinePeriod = {
	key: KLinePeriodKey;
	label: string;
	apiPeriod: string;
	limit: number;
	mode: 'intraday' | 'daily';
};

const KLINE_PERIODS: KLinePeriod[] = [
	{ key: 'intraday', label: '分时', apiPeriod: '1', limit: 240, mode: 'intraday' },
	{ key: 'five-day', label: '5日', apiPeriod: '5', limit: 240, mode: 'intraday' },
	{ key: 'day', label: '日K', apiPeriod: 'day', limit: 120, mode: 'daily' },
	{ key: 'week', label: '周K', apiPeriod: 'week', limit: 80, mode: 'daily' },
	{ key: 'month', label: '月K', apiPeriod: 'month', limit: 60, mode: 'daily' },
];

type Props = {
	config: BackendConfig | null;
	data: LimitUpLadderData | null;
	state: LoadState;
	error?: string;
	emotionData: MarketEmotionHistory | null;
	emotionState: LoadState;
	emotionError?: string;
	onRefresh: () => void;
};

export function LimitUpWorkspace({ config, data, state, error, emotionData, emotionState, emotionError, onRefresh }: Props) {
	const [showST, setShowST] = useState(false);
	const [previousExpanded, setPreviousExpanded] = useState(false);
	const [selectedStock, setSelectedStock] = useState<LimitUpLadderStock | null>(null);
	const [selectedKLines, setSelectedKLines] = useState<KLine[]>([]);
	const [selectedKLineState, setSelectedKLineState] = useState<LoadState>('idle');
	const [selectedKLineError, setSelectedKLineError] = useState('');
	const [selectedKLinePeriod, setSelectedKLinePeriod] = useState<KLinePeriodKey>('day');
	const [selectedBillboard, setSelectedBillboard] = useState<{ stock: LimitUpLadderStock; tradeDate: string } | null>(null);
	const [billboardItem, setBillboardItem] = useState<MarketBillboardItem | null>(null);
	const [billboardDetail, setBillboardDetail] = useState<MarketBillboardDetail | null>(null);
	const [billboardState, setBillboardState] = useState<BillboardState>('idle');
	const [billboardError, setBillboardError] = useState('');
	const selectedKLineCache = useRef(new Map<string, KLine[]>());
	const billboardCache = useRef(new Map<string, { item: MarketBillboardItem | null; detail: MarketBillboardDetail | null }>());
	const current = useMemo(() => summarizeVisibleDay(data?.current, showST), [data?.current, showST]);
	const previous = useMemo(() => summarizeVisibleDay(data?.previous, showST), [data?.previous, showST]);

	useEffect(() => {
		setSelectedKLinePeriod('day');
		selectedKLineCache.current.clear();
	}, [selectedStock?.symbol]);

	useEffect(() => {
		if (!selectedStock || !config) return;
		const period = KLINE_PERIODS.find((item) => item.key === selectedKLinePeriod) || KLINE_PERIODS[2];
		const cacheKey = `${selectedStock.symbol}:${period.key}`;
		const cached = selectedKLineCache.current.get(cacheKey);
		if (cached) {
			setSelectedKLines(cached);
			setSelectedKLineState('ready');
			setSelectedKLineError('');
			return;
		}
		let cancelled = false;
		setSelectedKLines([]);
		setSelectedKLineState('loading');
		setSelectedKLineError('');
		requestJSON<{ data: KLine[] }>(config, `/api/v1/quotes/kline?symbol=${encodeURIComponent(selectedStock.symbol)}&period=${encodeURIComponent(period.apiPeriod)}&limit=${period.limit}`)
			.then((payload) => {
				if (cancelled) return;
				const lines = period.key === 'intraday' ? latestTradingDayKLines(payload.data || []) : payload.data || [];
				selectedKLineCache.current.set(cacheKey, lines);
				setSelectedKLines(lines);
				setSelectedKLineState('ready');
			})
			.catch((error) => {
				if (cancelled) return;
				setSelectedKLineState('error');
				setSelectedKLineError(error instanceof Error ? error.message : `${period.label}数据加载失败`);
			});
		return () => { cancelled = true; };
	}, [config, selectedStock, selectedKLinePeriod]);

	useEffect(() => {
		if (!selectedBillboard || !config) return;
		const { stock, tradeDate } = selectedBillboard;
		const cacheKey = `${tradeDate}:${stock.symbol}`;
		const cached = billboardCache.current.get(cacheKey);
		if (cached) {
			setBillboardItem(cached.item);
			setBillboardDetail(cached.detail);
			setBillboardState(cached.item ? 'ready' : 'empty');
			setBillboardError('');
			return;
		}
		let cancelled = false;
		setBillboardItem(null);
		setBillboardDetail(null);
		setBillboardState('loading');
		setBillboardError('');
		const load = async () => {
			try {
				const listPayload = await requestJSON<{ data: MarketBillboardItem[] }>(config, `/api/v1/market/billboard?trade_date=${encodeURIComponent(tradeDate)}&limit=200`);
				if (cancelled) return;
				const item = (listPayload.data || []).find((candidate) => candidate.symbol === stock.symbol || normalizeSymbol(candidate.symbol) === normalizeSymbol(stock.symbol)) || null;
				if (!item) {
					billboardCache.current.set(cacheKey, { item: null, detail: null });
					setBillboardState('empty');
					return;
				}
				setBillboardItem(item);
				const params = new URLSearchParams({ symbol: item.symbol, trade_date: item.trade_date, reason: item.reason || '' });
				let detail: MarketBillboardDetail | null = null;
				try {
					const detailPayload = await requestJSON<{ data: MarketBillboardDetail }>(config, `/api/v1/market/billboard/detail?${params.toString()}`);
					detail = detailPayload.data || null;
				} catch (detailLoadError) {
					setBillboardError(detailLoadError instanceof Error ? detailLoadError.message : '买卖席位明细暂不可用');
				}
				if (cancelled) return;
				billboardCache.current.set(cacheKey, { item, detail });
				setBillboardDetail(detail);
				setBillboardState('ready');
			} catch (error) {
				if (cancelled) return;
				setBillboardState('error');
				setBillboardError(error instanceof Error ? error.message : '龙虎榜数据加载失败');
			}
		};
		void load();
		return () => { cancelled = true; };
	}, [config, selectedBillboard]);

	if (!data && state === 'loading') {
		return <div className="limit-up-loading"><RefreshCw className="spin" size={24} /><strong>载入短线连板结构</strong><span>正在读取开盘啦涨停池并补充东方财富历史梯队。</span></div>;
	}
	if (!data && state === 'error') {
		return <div className="limit-up-loading error"><ShieldAlert size={26} /><strong>连板数据暂不可用</strong><span>{error || '请刷新后重试东方财富涨停池。'}</span><button type="button" onClick={onRefresh}>重新加载</button></div>;
	}
	if (!data) {
		return null;
	}
	const conceptHeat = data.concept_heat || [];

	return (
		<section className="limit-up-workspace">
			<header className="limit-up-hero">
				<div>
					<div className="eyebrow"><Flame size={15} aria-hidden="true" />SHORT-TERM LIMIT-UP STRUCTURE</div>
					<h2>短线连板</h2>
					<p>{data.current.trade_date || '--'} · {data.session_status} · {current.sourceSummary} · 结构统计默认剔除 ST</p>
				</div>
				<div className="limit-up-actions">
					<label className="st-toggle"><input type="checkbox" checked={showST} onChange={(event) => setShowST(event.target.checked)} /><span>显示 ST</span></label>
					<button type="button" className="refresh-data-button" onClick={onRefresh} disabled={state === 'loading'}><RefreshCw className={state === 'loading' ? 'spin' : ''} size={15} />刷新梯队</button>
				</div>
			</header>

			<div className="limit-up-summary">
				<SummaryCard icon={<Flame size={17} />} label="涨停家数" value={current.limitUpCount} detail={stSummaryLabel(showST, current.stCount)} tone="hot" />
				<SummaryCard icon={<Layers3 size={17} />} label="连板家数" value={current.boardCount} detail={`首板 ${current.firstBoardCount} 只`} tone="blue" />
				<SummaryCard icon={<TrendingUp size={17} />} label="最高连板" value={`${current.maxStreak || 0}板`} detail={heightStructureLabel(current.maxStreak, current.boardCount)} tone="purple" />
				<SummaryCard icon={<TimerReset size={17} />} label="开板后回封" value={current.reopenedCount} detail="仅统计当前仍封板，非完整炸板率" tone="amber" />
				<SummaryCard icon={<BadgeCent size={17} />} label="封板成交额" value={formatMoney(current.totalAmount)} detail="当前可见涨停池合计" tone="green" />
			</div>

			<EmotionHistoryPanel data={emotionData} state={emotionState} error={emotionError} />

			<div className="limit-up-main-grid">
				<section className="limit-panel current-ladder-panel">
					<div className="limit-panel-heading">
						<div><span>今日结构</span><h3>连板梯队</h3></div>
						<small>按高度降序；同层优先无开板、早封板</small>
					</div>
					<LadderRows levels={current.levels} tradeDate={current.tradeDate} onSelectStock={setSelectedStock} onSelectBillboard={(stock) => setSelectedBillboard({ stock, tradeDate: current.tradeDate })} emptyText="当前涨停池没有可展示股票" />
				</section>

				<aside className="limit-up-side-stack">
					<section className="limit-panel advance-panel">
						<div className="limit-panel-heading"><div><span>昨日 → 今日</span><h3>封板晋级</h3></div><GitBranch size={18} /></div>
						<div className="advance-list">
							{data.advance.filter((item) => item.base > 0).map((item) => (
								<div className="advance-row" key={`${item.from_level}-${item.to_level}`}>
									<div><strong>{item.from_level}进{item.to_level}</strong><span>{item.success}/{item.base}</span></div>
									<i><b style={{ width: `${Math.round(item.rate * 100)}%` }} /></i>
									<em>{formatPercent(item.rate)}</em>
								</div>
							))}
							{!data.advance.some((item) => item.base > 0) && <p className="panel-empty">缺少可对照的昨日梯队。</p>}
						</div>
						<p className="metric-disclaimer">口径：昨日非ST封板股，今日继续封板且连板高度增加。盘中数据会随封板变化。</p>
					</section>

					<section className="limit-panel concept-panel">
						<div className="limit-panel-heading"><div><span>涨停与连板聚类</span><h3>炒作概念热度</h3></div><Activity size={18} /></div>
						<div className="concept-heat-list">
							{conceptHeat.slice(0, 10).map((item) => (
								<div key={item.name} title={item.leaders?.join(' · ')}>
									<span>{item.name}<small>{item.max_streak}板 / {item.count}只 / 连板{item.board_count}只</small></span>
									<i><b style={{ width: `${item.heat}%` }} /></i>
									<strong>{Math.round(item.heat)}</strong>
								</div>
							))}
							{!conceptHeat.length && <p className="panel-empty">概念目录暂不可用，个股仍按封板梯队展示。</p>}
						</div>
						<p className="metric-disclaimer">热度由涨停广度、连板数量、高度、昨日延续和概念宽泛度共同计算；宽泛标签会降权。</p>
					</section>
				</aside>
			</div>

			<section className={`limit-panel previous-ladder-panel ${previousExpanded ? 'expanded' : 'collapsed'}`}>
				<div className="limit-panel-heading previous-ladder-heading">
					<div><span>历史对照</span><h3>昨日连板梯队</h3></div>
					<div className="previous-ladder-controls">
						<div className="previous-summary"><History size={15} /><span>{previous.tradeDate || '暂无交易日'}</span><strong>{previous.limitUpCount}只涨停 · {previous.maxStreak || 0}板高度</strong><em>{previous.sourceSummary}</em></div>
						<button
							type="button"
							className="previous-ladder-toggle"
							aria-expanded={previousExpanded}
							aria-controls="previous-ladder-content"
							onClick={() => setPreviousExpanded((value) => !value)}
						>
							<span>{previousExpanded ? '收起' : '展开'}</span>
							<ChevronDown className={previousExpanded ? 'expanded' : ''} size={17} aria-hidden="true" />
						</button>
					</div>
				</div>
				{previousExpanded && <div id="previous-ladder-content"><LadderRows levels={previous.levels} tradeDate={previous.tradeDate} compact showCurrentChange onSelectStock={setSelectedStock} onSelectBillboard={(stock) => setSelectedBillboard({ stock, tradeDate: previous.tradeDate })} emptyText="暂无昨日梯队数据" /></div>}
			</section>

			<footer className="limit-up-note">
				<ShieldAlert size={14} />
				<span>当日及本地已留存交易日的涨停结构、连续板数和逐股炒作题材优先采用开盘啦，5 分钟内复用快照；东方财富只补充尚无开盘啦快照的历史交易日、缺失股票和行情字段。主炒题材仍会剔除本股后检验同题材梯队，并对宽泛标签降权。</span>
			</footer>
			{selectedStock && <KLineModal stock={selectedStock} lines={selectedKLines} state={selectedKLineState} error={selectedKLineError} periodKey={selectedKLinePeriod} onPeriodChange={setSelectedKLinePeriod} onClose={() => setSelectedStock(null)} />}
			{selectedBillboard && <BillboardModal stock={selectedBillboard.stock} tradeDate={selectedBillboard.tradeDate} item={billboardItem} detail={billboardDetail} state={billboardState} error={billboardError} onClose={() => setSelectedBillboard(null)} />}
		</section>
	);
}

function EmotionHistoryPanel({ data, state, error }: { data: MarketEmotionHistory | null; state: LoadState; error?: string }) {
	if (!data && state === 'loading') {
		return (
			<section className="limit-panel emotion-history-panel emotion-loading">
				<RefreshCw className="spin" size={19} />
				<div><strong>初始化市场情绪缓存</strong><span>首次读取最近7个交易日；完成后刷新页面只读取本地 SQLite。</span></div>
			</section>
		);
	}
	if (!data || !data.points.length) {
		return (
			<section className="limit-panel emotion-history-panel emotion-loading error">
				<ShieldAlert size={19} />
				<div><strong>情绪时间轴暂不可用</strong><span>{error || '等待首个本地日快照。'}</span></div>
			</section>
		);
	}
	const points = data.points.slice(-30);
	const latest = data.latest || points[points.length - 1];
	return (
		<section className="limit-panel emotion-history-panel">
			<div className="emotion-history-heading">
				<div>
					<span><LineChart size={15} />市场级日快照</span>
					<h3>每日情绪时间轴</h3>
					<p>交易时段展示盘中最新情绪，非交易时段展示最近交易日收盘后的情绪；历史分每日更新。</p>
				</div>
				<div className="emotion-cache-status" title={data.cache.last_error || '同一交易日不会重复请求外部行情'}>
					<Database size={15} />
					<span>本地缓存<strong>{data.cache.cached_days}日</strong></span>
					<small>历史同步 {formatTradeDate(data.cache.last_external_sync)}{data.intraday ? ` · 最新 ${formatDateTimeClock(data.intraday.updated_at)}` : ''}</small>
				</div>
			</div>
			<div className="emotion-history-body">
				<LatestEmotionCard data={data.intraday} error={data.intraday_error} />
				<EmotionLineChart points={points} />
				<div className="emotion-raw-grid">
					<EmotionRawMetric label="最终炸板率" value={formatRatio(latest.raw.final_break_rate)} />
					<EmotionRawMetric label="昨日涨停反馈" value={formatSignedPercent(latest.raw.previous_limit_up_return)} />
					<EmotionRawMetric label="昨日连板反馈" value={formatSignedPercent(latest.raw.previous_board_return)} />
					<EmotionRawMetric label="连板晋级率" value={formatRatio(latest.raw.advance_rate)} />
					<EmotionRawMetric label="主线集中度" value={formatRatio(latest.raw.theme_focus)} />
					<EmotionRawMetric label="收盘高位风险" value={`${latest.raw.high_risk_score.toFixed(0)} / 100`} />
				</div>
			</div>
			<p className="metric-disclaimer emotion-disclaimer">盘中结论重点观察昨日最高三个实际梯队的平均收益、下跌覆盖、重伤率、晋级失败和高度坍缩；严重负反馈会否决“高潮”。</p>
		</section>
	);
}

function LatestEmotionCard({ data, error }: { data?: MarketEmotionIntraday; error?: string }) {
	if (!data) {
		return (
			<div className="emotion-intraday-card unavailable">
				<span>最新情绪</span>
				<strong>暂不可用</strong>
				<small>{error || '等待最近交易日行情与高位梯队。'}</small>
			</div>
		);
	}
	const metrics = data.metrics;
	const tradingSnapshot = data.session_status === '盘中快照';
	const snapshotLabel = tradingSnapshot ? '交易时段实时快照' : '最近交易日收盘后快照';
	const confidenceLabel = data.stale ? '使用上次成功快照' : tradingSnapshot ? data.confidence : '收盘后确认';
	const levels = metrics.high_levels.length ? metrics.high_levels.map((level) => `${level}板`).join('、') : '暂无';
	return (
		<div className={`emotion-intraday-card risk-${intradayRiskTone(data.status)}`}>
			<div className="emotion-intraday-title"><span>最新情绪</span><strong>{data.risk_score.toFixed(0)}<small>风险</small></strong></div>
			<em>{data.status}</em>
			<b>{data.breadth}</b>
			<small>{data.trade_date} · {snapshotLabel} · 高位层 {levels}</small>
			<div className="emotion-intraday-metrics">
				<EmotionMiniMetric label="高位均值" value={metrics.high_average_return} suffix="%" signed />
				<EmotionMiniMetric label="下跌覆盖" value={metrics.high_down_rate * 100} suffix="%" />
				<EmotionMiniMetric label="高度坍缩" value={metrics.height_collapse} suffix="板" />
				<EmotionMiniMetric label="高位晋级" value={metrics.high_advance_rate * 100} suffix="%" />
			</div>
			<footer>{confidenceLabel} · 更新 {formatDateTimeClock(data.updated_at)} · 10分钟缓存</footer>
		</div>
	);
}

function EmotionMiniMetric({ label, value, suffix = '', signed = false }: { label: string; value: number; suffix?: string; signed?: boolean }) {
	const prefix = signed && value > 0 ? '+' : '';
	return <span><small>{label}</small><strong>{prefix}{value.toFixed(suffix === '%' && Math.abs(value) < 10 ? 1 : 0)}{suffix}</strong></span>;
}

function EmotionRawMetric({ label, value }: { label: string; value: string }) {
	return <div><span>{label}</span><strong>{value}</strong></div>;
}

function EmotionLineChart({ points }: { points: MarketEmotionPoint[] }) {
	const width = 760;
	const height = 226;
	const left = 38;
	const right = 16;
	const top = 18;
	const bottom = 34;
	const plotWidth = width - left - right;
	const plotHeight = height - top - bottom;
	const x = (index: number) => left + (points.length <= 1 ? plotWidth / 2 : (index / (points.length - 1)) * plotWidth);
	const y = (score: number) => top + (1 - Math.max(0, Math.min(100, score)) / 100) * plotHeight;
	const path = points.map((point, index) => `${index === 0 ? 'M' : 'L'} ${x(index).toFixed(2)} ${y(point.emotion_score).toFixed(2)}`).join(' ');
	const area = points.length ? `${path} L ${x(points.length - 1).toFixed(2)} ${top + plotHeight} L ${x(0).toFixed(2)} ${top + plotHeight} Z` : '';
	const grid = [0, 25, 50, 75, 100];
	const labels = points.length <= 7 ? points.map((_, index) => index) : [0, Math.floor((points.length - 1) / 2), points.length - 1];
	return (
		<div className="emotion-chart-wrap">
			<svg className="emotion-line-chart" viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`最近${points.length}个交易日市场情绪分折线图`}>
				<defs>
					<linearGradient id="emotionAreaFill" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stopColor="#2476d2" stopOpacity="0.24" /><stop offset="1" stopColor="#2476d2" stopOpacity="0.02" /></linearGradient>
				</defs>
				{grid.map((score) => <g key={score}><line x1={left} x2={width - right} y1={y(score)} y2={y(score)} /><text x={left - 8} y={y(score) + 4} textAnchor="end">{score}</text></g>)}
				{area && <path className="emotion-area" d={area} />}
				{path && <path className="emotion-line" d={path} />}
				{points.map((point, index) => (
					<g className={`emotion-point phase-${phaseTone(point.phase)}`} key={point.trade_date}>
						<circle cx={x(index)} cy={y(point.emotion_score)} r={index === points.length - 1 ? 5 : 3.5} />
						<title>{point.trade_date} · {point.emotion_score.toFixed(1)} · {point.phase}</title>
					</g>
				))}
				{labels.map((index) => <text className="emotion-date-label" x={x(index)} y={height - 8} textAnchor={index === 0 ? 'start' : index === points.length - 1 ? 'end' : 'middle'} key={points[index].trade_date}>{shortTradeDate(points[index].trade_date)}</text>)}
			</svg>
		</div>
	);
}

function phaseTone(phase: string) {
	if (phase === '高潮') return 'surge';
	if (phase === '发酵/主升') return 'strong';
	if (phase === '启动/修复') return 'repair';
	if (phase === '强分歧') return 'diverge';
	if (phase === '退潮') return 'weak';
	if (phase === '冰点') return 'freeze';
	return 'neutral';
}

function intradayRiskTone(status: string) {
	if (status === '高位退潮') return 'retreat';
	if (status === '强分歧' || status === '分歧') return 'diverge';
	if (status === '高位延续') return 'strong';
	return 'neutral';
}

function formatTradeDate(value?: string) {
	return value || '等待首次同步';
}

function formatDateTimeClock(value?: string) {
	if (!value) return '--:--';
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return '--:--';
	return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false });
}

function shortTradeDate(value: string) {
	return value.slice(5).replace('-', '/');
}

function formatRatio(value: number) {
	return `${(value * 100).toFixed(1)}%`;
}

function formatSignedPercent(value: number) {
	return `${value >= 0 ? '+' : ''}${value.toFixed(2)}%`;
}

function SummaryCard({ icon, label, value, detail, tone }: { icon: React.ReactNode; label: string; value: string | number; detail: string; tone: string }) {
	return <article className={`limit-summary-card ${tone}`}><div>{icon}<span>{label}</span></div><strong>{value}</strong><small>{detail}</small></article>;
}

function LadderRows({ levels, tradeDate, compact = false, showCurrentChange = false, onSelectStock, onSelectBillboard, emptyText }: { levels: LimitUpLadderLevel[]; tradeDate: string; compact?: boolean; showCurrentChange?: boolean; onSelectStock: (stock: LimitUpLadderStock) => void; onSelectBillboard: (stock: LimitUpLadderStock) => void; emptyText: string }) {
	const [collapsedLevels, setCollapsedLevels] = useState<Set<number>>(() => new Set([1]));

	if (!levels.length) {
		return <p className="panel-empty ladder-panel-empty">{emptyText}</p>;
	}
	return (
		<div className={`limit-ladder ${compact ? 'compact' : ''}`}>
			{levels.map((level) => (
				<details
					className={`limit-ladder-row level-${Math.min(level.level, 6)}`}
					open={!collapsedLevels.has(level.level)}
					onToggle={(event) => {
						const isOpen = event.currentTarget.open;
						setCollapsedLevels((current) => {
							if (isOpen === !current.has(level.level)) return current;
							const next = new Set(current);
							if (isOpen) next.delete(level.level);
							else next.add(level.level);
							return next;
						});
					}}
					key={level.level}
				>
					<summary className="limit-level-summary">
						<div className="limit-level-badge"><strong>{level.level}</strong><span>板</span><small>{level.stocks.length}只</small></div>
						<div className="limit-level-toggle-copy">
							<strong>{level.level === 1 ? '首板股票' : `${level.level}板梯队`}</strong>
							<span>{level.level === 1 ? '数量较多，点击展开全部股票' : '点击收拢本层股票'}</span>
						</div>
						<ChevronDown size={17} aria-hidden="true" />
					</summary>
									<div className="limit-stock-grid">
										{level.stocks.map((stock) => <LadderStockChip stock={stock} compact={compact} showCurrentChange={showCurrentChange} onSelect={() => onSelectStock(stock)} onSelectBillboard={() => onSelectBillboard(stock)} key={stock.symbol} />)}
					</div>
				</details>
			))}
		</div>
	);
}

function LadderStockChip({ stock, compact, showCurrentChange, onSelect, onSelectBillboard }: { stock: LimitUpLadderStock; compact: boolean; showCurrentChange: boolean; onSelect: () => void; onSelectBillboard: () => void }) {
	const primaryTheme = stock.primary_theme || stock.industry || '待归因';
	const secondary = stock.secondary_themes?.length ? stock.secondary_themes.join(' / ') : stock.industry || '暂无辅助题材';
	const tooltip = [
		`${stock.name} · 主炒：${primaryTheme}`,
		`数据源：${stock.source?.includes('duanxianxia') ? '开盘啦' : stock.source ? '东方财富补充' : '待确认'}`,
		stock.theme_source ? `题材口径：${stock.theme_source.includes('cross-day') ? '跨日开盘啦统一' : stock.theme_source.includes('duanxianxia') ? '开盘啦逐股题材' : '东方财富概念归因'}` : '',
		stock.raw_concepts?.length ? `原始概念：${stock.raw_concepts.join('、')}` : '',
		...(stock.theme_evidence || []),
	].filter(Boolean).join('\n');
	return (
		<article className={`limit-stock-chip clickable ${stock.open_count > 0 ? 'reopened' : ''} ${stock.is_st ? 'st' : ''}`} title={`${tooltip}\n点击查看多周期行情`} role="button" tabIndex={0} onClick={onSelect} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onSelect(); } }}>
			<div className="limit-stock-title">
				<strong>{stock.name}</strong>
				{showCurrentChange && stock.current_change_percent != null && (
					<em className={`current-change ${stock.current_change_percent > 0 ? 'up' : stock.current_change_percent < 0 ? 'down' : 'flat'}`}>
						当前 {formatSignedPercent(stock.current_change_percent)}
					</em>
				)}
				<span>{stock.limit_regime}</span>
			</div>
			<div className="limit-stock-theme"><span>主炒</span><strong>{primaryTheme}</strong>{stock.theme_confidence > 0 && <em>{Math.round(stock.theme_confidence * 100)}%</em>}</div>
			<div className="limit-stock-sub"><span>{stock.symbol}</span><em>{secondary}</em></div>
			{!compact ? <div className="limit-stock-meta"><span>{formatClock(stock.first_limit_time)}</span><span>{stock.board_type || (stock.open_count ? `开板${stock.open_count}次` : '封板未开')}</span><button type="button" className="limit-billboard-button" onClick={(event) => { event.stopPropagation(); onSelectBillboard(); }}>龙虎榜</button></div> : <button type="button" className="limit-billboard-button compact" onClick={(event) => { event.stopPropagation(); onSelectBillboard(); }}>龙虎榜</button>}
		</article>
	);
}

function BillboardModal({ stock, tradeDate, item, detail, state, error, onClose }: { stock: LimitUpLadderStock; tradeDate: string; item: MarketBillboardItem | null; detail: MarketBillboardDetail | null; state: BillboardState; error: string; onClose: () => void }) {
	useEffect(() => {
		const onKeyDown = (event: KeyboardEvent) => { if (event.key === 'Escape') onClose(); };
		window.addEventListener('keydown', onKeyDown);
		return () => window.removeEventListener('keydown', onKeyDown);
	}, [onClose]);
	const titleDate = item?.trade_date || tradeDate || '--';
	return (
		<div className="billboard-modal-overlay" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
			<section className="billboard-modal" role="dialog" aria-modal="true" aria-label={`${stock.name} 龙虎榜`}>
				<header className="billboard-modal-header">
					<div><span>连板梯队 · 龙虎榜</span><h2>{stock.name}<small>{stock.symbol}</small></h2><p>{titleDate} · {stock.streak}板 · {stock.primary_theme || stock.industry || '题材待归因'}</p></div>
					<button type="button" className="kline-modal-close" onClick={onClose} aria-label="关闭龙虎榜弹窗"><X size={19} /></button>
				</header>
				{state === 'loading' && <div className="billboard-modal-state"><RefreshCw className="spin" size={22} /><strong>正在读取龙虎榜</strong><span>查询 {titleDate} 的上榜记录与买卖席位…</span></div>}
				{state === 'empty' && <div className="billboard-modal-state empty"><Landmark size={26} /><strong>该交易日未上榜</strong><span>{stock.name} 在 {titleDate} 暂无龙虎榜记录。</span></div>}
				{state === 'error' && <div className="billboard-modal-state error"><ShieldAlert size={24} /><strong>龙虎榜数据暂不可用</strong><span>{error || '请稍后重试。'}</span></div>}
				{state === 'ready' && item && <>
					<div className="billboard-tag-row"><span className="billboard-tag primary">上榜</span><span className="billboard-tag">{item.reason || '上榜原因未提供'}</span>{item.institution_buyers > 0 && <span className="billboard-tag institution">机构参与</span>}<span className="billboard-tag">买方 {item.buy_seats}席</span><span className="billboard-tag">卖方 {item.sell_seats}席</span><span className={`billboard-tag ${item.net_amount >= 0 ? 'positive' : 'negative'}`}>{item.net_amount >= 0 ? '净买入' : '净卖出'} {formatMoney(Math.abs(item.net_amount))}</span></div>
					<div className="billboard-stat-grid"><BillboardStat label="收盘价" value={formatPrice(item.close_price)} /><BillboardStat label="涨跌幅" value={formatSignedPercent(item.change_percent)} tone={item.change_percent >= 0 ? 'up' : 'down'} /><BillboardStat label="换手率" value={`${item.turnover_rate.toFixed(2)}%`} /><BillboardStat label="买入金额" value={formatMoney(item.buy_amount)} /><BillboardStat label="卖出金额" value={formatMoney(item.sell_amount)} /><BillboardStat label="净买额" value={formatMoney(item.net_amount)} tone={item.net_amount >= 0 ? 'up' : 'down'} /><BillboardStat label="机构买方" value={`${item.institution_buyers}席`} /></div>
					{item.summary && <blockquote className="billboard-summary">{item.summary}</blockquote>}
					{!detail && <div className="billboard-detail-warning"><ShieldAlert size={15} /><span>买卖席位明细暂不可用，已展示榜单汇总。</span></div>}
					<div className="billboard-seat-grid"><BillboardSeatPanel title="买方五席" prefix="买" seats={detail?.buy_seats || []} /><BillboardSeatPanel title="卖方五席" prefix="卖" seats={detail?.sell_seats || []} /></div>
				</>}
				<footer className="billboard-modal-footer">数据源：龙虎榜行情接口 · 席位信息仅供研究参考。</footer>
			</section>
		</div>
	);
}

function BillboardStat({ label, value, tone }: { label: string; value: string; tone?: string }) {
	return <div className="billboard-stat"><span>{label}</span><strong className={tone || ''}>{value}</strong></div>;
}

function BillboardSeatPanel({ title, prefix, seats }: { title: string; prefix: '买' | '卖'; seats: MarketBillboardSeat[] }) {
	const seatByRank = new Map(seats.map((seat) => [seat.rank, seat]));
	return <section className={`billboard-seat-panel ${prefix === '买' ? 'buy' : 'sell'}`}><header><strong>{title}</strong><span>{prefix === '买' ? '按买入额排名' : '按卖出额排名'}</span></header><div className="billboard-seat-head"><span>排名 / 席位</span><span>买入</span><span>卖出</span><span>净额</span></div>{Array.from({ length: 5 }, (_, index) => { const rank = index + 1; const seat = seatByRank.get(rank); const label = seat ? classifyBillboardSeat(seat) : null; return <article key={rank}><span className="billboard-seat-name"><i>{prefix}{rank}</i><span><strong title={seat?.name || '数据源未提供'}>{seat?.name || '数据源未提供'}</strong>{label && <small className={`billboard-seat-label ${label.kind}`} title={label.note}>{label.label}</small>}</span></span><span>{seat ? formatMoney(seat.buy_amount) : '--'}</span><span>{seat ? formatMoney(seat.sell_amount) : '--'}</span><strong className={seat ? (seat.net_amount >= 0 ? 'up' : 'down') : ''}>{seat ? formatMoney(seat.net_amount) : '--'}</strong></article>; })}</section>;
}

function KLineModal({ stock, lines, state, error, periodKey, onPeriodChange, onClose }: { stock: LimitUpLadderStock; lines: KLine[]; state: LoadState; error: string; periodKey: KLinePeriodKey; onPeriodChange: (period: KLinePeriodKey) => void; onClose: () => void }) {
	useEffect(() => {
		const onKeyDown = (event: KeyboardEvent) => { if (event.key === 'Escape') onClose(); };
		window.addEventListener('keydown', onKeyDown);
		return () => window.removeEventListener('keydown', onKeyDown);
	}, [onClose]);
	const latest = lines.length ? [...lines].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime()).at(-1) : null;
	const activePeriod = KLINE_PERIODS.find((item) => item.key === periodKey) || KLINE_PERIODS[2];
	const periodRangeLabel = activePeriod.mode === 'intraday' ? `${activePeriod.label} · ${lines.length} 个数据点` : `${lines.length} 根${activePeriod.label}`;
	return (
		<div className="kline-modal-overlay" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
			<section className="kline-modal" role="dialog" aria-modal="true" aria-label={`${stock.name} K线`}>
				<header className="kline-modal-header">
					<div><span>连板梯队 · {activePeriod.label}</span><h2>{stock.name}<small>{stock.symbol}</small></h2><p>{stock.streak}板 · {stock.primary_theme || stock.industry || '题材待归因'}</p></div>
					<button type="button" className="kline-modal-close" onClick={onClose} aria-label="关闭 K 线弹窗"><X size={19} /></button>
				</header>
				<div className="kline-period-tabs" role="tablist" aria-label="K线周期">
					{KLINE_PERIODS.map((period) => <button key={period.key} type="button" role="tab" aria-selected={period.key === periodKey} className={period.key === periodKey ? 'active' : ''} onClick={() => onPeriodChange(period.key)}>{period.label}</button>)}
				</div>
				<div className="kline-modal-quote">
					<div><span>最新</span><strong>{latest?.close?.toFixed(2) || stock.price?.toFixed(2) || '--'}</strong></div>
					<div><span>涨跌幅</span><strong className={(latest?.change_percent ?? stock.change_percent) >= 0 ? 'up' : 'down'}>{formatSignedPercent(latest?.change_percent ?? stock.change_percent)}</strong></div>
					<div><span>开盘</span><strong>{latest?.open?.toFixed(2) || '--'}</strong></div>
					<div><span>最高</span><strong className="up">{latest?.high?.toFixed(2) || '--'}</strong></div>
					<div><span>最低</span><strong className="down">{latest?.low?.toFixed(2) || '--'}</strong></div>
					<div><span>成交量</span><strong>{latest ? formatMoney(latest.volume) : '--'}</strong></div>
					<div><span>成交额</span><strong>{latest?.amount ? formatMoney(latest.amount) : '--'}</strong></div>
					<div><span>换手率</span><strong>{latest?.turnover_rate != null ? `${latest.turnover_rate.toFixed(2)}%` : '--'}</strong></div>
				</div>
				<div className="kline-period-summary"><span>{periodRangeLabel}</span><span>前复权行情</span></div>
				{state === 'error' && <div className="kline-modal-error"><ShieldAlert size={17} /><span>{error || `${activePeriod.label}数据加载失败`}</span></div>}
				<KLineChart lines={lines} symbol={stock.symbol} state={state} mode={activePeriod.mode} periodLabel={activePeriod.label} />
				<footer className="kline-modal-footer">鼠标悬浮图表可查看行情细节；数据源：东方财富 / 新浪，结果仅供研究参考。</footer>
			</section>
		</div>
	);
}

function summarizeVisibleDay(day: LimitUpLadderDay | undefined, showST: boolean) {
	const levels = (day?.levels || [])
		.map((level) => ({ ...level, stocks: level.stocks.filter((stock) => showST || !stock.is_st) }))
		.filter((level) => level.stocks.length > 0);
	const stocks = levels.flatMap((level) => level.stocks);
	const kaipanlaCount = stocks.filter((stock) => stock.source?.includes('duanxianxia')).length;
	const fallbackCount = stocks.filter((stock) => stock.source && !stock.source.includes('duanxianxia')).length;
	return {
		tradeDate: day?.trade_date || '',
		levels,
		limitUpCount: stocks.length,
		boardCount: stocks.filter((stock) => stock.streak >= 2).length,
		firstBoardCount: stocks.filter((stock) => stock.streak <= 1).length,
		maxStreak: stocks.reduce((maximum, stock) => Math.max(maximum, stock.streak), 0),
		reopenedCount: stocks.filter((stock) => stock.open_count > 0).length,
		stCount: (day?.levels || []).flatMap((level) => level.stocks).filter((stock) => stock.is_st).length,
		totalAmount: stocks.reduce((total, stock) => total + stock.amount, 0),
		sourceSummary: ladderSourceSummary(kaipanlaCount, fallbackCount),
	};
}

function ladderSourceSummary(kaipanlaCount: number, fallbackCount: number) {
	if (kaipanlaCount > 0 && fallbackCount > 0) return `开盘啦 ${kaipanlaCount}只 · 东财补 ${fallbackCount}只`;
	if (kaipanlaCount > 0) return `开盘啦 ${kaipanlaCount}只`;
	if (fallbackCount > 0) return `东方财富历史兜底 ${fallbackCount}只`;
	return '数据源待确认';
}

function heightStructureLabel(maxStreak: number, boardCount: number) {
	if (maxStreak >= 6) return `高标突出，连板共 ${boardCount} 只`;
	if (maxStreak >= 4) return `高度活跃，连板共 ${boardCount} 只`;
	if (maxStreak >= 2) return `接力结构一般，连板共 ${boardCount} 只`;
	return '尚未形成连板结构';
}

function stSummaryLabel(showST: boolean, count: number) {
	if (count === 0) return '当前无 ST 涨停';
	return showST ? `含 ${count} 只 ST` : `另有 ${count} 只 ST 已隐藏`;
}

function formatClock(value?: string) {
	return value ? `首封 ${value.slice(0, 5)}` : '首封 --:--';
}

function formatPercent(value: number) {
	return `${Math.round(value * 100)}%`;
}

function formatMoney(value: number) {
	if (!Number.isFinite(value)) return '--';
	if (Math.abs(value) >= 100_000_000) return `${(value / 100_000_000).toFixed(1)}亿`;
	if (Math.abs(value) >= 10_000) return `${(value / 10_000).toFixed(0)}万`;
	return value.toFixed(0);
}

function formatPrice(value?: number) {
	if (value == null || !Number.isFinite(value)) return '--';
	return value.toFixed(2);
}

function normalizeSymbol(value: string) {
	return value.replace(/[^0-9]/g, '');
}
