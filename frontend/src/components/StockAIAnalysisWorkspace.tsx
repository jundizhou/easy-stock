import {
	Activity,
	ArrowDownRight,
	ArrowUpRight,
	BarChart3,
	Bot,
	BrainCircuit,
	Calculator,
	CheckCircle2,
	CircleAlert,
	Clipboard,
	Clock3,
	Flag,
	Gauge,
	History,
	Layers3,
	LineChart,
	ListChecks,
	LoaderCircle,
	RefreshCw,
	Scale,
	Search,
	Settings,
	ShieldAlert,
	ShieldCheck,
	Sparkles,
	Target,
	Trash2,
	TrendingUp,
	WalletCards,
	Zap,
} from 'lucide-react';
import { FormEvent, KeyboardEvent, ReactNode, useCallback, useEffect, useMemo, useState } from 'react';
import {
	BackendConfig,
	StockAIAnalysis,
	StockAIDataQuality,
	StockAINextDayScenario,
	StockAITrendPoint,
	StockDirectoryData,
	StockDirectoryEntry,
	requestJSON,
} from '../lib/backend';
import {
	analysisTone,
	calculatePositionSizing,
	formatCompactAmount,
	resolveStockDirectorySymbol,
	searchStockDirectory,
	signedPercent,
} from '../lib/stock-analysis';

export type StockAIWorkspaceMode = 'analysis' | 'expectation' | 'risk';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
	mode: StockAIWorkspaceMode;
	onAskAI: (analysis: StockAIAnalysis) => void;
	onOpenSettings: () => void;
};

type LoadState = 'idle' | 'loading' | 'ready' | 'error';

type AnalysisHistoryItem = {
	symbol: string;
	name: string;
	generatedAt: string;
	overall: number;
	direction: string;
	profile: string;
	analysis: StockAIAnalysis;
};

const symbolStorageKey = 'easy-stock.stock-ai-symbol.v1';
const historyStorageKey = 'easy-stock.stock-ai-history.v2';
const capitalStorageKey = 'easy-stock.stock-ai-capital.v1';
const directoryStorageKey = 'easy-stock.stock-directory.v1';
const directoryStorageTTL = 24 * 60 * 60 * 1000;
const examples = ['600519', '300750', '002594', '601138', '688981'];

type DirectoryState = 'idle' | 'loading' | 'cached' | 'ready' | 'error';

export function StockAIAnalysisWorkspace({ config, refreshKey, mode, onAskAI, onOpenSettings }: Props) {
	const [query, setQuery] = useState(() => window.localStorage.getItem(symbolStorageKey) || '');
	const [analysis, setAnalysis] = useState<StockAIAnalysis | null>(null);
	const [history, setHistory] = useState<AnalysisHistoryItem[]>(loadAnalysisHistory);
	const [state, setState] = useState<LoadState>('idle');
	const [error, setError] = useState('');
	const [copied, setCopied] = useState(false);
	const [directory, setDirectory] = useState<StockDirectoryEntry[]>(loadCachedStockDirectory);
	const [directoryState, setDirectoryState] = useState<DirectoryState>(() => directory.length > 0 ? 'cached' : 'idle');

	const saveAnalysis = useCallback((item: StockAIAnalysis) => {
		setHistory((current) => {
			const next: AnalysisHistoryItem[] = [{
				symbol: item.symbol,
				name: item.name,
				generatedAt: item.generated_at,
				overall: item.scorecard.overall,
				direction: item.scorecard.direction,
				profile: item.profile.type_label,
				analysis: item,
			}, ...current.filter((entry) => entry.symbol !== item.symbol)].slice(0, 10);
			window.localStorage.setItem(historyStorageKey, JSON.stringify(next));
			return next;
		});
	}, []);

	const runAnalysis = useCallback(async (rawSymbol: string) => {
		const symbol = resolveStockDirectorySymbol(rawSymbol, directory);
		if (!symbol) {
			setError(directory.length > 0 ? '未找到唯一匹配的股票，请从搜索结果中选择' : '请输入6位股票代码，例如 600519');
			setState('error');
			return;
		}
		if (!config) {
			setError('后端尚未连接');
			setState('error');
			return;
		}
		setState('loading');
		setError('');
		setQuery(symbol);
		window.localStorage.setItem(symbolStorageKey, symbol);
		try {
			const payload = await requestJSON<{ data: StockAIAnalysis }>(config, '/api/v1/stocks/ai-analysis', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ symbol }),
			});
			setAnalysis(payload.data);
			saveAnalysis(payload.data);
			setState('ready');
		} catch (loadError) {
			setError(loadError instanceof Error ? loadError.message : '个股AI分析失败');
			setState('error');
		}
	}, [config, directory, saveAnalysis]);

	useEffect(() => {
		if (!config) return;
		let cancelled = false;
		setDirectoryState((current) => current === 'cached' ? 'cached' : 'loading');
		void requestJSON<{ data: StockDirectoryData }>(config, '/api/v1/stocks/directory')
			.then((payload) => {
				if (cancelled) return;
				const stocks = payload.data.stocks || [];
				setDirectory(stocks);
				setDirectoryState('ready');
				saveCachedStockDirectory(stocks);
			})
			.catch(() => {
				if (cancelled) return;
				setDirectoryState(directory.length > 0 ? 'cached' : 'error');
			});
		return () => { cancelled = true; };
	}, [config]);

	useEffect(() => {
		if (refreshKey <= 0 || !analysis?.symbol) return;
		void runAnalysis(analysis.symbol);
	}, [analysis?.symbol, refreshKey, runAnalysis]);

	const submit = (event: FormEvent) => {
		event.preventDefault();
		void runAnalysis(query);
	};

	const selectHistory = (item: AnalysisHistoryItem) => {
		setAnalysis(item.analysis);
		setQuery(item.symbol);
		setError('');
		setState('ready');
	};

	const removeHistory = (symbol: string) => {
		setHistory((current) => {
			const next = current.filter((item) => item.symbol !== symbol);
			window.localStorage.setItem(historyStorageKey, JSON.stringify(next));
			return next;
		});
	};

	const copyPlan = async () => {
		if (!analysis) return;
		await navigator.clipboard.writeText(buildPlanText(analysis));
		setCopied(true);
		window.setTimeout(() => setCopied(false), 1600);
	};

	return (
		<section className="stock-ai-workspace">
			<AnalysisSearch query={query} mode={mode} directory={directory} directoryState={directoryState} onQuery={setQuery} onSubmit={submit} loading={state === 'loading'} />
			{history.length > 0 && <AnalysisHistory items={history} activeSymbol={analysis?.symbol} onSelect={selectHistory} onRemove={removeHistory} />}

			{state === 'loading' && (
				<div className="stock-ai-loading">
					<LoaderCircle className="spin" size={30} />
					<div><strong>正在建立完整决策画像</strong><span>同步300日趋势、基准指数、涨停结构、题材、市场情绪，并生成多周期评分、隔日情景与风控参数。</span></div>
				</div>
			)}

			{state === 'error' && (
				<div className="stock-ai-error">
					<CircleAlert size={22} />
					<div><strong>分析没有完成</strong><span>{error}</span></div>
					<button type="button" onClick={() => void runAnalysis(query)}><RefreshCw size={14} />重试</button>
				</div>
			)}

			{state !== 'loading' && !analysis && state !== 'error' && <StockAIEmpty onSelect={(symbol) => void runAnalysis(symbol)} />}
			{state !== 'loading' && analysis && (
				<>
					<AnalysisVerdict analysis={analysis} copied={copied} onRefresh={() => void runAnalysis(analysis.symbol)} onCopy={() => void copyPlan()} onAskAI={() => onAskAI(analysis)} onOpenSettings={onOpenSettings} />
					{mode === 'analysis' && <FullAnalysisView analysis={analysis} />}
					{mode === 'expectation' && <ExpectationView analysis={analysis} />}
					{mode === 'risk' && <RiskExecutionView analysis={analysis} />}
				</>
			)}
		</section>
	);
}

