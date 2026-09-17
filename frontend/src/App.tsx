import {
	Activity,
	BarChart3,
	Bot,
	BookMarked,
	BookOpen,
	BrainCircuit,
	ChevronRight,
	Clock3,
	Database,
	Flame,
	Gauge,
	History,
	Layers3,
	LayoutDashboard,
	LoaderCircle,
	Moon,
	Newspaper,
	PanelLeftClose,
	PanelLeftOpen,
	Radio,
	RefreshCw,
	Search,
	Server,
	Settings,
	ShieldAlert,
	ShieldCheck,
	Sun,
	Target,
	Wifi,
	WalletCards,
} from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
	BackendConfig,
	KLine,
	NewsItem,
	Quote,
	SectorMap,
	SourceHealth,
	StreamMessage,
	ThemeOverview,
	StockAIAnalysis,
	ThemeScreenLane,
	ThemeScreenPagination,
	ThemeScreenSort,
	buildStreamUrl,
	requestJSON,
	resolveBackendConfig,
} from './lib/backend';
import {
	QuoteLookup,
	StockRole,
	ThemeStrengthWindow,
	ThemeStock,
	buildThemeStocks,
	calculateThemeEmotion,
	rankThemeOverviews,
	themeStrengthScore,
} from './lib/short-term';
import { LimitUpWorkspace } from './components/LimitUpWorkspace';
import { ReviewDiary } from './components/ReviewDiary';
import { SettingsDrawer } from './components/SettingsDrawer';
import { AIChatWorkspace } from './components/AIChatWorkspace';
import { MarketOverviewWorkspace } from './components/MarketOverviewWorkspace';
import { TradingMastery } from './components/TradingMastery';
import { StockAIAnalysisWorkspace, StockAIWorkspaceMode } from './components/StockAIAnalysisWorkspace';
import { PortfolioInspectionWorkspace } from './components/PortfolioInspectionWorkspace';
import { TokenUsageWorkspace } from './components/TokenUsageWorkspace';
import { logRuntimeEvent } from './lib/runtime-log';
import { useTheme } from './lib/theme';
import { useLimitUpWorkspace } from './lib/use-limit-up-workspace';
import { useThemeOverview } from './lib/use-theme-overview';
import { useThemeConstituents, sameTheme } from './lib/use-theme-constituents';
import { useThemeKLines } from './lib/use-theme-klines';

type LoadState = 'idle' | 'loading' | 'ready' | 'error';
type WorkspaceMode = 'themes' | 'limit-up' | 'mastery' | 'reviews' | 'stock-ai' | 'portfolio-inspection' | 'ai' | 'market' | 'token-usage';

const emptyStockPagination = (): ThemeScreenPagination => ({
	page: 1,
	page_size: 20,
	total: 0,
	total_pages: 0,
	has_more: false,
});

