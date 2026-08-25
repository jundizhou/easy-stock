import {
	Activity,
	ArrowDownRight,
	ArrowUpRight,
	BarChart3,
	Bot,
	BrainCircuit,
	Building2,
	Calculator,
	CheckCircle2,
	ChevronLeft,
	ChevronRight,
	CircleAlert,
	Clipboard,
	Clock3,
	Download,
	Flag,
	Gauge,
	Github,
	History,
	Layers3,
	LineChart,
	ExternalLink,
	FileSearch,
	Flame,
	ListChecks,
	LoaderCircle,
	Newspaper,
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
import { FormEvent, KeyboardEvent, ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
	BackendConfig,
	StockAIActionPriceZone,
	StockAIAnalysis,
	StockAIDataQuality,
	StockAINewsAnalysis,
	StockAINextDayScenario,
	StockAIShortTermDecisionStage,
	StockAIShortTermQuantitativePlan,
	StockAITrendPoint,
	StockDirectoryData,
	StockDirectoryEntry,
	HotStockRankData,
	HotStockRankEntry,
	HotStockRankSource,
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
const hotStockSidebarStorageKey = 'easy-stock.stock-ai-popular-sidebar-collapsed.v1';
const directoryStorageTTL = 24 * 60 * 60 * 1000;
const examples = ['600519', '300750', '002594', '601138', '688981'];

type DirectoryState = 'idle' | 'loading' | 'cached' | 'ready' | 'error';
type HotRankState = 'idle' | 'loading' | 'ready' | 'error';

export function StockAIAnalysisWorkspace({ config, refreshKey, mode, onAskAI, onOpenSettings }: Props) {
	const [query, setQuery] = useState(() => window.localStorage.getItem(symbolStorageKey) || '');
	const [analysis, setAnalysis] = useState<StockAIAnalysis | null>(null);
	const [history, setHistory] = useState<AnalysisHistoryItem[]>(loadAnalysisHistory);
	const [state, setState] = useState<LoadState>('idle');
	const [error, setError] = useState('');
	const [copied, setCopied] = useState(false);
	const [exporting, setExporting] = useState(false);
	const [exportNotice, setExportNotice] = useState('');
	const [directory, setDirectory] = useState<StockDirectoryEntry[]>(loadCachedStockDirectory);
	const [directoryState, setDirectoryState] = useState<DirectoryState>(() => directory.length > 0 ? 'cached' : 'idle');
	const [hotRanks, setHotRanks] = useState<HotStockRankData | null>(null);
	const [hotRankState, setHotRankState] = useState<HotRankState>('idle');
	const [hotRankError, setHotRankError] = useState('');
	const [hotStockSidebarCollapsed, setHotStockSidebarCollapsed] = useState(() => window.localStorage.getItem(hotStockSidebarStorageKey) === '1');
	const exportRef = useRef<HTMLDivElement>(null);

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

	const loadHotRanks = useCallback(async (force = false) => {
		if (!config) return;
		setHotRankState('loading');
		setHotRankError('');
		try {
			const suffix = force ? '?refresh=1' : '';
			const payload = await requestJSON<{ data: HotStockRankData }>(config, `/api/v1/stocks/hot-ranks${suffix}`);
			setHotRanks(payload.data);
			setHotRankState('ready');
		} catch (loadError) {
			setHotRankError(loadError instanceof Error ? loadError.message : '人气股暂不可用');
			setHotRankState('error');
		}
	}, [config]);

	const toggleHotStockSidebar = () => {
		setHotStockSidebarCollapsed((current) => {
			const next = !current;
			window.localStorage.setItem(hotStockSidebarStorageKey, next ? '1' : '0');
			return next;
		});
	};

	useEffect(() => {
		void loadHotRanks();
	}, [loadHotRanks]);

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

	const exportLongImage = async () => {
		if (!analysis || !exportRef.current || exporting) return;
		setExporting(true);
		setExportNotice('');
		try {
			await document.fonts?.ready;
			const { default: html2canvas } = await import('html2canvas');
			const canvas = await html2canvas(exportRef.current, {
				backgroundColor: '#f4f7fb',
				logging: false,
				scale: Math.min(Math.max(window.devicePixelRatio || 1, 1.5), 2),
				useCORS: true,
				windowWidth: 1600,
				onclone: (_document, element) => element.classList.add('is-exporting'),
			});
			const blob = await canvasToPNGBlob(canvas);
			const href = URL.createObjectURL(blob);
			const link = document.createElement('a');
			link.href = href;
			link.download = buildAnalysisImageFilename(analysis, mode);
			document.body.appendChild(link);
			link.click();
			link.remove();
			window.setTimeout(() => URL.revokeObjectURL(href), 1000);
			setExportNotice('长图已生成并保存');
		} catch (exportError) {
			console.error('Failed to export stock analysis image', exportError);
			setExportNotice('长图生成失败，请稍后重试');
		} finally {
			setExporting(false);
			window.setTimeout(() => setExportNotice(''), 2600);
		}
	};

	return (
		<section className="stock-ai-workspace">
			<AnalysisSearch query={query} mode={mode} directory={directory} directoryState={directoryState} onQuery={setQuery} onSubmit={submit} loading={state === 'loading'} />
			<div className={`stock-ai-shell ${hotStockSidebarCollapsed ? 'is-hot-collapsed' : ''}`.trim()}>
				<HotStockSidebar data={hotRanks} state={hotRankState} error={hotRankError} activeSymbol={analysis?.symbol} collapsed={hotStockSidebarCollapsed} onToggle={toggleHotStockSidebar} onRefresh={() => void loadHotRanks(true)} onSelect={(symbol) => void runAnalysis(symbol)} />
				<div className="stock-ai-main">
					{history.length > 0 && <AnalysisHistory items={history} activeSymbol={analysis?.symbol} onSelect={selectHistory} onRemove={removeHistory} />}

					{state === 'loading' && (
						<div className="stock-ai-loading" role="status" aria-live="polite">
							<LoaderCircle className="spin" size={30} />
							<div><strong>正在建立完整决策画像</strong><span>为了保证分析的全面性和准确性，easy-stock 将获取行情、题材、公告、研报等完整数据，并进行多轮 AI 分析，预计耗时 5–10 分钟，请耐心等待。</span></div>
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
						<div className="stock-ai-result">
							<div className="stock-ai-export-sheet" ref={exportRef}>
								<AnalysisExportHeader analysis={analysis} mode={mode} />
								<AnalysisVerdict analysis={analysis} copied={copied} exporting={exporting} onRefresh={() => void runAnalysis(analysis.symbol)} onExport={() => void exportLongImage()} onCopy={() => void copyPlan()} onAskAI={() => onAskAI(analysis)} onOpenSettings={onOpenSettings} />
								{mode === 'analysis' && <FullAnalysisView analysis={analysis} />}
								{mode === 'expectation' && <ExpectationView analysis={analysis} />}
								{mode === 'risk' && <RiskExecutionView analysis={analysis} />}
								<AnalysisExportFooter analysis={analysis} />
							</div>
							{exportNotice && <div className={`stock-ai-export-notice ${exportNotice.includes('失败') ? 'error' : ''}`} role="status">{exportNotice}</div>}
						</div>
					)}
				</div>
			</div>
		</section>
	);
}

function HotStockSidebar({ data, state, error, activeSymbol, collapsed, onToggle, onRefresh, onSelect }: {
	data: HotStockRankData | null;
	state: HotRankState;
	error: string;
	activeSymbol?: string;
	collapsed: boolean;
	onToggle: () => void;
	onRefresh: () => void;
	onSelect: (symbol: string) => void;
}) {
	const [filter, setFilter] = useState<'all' | HotStockRankSource>('all');
	const [query, setQuery] = useState('');
	const stocks = useMemo(() => {
		const needle = query.trim().toLowerCase();
		return (data?.stocks || [])
			.map((stock, unionIndex) => ({ stock, unionIndex }))
			.filter(({ stock }) => {
				if (filter !== 'all' && !stock.ranks[filter]) return false;
				return !needle || stock.name.toLowerCase().includes(needle) || stock.code.includes(needle);
			});
	}, [data?.stocks, filter, query]);
	const filters: Array<{ id: 'all' | HotStockRankSource; label: string; count: number; disabled?: boolean; title?: string }> = [
		{ id: 'all', label: '并集', count: data?.total || 0 },
		...(data?.sources || []).map((source) => ({ id: source.id, label: source.id === 'ths' ? '同花顺' : '东方财富', count: source.count, disabled: !source.available, title: source.error })),
	];
	if (collapsed) {
		return <aside className="stock-hot-sidebar is-collapsed" aria-label="同花顺和东方财富人气股">
			<div className="stock-hot-collapsed">
				<span className="stock-hot-icon" title="人气股"><Flame size={17} /></span>
				<button type="button" onClick={onToggle} title="展开人气股" aria-label="展开人气股" aria-expanded="false"><ChevronRight size={16} /></button>
			</div>
		</aside>;
	}
	return (
		<aside className="stock-hot-sidebar" aria-label="同花顺和东方财富人气股">
			<header>
				<span className="stock-hot-icon"><Flame size={17} /></span>
				<div><strong>人气股</strong><small>同花顺 · 东方财富</small></div>
				<em>{data?.total || '--'}</em>
				<button type="button" onClick={onRefresh} disabled={state === 'loading'} title="刷新人气股" aria-label="刷新人气股"><RefreshCw className={state === 'loading' ? 'spin' : ''} size={14} /></button>
				<button type="button" onClick={onToggle} title="收拢人气股" aria-label="收拢人气股" aria-expanded="true"><ChevronLeft size={16} /></button>
			</header>
			<div className="stock-hot-filters" role="group" aria-label="人气股来源筛选">
				{filters.map((item) => <button type="button" className={filter === item.id ? 'active' : ''} onClick={() => setFilter(item.id)} disabled={item.disabled} title={item.title} aria-pressed={filter === item.id} key={item.id}><span>{item.label}</span><em>{item.count || '--'}</em></button>)}
			</div>
			<label className="stock-hot-search"><Search size={13} /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="筛选名称或代码" aria-label="筛选人气股" /></label>
			<div className="stock-hot-list">
				{state === 'loading' && !data && Array.from({ length: 9 }, (_, index) => <div className="stock-hot-skeleton" key={index}><i /><span /><em /></div>)}
				{state === 'error' && !data && <div className="stock-hot-empty"><CircleAlert size={18} /><strong>人气股暂不可用</strong><span>{error}</span></div>}
				{state !== 'loading' && data && stocks.length === 0 && <div className="stock-hot-empty"><FileSearch size={18} /><strong>没有匹配股票</strong></div>}
				{stocks.map(({ stock, unionIndex }) => <HotStockRow stock={stock} index={unionIndex} active={stock.symbol === activeSymbol} onSelect={onSelect} key={stock.symbol} />)}
			</div>
			{data && <footer><span>{data.stale ? '缓存快照' : '实时人气榜'}</span><time>{formatHotRankTime(data.updated_at)}</time></footer>}
		</aside>
	);
}

function HotStockRow({ stock, index, active, onSelect }: { stock: HotStockRankEntry; index: number; active: boolean; onSelect: (symbol: string) => void }) {
	return <button type="button" className={`${active ? 'active' : ''} ${index < 3 ? 'top' : ''}`.trim()} onClick={() => onSelect(stock.symbol)}>
		<i>{String(index + 1).padStart(2, '0')}</i>
		<span><strong>{stock.name}</strong><small>{stock.code} · {stockMarketLabel(stock.symbol)}</small></span>
		<em>
			{stock.ranks.ths && <span className="ths" title={`同花顺第 ${stock.ranks.ths} 名`}>同<b>{stock.ranks.ths}</b></span>}
			{stock.ranks.eastmoney && <span className="eastmoney" title={`东方财富第 ${stock.ranks.eastmoney} 名`}>东<b>{stock.ranks.eastmoney}</b></span>}
		</em>
	</button>;
}

function formatHotRankTime(value: string) {
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return '刚刚更新';
	return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
}

function AnalysisExportHeader({ analysis, mode }: { analysis: StockAIAnalysis; mode: StockAIWorkspaceMode }) {
	return <header className="stock-ai-export-header">
		<div className="stock-ai-export-brand"><span><BrainCircuit size={24} /></span><div><strong>easy-stock</strong><small>AI STOCK DECISION SYSTEM</small><em>开源 · 免费使用</em></div></div>
		<div><span>{workspaceModeLabel(mode)}</span><strong>{analysis.name} · {analysis.symbol}</strong><small>分析生成于 {formatExportDate(analysis.generated_at)}</small></div>
	</header>;
}

function AnalysisExportFooter({ analysis }: { analysis: StockAIAnalysis }) {
	return <footer className="stock-ai-export-footer">
		<div><strong>easy-stock</strong><span>让数据、逻辑与 AI 一起服务于交易决策</span></div>
		<div className="stock-ai-export-promo"><span><Github size={13} />个人非商业免费 · 欢迎 Star</span><strong>github.com/jundizhou/easy-stock</strong></div>
		<div><span>数据截至 {formatExportDate(analysis.generated_at)}</span><strong>仅供研究参考，不构成任何投资建议</strong></div>
	</footer>;
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
		analysis: '自动路由新股价格发现、趋势容量、成长趋势、情绪短线与风险结构，形成与样本成熟度匹配的结论。',
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

function AnalysisVerdict({ analysis, copied, exporting, onRefresh, onExport, onCopy, onAskAI, onOpenSettings }: {
	analysis: StockAIAnalysis;
	copied: boolean;
	exporting: boolean;
	onRefresh: () => void;
	onExport: () => void;
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
				<div className="stock-ai-tags"><span>{analysis.action_plan.decision_label || analysis.profile.type_label}</span><span>{analysis.profile.price_phase}</span><span>{analysis.profile.market_role}</span><span>{analysis.scorecard.direction} · {analysis.scorecard.grade}</span></div>
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
				<button type="button" className="export" onClick={onExport} disabled={exporting}>{exporting ? <LoaderCircle className="spin" size={14} /> : <Download size={14} />}{exporting ? '正在生成…' : '导出长图'}</button>
				<button type="button" onClick={onCopy}><Clipboard size={14} />{copied ? '已复制' : '复制预案'}</button>
				<button type="button" className="primary" onClick={onAskAI}><Bot size={14} />继续推演</button>
			</div>
		</section>
	);
}

function NewListingNotice({ analysis }: { analysis: StockAIAnalysis }) {
	const days = analysis.trend.history_days || analysis.chart.length;
	const remaining = Math.max(20 - days, 1);
	return <section className="stock-ai-new-listing-notice">
		<span><CircleAlert size={18} /></span>
		<div><strong>已切换至新股分析流程</strong><p>当前仅有 {days} 个交易日样本，采用上市期价格发现、流动性、换手与高风险约束模型；MA20 / MA60 / MA120、ATR14 和成熟趋势评分均未启用。</p></div>
		<em>还需 {remaining} 个交易日形成 MA20</em>
	</section>;
}

function FullAnalysisView({ analysis }: { analysis: StockAIAnalysis }) {
	const isNewListing = analysis.profile.primary_type === 'new_listing';
	const theme = normalizeStockAITheme(analysis.theme);
	const themeSource = stockThemeSourceLabel(theme.source);
	const themeRole = theme.role && theme.role !== '待确认' ? theme.role : '';
	const themeDetail = theme.is_hot
		? [themeRole, themeSource, theme.business ? `主业：${theme.business}` : ''].filter(Boolean).join(' · ')
		: [themeSource, theme.resonance.state === '价格未确认' ? '热点涨幅未通过验证' : '未发现明确热点炒作'].filter(Boolean).join(' · ');
	return (
		<>
			{isNewListing && <NewListingNotice analysis={analysis} />}
			<section className="stock-ai-kpis stock-ai-kpis-complete">
				<KPICard icon={<Gauge size={17} />} label={isNewListing ? '观察评分' : '综合评分'} value={`${analysis.scorecard.overall} · ${analysis.scorecard.grade}`} detail={`${analysis.scorecard.direction} · 置信度${analysis.scorecard.conviction}`} tone="blue" />
				<KPICard icon={<TrendingUp size={17} />} label={isNewListing ? '价格发现' : '趋势强度'} value={`${analysis.trend.score}`} detail={`${analysis.trend.strength} · ${analysis.trend.setup}`} tone="green" />
				<KPICard icon={<Scale size={17} />} label="相对强度" value={analysis.relative_strength.available ? `${analysis.relative_strength.score}` : '--'} detail={analysis.relative_strength.available ? `${analysis.relative_strength.state} · ${analysis.relative_strength.benchmark_name}` : analysis.relative_strength.detail} tone="purple" />
				<KPICard icon={<ShieldCheck size={17} />} label="交易风险" value={`${analysis.risk_control.score} · ${analysis.risk_control.level}`} detail={`仓位${analysis.risk_control.suggested_position_min_percent}%—${analysis.risk_control.suggested_position_max_percent}%`} tone="amber" />
				<KPICard icon={<Zap size={17} />} label="短线状态" value={analysis.short_term.state} detail={isNewListing ? `上市 ${analysis.trend.history_days || analysis.chart.length} 日 · ${analysis.short_term.tradability}` : `近20日 ${analysis.short_term.limit_up_count_20d} 次涨停 · ${analysis.short_term.tradability}`} tone="amber" />
				<KPICard icon={<Target size={17} />} label={theme.is_hot ? '热点定位' : '主业定位'} value={theme.primary || '独立结构'} detail={themeDetail} tone="purple" />
			</section>

			<div className="stock-ai-analysis-grid">
				<ScorecardPanel analysis={analysis} />
				<SignalMatrix analysis={analysis} />
			</div>

			<ThemeAttributionPanel analysis={analysis} />

			{!isShortTermDecision(analysis) && <div className="stock-ai-fundamental-grid">
				<FundamentalPanel analysis={analysis} />
				<ResearchPanel analysis={analysis} />
			</div>}

			<div className="stock-ai-news-grid">
				<NewsAnalysisPanel eyebrow="个股资讯" title="近期个股新闻分析" item={analysis.stock_news} icon={<Newspaper size={19} />} />
				<NewsAnalysisPanel eyebrow="题材催化" title="近期题材新闻分析" item={analysis.theme_news} icon={<Zap size={19} />} />
			</div>

			<div className="stock-ai-main-grid">
				<section className="stock-ai-panel stock-ai-chart-panel">
					<header><div><span>{isNewListing ? '价格发现' : '趋势结构'}</span><h3>{isNewListing ? '上市期价格区间' : '价格与 MA20 / MA60 / MA120'}</h3></div><LineChart size={19} /></header>
					<TrendChart points={analysis.chart} newListing={isNewListing} />
					{isNewListing ? <div className="stock-ai-trend-metrics">
						<Metric label="上市期" value={signedPercent(analysis.trend.listing_return || 0)} />
						<Metric label="区间位置" value={`${(analysis.trend.listing_range_position || 0).toFixed(0)}%`} />
						<Metric label="平均换手" value={`${(analysis.trend.average_turnover || 0).toFixed(1)}%`} />
						<Metric label="平均成交额" value={formatCompactAmount(analysis.trend.average_amount || 0)} />
						<Metric label="观测波动" value={`${(analysis.trend.observed_volatility || 0).toFixed(1)}%`} />
					</div> : <div className="stock-ai-trend-metrics">
						<Metric label="20日" value={signedPercent(analysis.trend.return_20d)} />
						<Metric label="60日" value={signedPercent(analysis.trend.return_60d)} />
						<Metric label="区间位置" value={`${analysis.trend.range_position_60d.toFixed(0)}%`} />
						<Metric label="量比 5/20" value={analysis.trend.volume_ratio_5d_20d.toFixed(2)} />
						<Metric label="ATR14" value={`${analysis.trend.atr_14_percent.toFixed(1)}%`} />
					</div>}
					<div className="stock-ai-levels">
						<div><span>{isNewListing ? '上市区间低点' : '参考支撑'}</span><strong>{formatPrice(analysis.trend.support)}</strong></div>
						<div><span>{isNewListing ? '上市区间高点' : '阶段压力'}</span><strong>{formatPrice(analysis.trend.resistance)}</strong></div>
						<div><span>失效条件</span><strong>{analysis.trend.invalidation}</strong></div>
					</div>
				</section>

				<section className="stock-ai-panel stock-ai-timeframe-panel">
					<header><div><span>{isNewListing ? '样本成熟度' : '周期共振'}</span><h3>{isNewListing ? '交易日积累进度' : '五周期一致性'}</h3></div><Layers3 size={19} /></header>
					<div className="stock-ai-timeframes">
						{analysis.timeframes.map((item) => <article className={scoreClass(item.score)} key={item.key}><div><strong>{item.label}</strong><em>{item.score || '--'}</em></div><span>{item.state}</span><div className="stock-ai-score-bar"><i style={{ width: `${item.score}%` }} /></div><small>{isNewListing ? (item.key === 'listing_period' ? `${item.window}个交易日 · 上市期${signedPercent(item.return_percent)}` : `目标 ${item.window} 个交易日 · 暂不计算均线`) : `${signedPercent(item.return_percent)} · 均线${item.above_moving_average ? '上方' : '下方'} · 斜率${signedPercent(item.slope_percent)}`}</small></article>)}
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

function ThemeAttributionPanel({ analysis }: { analysis: StockAIAnalysis }) {
	const theme = normalizeStockAITheme(analysis.theme);
	const resonance = theme.resonance;
	return <section className="stock-ai-panel stock-ai-theme-attribution-panel">
		<header><div><span>事件驱动归因</span><h3>主业 · 事实题材 · 当前炒作 · 市场延伸</h3></div><Target size={19} /></header>
		<div className="stock-ai-theme-attribution-body">
			<div className="stock-ai-theme-primary"><span>{theme.is_hot ? '当前主炒作' : '当前主业'}</span><strong>{theme.is_hot ? theme.hot_theme || theme.primary : theme.business_theme || theme.primary || '暂无有效题材'}</strong><em>{theme.is_hot ? `置信度${theme.confidence} · 炒作相关性${theme.hot_score}` : theme.resonance.state === '价格未确认' ? '热点涨幅未通过验证 · 已回退主业' : '未发现事件与盘面共振'}</em><small>{theme.description}</small></div>
			<div className="stock-ai-theme-columns">
				<div><span>事实支撑</span>{(theme.confirmed_themes || []).length ? theme.confirmed_themes?.map((item) => <article key={item.name}><strong>{item.name}</strong><em>{item.confidence} · {item.score}</em><small>{item.detail}</small></article>) : <small className="empty">暂无结构化事实题材</small>}</div>
				<div><span>市场延伸 / 映射</span>{(theme.speculative_themes || []).length ? theme.speculative_themes?.map((item) => <article key={item.name}><strong>{item.name}</strong><em>{item.confidence} · {item.score}</em><small>{item.detail}</small></article>) : <small className="empty">暂无有效延伸题材</small>}</div>
			</div>
			<div className="stock-ai-theme-resonance"><div><span>题材共振</span><strong>{resonance.available ? `${resonance.score} · ${resonance.state}` : '暂无有效题材'}</strong><small>{resonance.detail}</small></div>{resonance.available && <div className="stock-ai-theme-resonance-metrics">{[['个股动能', resonance.stock_momentum], ['相对强度', resonance.relative_strength], ['上涨广度', resonance.breadth], ['涨停能量', resonance.limit_up_energy], ['持续性', resonance.persistence], ['证据质量', resonance.evidence_quality], ['资金扩散', resonance.capital_diffusion]].map(([label, value]) => <article key={String(label)}><span>{label}</span><strong>{value}</strong><i><b style={{ width: `${value}%` }} /></i></article>)}</div>}</div>
		</div>
	</section>;
}

function normalizeStockAITheme(theme?: StockAIAnalysis['theme']): NonNullable<StockAIAnalysis['theme']> {
	const fallback = {
		primary: '', business: '', is_hot: false, concepts: [], source: '', route: 'trend', trend_score: 0, trend_stage: '', active_days: 0, max_streak: 0, role: '待确认', description: '当前结果由旧版本缓存生成，请重新分析以获得题材共振明细', hot_score: 0, confidence: '低', resonance: { available: false, score: 0, state: '暂无题材', detail: '当前结果由旧版本缓存生成，请重新分析以获得题材共振明细', stock_momentum: 0, relative_strength: 0, breadth: 0, limit_up_energy: 0, persistence: 0, leader_position: 0, evidence_quality: 0, capital_diffusion: 0 },
	};
	return { ...fallback, ...(theme || {}), resonance: { ...fallback.resonance, ...(theme?.resonance || {}) } };
}

function FundamentalPanel({ analysis }: { analysis: StockAIAnalysis }) {
	const item = analysis.fundamental;
	return <section className="stock-ai-panel stock-ai-fundamental-panel">
		<header><div><span>公司质量</span><h3>基本面 · 最新财报</h3></div><Building2 size={19} /></header>
		{item?.available ? <>
			<div className="stock-ai-fundamental-summary"><strong>{item.score} · {item.quality}</strong><span>{item.report_name || item.report_date}</span><p>{item.summary}</p></div>
			<div className="stock-ai-fundamental-metrics">
				<FundamentalMetric label="营业收入" value={formatCompactAmount(item.revenue)} detail={`同比 ${signedPercent(item.revenue_yoy)}`} tone={item.revenue_yoy >= 0 ? 'positive' : 'negative'} />
				<FundamentalMetric label="归母净利润" value={formatCompactAmount(item.net_profit)} detail={`同比 ${signedPercent(item.net_profit_yoy)}`} tone={item.net_profit_yoy >= 0 ? 'positive' : 'negative'} />
				<FundamentalMetric label="ROE" value={`${item.roe.toFixed(1)}%`} detail={`EPS ${item.eps.toFixed(2)}`} />
				<FundamentalMetric label="毛利率" value={`${item.gross_margin.toFixed(1)}%`} detail={`负债率 ${item.debt_ratio.toFixed(1)}%`} />
			</div>
		</> : <div className="stock-ai-panel-empty">最新F10财务数据暂不可用</div>}
	</section>;
}

function FundamentalMetric({ label, value, detail, tone = '' }: { label: string; value: string; detail: string; tone?: string }) {
	return <article><span>{label}</span><strong className={tone}>{value}</strong><small>{detail}</small></article>;
}

function ResearchPanel({ analysis }: { analysis: StockAIAnalysis }) {
	const item = analysis.research;
	return <section className="stock-ai-panel stock-ai-research-panel">
		<header><div><span>第三方预期</span><h3>机构研报 · 近45日</h3></div><FileSearch size={19} /></header>
		{item?.available ? <>
			<div className="stock-ai-research-summary"><div><strong>{item.report_count} 篇</strong><span>{item.organization_count} 家机构 · 覆盖{item.coverage}{item.latest_rating ? ` · 最新评级${item.latest_rating}` : ''}</span></div><small>机构评级仅代表第三方观点，不作为系统买卖结论。</small></div>
			<div className="stock-ai-research-list">{item.reports.map((report) => <article key={report.id}><div><header><span>{report.organization || '研究机构'}</span><time>{formatShortDate(report.published_at)}</time></header><strong>{report.title}</strong><small>{[report.rating ? `评级 ${report.rating}` : '', report.target_low || report.target_high ? `目标价 ${formatTargetPrice(report.target_low, report.target_high)}` : '', report.eps ? `EPS ${report.eps.toFixed(2)}` : ''].filter(Boolean).join(' · ')}</small></div>{report.url && <a href={report.url} target="_blank" rel="noreferrer" title="查看研报原文"><ExternalLink size={14} /></a>}</article>)}</div>
		</> : <div className="stock-ai-panel-empty">近45日暂无可用机构研报</div>}
	</section>;
}

function NewsAnalysisPanel({ eyebrow, title, item, icon }: { eyebrow: string; title: string; item?: StockAINewsAnalysis; icon: ReactNode }) {
	return <section className="stock-ai-panel stock-ai-news-panel">
		<header><div><span>{eyebrow}</span><h3>{title}</h3></div>{icon}</header>
		{item ? <>
			<div className="stock-ai-news-summary">
				<div><strong>{item.article_count} 条</strong><span>{item.source_count} 个来源 · 近{item.window_days || 30}日</span><em className={newsToneClass(item.tone)}>{item.tone || '信息不足'}</em><small>{item.analysis_source === 'hermes-ai' ? 'Hermes AI 归纳' : '本地规则归纳'}</small></div>
				{item.keywords?.length > 0 && <div className="stock-ai-news-keywords">{item.keywords.map((keyword) => <span key={keyword}>{keyword}</span>)}</div>}
				<p>{item.summary}</p>
			</div>
			<div className="stock-ai-news-signals">
				<div className="positive"><strong><Sparkles size={13} />潜在催化</strong>{item.catalysts?.length ? <ul>{item.catalysts.map((value) => <li key={value}>{value}</li>)}</ul> : <small>暂无可验证的明确催化</small>}</div>
				<div className="negative"><strong><ShieldAlert size={13} />风险信号</strong>{item.risks?.length ? <ul>{item.risks.map((value) => <li key={value}>{value}</li>)}</ul> : <small>暂无突出风险关键词</small>}</div>
			</div>
			{item.articles?.length ? <div className="stock-ai-news-list">{item.articles.map((article, index) => <article key={article.id || article.url || `${article.title}-${index}`}>
				<div><header><span>{formatNewsSource(article.meta?.source)}</span><time>{article.published_at ? formatShortDate(article.published_at) : '--'}</time></header><strong>{article.title}</strong>{article.content && article.content !== article.title && <small>{article.content}</small>}</div>
				{article.url && <a href={article.url} target="_blank" rel="noreferrer" title="查看新闻原文"><ExternalLink size={14} /></a>}
			</article>)}</div> : <div className="stock-ai-news-empty">暂无匹配新闻，当前不对新闻催化作额外加分。</div>}
			<footer>新闻与公告仅作信息参考，需结合事件落地和价格反应验证，不构成买卖建议。</footer>
		</> : <div className="stock-ai-panel-empty">旧版分析记录未包含新闻数据，请重新分析后查看</div>}
	</section>;
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
	if (isShortTermDecision(analysis)) return <ShortTermExpectationView analysis={analysis} />;
	const plan = analysis.next_day;
	return (
		<>
			<section className={`stock-ai-expectation-hero ${scoreClass(plan.score)}`}>
				<div><span>隔日基础预期</span><h2>{plan.bias}</h2><p>{plan.expectation}</p></div>
				<div className="stock-ai-expectation-score"><strong>{plan.score}</strong><span>情景评分</span></div>
				<div className="stock-ai-expected-range"><span>预估正常波动区间</span><strong>{formatPrice(plan.expected_low)} — {formatPrice(plan.expected_high)}</strong><small>{analysis.profile.primary_type === 'new_listing' ? '基于上市期观测振幅的风险区间，不是价格预测' : '基于ATR的观察区间，不是价格预测'}</small></div>
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

function ShortTermExpectationView({ analysis }: { analysis: StockAIAnalysis }) {
	return <>
		<ActionPlanPanel analysis={analysis} />
		<div className="stock-ai-detail-grid">
			<RiskBoundaryPanel analysis={analysis} />
			<EvidencePanel analysis={analysis} />
			<DataQualityPanel analysis={analysis} />
		</div>
	</>;
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
	if (isShortTermDecision(analysis)) return <ShortTermRiskExecutionView analysis={analysis} />;
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

function ShortTermRiskExecutionView({ analysis }: { analysis: StockAIAnalysis }) {
	const risk = analysis.risk_control;
	return <>
		<section className={`stock-ai-risk-hero ${risk.level === '高' ? 'high' : risk.level === '较低' ? 'low' : 'medium'}`}>
			<div><ShieldCheck size={26} /><span><small>超短执行风险</small><strong>{risk.level} · {risk.score}分</strong></span></div>
			<div><small>建议仓位区间</small><strong>{risk.suggested_position_min_percent}% — {risk.suggested_position_max_percent}%</strong></div>
			<div><small>交易周期</small><strong>{analysis.action_plan.horizon || '隔日 / 1—3日'}</strong></div>
			<div><small>核心约束</small><strong>竞价确认 + T+1</strong></div>
		</section>
		<ActionPlanPanel analysis={analysis} />
		<div className="stock-ai-detail-grid">
			<RiskBoundaryPanel analysis={analysis} />
			<EvidencePanel analysis={analysis} />
			<DataQualityPanel analysis={analysis} />
		</div>
	</>;
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
	const plan = analysis.action_plan;
	if (isShortTermDecision(analysis)) return <ShortTermActionPlanPanel analysis={analysis} />;
	const pricePlan = resolveActionPricePlan(analysis);
	return (
		<section className="stock-ai-panel stock-ai-plan-panel">
			<header><div><span>{plan.pricing_source === 'hermes-ai' ? 'AI全局综合定价' : '价格决策'}</span><h3>{plan.current_action}</h3></div><Target size={19} /></header>
			{(plan.rationale || plan.horizon) && <div className="stock-ai-decision-route"><div><span>{plan.decision_label || '趋势与价值定价'}</span><em>{plan.horizon || '波段 / 中期趋势'}</em>{typeof plan.decision_confidence === 'number' && <small>置信度 {Math.round(plan.decision_confidence * 100)}%</small>}</div><p>{plan.rationale}</p></div>}
			{pricePlan ? <div className="stock-ai-price-decisions">
				<ActionPriceCard tone="entry" zone={pricePlan.entry} />
				<ActionPriceCard tone="hold" zone={pricePlan.hold} />
				<ActionPriceCard tone="take-profit" zone={pricePlan.takeProfit} />
				<ActionPriceCard tone="stop-loss" zone={pricePlan.stopLoss} />
			</div> : <div className="stock-ai-plan-legacy">
				<PlanGroup tone="entry" title="允许介入" items={plan.entry_conditions || []} />
				<PlanGroup tone="hold" title="持有条件" items={plan.hold_conditions || []} />
				<PlanGroup tone="avoid" title="禁止条件" items={plan.avoid_conditions || []} />
				<p>该记录生成于价格决策升级前，重新分析后可查看允许介入、持有、止盈和止损价格。</p>
			</div>}
			<div className="stock-ai-position"><Activity size={15} /><div><span>仓位约束</span><strong>{plan.position_hint}</strong></div></div>
		</section>
	);
}

function ShortTermActionPlanPanel({ analysis }: { analysis: StockAIAnalysis }) {
	const plan = analysis.action_plan;
	const playbook = plan.short_term_playbook;
	return <section className="stock-ai-panel stock-ai-short-plan-panel">
		<header><div><span>短线动态决策</span><h3>{plan.current_action}</h3></div><Zap size={19} /></header>
		<div className="stock-ai-decision-route short"><div><span>{plan.decision_label || '超短次日作战'}</span><em>{plan.horizon || '隔日 / 1—3个交易日'}</em>{typeof plan.decision_confidence === 'number' && <small>置信度 {Math.round(plan.decision_confidence * 100)}%</small>}</div><p>{plan.rationale || '短线结果依赖次日竞价、题材反馈和开盘承接，不使用盘后静态价格替代执行确认。'}</p></div>
		{playbook ? <>
			<div className="stock-ai-short-overview">
				<article><span>个股定位</span><strong>{playbook.positioning}</strong></article>
				<article><span>情绪阶段</span><strong>{playbook.sentiment_cycle}</strong></article>
				<article><span>预期模式</span><strong>{playbook.expected_pattern}</strong></article>
			</div>
			<div className="stock-ai-short-conclusion"><Sparkles size={16} /><div><span>盘后结论</span><strong>{playbook.overnight_conclusion}</strong><small>{playbook.data_status}</small></div></div>
			{playbook.quantitative && <ShortTermQuantitativePanel quantitative={playbook.quantitative} />}
			<div className="stock-ai-short-stages">
				<ShortTermStageCard stage={playbook.auction} icon={<Clock3 size={16} />} />
				<ShortTermStageCard stage={playbook.opening} icon={<Activity size={16} />} />
			</div>
			<div className="stock-ai-short-condition-grid">
				<PlanGroup tone="entry" title="全部满足才参与" items={playbook.participation_conditions || []} />
				<PlanGroup tone="hold" title="持有条件" items={playbook.hold_conditions || []} />
				<PlanGroup tone="avoid" title="退出条件" items={playbook.exit_conditions || []} />
				<PlanGroup tone="avoid" title="一票否决" items={playbook.veto_conditions || []} />
			</div>
			<div className="stock-ai-short-scenarios">{(playbook.scenarios || []).map((scenario) => <article className={scenario.tone} key={`${scenario.name}-${scenario.condition}`}><span>{scenario.name}</span><strong>{scenario.condition}</strong><p>{scenario.action}</p></article>)}</div>
		</> : <div className="stock-ai-plan-legacy">
			<PlanGroup tone="entry" title="允许参与" items={plan.entry_conditions || []} />
			<PlanGroup tone="hold" title="持有条件" items={plan.hold_conditions || []} />
			<PlanGroup tone="avoid" title="禁止条件" items={plan.avoid_conditions || []} />
			<p>该记录来自旧版缓存，重新分析后可查看竞价与开盘动态作战计划。</p>
		</div>}
		<div className="stock-ai-position"><ShieldCheck size={15} /><div><span>仓位约束</span><strong>{plan.position_hint}</strong></div></div>
	</section>;
}

function ShortTermQuantitativePanel({ quantitative }: { quantitative: StockAIShortTermQuantitativePlan }) {
	const stock = quantitative.stock;
	const benchmark = quantitative.benchmark;
	const theme = quantitative.theme;
	return <section className="stock-ai-short-quantitative">
		<header><div><span>量化执行锚点</span><strong>{quantitative.baseline_date ? `${quantitative.baseline_date} 盘后基线` : '盘后基线'}</strong></div><Calculator size={16} /></header>
		<div className="stock-ai-short-quant-grid">
			<article>
				<span>个股竞价与开盘</span>
				<strong>{signedPercent(stock.auction_change_min)}—{signedPercent(stock.auction_change_max)}</strong>
				<ul>
					<li>竞价价格 {formatPriceRange(stock.auction_price_min, stock.auction_price_max)}</li>
					<li>竞价额 {formatCompactAmount(stock.auction_amount_min)}—{formatCompactAmount(stock.auction_amount_max)}</li>
					<li>9:35回撤 ≤ {stock.opening_drawdown_max.toFixed(1)}%，累计额 ≥ {formatCompactAmount(stock.opening_amount_min)}</li>
				</ul>
			</article>
			<article>
				<span>基准指数门槛</span>
				<strong>{benchmark.name}<small>{benchmark.symbol}</small></strong>
				<ul>
					<li>9:25竞价 ≥ {signedPercent(benchmark.auction_change_min)}</li>
					<li>9:35涨幅 ≥ {signedPercent(benchmark.opening_change_min)}</li>
					<li>跌至 {signedPercent(benchmark.failure_change)} 以下视为环境否决</li>
				</ul>
			</article>
			<article>
				<span>同题材联动门槛</span>
				<strong>{theme.name}</strong>
				<ul>
					<li>基线：涨停 {theme.limit_up_count}、连板 {theme.board_count}、最高 {theme.max_streak} 板</li>
					<li>核心股至少 {theme.minimum_positive_peers} 只涨幅 ≥ 0%</li>
					<li>跌幅 ≤ {theme.weak_threshold.toFixed(1)}% 的核心股不超过 {theme.maximum_weak_peers} 只</li>
				</ul>
			</article>
		</div>
		{quantitative.peers?.length > 0 && <div className="stock-ai-short-peer-list"><span>同板块验证个股</span><div>{quantitative.peers.map((peer) => <article key={`${peer.symbol}-${peer.name}`}><strong>{peer.name}<small>{peer.symbol}</small></strong><em>{peer.streak > 0 ? `${peer.streak}板` : peer.role}</em>{peer.has_quote && <span>基线 {signedPercent(peer.change_percent)}</span>}</article>)}</div></div>}
		{(quantitative.missing || []).length > 0 && <p className="stock-ai-short-quant-missing">数据缺项：{quantitative.missing?.join('；')}</p>}
	</section>;
}

function ShortTermStageCard({ stage, icon }: { stage: StockAIShortTermDecisionStage; icon: ReactNode }) {
	return <article className="stock-ai-short-stage">
		<header>{icon}<div><span>{stage.label}</span><strong>{stage.status}</strong></div></header>
		<p>{stage.summary}</p>
		<div><section><strong>必须观察</strong><ul>{(stage.required || []).map((item) => <li key={item}>{item}</li>)}</ul></section><section className="avoid"><strong>否决信号</strong><ul>{(stage.avoid || []).map((item) => <li key={item}>{item}</li>)}</ul></section></div>
	</article>;
}

function ActionPriceCard({ tone, zone }: { tone: 'entry' | 'hold' | 'take-profit' | 'stop-loss'; zone: StockAIActionPriceZone }) {
	return <article className={`stock-ai-price-decision ${tone}`}>
		<div className="stock-ai-price-decision-head"><span>{zone.label}</span><strong>{zone.price_text}</strong></div>
		<div className="stock-ai-price-decision-reason"><em>{zone.label.replace(/价格$/, '')}原因</em><p>{zone.reason}</p></div>
		<div className="stock-ai-price-decision-action"><Flag size={13} /><p>{zone.action}</p></div>
	</article>;
}

function resolveActionPricePlan(analysis: StockAIAnalysis) {
	const plan = analysis.action_plan;
	if (isShortTermDecision(analysis)) return null;
	if (!plan.entry?.price_text || !plan.hold?.price_text) return null;
	const stopPrice = analysis.risk_control.stop_price;
	const currentPrice = Number(analysis.quote?.price || analysis.trend.latest_close || 0);
	const plannedEntry = (plan.entry.price_low + plan.entry.price_high) / 2;
	const riskDistance = Math.max(plannedEntry - stopPrice, plannedEntry * 0.01);
	const minimumForwardTarget = currentPrice > 0 ? currentPrice + Math.max(currentPrice * 0.01, 0.01) : 0;
	const firstTarget = Math.max(plannedEntry + riskDistance, plan.entry.price_high + Math.max(plannedEntry * 0.005, 0.01), minimumForwardTarget);
	const secondTarget = Math.max(plannedEntry + riskDistance * 2, firstTarget + riskDistance, currentPrice + Math.max(currentPrice * 0.02, 0.02));
	const hasForwardTakeProfit = !!plan.take_profit?.price_text && plan.take_profit.price_low > plan.entry.price_high && (!currentPrice || plan.take_profit.price_low > currentPrice);
	const takeProfit = hasForwardTakeProfit ? plan.take_profit : firstTarget > 0 ? {
		label: '止盈价格',
		price_low: firstTarget,
		price_high: secondTarget,
		price_text: formatDecisionPriceRange(firstTarget, secondTarget),
		reason: currentPrice >= plan.entry.price_high ? `现价${currentPrice.toFixed(2)}已高于原计划介入区，止盈区改按现价上方的趋势延伸空间重算；第一目标${firstTarget.toFixed(2)}，第二目标${secondTarget.toFixed(2)}。` : `以允许介入区间中枢${plannedEntry.toFixed(2)}为计划成本、${stopPrice.toFixed(2)}为止损，第一目标${firstTarget.toFixed(2)}参考约1R，第二目标${secondTarget.toFixed(2)}参考约2R。`,
		action: currentPrice >= plan.entry.price_high ? '已有仓位分批兑现并上移保护位；新仓不追高，等待回踩或重新确认。' : '到达第一目标后分批兑现；若量价保持强势可保留部分仓位，接近第二目标不再盲目加仓。',
	} : undefined;
	const stopSource = plan.stop_loss?.price_text ? plan.stop_loss : plan.forbidden;
	const stopLoss = stopSource?.price_text ? stopSource : stopPrice > 0 ? {
		label: '止损价格',
		price_low: 0,
		price_high: stopPrice,
		price_text: `≤ ${stopPrice.toFixed(2)} 元`,
		reason: `${stopPrice.toFixed(2)}为计划失效位，跌破说明原有承接和趋势假设已经失效。`,
		action: '触发后停止介入，已有仓位执行减仓或止损，不在止损位下方补仓。',
	} : undefined;
	if (!takeProfit || !stopLoss) return null;
	return {
		entry: { ...plan.entry, label: '允许介入价格' },
		hold: { ...plan.hold, label: '持有价格' },
		takeProfit: { ...takeProfit, label: '止盈价格' },
		stopLoss: { ...stopLoss, label: '止损价格' },
	};
}

function isShortTermDecision(analysis: StockAIAnalysis) {
	const mode = analysis.action_plan?.decision_mode;
	if (mode) return mode === 'short_term';
	return analysis.profile.primary_type === 'emotion_leader';
}

function formatDecisionPriceRange(low: number, high: number) {
	return Math.abs(high - low) < 0.005 ? `${low.toFixed(2)} 元` : `${low.toFixed(2)}—${high.toFixed(2)} 元`;
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

function TrendChart({ points, newListing = false }: { points: StockAITrendPoint[]; newListing?: boolean }) {
	const geometry = useMemo(() => buildChartGeometry(points), [points]);
	if (!geometry) return <div className="stock-ai-chart-empty">趋势数据不足</div>;
	return (
		<div className="stock-ai-chart-wrap">
			<svg viewBox="0 0 760 260" role="img" aria-label={newListing ? '个股上市期价格图' : '个股近120日趋势图'}>
				{[0, 1, 2, 3].map((row) => <line x1="42" x2="744" y1={30 + row * 58} y2={30 + row * 58} key={row} />)}
				{!newListing && <><polyline className="ma120" points={geometry.ma120} /><polyline className="ma60" points={geometry.ma60} /><polyline className="ma20" points={geometry.ma20} /></>}
				<polyline className="close" points={geometry.close} />
				{points.length === 1 && <circle className="close-point" cx={geometry.lastX} cy={geometry.lastY} r="4" />}
				<text x="5" y="34">{geometry.max.toFixed(2)}</text>
				<text x="5" y="208">{geometry.min.toFixed(2)}</text>
			</svg>
			<div className="stock-ai-chart-legend"><span className="close">收盘</span>{!newListing && <><span className="ma20">MA20</span><span className="ma60">MA60</span><span className="ma120">MA120</span></>}<em>{points[0]?.date} — {points.at(-1)?.date}</em></div>
		</div>
	);
}

function buildChartGeometry(points: StockAITrendPoint[]) {
	if (points.length < 1) return null;
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
	const lastIndex = points.length - 1;
	return { min, max, close: line((point) => point.close), ma20: line((point) => point.ma20), ma60: line((point) => point.ma60), ma120: line((point) => point.ma120), lastX: x(lastIndex), lastY: y(points[lastIndex].close) };
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
	if (isShortTermDecision(analysis)) {
		const playbook = analysis.action_plan.short_term_playbook;
		return [
			`${analysis.name}（${analysis.symbol}）短线次日作战计划`,
			`决策模型：${analysis.action_plan.decision_label || '超短次日作战'}；周期：${analysis.action_plan.horizon || '隔日 / 1—3个交易日'}`,
			`当前动作：${analysis.action_plan.current_action}`,
			`盘后结论：${playbook?.overnight_conclusion || analysis.conclusion.summary}`,
			...(playbook?.quantitative ? buildShortTermQuantitativeText(playbook.quantitative) : []),
			`竞价确认：${playbook?.auction?.status || '待9:25竞价确认'}；${playbook?.auction?.summary || '观察竞价强度、题材同步性和预期差'}`,
			`竞价必要条件：${(playbook?.auction?.required || analysis.action_plan.entry_conditions || []).join('；')}`,
			`开盘确认：${playbook?.opening?.status || '待9:30—9:35确认'}；${playbook?.opening?.summary || '观察开盘承接与板块反馈'}`,
			`允许参与：${(playbook?.participation_conditions || analysis.action_plan.entry_conditions || []).join('；')}`,
			`持有条件：${(playbook?.hold_conditions || analysis.action_plan.hold_conditions || []).join('；')}`,
			`退出条件：${(playbook?.exit_conditions || []).join('；')}`,
			`一票否决：${(playbook?.veto_conditions || analysis.action_plan.avoid_conditions || []).join('；')}`,
			`仓位：${analysis.action_plan.position_hint}`,
			`主要风险：${analysis.conclusion.main_risk}`,
			`生成时间：${analysis.generated_at}`,
		].join('\n');
	}
	const resolvedPricePlan = resolveActionPricePlan(analysis);
	const pricePlan = resolvedPricePlan
		? [
			`允许介入价格：${resolvedPricePlan.entry.price_text}；允许介入原因：${resolvedPricePlan.entry.reason}`,
			`持有价格：${resolvedPricePlan.hold.price_text}；持有原因：${resolvedPricePlan.hold.reason}`,
			`止盈价格：${resolvedPricePlan.takeProfit.price_text}；止盈原因：${resolvedPricePlan.takeProfit.reason}`,
			`止损价格：${resolvedPricePlan.stopLoss.price_text}；止损原因：${resolvedPricePlan.stopLoss.reason}`,
		]
		: [
			`允许介入：${(analysis.action_plan.entry_conditions || []).join('；')}`,
			`禁止条件：${(analysis.action_plan.avoid_conditions || []).join('；')}`,
		];
	return [
		`${analysis.name}（${analysis.symbol}）个股AI分析`,
		`综合评分：${analysis.scorecard.overall} / ${analysis.scorecard.grade} / ${analysis.scorecard.direction}`,
		`画像：${analysis.profile.type_label} / ${analysis.profile.price_phase} / ${analysis.profile.market_role}`,
		`结论：${analysis.conclusion.headline}`,
		`隔日预期：${analysis.next_day.bias}；${analysis.next_day.expectation}`,
		...pricePlan,
		`风控：参考价${analysis.risk_control.entry_reference}，止损${analysis.risk_control.stop_price}，目标${analysis.risk_control.take_profit_first}/${analysis.risk_control.take_profit_second}，仓位${analysis.risk_control.suggested_position_min_percent}%—${analysis.risk_control.suggested_position_max_percent}%`,
		`主要风险：${analysis.conclusion.main_risk}`,
		`生成时间：${analysis.generated_at}`,
	].join('\n');
}

function buildShortTermQuantitativeText(quantitative: StockAIShortTermQuantitativePlan) {
	const stock = quantitative.stock;
	const benchmark = quantitative.benchmark;
	const theme = quantitative.theme;
	const peers = (quantitative.peers || []).map((peer) => `${peer.name}${peer.symbol ? `（${peer.symbol}）` : ''}${peer.streak > 0 ? ` ${peer.streak}板` : peer.role ? ` ${peer.role}` : ''}`).join('、') || '缺少名单';
	return [
		`个股量化：竞价${signedPercent(stock.auction_change_min)}—${signedPercent(stock.auction_change_max)}，价格${formatPriceRange(stock.auction_price_min, stock.auction_price_max)}，竞价额${formatCompactAmount(stock.auction_amount_min)}—${formatCompactAmount(stock.auction_amount_max)}，9:35回撤≤${stock.opening_drawdown_max.toFixed(1)}%，累计额≥${formatCompactAmount(stock.opening_amount_min)}`,
		`指数条件：${benchmark.name}（${benchmark.symbol}）9:25≥${signedPercent(benchmark.auction_change_min)}，9:35≥${signedPercent(benchmark.opening_change_min)}，环境否决≤${signedPercent(benchmark.failure_change)}`,
		`题材条件：${theme.name}基线涨停${theme.limit_up_count}只、连板${theme.board_count}只、最高${theme.max_streak}板；核心股至少${theme.minimum_positive_peers}只不低于0%，≤${theme.weak_threshold.toFixed(1)}%的不超过${theme.maximum_weak_peers}只`,
		`同板块验证个股：${peers}`,
	];
}

function formatPriceRange(low: number, high: number) {
	if (!(low > 0) || !(high > 0)) return '数据不足';
	return `${low.toFixed(2)}—${high.toFixed(2)}元`;
}

function canvasToPNGBlob(canvas: HTMLCanvasElement) {
	return new Promise<Blob>((resolve, reject) => {
		canvas.toBlob((blob) => blob ? resolve(blob) : reject(new Error('浏览器未能生成 PNG 图片')), 'image/png');
	});
}

function buildAnalysisImageFilename(analysis: StockAIAnalysis, mode: StockAIWorkspaceMode) {
	const date = new Date();
	const datePart = `${date.getFullYear()}${String(date.getMonth() + 1).padStart(2, '0')}${String(date.getDate()).padStart(2, '0')}`;
	const stockName = sanitizeFilename(analysis.name || '个股');
	const symbol = sanitizeFilename(analysis.symbol || 'A股');
	return `easy-stock-${stockName}-${symbol}-${sanitizeFilename(workspaceModeLabel(mode))}-${datePart}.png`;
}

function sanitizeFilename(value: string) {
	return value.replace(/[\\/:*?"<>|]/g, '-').replace(/\s+/g, '').slice(0, 48);
}

function workspaceModeLabel(mode: StockAIWorkspaceMode) {
	if (mode === 'expectation') return '隔日预期';
	if (mode === 'risk') return '风险与执行';
	return '个股 AI 分析';
}

function formatExportDate(value: string) {
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return value || '--';
	return date.toLocaleString('zh-CN', {
		year: 'numeric',
		month: '2-digit',
		day: '2-digit',
		hour: '2-digit',
		minute: '2-digit',
		hour12: false,
	});
}

function qualityLabel(key: string) {
	const labels: Record<string, string> = { kline: '趋势K线', technical_window: '技术窗口', quote: '实时行情', limit_up: '涨停结构', theme: '题材归因', fundamental: '基本面', research: '机构研报', stock_news: '个股新闻', theme_news: '题材新闻', market_emotion: '市场情绪', benchmark: '基准指数', collection: '数据采集' };
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

function newsToneClass(tone?: string) {
	if (tone === '偏多') return 'positive';
	if (tone === '偏空') return 'negative';
	return 'neutral';
}

function formatNewsSource(source?: string) {
	if (!source) return '新闻资讯';
	if (source.includes('announcement')) return '公司公告';
	if (source.includes('cls')) return '财联社';
	if (source.includes('eastmoney')) return '东方财富';
	return source;
}

function formatPrice(value: number) {
	return Number.isFinite(value) && value > 0 ? value.toFixed(2) : '--';
}

function formatMoney(value: number) {
	return Number.isFinite(value) && value > 0 ? `${value.toLocaleString('zh-CN', { maximumFractionDigits: 0 })} 元` : '--';
}

function formatShortDate(value: string) {
	const date = new Date(value);
	return Number.isNaN(date.getTime()) ? '' : date.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' });
}

function formatTargetPrice(low?: number, high?: number) {
	if (low && high) return `${low.toFixed(2)}—${high.toFixed(2)}`;
	return (high || low || 0).toFixed(2);
}