function AnalysisSearch({ query, mode, directory, directoryState, onQuery, onSubmit, loading }: {
	query: string;
	mode: StockAIWorkspaceMode;
	directory: StockDirectoryEntry[];
	directoryState: DirectoryState;
	onQuery: (value: string) => void;
	onSubmit: (event: FormEvent) => void;
	loading: boolean;
}) {
	const [expanded, setExpanded] = useState(false);
	const [activeIndex, setActiveIndex] = useState(0);
	const suggestions = useMemo(() => searchStockDirectory(directory, query), [directory, query]);
	const resolvedSymbol = useMemo(() => resolveStockDirectorySymbol(query, directory), [directory, query]);
	const descriptions: Record<StockAIWorkspaceMode, string> = {
		analysis: '自动路由趋势容量、成长趋势、情绪短线与风险结构，形成多周期、相对强度和题材共振结论。',
		expectation: '将确定性预测改为可验证的隔日情景，覆盖高开承接、平开确认、低开修复与破位失效。',
		risk: '依据结构失效位、账户风险预算和仓位上限，反推可执行股数、止盈与纪律清单。',
	};
	const chooseSuggestion = (stock: StockDirectoryEntry) => {
		onQuery(stock.symbol);
		setExpanded(false);
		setActiveIndex(0);
	};
	const handleKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
		if (!expanded || suggestions.length === 0) return;
		if (event.key === 'ArrowDown') {
			event.preventDefault();
			setActiveIndex((current) => (current + 1) % suggestions.length);
		} else if (event.key === 'ArrowUp') {
			event.preventDefault();
			setActiveIndex((current) => (current - 1 + suggestions.length) % suggestions.length);
		} else if (event.key === 'Enter' && !resolvedSymbol) {
			event.preventDefault();
			chooseSuggestion(suggestions[activeIndex] || suggestions[0]);
		} else if (event.key === 'Escape') {
			setExpanded(false);
		}
	};
	const directoryHint = directoryState === 'loading' || directoryState === 'idle'
		? '正在缓存全市场股票目录…'
		: directoryState === 'error'
			? '目录暂不可用，可直接输入6位代码'
			: `${directoryState === 'cached' ? '本机缓存' : '已缓存'} ${directory.length.toLocaleString('zh-CN')} 只A股 · 名称/代码均可搜索`;
	return (
		<header className="stock-ai-search-hero">
			<div>
				<span><BrainCircuit size={15} />STOCK DECISION SYSTEM</span>
				<h2>个股 AI 分析</h2>
				<p>{descriptions[mode]}</p>
			</div>
			<form onSubmit={onSubmit}>
				<div className="stock-ai-search-control" onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setExpanded(false); }}>
					<label><Search size={17} /><input value={query} onChange={(event) => { onQuery(event.target.value); setExpanded(true); setActiveIndex(0); }} onFocus={() => setExpanded(true)} onKeyDown={handleKeyDown} placeholder="输入股票名称或代码，如 贵州茅台 / 600519" aria-label="股票名称或代码" aria-autocomplete="list" aria-controls="stock-ai-search-suggestions" aria-expanded={expanded && suggestions.length > 0} /></label>
					<small className={directoryState === 'error' ? 'error' : ''}>{directoryHint}</small>
					{expanded && query.trim() && suggestions.length > 0 && (
						<div className="stock-ai-search-suggestions" id="stock-ai-search-suggestions" role="listbox">
							{suggestions.map((stock, index) => (
								<button type="button" role="option" aria-selected={index === activeIndex} className={index === activeIndex ? 'active' : ''} onMouseDown={(event) => event.preventDefault()} onClick={() => chooseSuggestion(stock)} onMouseEnter={() => setActiveIndex(index)} key={stock.symbol}>
									<span><strong>{stock.name}</strong><small>{stockMarketLabel(stock.symbol)}</small></span>
									<em>{stock.code}</em>
								</button>
							))}
						</div>
					)}
				</div>
				<button type="submit" disabled={loading || !query.trim()}>{loading ? <LoaderCircle className="spin" size={16} /> : <Sparkles size={16} />}完整分析</button>
			</form>
		</header>
	);
}