export function App() {
	const { theme, toggleTheme } = useTheme();
	const [workspaceMode, setWorkspaceMode] = useState<WorkspaceMode>(() => {
		if (window.location.hash === '#limit-up') return 'limit-up';
		if (window.location.hash === '#mastery') return 'mastery';
		if (window.location.hash === '#reviews') return 'reviews';
		if (window.location.hash === '#stock-ai') return 'stock-ai';
		if (window.location.hash === '#portfolio-inspection') return 'portfolio-inspection';
		if (window.location.hash === '#ai') return 'ai';
		if (window.location.hash.startsWith('#market')) return 'market';
		if (window.location.hash === '#token-usage') return 'token-usage';
		return 'themes';
	});
	const [sidebarExpanded, setSidebarExpanded] = useState(true);
	const [config, setConfig] = useState<BackendConfig | null>(null);
	const [themeStrengthWindow, setThemeStrengthWindow] = useState<ThemeStrengthWindow>('daily');
	const [activeTheme, setActiveTheme] = useState('');
	const [selectedNode, setSelectedNode] = useState('all');
	const [selectedSymbol, setSelectedSymbol] = useState('');
	const [liveQuotes, setLiveQuotes] = useState<QuoteLookup>({});
	const [sources, setSources] = useState<SourceHealth[]>([]);
	const [news, setNews] = useState<NewsItem[]>([]);
	const [streamStatus, setStreamStatus] = useState('实时流待命');
	const [configError, setConfigError] = useState('');
	const [themeRefreshKey, setThemeRefreshKey] = useState(0);
	const [newsError, setNewsError] = useState('');
	const [newsRetryKey, setNewsRetryKey] = useState(0);
	const [sourcesRetryKey, setSourcesRetryKey] = useState(0);
	const [sourcesError, setSourcesError] = useState('');
	const [stockQuery, setStockQuery] = useState('');
	const [debouncedStockQuery, setDebouncedStockQuery] = useState('');
	const [stockPage, setStockPage] = useState(1);
	const [stockSort, setStockSort] = useState<ThemeScreenSort>('rank_score');
	const [stockLane, setStockLane] = useState<ThemeScreenLane>('all');
	const limitUp = useLimitUpWorkspace(config, workspaceMode === 'limit-up');
	const { data: limitUpData, state: limitUpState, error: limitUpError } = limitUp.ladder;
	const { data: marketEmotionData, state: marketEmotionState, error: marketEmotionError } = limitUp.history;
	const refreshLimitUpWorkspace = limitUp.refresh;
	const [reviewRefreshKey, setReviewRefreshKey] = useState(0);
	const [masteryRefreshKey, setMasteryRefreshKey] = useState(0);
	const [stockAIRefreshKey, setStockAIRefreshKey] = useState(0);
	const [portfolioInspectionRefreshKey, setPortfolioInspectionRefreshKey] = useState(0);
	const [stockAIWorkspaceMode, setStockAIWorkspaceMode] = useState<StockAIWorkspaceMode>('analysis');
	const [stockAIInitialAnalysis, setStockAIInitialAnalysis] = useState<StockAIAnalysis | null>(null);
	const [aiRefreshKey, setAIRefreshKey] = useState(0);
	const [marketRefreshKey, setMarketRefreshKey] = useState(0);
	const [aiPrefill, setAIPrefill] = useState('');
	const [aiAnalysisID, setAIAnalysisID] = useState<string | undefined>();
	const [settingsOpen, setSettingsOpen] = useState(false);
	const [tokenUsageRefreshKey, setTokenUsageRefreshKey] = useState(0);

	const overview = useThemeOverview(config, workspaceMode === 'themes');
	const themeOverviews = overview.data;
	const overviewMeta = overview.meta;
	const foundationState = overview.state;
	const activeOverviewRef = useRef<{ selection: string; data: ThemeOverview } | null>(null);
	const previousOverview = activeOverviewRef.current?.selection === activeTheme ? activeOverviewRef.current.data : null;
	const activeOverview = themeOverviews.find(item => sameTheme(item, activeTheme))
		|| themeOverviews.find(item => previousOverview && (sameTheme(previousOverview, item.theme) || item.aliases?.includes(previousOverview.theme)))
		|| previousOverview;
	if (activeOverview) activeOverviewRef.current = { selection: activeTheme, data: activeOverview };
	const rankedThemes = useMemo(() => {
		const items = rankThemeOverviews(themeOverviews, themeStrengthWindow).slice(0, 16);
		if (activeOverview && !items.some(item => item.theme === activeOverview.theme)) return [...items.slice(0, 15), activeOverview];
		return items;
	}, [themeOverviews, themeStrengthWindow, activeOverview]);
	const activeStrengthScore = activeOverview && !activeOverview.provisional ? themeStrengthScore(activeOverview, themeStrengthWindow) : null;
	const activeEmotion = activeOverview && activeStrengthScore !== null ? calculateThemeEmotion(activeOverview, activeStrengthScore) : null;
	const isTrendOverview = typeof activeOverview?.trend_score === 'number';
	const isKaipanlaOverview = activeOverview?.theme.startsWith('kpl:') || activeOverview?.theme.startsWith('fusion:') || false;
	const constituents = useThemeConstituents(config, workspaceMode === 'themes', activeOverview, { page: stockPage, node: selectedNode, lane: stockLane, sort: stockSort, query: debouncedStockQuery }, themeRefreshKey, overview.refresh);
	const sectorMap = constituents.data?.map || null;
	const stockPagination = constituents.data?.pagination || emptyStockPagination();
	const themeState = constituents.status;
	const constituentsComplete = constituents.data?.complete === true;
	const baseThemeStocks = useMemo(() => {
		const order = new Map((constituents.data?.order || []).map((symbol, index) => [symbol, index]));
		return buildThemeStocks(sectorMap).sort((a,b) => (order.get(a.symbol) ?? 999) - (order.get(b.symbol) ?? 999));
	}, [sectorMap, constituents.data?.order]);
	const selectedBaseRef = useRef<{ theme: string; stock: ThemeStock } | null>(null);
	const selectedBase = baseThemeStocks.find(stock => stock.symbol === selectedSymbol)
		|| (selectedBaseRef.current?.theme === activeTheme && selectedBaseRef.current.stock.symbol === selectedSymbol ? selectedBaseRef.current.stock : null)
		|| baseThemeStocks[0] || null;
	if (selectedBase) selectedBaseRef.current = { theme: activeTheme, stock: selectedBase };
	const historySymbols = useMemo(() => baseThemeStocks.map(stock => stock.symbol), [baseThemeStocks]);
	const prefetchSymbols = useMemo(() => rankedThemes.filter(item => item.theme !== activeOverview?.theme).slice(0, 2).flatMap(item => item.leader_stocks?.slice(0, 5).map(stock => stock.symbol) || []), [rankedThemes, activeOverview?.theme]);
	const history = useThemeKLines(config, workspaceMode === 'themes', historySymbols, selectedBase?.symbol || '', prefetchSymbols, themeRefreshKey);
	const leadershipHistories = history.histories;
	const historyErrorSymbols = history.failed;
	const historyState = history.historyState;
	const historyReadyCount = history.ready;
	const kLines = history.lines;
	const klineState = history.klineState;
	const themeStocks = useMemo(() => {
		if (!sectorMap || !selectedBase || baseThemeStocks.some(stock => stock.symbol === selectedBase.symbol)) return buildThemeStocks(sectorMap, liveQuotes, leadershipHistories);
		const withSelection = { ...sectorMap, groups: [...sectorMap.groups, { id: 'selected', name: '当前选择', nodes: [{ id: 'selected', name: selectedBase.nodes[0] || '当前选择', change_percent: 0, main_net_inflow: 0, match_status: 'matched', stocks: [selectedBase] }] }] };
		return buildThemeStocks(withSelection, liveQuotes, leadershipHistories);
	}, [leadershipHistories, liveQuotes, sectorMap, selectedBase, baseThemeStocks]);
	// The server orders the complete pool. Keep rows stable as individual metrics arrive.
	const visibleStocks = useMemo(() => {
		const order = new Map(baseThemeStocks.map((stock, index) => [stock.symbol, index]));
		return themeStocks.filter(stock => order.has(stock.symbol)).sort((a, b) => (order.get(a.symbol) ?? 0) - (order.get(b.symbol) ?? 0));
	}, [baseThemeStocks, themeStocks]);
	const selectedStock = themeStocks.find(stock => stock.symbol === selectedBase?.symbol) || selectedBase;
	const selectedHistoryReady = Boolean(selectedStock && leadershipHistories[selectedStock.symbol]?.length);
	const selectedHistoryFailed = Boolean(selectedStock && historyErrorSymbols.has(selectedStock.symbol));
	const themeNodes = useMemo(() => sectorMap?.groups.flatMap(group => group.nodes) || [], [sectorMap]);
	const streamSymbols = useMemo(() => [...new Set([...historySymbols, selectedStock?.symbol || ''].filter(Boolean))], [historySymbols, selectedStock?.symbol]);
	const streamKey = streamSymbols.join(',');
	const marketPulse = useMemo(() => {
		if (!rankedThemes.length) {
			return { average: 0, active: 0 };
		}
		const scores = rankedThemes.filter(theme => !theme.provisional).map(theme => themeStrengthScore(theme, themeStrengthWindow));
		if (!scores.length) return { average: 0, active: 0 };
		return {
			average: Math.round(scores.reduce((total, score) => total + score, 0) / scores.length),
			active: scores.filter((score) => score >= 60).length,
		};
	}, [rankedThemes, themeStrengthWindow]);

	useEffect(() => {
		resolveBackendConfig()
			.then(setConfig)
			.catch((error) => {
				setConfigError(error instanceof Error ? error.message : '后端配置失败');
			});
	}, []);

	useEffect(() => {
		if (!config || workspaceMode !== 'themes') return;
		const abort = new AbortController();
		setSourcesError('');
		void requestJSON<{ sources: SourceHealth[] }>(config, '/api/v1/sources', { signal: abort.signal })
			.then(payload => { if (!abort.signal.aborted) setSources(payload.sources); })
			.catch(() => { if (!abort.signal.aborted) setSourcesError('数据源状态暂不可用'); });
		return () => abort.abort();
	}, [config, workspaceMode, themeRefreshKey, sourcesRetryKey]);

	useEffect(() => {
		if (!config || workspaceMode !== 'themes') return;
		const abort = new AbortController();
		setNewsError('');
		void requestJSON<{ data: NewsItem[] }>(config, '/api/v1/market/news?source=cls&limit=12', { signal: abort.signal })
			.then(payload => { if (!abort.signal.aborted) setNews(payload.data); })
			.catch(() => { if (!abort.signal.aborted) setNewsError('快讯暂不可用'); });
		return () => abort.abort();
	}, [config, workspaceMode, themeRefreshKey, newsRetryKey]);

	useEffect(() => {
		if (!activeOverview && rankedThemes.length) setActiveTheme(rankedThemes[0].theme);
	}, [activeOverview, rankedThemes]);

	useEffect(() => {
		const timer = window.setTimeout(() => { setDebouncedStockQuery(stockQuery.trim()); setStockPage(1); }, 300);
		return () => window.clearTimeout(timer);
	}, [stockQuery]);

	useEffect(() => {
		if (selectedBase && selectedSymbol !== selectedBase.symbol) setSelectedSymbol(selectedBase.symbol);
	}, [selectedBase?.symbol, selectedSymbol]);

	useEffect(() => {
		if (!config || !streamKey || workspaceMode !== 'themes') {
			setStreamStatus('实时流待命');
			return;
		}
		let disposed = false;
		const socket = new WebSocket(buildStreamUrl(config, streamSymbols, 3000));
		socket.onopen = () => setStreamStatus('实时行情已连接');
		socket.onclose = () => {
			setStreamStatus('实时行情已断开');
			if (!disposed) logRuntimeEvent('warn', 'quotes', { event: 'websocket_closed' });
		};
		socket.onerror = () => {
			setStreamStatus('实时行情异常');
			logRuntimeEvent('error', 'quotes', { event: 'websocket_failure' });
		};
		socket.onmessage = (event) => {
			const message = JSON.parse(event.data) as StreamMessage;
			if (message.type === 'quotes' && message.quotes) {
				setLiveQuotes((current) => mergeQuotes(current, message.quotes || []));
				setStreamStatus('实时行情更新中');
			}
			if (message.type === 'error') {
				setStreamStatus(message.error || '实时行情异常');
				logRuntimeEvent('warn', 'quotes', { event: 'websocket_message_error' });
			}
		};
		return () => {
			disposed = true;
			socket.close();
		};
	}, [config, streamKey, workspaceMode]);

	const refreshAll = () => {
		if (workspaceMode === 'limit-up') {
			refreshLimitUpWorkspace();
			return;
		}
		if (workspaceMode === 'reviews') {
			setReviewRefreshKey((current) => current + 1);
			return;
		}
		if (workspaceMode === 'mastery') {
			setMasteryRefreshKey((current) => current + 1);
			return;
		}
		if (workspaceMode === 'stock-ai') {
			setStockAIRefreshKey((current) => current + 1);
			return;
		}
		if (workspaceMode === 'portfolio-inspection') {
			setPortfolioInspectionRefreshKey((current) => current + 1);
			return;
		}
		if (workspaceMode === 'ai') {
			setAIRefreshKey((current) => current + 1);
			return;
		}
		if (workspaceMode === 'market') {
			setMarketRefreshKey((current) => current + 1);
			return;
		}
		if (workspaceMode === 'token-usage') {
			setTokenUsageRefreshKey((current) => current + 1);
			return;
		}
		void overview.refresh();
		setThemeRefreshKey(key => key + 1);
		history.retry();
	};

	const switchWorkspace = (mode: WorkspaceMode) => {
		setWorkspaceMode(mode);
		window.history.replaceState(null, '', mode === 'limit-up' ? '#limit-up' : mode === 'mastery' ? '#mastery' : mode === 'reviews' ? '#reviews' : mode === 'stock-ai' ? '#stock-ai' : mode === 'portfolio-inspection' ? '#portfolio-inspection' : mode === 'ai' ? '#ai' : mode === 'market' ? '#market/pulse' : mode === 'token-usage' ? '#token-usage' : '#themes');
	};

	const askMasteryAI = (traderName: string) => {
		setAIAnalysisID(undefined);
		setAIPrefill(`请基于本地游资心法知识库，系统梳理${traderName}的核心交易理念、适用市场环境、选股与买卖规则、仓位风控，并指出资料中可能存在的事后归因、占位或不可验证之处。`);
		switchWorkspace('ai');
	};

	const askStockAnalysisAI = (analysis: StockAIAnalysis) => {
		setAIAnalysisID(analysis.analysis_id);
		if (analysis.analysis_id) {
			setAIPrefill(`请基于绑定的 ${analysis.name}（${analysis.symbol}）研究报告和原始证据，挑战主判断：哪些结论证据最弱、什么新增事实会改变判断、下一步应核实什么？区分原文陈述与研究推断，沿用报告时点，不编造当前行情。`);
			switchWorkspace('ai');
			return;
		}
		const shortTermContext = analysis.action_plan.decision_mode === 'short_term'
			? `短线决策：${analysis.action_plan.decision_label || '超短次日作战'}；盘后结论：${analysis.action_plan.short_term_playbook?.overnight_conclusion || analysis.conclusion.summary}；竞价状态：${analysis.action_plan.short_term_playbook?.auction?.status || '待9:25确认'}；一票否决：${(analysis.action_plan.short_term_playbook?.veto_conditions || analysis.action_plan.avoid_conditions || []).join('、')}`
			: `非短线决策：${analysis.action_plan.decision_label || '趋势与价值定价'}；允许介入：${analysis.action_plan.entry?.price_text || '--'}；止盈：${analysis.action_plan.take_profit?.price_text || '--'}；止损：${analysis.action_plan.stop_loss?.price_text || '--'}`;
		setAIPrefill(`请继续推演 ${analysis.name}（${analysis.symbol}）的个股AI分析。当前画像：${analysis.profile.type_label} / ${analysis.profile.price_phase} / ${analysis.profile.market_role}；综合评分 ${analysis.scorecard.overall}（${analysis.scorecard.direction}）；趋势得分 ${analysis.trend.score}；短线状态 ${analysis.short_term.state}；${shortTermContext}；当前动作：${analysis.action_plan.current_action}。请重点挑战现有结论，给出支持证据、反对证据、最优验证路径和失效条件；如果是短线票，围绕竞价与开盘条件推演，不要把盘后静态价格当成确定买点，也不要编造实时数据。`);
		switchWorkspace('ai');
	};

	const openPortfolioStockAnalysis = (analysis: StockAIAnalysis) => {
		setStockAIInitialAnalysis(analysis);
		setStockAIWorkspaceMode('analysis');
		switchWorkspace('stock-ai');
	};

	const askMarketAI = (prompt: string) => {
		setAIAnalysisID(undefined);
		setAIPrefill(prompt);
		switchWorkspace('ai');
	};

	const activateTheme = (theme: string) => {
		if (theme === activeTheme) {
			return;
		}
		setSelectedNode('all');
		setStockPage(1);
		setStockQuery('');
		setDebouncedStockQuery('');
		setStockSort('rank_score');
		setStockLane('all');
		setSelectedSymbol('');
		setLiveQuotes({});
		setActiveTheme(theme);
	};

	const statusText = configError || (overview.fetching || constituents.fetching || historyState === 'loading' ? '部分数据更新中' : overview.error || constituents.error || historyErrorSymbols.size ? '部分数据暂不可用' : '题材数据已更新');
	const currentLoadState = workspaceMode === 'limit-up' ? limitUpState : workspaceMode === 'mastery' || workspaceMode === 'reviews' || workspaceMode === 'stock-ai' || workspaceMode === 'portfolio-inspection' || workspaceMode === 'ai' || workspaceMode === 'market' || workspaceMode === 'token-usage' ? 'ready' : foundationState;
	const currentStatusText = workspaceMode === 'limit-up'
		? limitUpState === 'loading' || marketEmotionState === 'loading' ? '部分数据更新中' : limitUpState === 'error' || marketEmotionState === 'error' ? '部分数据暂不可用' : '连板结构已更新'
		: workspaceMode === 'mastery' ? '游资心法库已连接' : workspaceMode === 'reviews' ? '复盘资料库已连接' : workspaceMode === 'stock-ai' ? '个股分析引擎已连接' : workspaceMode === 'portfolio-inspection' ? '持仓巡检引擎已连接' : workspaceMode === 'ai' ? 'AI 助手已连接' : workspaceMode === 'market' ? '行情数据层已连接' : workspaceMode === 'token-usage' ? 'Token 统计已连接' : statusText;
	const themeSourceStatus = overviewMeta?.source === 'theme-radar:fusion'
		? overviewMeta.carry_forward ? '行业趋势 · 开盘啦衰减融合' : '行业趋势 · 开盘啦融合'
		: overviewMeta?.source === 'duanxianxia:kaipanla'
			? overviewMeta.carry_forward ? '沿用 ' + (overviewMeta.trade_date || '上一交易日') + ' 开盘啦' : (overviewMeta.trade_date || '当日') + ' 开盘啦'
			: '行业趋势强度';
	const currentSubStatus = workspaceMode === 'limit-up'
		? limitUpData ? `${limitUpData.current.trade_date} · ${limitUpData.session_status} · ${limitUpData.meta.source.includes('duanxianxia') ? '开盘啦涨停池' : '东方财富兜底'} · ${limitUpData.concept_status === 'ready' ? '题材已归因' : limitUpState === 'loading' ? '题材补充中' : '题材暂不完整'}` : '开盘啦涨停池优先'
		: workspaceMode === 'mastery' ? 'GitHub 原始资料 · 每日缓存 · Hermes 本地知识库' : workspaceMode === 'reviews' ? '雪球 · 淘股吧 · 微信公众号' : workspaceMode === 'stock-ai' ? '多周期评分 · 基准超额 · 隔日情景 · 动态风控' : workspaceMode === 'portfolio-inspection' ? '逐股分析 · 组合风险 · 后台任务' : workspaceMode === 'ai' ? '本机 Hermes AI 对话' : workspaceMode === 'market' ? '全球指数 · 行业资金 · 龙虎榜 · 公告研报' : workspaceMode === 'token-usage' ? '模型输入、输出与功能模块消耗' : themeSourceStatus + ' · ' + streamStatus;
	const topbarTitle = workspaceMode === 'themes' ? '趋势题材雷达' : workspaceMode === 'limit-up' ? '短线连板雷达' : workspaceMode === 'mastery' ? '游资心法库' : workspaceMode === 'reviews' ? '大V复盘日记' : workspaceMode === 'stock-ai' ? '个股 AI 分析' : workspaceMode === 'portfolio-inspection' ? '持仓 AI 巡检' : workspaceMode === 'market' ? '行情总览' : workspaceMode === 'token-usage' ? 'Token 统计' : 'AI 对话';
	const topbarDescription = workspaceMode === 'themes' ? '炒作主线、趋势强度、个股梯队与日 K 联动工作台' : workspaceMode === 'limit-up' ? '连板高度、炒作概念与晋级结构工作台' : workspaceMode === 'mastery' ? '阅读不同游资的交易经验，并由 Hermes 按原文辅助研读' : workspaceMode === 'reviews' ? '多平台复盘内容、作者观点与原文归档工作台' : workspaceMode === 'stock-ai' ? '多周期评分、隔日情景推演与账户级风控执行工作台' : workspaceMode === 'portfolio-inspection' ? '逐股研判、集中度识别与组合风险巡检工作台' : workspaceMode === 'market' ? '从盘面快讯到资金与研究信号的统一行情工作台' : workspaceMode === 'token-usage' ? '按日、按月和功能模块查看模型 Token 消耗' : '像 Codex 一样持续协作、拆解问题并形成可执行结果';

	return (
		<main className={`workspace-frame ${sidebarExpanded ? 'sidebar-expanded' : 'sidebar-collapsed'}`}>
			<aside className="app-sidebar" aria-label="功能导航">
				<div className="sidebar-brand"><div className="sidebar-logo"><img src={`${import.meta.env.BASE_URL}easy-stock-mark.svg`} alt="easy-stock" /></div>{sidebarExpanded && <div><strong>easy-stock</strong><span>AI STOCK LAB</span></div>}</div>
				<nav>
					<button type="button" className={workspaceMode === 'reviews' ? 'active' : ''} onClick={() => switchWorkspace('reviews')} title="大V复盘日记"><BookOpen size={18} /><span>大V复盘日记</span></button>
					<button type="button" className={workspaceMode === 'stock-ai' ? 'active' : ''} onClick={() => switchWorkspace('stock-ai')} title="个股分析"><BrainCircuit size={18} /><span>个股分析</span></button>
					<button type="button" className={workspaceMode === 'portfolio-inspection' ? 'active' : ''} onClick={() => switchWorkspace('portfolio-inspection')} title="持仓AI巡检"><WalletCards size={18} /><span>持仓AI巡检</span></button>
					<button type="button" className={workspaceMode === 'limit-up' ? 'active' : ''} onClick={() => switchWorkspace('limit-up')} title="短线连板"><Flame size={18} /><span>短线连板</span></button>
					<button type="button" className={workspaceMode === 'themes' ? 'active' : ''} onClick={() => switchWorkspace('themes')} title="趋势题材"><LayoutDashboard size={18} /><span>趋势题材</span></button>
					<button type="button" className={workspaceMode === 'market' ? 'active' : ''} onClick={() => switchWorkspace('market')} title="行情总览"><BarChart3 size={18} /><span>行情总览</span></button>
					<button type="button" className={workspaceMode === 'mastery' ? 'active' : ''} onClick={() => switchWorkspace('mastery')} title="游资心法"><BookMarked size={18} /><span>游资心法</span></button>
				<button type="button" className={workspaceMode === 'ai' ? 'active' : ''} onClick={() => switchWorkspace('ai')} title="AI 对话"><Bot size={18} /><span>AI 对话</span></button>
				</nav>
				<div className="sidebar-bottom-actions">
					<button type="button" className={`sidebar-settings ${workspaceMode === 'token-usage' ? 'active' : ''}`} onClick={() => switchWorkspace('token-usage')} aria-label="打开Token统计" title="Token统计"><BarChart3 size={17} />{sidebarExpanded && <span>Token统计</span>}</button>
					<button type="button" className="sidebar-settings" onClick={() => setSettingsOpen(true)} aria-label="打开系统设置" title="系统设置"><Settings size={17} />{sidebarExpanded && <span>系统设置</span>}</button>
					<button type="button" className="sidebar-toggle" onClick={() => setSidebarExpanded((value) => !value)} aria-label={sidebarExpanded ? '收起侧边栏' : '展开侧边栏'}>{sidebarExpanded ? <PanelLeftClose size={17} /> : <PanelLeftOpen size={17} />} {sidebarExpanded && <span>收起侧栏</span>}</button>
				</div>
			</aside>
			<div className="app-shell">
			<header className="topbar">
				<div className="brand-block">
					<div className="brand-mark"><img src={`${import.meta.env.BASE_URL}easy-stock-mark.svg`} alt="easy-stock" /></div>
					<div>
						<h1>{topbarTitle}</h1>
						<p>{topbarDescription}</p>
					</div>
				</div>
				<nav className="mode-nav" aria-label="工作台模式">
					{workspaceMode === 'token-usage' ? <button type="button" className="active"><BarChart3 size={16} aria-hidden="true" />Token统计</button> : null}
					{workspaceMode === 'token-usage' ? null : workspaceMode === 'stock-ai' ? <>
						<button type="button" className={stockAIWorkspaceMode === 'analysis' ? 'active' : ''} onClick={() => setStockAIWorkspaceMode('analysis')}><BrainCircuit size={16} aria-hidden="true" />个股分析</button>
						<button type="button" className={stockAIWorkspaceMode === 'expectation' ? 'active' : ''} onClick={() => setStockAIWorkspaceMode('expectation')}><Target size={16} aria-hidden="true" />隔日预期</button>
						<button type="button" className={stockAIWorkspaceMode === 'risk' ? 'active' : ''} onClick={() => setStockAIWorkspaceMode('risk')}><ShieldCheck size={16} aria-hidden="true" />风控执行</button>
					</> : <>
						<button type="button" className="active">{workspaceMode === 'mastery' ? <BookMarked size={16} aria-hidden="true" /> : workspaceMode === 'reviews' ? <BookOpen size={16} aria-hidden="true" /> : workspaceMode === 'portfolio-inspection' ? <WalletCards size={16} aria-hidden="true" /> : workspaceMode === 'ai' ? <Bot size={16} aria-hidden="true" /> : workspaceMode === 'market' ? <BarChart3 size={16} aria-hidden="true" /> : <Flame size={16} aria-hidden="true" />}{workspaceMode === 'themes' ? '趋势题材' : workspaceMode === 'limit-up' ? '短线连板' : workspaceMode === 'mastery' ? '游资心法' : workspaceMode === 'reviews' ? '复盘日记' : workspaceMode === 'portfolio-inspection' ? '持仓巡检' : workspaceMode === 'market' ? '行情总览' : 'AI 对话'}</button>
						<button type="button" disabled><Target size={16} aria-hidden="true" />隔日预期</button>
						<button type="button" disabled><ShieldCheck size={16} aria-hidden="true" />风控执行</button>
					</>}
				</nav>
				<div className="top-actions">
					<div className={`data-status ${currentLoadState}`}>
						<span className="status-dot" />
						<div><strong>{currentStatusText}</strong><small>{currentSubStatus}</small></div>
					</div>
					<button type="button" className="icon-button" onClick={refreshAll} aria-label="刷新全部数据">
						<RefreshCw size={18} aria-hidden="true" />
					</button>
					<button type="button" className="icon-button theme-toggle" onClick={toggleTheme} aria-label={theme === 'light' ? '切换至深色模式' : '切换至浅色模式'} title={theme === 'light' ? '切换至深色模式' : '切换至浅色模式'} aria-pressed={theme === 'dark'}>
						{theme === 'light' ? <Moon size={18} aria-hidden="true" /> : <Sun size={18} aria-hidden="true" />}
					</button>
				</div>
			</header>

			{workspaceMode === 'token-usage' ? <TokenUsageWorkspace config={config} refreshKey={tokenUsageRefreshKey} /> : workspaceMode === 'themes' ? <>
			<section className="market-strip" aria-label="市场概览">
				<div><Activity size={16} aria-hidden="true" /><span>主线平均热度</span><strong>{marketPulse.average || '--'}</strong></div>
				<div><Flame size={16} aria-hidden="true" /><span>活跃主线</span><strong>{rankedThemes.some(item => !item.provisional) ? marketPulse.active : '--'}</strong></div>
				<div
					className="source-health-summary"
					tabIndex={0}
					aria-label={`数据源状态，${sources.filter((source) => source.ok).length} 个正常，${sources.filter((source) => !source.ok).length} 个异常`}
				>
					<Database size={16} aria-hidden="true" />
					<span>数据源</span>
					<strong>{sources.filter((source) => source.ok).length}/{sources.length || '--'}</strong>
					<div className="source-health-popover" role="tooltip">
						<header>
							<div><strong>数据源状态</strong><span>查看当前可用情况</span></div>
							<em>{sources.filter((source) => source.ok).length} 正常 · {sources.filter((source) => !source.ok).length} 异常</em>
						</header>
						<div className="source-health-list">
							{sources.map((source) => (
								<div className={source.ok ? 'healthy' : 'unhealthy'} key={source.id}>
									<i aria-hidden="true" />
									<span><strong>{source.name}</strong><small>{sourceCategoryLabel(source.category)}</small></span>
									<em>{source.ok ? '正常' : sourceHealthMessage(source.message)}</em>
								</div>
							))}
							{!sources.length && !sourcesError && <p>数据源状态加载中…</p>}
							{sourcesError && <p className="load-notice">{sourcesError} <button type="button" onClick={() => setSourcesRetryKey(key => key + 1)}>重试</button></p>}
						</div>
					</div>
				</div>
				<div><Clock3 size={16} aria-hidden="true" /><span>题材快照</span><strong>{overviewMeta?.trade_date || formatTime(overviewMeta?.fetched_at)}</strong></div>
			</section>

			<div className="trading-layout">
				<aside className="theme-rail">
					<div className="rail-heading">
						<div className="rail-heading-copy">
							<span>近期主线</span>
							<strong>{rankedThemes.some(item => !item.provisional) ? `按${themeStrengthWindow === 'daily' ? '当日' : '5日'}强度排序` : '基础题材 · 来源顺序'}</strong>
							<div className="strength-window-toggle" role="group" aria-label="趋势强度周期">
								<button type="button" title="按题材成分股实时涨跌计算，最多每10分钟更新一次" className={themeStrengthWindow === 'daily' ? 'active' : ''} aria-pressed={themeStrengthWindow === 'daily'} onClick={() => setThemeStrengthWindow('daily')}>当日强度</button>
								<button type="button" title="按题材成分股近5个交易日累计涨跌计算，最多每10分钟更新一次" className={themeStrengthWindow === 'five_day' ? 'active' : ''} aria-pressed={themeStrengthWindow === 'five_day'} onClick={() => setThemeStrengthWindow('five_day')}>5日强度</button>
							</div>
						</div>
						<BarChart3 size={17} aria-hidden="true" />
					</div>
					<div className="theme-list">
						{rankedThemes.map((theme, index) => {
							const emotion = calculateThemeEmotion(theme, themeStrengthScore(theme, themeStrengthWindow));
							return (
								<button
									type="button"
									className={`theme-item ${theme.theme === activeOverview?.theme ? 'active' : ''}`}
									key={theme.theme}
									onClick={() => activateTheme(theme.theme)}
								>
									<span className="theme-rank">{String(index + 1).padStart(2, '0')}</span>
									<span className="theme-copy">
										<span className="theme-name-line">
											<strong>{theme.name}</strong>
										</span>
										<small>{theme.provisional ? (overview.fetching ? '基础题材 · 强度更新中' : '基础题材 · 强度未齐') : emotion.stage} · {theme.leaders?.[0] || theme.top_node || '等待主线证据'}</small>
										<span className="theme-meter"><i style={{ width: `${theme.provisional ? 0 : emotion.score}%` }} /></span>
									</span>
									<span className={`emotion-score ${emotion.tone}`}>{theme.provisional ? '--' : emotion.score}</span>
								</button>
							);
						})}
						{foundationState === 'loading' && !rankedThemes.length && <RailSkeleton />}
						{overview.error && <p className="load-notice">题材部分数据暂不可用 <button type="button" onClick={() => void overview.refresh()}>重试</button></p>}
					</div>
				</aside>

				<section className="theme-workspace">
					<header className="theme-hero">
						<div className="theme-title-row">
							<div>
								<div className="eyebrow"><Layers3 size={15} aria-hidden="true" />当前主线</div>
								<h2>{sectorMap?.name || activeOverview?.name || '加载题材中'}</h2>
								<p>{isTrendOverview
									? activeOverview?.leaders?.length ? `核心标的：${activeOverview.leaders.join(' · ')}` : '等待主线共振证据'
									: activeOverview?.top_node ? `最强细分：${activeOverview.top_node} ${formatPercent(activeOverview.top_node_change_percent)}` : '等待板块映射数据'}</p>
							</div>
							{activeEmotion && (
								<div className={`hero-emotion ${activeEmotion.tone}`}>
									<span>{isTrendOverview ? '趋势热度' : '基础情绪'}</span>
									<strong>{activeEmotion.score}</strong>
									<small>{activeEmotion.label} · {activeEmotion.stage}</small>
								</div>
							)}
						</div>
						<div className="theme-metrics">
							<Metric label={isKaipanlaOverview ? '领涨股均涨幅' : isTrendOverview ? '活跃股均涨幅' : '板块均涨幅'} value={formatPercent(activeOverview?.change_percent)} tone={toneForValue(activeOverview?.change_percent)} />
							<Metric label="上涨广度" value={activeOverview ? `${activeOverview.rising_nodes}/${activeOverview.matched_nodes}` : '--'} sub={activeEmotion ? formatRatio(activeEmotion.breadth) : undefined} />
							<Metric label={isKaipanlaOverview ? '领涨股 / 强度' : isTrendOverview ? '涨停 / 连板' : '主力净流入'} value={isKaipanlaOverview ? `${activeOverview?.leaders?.length || 0} / ${formatSourceStrength(activeOverview?.source_strength)}` : isTrendOverview ? `${activeOverview?.limit_up_count || 0} / ${activeOverview?.board_count || 0}` : formatMoney(activeOverview?.main_net_inflow)} tone={isTrendOverview ? 'flat' : toneForValue(activeOverview?.main_net_inflow)} />
							<Metric label={isKaipanlaOverview ? '上榜 / 原排名' : isTrendOverview ? '延续 / 高度' : '数据覆盖'} value={isKaipanlaOverview ? `${activeOverview?.active_days || 0}日 / #${activeOverview?.provider_rank || '--'}` : isTrendOverview ? `${activeOverview?.active_days || 0}日 / ${activeOverview?.max_streak || 0}板` : activeOverview ? `${activeOverview.matched_nodes}/${activeOverview.total_nodes}` : '--'} sub={isKaipanlaOverview ? activeOverview?.carry_forward ? `沿用 ${activeOverview.trade_date}` : '开盘啦板块' : isTrendOverview ? `昨日 ${activeOverview?.previous_count || 0} 家` : activeEmotion ? formatRatio(activeEmotion.coverage) : undefined} />
						</div>
					</header>

					<section className="node-filter" aria-label="题材细分">
						<button type="button" className={selectedNode === 'all' ? 'active' : ''} onClick={() => { setSelectedNode('all'); setStockPage(1); }}>
							{constituentsComplete ? '全部关联' : '已加载'} <span>{constituents.data ? stockPagination.total : '--'}</span>
						</button>
						{themeNodes.map((node) => (
							<button type="button" className={selectedNode === node.id ? 'active' : ''} key={node.id} onClick={() => { setSelectedNode(node.id); setStockPage(1); }}>
								{node.name} <span>{node.candidate_count ?? node.stocks.length}只</span>
							</button>
						))}
					</section>

					<section className="stock-board">
						<div className="board-heading">
							<div>
								<h3>主线个股梯队</h3>
								<p>综合多日高度、启动时序、持续性、关注度、主线影响代理和分歧承接。</p>
							</div>
							<div className={`history-status ${historyState}`}>
								<History size={14} aria-hidden="true" />
								<span>{historyStatusLabel(historyState, historyReadyCount, historySymbols.length)}</span>
								{historyErrorSymbols.size > 0 && <button type="button" onClick={history.retry}>重试失败项</button>}
							</div>
						</div>
						<div className="stock-controls">
							<label><span>涨跌幅制度</span><select value={stockLane} onChange={(event) => { setStockLane(event.target.value as ThemeScreenLane); setStockPage(1); }}><option value="all">全部</option><option value="10cm">10cm</option><option value="20cm">20cm</option><option value="30cm">30cm</option></select></label>
							<label><span>排序</span><select value={stockSort} onChange={(event) => { setStockSort(event.target.value as ThemeScreenSort); setStockPage(1); }}><option value="rank_score">{constituentsComplete ? '全池领导力' : '来源优先顺序'}</option><option value="change_percent">当日涨幅</option><option value="amount">成交额</option><option value="limit_up_streak">连板高度</option></select></label>
							<label className="stock-search">
								<Search size={16} aria-hidden="true" />
								<input value={stockQuery} onChange={(event) => setStockQuery(event.target.value)} placeholder="搜索完整候选池中的代码、名称或细分" />
							</label>
						</div>
						<div className="stock-table-wrap">
							<table className="stock-table">
								<thead>
									<tr>
										<th>身份</th><th>股票</th><th>阶段</th><th>领导力</th><th>可交易</th><th>近5日</th><th>当日</th><th>所属细分</th>
									</tr>
								</thead>
								<tbody>
									{visibleStocks.map((stock) => {
										const metricsReady = Boolean(leadershipHistories[stock.symbol]?.length);
										const metricsFailed = historyErrorSymbols.has(stock.symbol);
										return <tr
											key={stock.symbol}
											className={stock.symbol === selectedStock?.symbol ? 'selected' : ''}
											onClick={() => {
												setSelectedSymbol(stock.symbol);
											}}
										>
											<td>{metricsReady ? <RoleBadge role={stock.role} regime={stock.limit_regime} /> : <span className="role-badge watch">{stock.rank_role || '待计算'}</span>}</td>
											<td><strong>{stock.name}</strong><small>{stock.symbol}{stock.live ? ' · 实时' : ''}</small></td>
											<td>{metricsReady ? <StateBadge state={stock.state} /> : <MetricPending failed={metricsFailed} />}</td>
											<td>{metricsReady ? <ScoreCell value={stock.leader_score} /> : <MetricPending failed={metricsFailed} />}</td>
											<td>{metricsReady ? <ScoreCell value={stock.tradability_score} /> : <MetricPending failed={metricsFailed} />}</td>
											<td className={metricsReady ? toneForValue(stock.metrics.return_5d) : undefined}>{metricsReady ? formatPercent(stock.metrics.return_5d) : <MetricPending failed={metricsFailed} />}</td>
											<td className={toneForValue(stock.change_percent)}>{stock.live || stock.price > 0 ? formatPercent(stock.change_percent) : '--'}</td>
											<td><span className="node-tags">{stock.nodes.slice(0, 2).join(' / ')}</span></td>
										</tr>
									})}
									{themeState === 'loading' && !visibleStocks.length && <TableSkeleton />}
								</tbody>
							</table>
							{themeState === 'error' && <EmptyState icon={<Server size={22} />} title="主线成分加载失败" detail="保留趋势总览，刷新后重试关联个股数据。" />}
							{themeState === 'ready' && constituentsComplete && !visibleStocks.length && <EmptyState icon={<Search size={22} />} title="没有匹配个股" detail="更换细分节点或清空搜索条件。" />}
						</div>
						{constituents.error && <p className="load-notice">{constituents.error} <button type="button" onClick={() => setThemeRefreshKey(key => key + 1)}>重试成分</button></p>}
						<div className="stock-pagination" aria-label="题材个股分页">
							<span>{constituentsComplete ? `共 ${stockPagination.total} 只 · 每页 ${stockPagination.page_size} 只` : `已加载 ${visibleStocks.length} 只 · 完整成分${constituents.fetching ? '更新中' : '暂不可用'}`}</span>
							<div>
								<button type="button" disabled={!constituentsComplete || stockPage <= 1 || constituents.fetching} onClick={() => setStockPage((current) => Math.max(1, current - 1))}>上一页</button>
								<strong>{constituentsComplete ? `${stockPagination.page}/${stockPagination.total_pages || 1}` : '--'}</strong>
								<button type="button" disabled={!constituentsComplete || !stockPagination.has_more || constituents.fetching} onClick={() => setStockPage((current) => current + 1)}>下一页</button>
							</div>
						</div>
					</section>
				</section>

				<aside className="stock-detail">
					<section className="detail-panel chart-panel">
						<div className="detail-heading">
							<div>
								<span>个股日 K</span>
								<h3>{selectedStock?.name || '选择个股'}</h3>
								<small>{selectedStock?.symbol || '--'}</small>
							</div>
							{selectedStock && (
								<div className={`quote-block ${toneForValue(selectedStock.change_percent)}`}>
									<strong>{selectedStock.price > 0 ? formatNumber(selectedStock.price) : '--'}</strong>
									<span>{selectedStock.live || selectedStock.price > 0 ? formatPercent(selectedStock.change_percent) : '--'}</span>
								</div>
							)}
						</div>
						{selectedStock && !visibleStocks.some(stock => stock.symbol === selectedStock.symbol) && <p className="load-notice">当前选择不在本页筛选结果中。</p>}
						<CandlestickChart lines={kLines} state={klineState} />
						{selectedHistoryFailed && <p className="load-notice">日 K 更新失败 <button type="button" onClick={history.retry}>重试</button></p>}
						{selectedStock && (selectedHistoryReady
							? <StockSnapshot stock={selectedStock} />
							: <MetricLoadingPanel failed={selectedHistoryFailed} />)}
					</section>

					<section className="detail-panel identity-panel">
						<div className="panel-label"><Target size={16} aria-hidden="true" />龙头身份模型</div>
						{selectedHistoryReady && (!constituentsComplete || historyState !== 'ready') && <p className="load-notice">当前为部分样本结果，其他个股数据到齐后继续更新。</p>}
						{selectedStock && selectedHistoryReady && selectedStock.metrics.history_days < 15 && <p className="load-notice">历史不足 15 个交易日，指标仅供参考。</p>}
						{selectedStock && selectedHistoryReady ? (
							<>
								<div className="identity-row">
									<div><RoleBadge role={selectedStock.role} regime={selectedStock.limit_regime} /><ConfirmationBadge level={selectedStock.confirmation} /></div>
									<StateBadge state={selectedStock.state} />
								</div>
								<div className="leadership-score-grid">
									<LeadershipScore label="领导力" value={selectedStock.leader_score} icon={<Target size={14} />} />
									<LeadershipScore label="可交易性" value={selectedStock.tradability_score} icon={<Gauge size={14} />} />
									<LeadershipScore label="置信度" value={Math.round(selectedStock.confidence * 100)} suffix="%" icon={<ShieldCheck size={14} />} />
								</div>
								<LeadershipBreakdown stock={selectedStock} />
								<div className="evidence-section">
									<strong>支持证据</strong>
									<ul>{selectedStock.evidence.map((item) => <li key={item}>{item}</li>)}</ul>
								</div>
								<div className="identity-tags">{selectedStock.nodes.map((node) => <span key={node}>{node}</span>)}</div>
								<div className="risk-section">
									<strong><ShieldAlert size={14} aria-hidden="true" />确认限制</strong>
									<ul>{selectedStock.risks.map((item) => <li key={item}>{item}</li>)}</ul>
								</div>
							</>
						) : selectedStock ? <MetricLoadingPanel failed={selectedHistoryFailed} /> : <p>从个股梯队中选择股票查看。</p>}
					</section>

					<section className="detail-panel news-panel">
						<div className="panel-label"><Newspaper size={16} aria-hidden="true" />市场快讯</div>
						<div className="news-list">
							{newsError && <p className="load-notice">{newsError} <button type="button" onClick={() => setNewsRetryKey(key => key + 1)}>重试快讯</button></p>}
							{!news.length && !newsError && <p className="load-notice">快讯加载中…</p>}
							{news.slice(0, 7).map((item) => (
								<a href={item.url} target="_blank" rel="noreferrer" key={`${item.id}-${item.published_at}`}>
									<time>{formatTime(item.published_at)}</time><span>{item.title}</span><ChevronRight size={14} aria-hidden="true" />
								</a>
							))}
						</div>
					</section>
				</aside>
			</div>
			</> : workspaceMode === 'limit-up' ? <LimitUpWorkspace config={config} data={limitUpData} state={limitUpState} error={limitUpError} emotionData={marketEmotionData} emotionState={marketEmotionState} emotionError={marketEmotionError} progress={limitUp.ladder.progress} onRefresh={refreshLimitUpWorkspace} /> : workspaceMode === 'mastery' ? <TradingMastery config={config} refreshKey={masteryRefreshKey} onAskAI={askMasteryAI} /> : workspaceMode === 'reviews' ? <ReviewDiary config={config} refreshKey={reviewRefreshKey} /> : workspaceMode === 'stock-ai' ? <StockAIAnalysisWorkspace config={config} refreshKey={stockAIRefreshKey} mode={stockAIWorkspaceMode} initialAnalysis={stockAIInitialAnalysis} onInitialAnalysisConsumed={() => setStockAIInitialAnalysis(null)} onAskAI={askStockAnalysisAI} onOpenSettings={() => setSettingsOpen(true)} /> : workspaceMode === 'portfolio-inspection' ? <PortfolioInspectionWorkspace config={config} refreshKey={portfolioInspectionRefreshKey} onOpenSettings={() => setSettingsOpen(true)} onOpenStockAnalysis={openPortfolioStockAnalysis} /> : workspaceMode === 'market' ? <MarketOverviewWorkspace config={config} refreshKey={marketRefreshKey} onAskAI={askMarketAI} /> : <AIChatWorkspace config={config} refreshKey={aiRefreshKey} initialPrompt={aiPrefill} initialAnalysisID={aiAnalysisID} onInitialPromptConsumed={() => { setAIPrefill(''); setAIAnalysisID(undefined); }} onOpenSettings={() => setSettingsOpen(true)} />}

			<footer className="data-footer">
				<div><Wifi size={15} aria-hidden="true" /><span>{config?.backendUrl || '连接本地数据服务中'}</span></div>
					<div><Radio size={15} aria-hidden="true" /><span>{workspaceMode === 'themes' ? '题材与龙一至龙五：开盘啦 · 实时行情：新浪 · K线与领导力：东方财富/新浪' : workspaceMode === 'limit-up' ? '当日涨停池与逐股题材：开盘啦优先 · 历史梯队、缺失股票与行情字段：东方财富补充 · 默认剔除ST' : workspaceMode === 'mastery' ? '来源：trading-mastery/游资心法 · 每日缓存 · 同步至 Hermes Skill 与本地记忆索引' : workspaceMode === 'reviews' ? '复盘文章：本地 SQLite 归档 · 原文观点不代表系统结论' : workspaceMode === 'stock-ai' ? '行情与K线：东方财富/新浪 · 涨停与题材：开盘啦/东方财富 · AI只基于结构化证据总结' : workspaceMode === 'portfolio-inspection' ? '逐股分析复用个股引擎 · 组合指标由本地程序计算 · AI只基于结构化证据汇总' : workspaceMode === 'market' ? '行情与行业强度：腾讯/东方财富 · 资金与领涨标的：新浪/东方财富 · 龙虎榜、公告与研报：东方财富 · 盘面快讯：财联社 · AI 只读取带时间和来源的证据' : workspaceMode === 'token-usage' ? '真实用量来自模型返回的 usage · 本地估算单独记录，不并入真实总量' : '模型请求由本地后端转发 · API Key 不会暴露给页面 · 对话历史保存在当前设备'}</span></div>
			</footer>
			</div>
			<SettingsDrawer config={config} open={settingsOpen} onClose={() => setSettingsOpen(false)} onSaved={() => { setAIRefreshKey((current) => current + 1); setStockAIRefreshKey((current) => current + 1); }} />
		</main>
	);
}