function stockMarketLabel(symbol: string) {
	if (symbol.endsWith('.SH')) return '沪市';
	if (symbol.endsWith('.SZ')) return '深市';
	if (symbol.endsWith('.BJ')) return '北交所';
	return 'A股';
}

function loadCachedStockDirectory(): StockDirectoryEntry[] {
	try {
		const raw = window.localStorage.getItem(directoryStorageKey);
		if (!raw) return [];
		const cached = JSON.parse(raw) as { cachedAt?: number; stocks?: StockDirectoryEntry[] };
		if (!cached.cachedAt || Date.now() - cached.cachedAt > directoryStorageTTL || !Array.isArray(cached.stocks)) return [];
		return cached.stocks.filter((stock) => stock && typeof stock.symbol === 'string' && typeof stock.code === 'string' && typeof stock.name === 'string');
	} catch {
		return [];
	}
}

function saveCachedStockDirectory(stocks: StockDirectoryEntry[]) {
	try {
		window.localStorage.setItem(directoryStorageKey, JSON.stringify({ cachedAt: Date.now(), stocks }));
	} catch {
		// Search remains available from the in-memory directory when storage quota is unavailable.
	}
}

function AnalysisHistory({ items, activeSymbol, onSelect, onRemove }: {
	items: AnalysisHistoryItem[];
	activeSymbol?: string;
	onSelect: (item: AnalysisHistoryItem) => void;
	onRemove: (symbol: string) => void;
}) {
	return (
		<section className="stock-ai-history">
			<header><History size={15} /><strong>最近分析</strong><span>本机保存，可直接恢复结果</span></header>
			<div>
				{items.map((item) => (
					<button type="button" className={item.symbol === activeSymbol ? 'active' : ''} onClick={() => onSelect(item)} key={item.symbol}>
						<span><strong>{item.name}</strong><small>{item.symbol} · {item.profile}</small></span>
						<em>{item.overall}<small>{item.direction}</small></em>
						<i role="button" tabIndex={0} aria-label={`删除${item.name}历史`} onClick={(event) => { event.stopPropagation(); onRemove(item.symbol); }} onKeyDown={(event) => { if (event.key === 'Enter') { event.stopPropagation(); onRemove(item.symbol); } }}><Trash2 size={12} /></i>
					</button>
				))}
			</div>
		</section>
	);
}

function StockAIEmpty({ onSelect }: { onSelect: (symbol: string) => void }) {
	return (
		<div className="stock-ai-empty">
			<div className="stock-ai-empty-orbit"><BrainCircuit size={34} /></div>
			<h3>从一只股票开始建立完整交易预案</h3>
			<p>系统会同时评估趋势、动能、量价、题材、市场环境、基准超额收益与风险，不需要预先选择分析流派。</p>
			<div>{examples.map((symbol) => <button type="button" onClick={() => onSelect(symbol)} key={symbol}>{symbol}</button>)}</div>
		</div>
	);
}

function AnalysisVerdict({ analysis, copied, onRefresh, onCopy, onAskAI, onOpenSettings }: {
	analysis: StockAIAnalysis;
	copied: boolean;
	onRefresh: () => void;
	onCopy: () => void;
	onAskAI: () => void;
	onOpenSettings: () => void;
}) {
	const tone = analysisTone(analysis);
	return (
		<section className={`stock-ai-verdict ${tone}`}>
			<div className="stock-ai-identity">
				<span>{analysis.symbol}</span>
				<h2>{analysis.name}</h2>
				<div className="stock-ai-quote"><strong>{formatPrice(analysis.quote.price)}</strong><em className={analysis.quote.change_percent >= 0 ? 'up' : 'down'}>{signedPercent(analysis.quote.change_percent)}</em></div>
			</div>
			<div className="stock-ai-conclusion">
				<div className="stock-ai-tags"><span>{analysis.profile.type_label}</span><span>{analysis.profile.price_phase}</span><span>{analysis.profile.market_role}</span><span>{analysis.scorecard.direction} · {analysis.scorecard.grade}</span></div>
				<h3>{analysis.conclusion.headline}</h3>
				<p>{analysis.conclusion.summary}</p>
				<div className="stock-ai-ai-status">
					{analysis.ai.status === 'ready' ? <CheckCircle2 size={14} /> : <CircleAlert size={14} />}
					<span>{analysis.ai.message || (analysis.ai.status === 'ready' ? 'AI综合研判完成' : '本地规则研判')}</span>
					{analysis.ai.status === 'unavailable' && <button type="button" onClick={onOpenSettings}><Settings size={12} />配置模型</button>}
				</div>
			</div>
			<div className="stock-ai-verdict-actions">
				<button type="button" onClick={onRefresh}><RefreshCw size={14} />刷新</button>
				<button type="button" onClick={onCopy}><Clipboard size={14} />{copied ? '已复制' : '复制预案'}</button>
				<button type="button" className="primary" onClick={onAskAI}><Bot size={14} />继续推演</button>
			</div>
		</section>
	);
}

function FullAnalysisView({ analysis }: { analysis: StockAIAnalysis }) {
	const themeSource = stockThemeSourceLabel(analysis.theme.source);
	const themeRole = analysis.theme.role && analysis.theme.role !== '待确认' ? analysis.theme.role : '';
	const themeDetail = analysis.theme.is_hot
		? [themeRole, themeSource, analysis.theme.business ? `主业：${analysis.theme.business}` : ''].filter(Boolean).join(' · ')
		: [themeSource, '未发现明确热点炒作'].filter(Boolean).join(' · ');
	return (
		<>
			<section className="stock-ai-kpis stock-ai-kpis-complete">
				<KPICard icon={<Gauge size={17} />} label="综合评分" value={`${analysis.scorecard.overall} · ${analysis.scorecard.grade}`} detail={`${analysis.scorecard.direction} · 置信度${analysis.scorecard.conviction}`} tone="blue" />
				<KPICard icon={<TrendingUp size={17} />} label="趋势强度" value={`${analysis.trend.score}`} detail={`${analysis.trend.strength} · ${analysis.trend.setup}`} tone="green" />
				<KPICard icon={<Scale size={17} />} label="相对强度" value={analysis.relative_strength.available ? `${analysis.relative_strength.score}` : '--'} detail={analysis.relative_strength.available ? `${analysis.relative_strength.state} · ${analysis.relative_strength.benchmark_name}` : analysis.relative_strength.detail} tone="purple" />
				<KPICard icon={<ShieldCheck size={17} />} label="交易风险" value={`${analysis.risk_control.score} · ${analysis.risk_control.level}`} detail={`仓位${analysis.risk_control.suggested_position_min_percent}%—${analysis.risk_control.suggested_position_max_percent}%`} tone="amber" />
				<KPICard icon={<Zap size={17} />} label="短线状态" value={analysis.short_term.state} detail={`近20日 ${analysis.short_term.limit_up_count_20d} 次涨停 · ${analysis.short_term.tradability}`} tone="amber" />
				<KPICard icon={<Target size={17} />} label={analysis.theme.is_hot ? '热点定位' : '主业定位'} value={analysis.theme.primary || '独立结构'} detail={themeDetail} tone="purple" />
			</section>

			<div className="stock-ai-analysis-grid">
				<ScorecardPanel analysis={analysis} />
				<SignalMatrix analysis={analysis} />
			</div>

			<div className="stock-ai-main-grid">
				<section className="stock-ai-panel stock-ai-chart-panel">
					<header><div><span>趋势结构</span><h3>价格与 MA20 / MA60 / MA120</h3></div><LineChart size={19} /></header>
					<TrendChart points={analysis.chart} />
					<div className="stock-ai-trend-metrics">
						<Metric label="20日" value={signedPercent(analysis.trend.return_20d)} />
						<Metric label="60日" value={signedPercent(analysis.trend.return_60d)} />
						<Metric label="区间位置" value={`${analysis.trend.range_position_60d.toFixed(0)}%`} />
						<Metric label="量比 5/20" value={analysis.trend.volume_ratio_5d_20d.toFixed(2)} />
						<Metric label="ATR14" value={`${analysis.trend.atr_14_percent.toFixed(1)}%`} />
					</div>
					<div className="stock-ai-levels">
						<div><span>参考支撑</span><strong>{formatPrice(analysis.trend.support)}</strong></div>
						<div><span>阶段压力</span><strong>{formatPrice(analysis.trend.resistance)}</strong></div>
						<div><span>失效条件</span><strong>{analysis.trend.invalidation}</strong></div>
					</div>
				</section>

				<section className="stock-ai-panel stock-ai-timeframe-panel">
					<header><div><span>周期共振</span><h3>五周期一致性</h3></div><Layers3 size={19} /></header>
					<div className="stock-ai-timeframes">
						{analysis.timeframes.map((item) => <article className={scoreClass(item.score)} key={item.key}><div><strong>{item.label}</strong><em>{item.score}</em></div><span>{item.state}</span><div className="stock-ai-score-bar"><i style={{ width: `${item.score}%` }} /></div><small>{signedPercent(item.return_percent)} · 均线{item.above_moving_average ? '上方' : '下方'} · 斜率{signedPercent(item.slope_percent)}</small></article>)}
					</div>
					<RelativeStrengthCard analysis={analysis} />
				</section>
			</div>

			<div className="stock-ai-detail-grid stock-ai-detail-grid-complete">
				<ActionPlanPanel analysis={analysis} />
				<EvidencePanel analysis={analysis} />
				<RiskBoundaryPanel analysis={analysis} />
				<DataQualityPanel analysis={analysis} />
			</div>
		</>
	);
}