function Metric({ label, value, sub, tone = '' }: { label: string; value: string; sub?: string; tone?: string }) {
	return <div className="theme-metric"><span>{label}</span><strong className={tone}>{value}</strong>{sub && <small>{sub}</small>}</div>;
}

function RoleBadge({ role, regime }: { role: StockRole; regime?: ThemeStock['limit_regime'] }) {
	const roleClass: Record<StockRole, string> = {
		'高度龙头候选': 'leader',
		'先锋候选': 'pioneer',
		'容量核心候选': 'capacity',
		'补涨候选': 'catchup',
		'核心候选': 'core',
		'中位跟随': 'middle',
		'低位观察': 'watch',
		'掉队': 'lagging',
	};
	return <span className={`role-badge ${roleClass[role]}`}>{regime && <em>{regime}</em>}<span className="role-badge-label">{role}</span></span>;
}

function StateBadge({ state }: { state: ThemeStock['state'] }) {
	return <span className={`state-badge state-${state}`}>{state}</span>;
}

function ConfirmationBadge({ level }: { level: ThemeStock['confirmation'] }) {
	return <span className={`confirmation-badge confirmation-${level}`}>{level}</span>;
}

function ScoreCell({ value }: { value: number }) {
	return (
		<div className="score-cell" aria-label={`${value}分`}>
			<strong>{value}</strong>
			<span><i style={{ width: `${value}%` }} /></span>
		</div>
	);
}

function MetricPending({ failed = false }: { failed?: boolean }) {
	return (
		<span className={`metric-pending ${failed ? 'failed' : ''}`} aria-label={failed ? '指标计算失败' : '指标计算中'} title={failed ? '指标计算失败' : '指标计算中'}>
			{failed ? '--' : <LoaderCircle size={15} aria-hidden="true" />}
		</span>
	);
}

function MetricLoadingPanel({ failed = false }: { failed?: boolean }) {
	return (
		<div className={`metric-loading-panel ${failed ? 'failed' : ''}`}>
			{failed ? <Server size={18} aria-hidden="true" /> : <LoaderCircle className="spin" size={18} aria-hidden="true" />}
			<span>{failed ? '多日指标暂不可用' : '正在计算 5 日涨幅与领导力指标'}</span>
		</div>
	);
}

function LeadershipScore({
	label,
	value,
	suffix = '',
	icon,
}: {
	label: string;
	value: number;
	suffix?: string;
	icon: React.ReactNode;
}) {
	return (
		<div className="leadership-score">
			<span>{icon}{label}</span>
			<strong>{value}{suffix}</strong>
			<i><b style={{ width: `${value}%` }} /></i>
		</div>
	);
}

function LeadershipBreakdown({ stock }: { stock: ThemeStock }) {
	const dimensions: Array<[string, number]> = [
		['高度强度', stock.breakdown.height],
		['启动时序', stock.breakdown.timing],
		['持续性', stock.breakdown.persistence],
		['市场关注', stock.breakdown.attention],
		['影响代理', stock.breakdown.influence_proxy],
		['分歧承接', stock.breakdown.resilience],
		['题材纯度', stock.breakdown.purity],
	];
	return (
		<div className="leadership-breakdown" aria-label="龙头评分构成">
			{dimensions.map(([label, value]) => (
				<div key={label}><span>{label}</span><i><b style={{ width: `${value}%` }} /></i><strong>{value}</strong></div>
			))}
		</div>
	);
}