function ScorecardPanel({ analysis }: { analysis: StockAIAnalysis }) {
	return (
		<section className="stock-ai-panel stock-ai-scorecard-panel">
			<header><div><span>综合决策</span><h3>多维加权评分</h3></div><BarChart3 size={19} /></header>
			<div className="stock-ai-scorecard-body">
				<div className="stock-ai-score-ring" style={{ '--score': analysis.scorecard.overall } as React.CSSProperties}><div><strong>{analysis.scorecard.overall}</strong><span>{analysis.scorecard.grade} · {analysis.scorecard.direction}</span></div></div>
				<div className="stock-ai-dimensions">
					{analysis.scorecard.dimensions.map((item) => <div key={item.key}><span>{item.label}<small>{Math.round(item.weight * 100)}%</small></span><div><i style={{ width: `${item.score}%` }} /></div><strong>{item.score}</strong></div>)}
				</div>
			</div>
			<div className="stock-ai-score-evidence"><div className="positive"><strong><ArrowUpRight size={14} />支持证据</strong>{analysis.scorecard.positive_signals.map((item) => <span key={item}>{item}</span>)}</div><div className="negative"><strong><ArrowDownRight size={14} />反对证据</strong>{analysis.scorecard.negative_signals.map((item) => <span key={item}>{item}</span>)}</div></div>
		</section>
	);
}

function SignalMatrix({ analysis }: { analysis: StockAIAnalysis }) {
	return (
		<section className="stock-ai-panel stock-ai-signal-panel">
			<header><div><span>信号矩阵</span><h3>判断来源拆解</h3></div><Activity size={19} /></header>
			<div className="stock-ai-signals">{analysis.signals.map((signal) => <article className={signal.tone} key={signal.key}><div><strong>{signal.label}</strong><em>{signal.strength}</em></div><div className="stock-ai-score-bar"><i style={{ width: `${signal.strength}%` }} /></div><p>{signal.detail}</p></article>)}</div>
		</section>
	);
}

function RelativeStrengthCard({ analysis }: { analysis: StockAIAnalysis }) {
	const relative = analysis.relative_strength;
	return (
		<div className={`stock-ai-relative ${relative.available ? scoreClass(relative.score) : 'neutral'}`}>
			<header><Scale size={15} /><div><span>基准对照</span><strong>{relative.available ? `${relative.benchmark_name} · ${relative.state}` : '相对强度暂不可用'}</strong></div><em>{relative.available ? relative.score : '--'}</em></header>
			{relative.available && <div><Metric label="个股20日" value={signedPercent(relative.stock_return_20d)} /><Metric label="基准20日" value={signedPercent(relative.benchmark_return_20d)} /><Metric label="20日超额" value={signedPercent(relative.excess_return_20d)} /><Metric label="60日超额" value={signedPercent(relative.excess_return_60d)} /></div>}
			<small>{relative.detail}</small>
		</div>
	);
}

function ExpectationView({ analysis }: { analysis: StockAIAnalysis }) {
	const plan = analysis.next_day;
	return (
		<>
			<section className={`stock-ai-expectation-hero ${scoreClass(plan.score)}`}>
				<div><span>隔日基础预期</span><h2>{plan.bias}</h2><p>{plan.expectation}</p></div>
				<div className="stock-ai-expectation-score"><strong>{plan.score}</strong><span>情景评分</span></div>
				<div className="stock-ai-expected-range"><span>预估正常波动区间</span><strong>{formatPrice(plan.expected_low)} — {formatPrice(plan.expected_high)}</strong><small>基于ATR的观察区间，不是价格预测</small></div>
			</section>

			<section className="stock-ai-level-strip">{plan.levels.map((level) => <article key={level.label}><span>{level.label}</span><strong>{formatPrice(level.price)}</strong><small>{level.detail}</small></article>)}</section>

			<section className="stock-ai-scenario-grid">{plan.scenarios.map((scenario) => <ScenarioCard scenario={scenario} key={scenario.key} />)}</section>

			<section className="stock-ai-panel stock-ai-timeline-panel">
				<header><div><span>盘前到收盘</span><h3>隔日检查清单</h3></div><Clock3 size={19} /></header>
				<div className="stock-ai-timeline">
					<ChecklistStage icon={<Flag size={16} />} title="盘前" items={plan.pre_open_checks} />
					<ChecklistStage icon={<Zap size={16} />} title="开盘" items={plan.opening_checks} />
					<ChecklistStage icon={<Activity size={16} />} title="盘中" items={plan.intraday_checks} />
					<ChecklistStage icon={<CheckCircle2 size={16} />} title="收盘" items={plan.close_checks} />
				</div>
			</section>

			<div className="stock-ai-detail-grid">
				<ActionPlanPanel analysis={analysis} />
				<RiskBoundaryPanel analysis={analysis} />
				<DataQualityPanel analysis={analysis} />
			</div>
		</>
	);
}

function ScenarioCard({ scenario }: { scenario: StockAINextDayScenario }) {
	return (
		<article className={`stock-ai-scenario ${scenario.key}`}>
			<header><div><span>{scenario.priority}</span><h3>{scenario.name}</h3></div><Target size={18} /></header>
			<dl><div><dt>触发</dt><dd>{scenario.trigger}</dd></div><div><dt>确认</dt><dd>{scenario.confirmation}</dd></div><div><dt>动作</dt><dd>{scenario.action}</dd></div><div><dt>失效</dt><dd>{scenario.invalidation}</dd></div></dl>
		</article>
	);
}

function ChecklistStage({ icon, title, items }: { icon: ReactNode; title: string; items: string[] }) {
	return <article><header>{icon}<strong>{title}</strong></header><ol>{items.map((item) => <li key={item}>{item}</li>)}</ol></article>;
}

function RiskExecutionView({ analysis }: { analysis: StockAIAnalysis }) {
	const risk = analysis.risk_control;
	return (
		<>
			<section className={`stock-ai-risk-hero ${risk.level === '高' ? 'high' : risk.level === '较低' ? 'low' : 'medium'}`}>
				<div><ShieldCheck size={26} /><span><small>当前交易风险</small><strong>{risk.level} · {risk.score}分</strong></span></div>
				<div><small>建议仓位区间</small><strong>{risk.suggested_position_min_percent}% — {risk.suggested_position_max_percent}%</strong></div>
				<div><small>单笔账户风险</small><strong>≤ {risk.single_trade_risk_percent.toFixed(1)}%</strong></div>
				<div><small>结构盈亏比</small><strong>{risk.risk_reward.toFixed(2)} R</strong></div>
			</section>

			<div className="stock-ai-risk-grid">
				<PositionCalculator analysis={analysis} />
				<section className="stock-ai-panel stock-ai-price-ladder-panel">
					<header><div><span>价格边界</span><h3>执行价位表</h3></div><Scale size={19} /></header>
					<div className="stock-ai-price-ladder">
						<PriceLadderRow label="第二目标" value={risk.take_profit_second} tone="profit" detail="达到后根据趋势强度移动保护位" />
						<PriceLadderRow label="第一目标" value={risk.take_profit_first} tone="profit" detail="约1R位置，优先处理本金风险" />
						<PriceLadderRow label="计划买入" value={risk.entry_reference} tone="entry" detail="仅为仓位计算参考，不等同触发买点" />
						<PriceLadderRow label="计划止损" value={risk.stop_price} tone="stop" detail={`距参考价${risk.stop_percent.toFixed(1)}%，触发后停止解释`} />
					</div>
				</section>
			</div>

			<div className="stock-ai-risk-grid stock-ai-risk-grid-secondary">
				<section className="stock-ai-panel stock-ai-discipline-panel">
					<header><div><span>纪律清单</span><h3>风控执行规则</h3></div><ListChecks size={19} /></header>
					<ol>{risk.rules.map((rule, index) => <li key={rule}><span>{index + 1}</span><p>{rule}</p></li>)}</ol>
					<div><strong>仓位公式</strong><span>{risk.position_formula}</span></div>
				</section>
				<ActionPlanPanel analysis={analysis} />
			</div>

			<div className="stock-ai-detail-grid">
				<RiskBoundaryPanel analysis={analysis} />
				<EvidencePanel analysis={analysis} />
				<DataQualityPanel analysis={analysis} />
			</div>
		</>
	);
}

function PositionCalculator({ analysis }: { analysis: StockAIAnalysis }) {
	const risk = analysis.risk_control;
	const [capital, setCapital] = useState(() => Number(window.localStorage.getItem(capitalStorageKey)) || 200_000);
	const [riskPercent, setRiskPercent] = useState(risk.single_trade_risk_percent);
	const [entryPrice, setEntryPrice] = useState(risk.entry_reference);
	const [stopPrice, setStopPrice] = useState(risk.stop_price);
	const [maxPosition, setMaxPosition] = useState(risk.suggested_position_max_percent);

	useEffect(() => {
		setRiskPercent(risk.single_trade_risk_percent);
		setEntryPrice(risk.entry_reference);
		setStopPrice(risk.stop_price);
		setMaxPosition(risk.suggested_position_max_percent);
	}, [analysis.symbol, risk.entry_reference, risk.single_trade_risk_percent, risk.stop_price, risk.suggested_position_max_percent]);

	const result = useMemo(() => calculatePositionSizing({ accountCapital: capital, riskPercent, entryPrice, stopPrice, maxPositionPercent: maxPosition }), [capital, entryPrice, maxPosition, riskPercent, stopPrice]);
	const emptySizingReason = maxPosition <= 0
		? '当前建议不建仓'
		: capital * maxPosition / 100 < entryPrice * 100
			? '仓位上限不足1手'
			: capital * riskPercent / 100 < Math.max(entryPrice - stopPrice, 0) * 100
				? '风险预算不足1手'
				: '参数无效';
	const updateCapital = (value: number) => {
		setCapital(value);
		if (Number.isFinite(value) && value > 0) window.localStorage.setItem(capitalStorageKey, String(value));
	};
	return (
		<section className="stock-ai-panel stock-ai-calculator-panel">
			<header><div><span>账户风险预算</span><h3>动态仓位计算器</h3></div><Calculator size={19} /></header>
			<div className="stock-ai-calculator-inputs">
				<NumberField label="账户资金" value={capital} onChange={updateCapital} suffix="元" step={10000} />
				<NumberField label="单笔风险" value={riskPercent} onChange={setRiskPercent} suffix="%" step={0.1} />
				<NumberField label="计划买入价" value={entryPrice} onChange={setEntryPrice} suffix="元" step={0.01} />
				<NumberField label="计划止损价" value={stopPrice} onChange={setStopPrice} suffix="元" step={0.01} />
				<NumberField label="仓位上限" value={maxPosition} onChange={setMaxPosition} suffix="%" step={1} />
			</div>
			<div className="stock-ai-sizing-result">
				<div className="primary"><WalletCards size={20} /><span><small>建议股数</small><strong>{result.shares > 0 ? `${result.shares.toLocaleString('zh-CN')} 股` : emptySizingReason}</strong></span></div>
				<div><small>计划市值</small><strong>{formatMoney(result.positionValue)}</strong></div>
				<div><small>实际仓位</small><strong>{result.positionPercent.toFixed(1)}%</strong></div>
				<div><small>止损亏损</small><strong>{formatMoney(result.maxLoss)}</strong></div>
				<div><small>允许亏损</small><strong>{formatMoney(result.allowedLoss)}</strong></div>
			</div>
			<p>计算结果同时受“账户允许亏损”和“仓位上限”约束，并按A股100股整数手向下取整。</p>
		</section>
	);
}