function StockSnapshot({ stock }: { stock: ThemeStock }) {
	return (
		<div className="stock-snapshot">
			<div><span>近5日</span><strong className={toneForValue(stock.metrics.return_5d)}>{formatPercent(stock.metrics.return_5d)}</strong></div>
			<div><span>20日位置</span><strong>{stock.metrics.position_20d.toFixed(0)}%</strong></div>
			<div><span>最高连板</span><strong>{stock.metrics.max_limit_streak_20d || '--'}</strong></div>
		</div>
	);
}

function CandlestickChart({ lines, state }: { lines: KLine[]; state: LoadState }) {
	if (state === 'loading') {
		return <div className="chart-placeholder"><RefreshCw className="spin" size={20} /><span>加载日 K 数据</span></div>;
	}
	if (!lines.length) {
		return <div className="chart-placeholder"><BarChart3 size={22} /><span>{state === 'error' ? '日 K 数据暂不可用' : '选择个股查看日 K'}</span></div>;
	}
	const sorted = [...lines].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime());
	const width = 640;
	const height = 278;
	const chartTop = 18;
	const chartBottom = 200;
	const volumeTop = 220;
	const volumeBottom = 258;
	const labelWidth = 52;
	const plotWidth = width - labelWidth;
	const minPrice = Math.min(...sorted.map((line) => line.low));
	const maxPrice = Math.max(...sorted.map((line) => line.high));
	const priceRange = Math.max(maxPrice - minPrice, 0.01);
	const maxVolume = Math.max(...sorted.map((line) => line.volume), 1);
	const step = plotWidth / sorted.length;
	const bodyWidth = Math.max(2, Math.min(7, step * 0.58));
	const priceY = (value: number) => chartBottom - ((value - minPrice) / priceRange) * (chartBottom - chartTop);
	const volumeY = (value: number) => volumeBottom - (value / maxVolume) * (volumeBottom - volumeTop);
	const priceTicks = Array.from({ length: 5 }, (_, index) => maxPrice - (priceRange * index) / 4);
	const labelIndexes = [0, Math.floor((sorted.length - 1) / 2), sorted.length - 1];

	return (
		<div className="candlestick-wrap">
			<svg className="candlestick" viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`最近 ${sorted.length} 个交易日的日 K 线和成交量`}>
				{priceTicks.map((tick) => {
					const y = priceY(tick);
					return <g key={tick}><line className="chart-grid" x1="0" x2={plotWidth} y1={y} y2={y} /><text className="chart-label" x={width - 4} y={y + 4} textAnchor="end">{tick.toFixed(2)}</text></g>;
				})}
				{sorted.map((line, index) => {
					const x = index * step + step / 2;
					const rising = line.close >= line.open;
					const top = priceY(Math.max(line.open, line.close));
					const bottom = priceY(Math.min(line.open, line.close));
					return (
						<g className={rising ? 'candle-up' : 'candle-down'} key={`${line.time}-${index}`}>
							<line x1={x} x2={x} y1={priceY(line.high)} y2={priceY(line.low)} />
							<rect x={x - bodyWidth / 2} y={top} width={bodyWidth} height={Math.max(bottom - top, 1.5)} />
							<rect className="volume-bar" x={x - bodyWidth / 2} y={volumeY(line.volume)} width={bodyWidth} height={volumeBottom - volumeY(line.volume)} />
						</g>
					);
				})}
				{labelIndexes.map((index) => {
					const line = sorted[index];
					return <text className="chart-label" x={index * step + step / 2} y={height - 4} textAnchor={index === 0 ? 'start' : index === sorted.length - 1 ? 'end' : 'middle'} key={line.time}>{formatDate(line.time)}</text>;
				})}
			</svg>
		</div>
	);
}