function NumberField({ label, value, onChange, suffix, step }: { label: string; value: number; onChange: (value: number) => void; suffix: string; step: number }) {
	return <label><span>{label}</span><div><input type="number" min="0" step={step} value={Number.isFinite(value) ? value : 0} onChange={(event) => onChange(Number(event.target.value))} /><em>{suffix}</em></div></label>;
}

function PriceLadderRow({ label, value, tone, detail }: { label: string; value: number; tone: string; detail: string }) {
	return <div className={tone}><span>{label}</span><strong>{formatPrice(value)}</strong><small>{detail}</small></div>;
}

function ActionPlanPanel({ analysis }: { analysis: StockAIAnalysis }) {
	return (
		<section className="stock-ai-panel stock-ai-plan-panel">
			<header><div><span>条件化决策</span><h3>{analysis.action_plan.current_action}</h3></div><Target size={19} /></header>
			<PlanGroup tone="entry" title="允许介入" items={analysis.action_plan.entry_conditions} />
			<PlanGroup tone="hold" title="持有条件" items={analysis.action_plan.hold_conditions} />
			<PlanGroup tone="avoid" title="禁止条件" items={analysis.action_plan.avoid_conditions} />
			<div className="stock-ai-position"><Activity size={15} /><div><span>仓位约束</span><strong>{analysis.action_plan.position_hint}</strong></div></div>
		</section>
	);
}

function EvidencePanel({ analysis }: { analysis: StockAIAnalysis }) {
	return (
		<section className="stock-ai-panel">
			<header><div><span>分析证据</span><h3>事实链</h3></div><BarChart3 size={18} /></header>
			<div className="stock-ai-evidence-list">{analysis.evidence.map((item, index) => <article key={`${item.category}-${index}`}><em>{item.category}</em><div><strong>{item.title}</strong><p>{item.detail || '当前仅有结构化指标'}</p><small>{item.source}{item.as_of ? ` · ${item.as_of}` : ''}</small></div></article>)}</div>
		</section>
	);
}

function RiskBoundaryPanel({ analysis }: { analysis: StockAIAnalysis }) {
	return (
		<section className="stock-ai-panel">
			<header><div><span>风险边界</span><h3>不能忽略的条件</h3></div><ShieldAlert size={18} /></header>
			<ul className="stock-ai-risk-list">{analysis.risks.map((risk) => <li key={risk}>{risk}</li>)}</ul>
			<div className="stock-ai-best-path"><span>最优验证路径</span><strong>{analysis.conclusion.best_path}</strong><small>主要风险：{analysis.conclusion.main_risk}</small></div>
		</section>
	);
}

function DataQualityPanel({ analysis }: { analysis: StockAIAnalysis }) {
	return (
		<section className="stock-ai-panel">
			<header><div><span>数据质量</span><h3>本次分析覆盖</h3></div><CheckCircle2 size={18} /></header>
			<div className="stock-ai-quality-list">{analysis.data_quality.map((item) => <QualityRow item={item} key={`${item.key}-${item.message}`} />)}</div>
		</section>
	);
}

function KPICard({ icon, label, value, detail, tone }: { icon: ReactNode; label: string; value: string; detail: string; tone: string }) {
	return <article className={`stock-ai-kpi ${tone}`}><div>{icon}<span>{label}</span></div><strong>{value}</strong><small>{detail}</small></article>;
}

function Metric({ label, value }: { label: string; value: string }) {
	return <div><span>{label}</span><strong>{value}</strong></div>;
}

function PlanGroup({ tone, title, items }: { tone: string; title: string; items: string[] }) {
	return <div className={`stock-ai-plan-group ${tone}`}><strong>{title}</strong><ul>{items.map((item) => <li key={item}>{item}</li>)}</ul></div>;
}