function EmptyState({ icon, title, detail }: { icon: React.ReactNode; title: string; detail: string }) {
	return <div className="empty-state">{icon}<strong>{title}</strong><span>{detail}</span></div>;
}

function RailSkeleton() {
	return <>{Array.from({ length: 6 }, (_, index) => <div className="theme-skeleton" key={index}><span /><i /></div>)}</>;
}

function TableSkeleton() {
	return <>{Array.from({ length: 6 }, (_, index) => <tr className="table-skeleton" key={index}><td colSpan={8}><span /></td></tr>)}</>;
}

function mergeQuotes(current: QuoteLookup, quotes: Quote[]): QuoteLookup {
	const next = { ...current };
	for (const quote of quotes) {
		next[quote.symbol] = {
			price: quote.price,
			change: quote.change,
			change_percent: quote.change_percent,
		};
	}
	return next;
}

function toneForValue(value?: number): 'up' | 'down' | 'flat' {
	if (typeof value !== 'number' || value === 0) {
		return 'flat';
	}
	return value > 0 ? 'up' : 'down';
}

function formatNumber(value?: number) {
	return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(2) : '--';
}

function formatPercent(value?: number) {
	return typeof value === 'number' && Number.isFinite(value) ? `${value >= 0 ? '+' : ''}${value.toFixed(2)}%` : '--';
}

function formatRatio(value: number) {
	return `${Math.round(value * 100)}%`;
}

function formatMoney(value?: number) {
	if (typeof value !== 'number' || !Number.isFinite(value)) {
		return '--';
	}
	const absolute = Math.abs(value);
	if (absolute >= 100_000_000) {
		return `${(value / 100_000_000).toFixed(2)}亿`;
	}
	if (absolute >= 10_000) {
		return `${(value / 10_000).toFixed(0)}万`;
	}
	return value.toFixed(0);
}

function formatTime(value?: string) {
	if (!value) {
		return '--:--';
	}
	return new Date(value).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
}

function sourceCategoryLabel(category: string) {
	const labels: Record<string, string> = {
		theme: '题材',
		leaders: '龙头榜单',
		'limit-up': '涨停池',
		concept: '概念归因',
		quote: '实时行情',
		kline: 'K 线',
		f10: '公司资料',
		report: '研报',
		'money-flow': '资金流',
		index: '指数',
		hk: '港股',
		news: '资讯',
		calendar: '日历',
		basic: '基础资料',
		daily: '日线',
	};
	return category.split(',').map((item) => labels[item.trim()] || item.trim()).filter(Boolean).join(' · ');
}

function sourceHealthMessage(message?: string) {
	if (!message) return '异常';
	if (message === 'requires token') return '需要 Token';
	return message;
}

function formatSourceStrength(value?: number) {
	if (typeof value !== 'number' || !Number.isFinite(value)) {
		return '--';
	}
	if (Math.abs(value) >= 10_000) {
		return (value / 10_000).toFixed(1) + '万';
	}
	return value.toFixed(0);
}

function formatDate(value: string) {
	return new Date(value).toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' });
}

function historyStatusLabel(state: LoadState | 'partial', ready: number, total: number) {
	switch (state) {
		case 'loading': return total > 0 ? `多日指标 ${ready}/${total}` : '准备多日指标';
		case 'ready': return '多日身份已计算';
		case 'error': return '多日数据暂不可用';
		case 'partial': return `多日指标已完成 ${ready}/${total}，部分暂不可用`;
		case 'idle':
		default: return '等待多日数据';
	}
}