function QualityRow({ item }: { item: StockAIDataQuality }) {
	return <div className={item.status}><span>{item.status === 'ready' ? <CheckCircle2 size={14} /> : <CircleAlert size={14} />}</span><div><strong>{qualityLabel(item.key)}</strong><small>{item.message}</small></div><em>{item.status === 'ready' ? '完整' : item.status === 'missing' ? '缺失' : '受限'}</em></div>;
}

function TrendChart({ points }: { points: StockAITrendPoint[] }) {
	const geometry = useMemo(() => buildChartGeometry(points), [points]);
	if (!geometry) return <div className="stock-ai-chart-empty">趋势数据不足</div>;
	return (
		<div className="stock-ai-chart-wrap">
			<svg viewBox="0 0 760 260" role="img" aria-label="个股近120日趋势图">
				{[0, 1, 2, 3].map((row) => <line x1="42" x2="744" y1={30 + row * 58} y2={30 + row * 58} key={row} />)}
				<polyline className="ma120" points={geometry.ma120} />
				<polyline className="ma60" points={geometry.ma60} />
				<polyline className="ma20" points={geometry.ma20} />
				<polyline className="close" points={geometry.close} />
				<text x="5" y="34">{geometry.max.toFixed(2)}</text>
				<text x="5" y="208">{geometry.min.toFixed(2)}</text>
			</svg>
			<div className="stock-ai-chart-legend"><span className="close">收盘</span><span className="ma20">MA20</span><span className="ma60">MA60</span><span className="ma120">MA120</span><em>{points[0]?.date} — {points.at(-1)?.date}</em></div>
		</div>
	);
}

function buildChartGeometry(points: StockAITrendPoint[]) {
	if (points.length < 2) return null;
	const values = points.flatMap((point) => [point.close, point.ma20, point.ma60, point.ma120]).filter((value): value is number => typeof value === 'number' && Number.isFinite(value) && value > 0);
	if (!values.length) return null;
	const min = Math.min(...values);
	const max = Math.max(...values);
	const range = Math.max(max - min, max * 0.03, 1);
	const x = (index: number) => 42 + (index / Math.max(points.length - 1, 1)) * 702;
	const y = (value: number) => 208 - ((value - min) / range) * 178;
	const line = (getter: (point: StockAITrendPoint) => number | undefined) => points.map((point, index) => {
		const value = getter(point);
		return value && Number.isFinite(value) ? `${x(index).toFixed(1)},${y(value).toFixed(1)}` : '';
	}).filter(Boolean).join(' ');
	return { min, max, close: line((point) => point.close), ma20: line((point) => point.ma20), ma60: line((point) => point.ma60), ma120: line((point) => point.ma120) };
}

function loadAnalysisHistory(): AnalysisHistoryItem[] {
	try {
		const parsed = JSON.parse(window.localStorage.getItem(historyStorageKey) || '[]');
		return Array.isArray(parsed) ? parsed.filter((item) => item?.analysis?.symbol && item?.analysis?.scorecard).slice(0, 10) : [];
	} catch {
		return [];
	}
}

function buildPlanText(analysis: StockAIAnalysis) {
	return [
		`${analysis.name}（${analysis.symbol}）个股AI分析`,
		`综合评分：${analysis.scorecard.overall} / ${analysis.scorecard.grade} / ${analysis.scorecard.direction}`,
		`画像：${analysis.profile.type_label} / ${analysis.profile.price_phase} / ${analysis.profile.market_role}`,
		`结论：${analysis.conclusion.headline}`,
		`隔日预期：${analysis.next_day.bias}；${analysis.next_day.expectation}`,
		`允许介入：${analysis.action_plan.entry_conditions.join('；')}`,
		`禁止条件：${analysis.action_plan.avoid_conditions.join('；')}`,
		`风控：参考价${analysis.risk_control.entry_reference}，止损${analysis.risk_control.stop_price}，目标${analysis.risk_control.take_profit_first}/${analysis.risk_control.take_profit_second}，仓位${analysis.risk_control.suggested_position_min_percent}%—${analysis.risk_control.suggested_position_max_percent}%`,
		`主要风险：${analysis.conclusion.main_risk}`,
		`生成时间：${analysis.generated_at}`,
	].join('\n');
}

function qualityLabel(key: string) {
	const labels: Record<string, string> = { kline: '趋势K线', quote: '实时行情', limit_up: '涨停结构', theme: '题材归因', market_emotion: '市场情绪', benchmark: '基准指数', collection: '数据采集' };
	return labels[key] || key;
}

function stockThemeSourceLabel(source?: string) {
	if (!source) return '';
	if (source.includes('kaipanla-limit-up')) return '开盘啦连板缓存';
	if (source.includes('kaipanla-theme-leader')) return '开盘啦趋势题材';
	if (source === 'eastmoney:f10-business' || source === 'eastmoney-f10-business') return '东方财富 F10 主营';
	if (source === 'eastmoney-stock-industry') return '东方财富行业（降级）';
	if (source === 'theme-radar' || source.includes('kaipanla')) return '趋势题材雷达';
	return '题材归因';
}

function scoreClass(score: number) {
	if (score >= 65) return 'strong';
	if (score < 40) return 'weak';
	return 'neutral';
}

function formatPrice(value: number) {
	return Number.isFinite(value) && value > 0 ? value.toFixed(2) : '--';
}

function formatMoney(value: number) {
	return Number.isFinite(value) && value > 0 ? `${value.toLocaleString('zh-CN', { maximumFractionDigits: 0 })} 元` : '--';
}
