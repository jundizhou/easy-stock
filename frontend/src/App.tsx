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
	Target,
	Wifi,
} from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
	BackendConfig,
	KLine,
	NewsItem,
	Quote,
	SectorMap,
	SourceHealth,
	SourceMeta,
	StreamMessage,
	ThemeOverview,
	LimitUpLadderData,
	MarketEmotionHistory,
	StockAIAnalysis,
	ThemeScreenData,
	ThemeScreenLane,
	ThemeScreenPagination,
	ThemeScreenSort,
	buildStreamUrl,
	requestJSON,
	resolveBackendConfig,
} from './lib/backend';
import {
	QuoteLookup,
	KLineLookup,
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
import { TradingMastery } from './components/TradingMastery';
import { StockAIAnalysisWorkspace, StockAIWorkspaceMode } from './components/StockAIAnalysisWorkspace';

type LoadState = 'idle' | 'loading' | 'ready' | 'error';
type WorkspaceMode = 'themes' | 'limit-up' | 'mastery' | 'reviews' | 'stock-ai' | 'ai';

const emptyStockPagination = (): ThemeScreenPagination => ({
	page: 1,
	page_size: 20,
	total: 0,
	total_pages: 0,
	has_more: false,
});

export function App() {
	const [workspaceMode, setWorkspaceMode] = useState<WorkspaceMode>(() => {
		if (window.location.hash === '#limit-up') return 'limit-up';
		if (window.location.hash === '#mastery') return 'mastery';
		if (window.location.hash === '#reviews') return 'reviews';
		if (window.location.hash === '#stock-ai') return 'stock-ai';
		if (window.location.hash === '#ai') return 'ai';
		return 'themes';
	});
	const [sidebarExpanded, setSidebarExpanded] = useState(true);
	const [config, setConfig] = useState<BackendConfig | null>(null);
	const [themeOverviews, setThemeOverviews] = useState<ThemeOverview[]>([]);
	const [themeStrengthWindow, setThemeStrengthWindow] = useState<ThemeStrengthWindow>('daily');
	const [overviewMeta, setOverviewMeta] = useState<SourceMeta | null>(null);
	const [activeTheme, setActiveTheme] = useState('');
	const [sectorMap, setSectorMap] = useState<SectorMap | null>(null);
	const [selectedNode, setSelectedNode] = useState('all');
	const [selectedSymbol, setSelectedSymbol] = useState('');
	const [hasManualStockSelection, setHasManualStockSelection] = useState(false);
	const [liveQuotes, setLiveQuotes] = useState<QuoteLookup>({});
	const [leadershipHistories, setLeadershipHistories] = useState<KLineLookup>({});
	const [historyLoadingSymbols, setHistoryLoadingSymbols] = useState<Set<string>>(() => new Set());
	const [historyErrorSymbols, setHistoryErrorSymbols] = useState<Set<string>>(() => new Set());
	const [kLines, setKLines] = useState<KLine[]>([]);
	const [sources, setSources] = useState<SourceHealth[]>([]);
	const [news, setNews] = useState<NewsItem[]>([]);
	const [foundationState, setFoundationState] = useState<LoadState>('idle');
	const [themeState, setThemeState] = useState<LoadState>('idle');
	const [historyState, setHistoryState] = useState<LoadState>('idle');
	const [klineState, setKlineState] = useState<LoadState>('idle');
	const [streamStatus, setStreamStatus] = useState('实时流待命');
	const [statusText, setStatusText] = useState('连接数据层');
	const [stockQuery, setStockQuery] = useState('');
	const [debouncedStockQuery, setDebouncedStockQuery] = useState('');
	const [stockPage, setStockPage] = useState(1);
	const [stockPagination, setStockPagination] = useState<ThemeScreenPagination>(emptyStockPagination);
	const [stockSort, setStockSort] = useState<ThemeScreenSort>('rank_score');
	const [stockLane, setStockLane] = useState<ThemeScreenLane>('all');
	const [limitUpData, setLimitUpData] = useState<LimitUpLadderData | null>(null);
	const [limitUpState, setLimitUpState] = useState<LoadState>('idle');
	const [limitUpError, setLimitUpError] = useState('');
	const [marketEmotionData, setMarketEmotionData] = useState<MarketEmotionHistory | null>(null);
	const [marketEmotionState, setMarketEmotionState] = useState<LoadState>('idle');
	const [marketEmotionError, setMarketEmotionError] = useState('');
	const [reviewRefreshKey, setReviewRefreshKey] = useState(0);
	const [masteryRefreshKey, setMasteryRefreshKey] = useState(0);
	const [stockAIRefreshKey, setStockAIRefreshKey] = useState(0);
	const [stockAIWorkspaceMode, setStockAIWorkspaceMode] = useState<StockAIWorkspaceMode>('analysis');
	const [aiRefreshKey, setAIRefreshKey] = useState(0);
	const [aiPrefill, setAIPrefill] = useState('');
	const [settingsOpen, setSettingsOpen] = useState(false);
	const themeRequestID = useRef(0);
	const leadershipHistoriesRef = useRef<KLineLookup>({});
	const historyFlightsRef = useRef<Map<string, Promise<void>>>(new Map());
	const priorityPrefetchPromiseRef = useRef<Promise<void> | null>(null);

	const rankedThemes = useMemo(() => rankThemeOverviews(themeOverviews, themeStrengthWindow), [themeOverviews, themeStrengthWindow]);
	const activeOverview = useMemo(
		() => themeOverviews.find((item) => item.theme === activeTheme) || null,
		[activeTheme, themeOverviews],
	);
	const activeStrengthScore = activeOverview ? themeStrengthScore(activeOverview, themeStrengthWindow) : null;
	const activeEmotion = activeOverview ? calculateThemeEmotion(activeOverview, activeStrengthScore ?? undefined) : null;
	const isTrendOverview = typeof activeOverview?.trend_score === 'number';
	const isKaipanlaOverview = activeOverview?.source === 'duanxianxia:kaipanla';
	const activeSnapshotID = activeOverview?.snapshot_id || '';
	const baseThemeStocks = useMemo(() => buildThemeStocks(sectorMap), [sectorMap]);
	const themeStocks = useMemo(
		() => buildThemeStocks(sectorMap, liveQuotes, leadershipHistories),
		[leadershipHistories, liveQuotes, sectorMap],
	);
	const visibleStocks = useMemo(() => [...themeStocks].sort((a, b) => {
		switch (stockSort) {
			case 'change_percent':
				return b.change_percent - a.change_percent || b.leader_score - a.leader_score;
			case 'amount':
				return b.amount - a.amount || b.leader_score - a.leader_score;
			case 'limit_up_streak':
				return (b.limit_up_streak || 0) - (a.limit_up_streak || 0) || b.leader_score - a.leader_score;
			case 'rank_score':
			default:
				{
					const aLeaderRank = kaipanlaLeaderRank(a.rank_role);
					const bLeaderRank = kaipanlaLeaderRank(b.rank_role);
					if (aLeaderRank || bLeaderRank) {
						if (!aLeaderRank) return 1;
						if (!bLeaderRank) return -1;
						if (aLeaderRank !== bLeaderRank) return aLeaderRank - bLeaderRank;
					}
				}
				return b.leader_score - a.leader_score || b.tradability_score - a.tradability_score;
		}
	}), [stockSort, themeStocks]);
	const selectedStock = useMemo(
		() => themeStocks.find((stock) => stock.symbol === selectedSymbol) || visibleStocks[0] || null,
		[selectedSymbol, themeStocks, visibleStocks],
	);
	const selectedHistoryReady = Boolean(selectedStock && leadershipHistories[selectedStock.symbol]?.length);
	const selectedHistoryFailed = Boolean(selectedStock && historyErrorSymbols.has(selectedStock.symbol));
	const themeNodes = useMemo(
		() => sectorMap?.groups.flatMap((group) => group.nodes) || [],
		[sectorMap],
	);
	const streamSymbols = useMemo(() => baseThemeStocks.map((stock) => stock.symbol), [baseThemeStocks]);
	const streamKey = streamSymbols.join(',');
	const historySymbols = useMemo(() => baseThemeStocks.map((stock) => stock.symbol), [baseThemeStocks]);
	const historyKey = historySymbols.join(',');
	const historyReadyCount = useMemo(
		() => historySymbols.filter((symbol) => Boolean(leadershipHistories[symbol]?.length)).length,
		[historySymbols, leadershipHistories],
	);
	const marketPulse = useMemo(() => {
		if (!rankedThemes.length) {
			return { average: 0, active: 0 };
		}
		const scores = rankedThemes.map((theme) => themeStrengthScore(theme, themeStrengthWindow));
		return {
			average: Math.round(scores.reduce((total, score) => total + score, 0) / scores.length),
			active: scores.filter((score) => score >= 60).length,
		};
	}, [rankedThemes, themeStrengthWindow]);

	useEffect(() => {
		resolveBackendConfig()
			.then(setConfig)
			.catch((error) => {
				setFoundationState('error');
				setStatusText(error instanceof Error ? error.message : '后端配置失败');
			});
	}, []);

	const loadFoundation = useCallback(async () => {
		if (!config) {
			return;
		}
		setFoundationState('loading');
		setStatusText('同步趋势主线');
		const [overviewResult, sourceResult, newsResult] = await Promise.allSettled([
			requestJSON<{ data: ThemeOverview[]; meta: SourceMeta }>(config, '/api/v1/themes/overview'),
			requestJSON<{ sources: SourceHealth[] }>(config, '/api/v1/sources'),
			requestJSON<{ data: NewsItem[] }>(config, '/api/v1/market/news?source=cls&limit=12'),
		]);

		if (overviewResult.status === 'fulfilled') {
			setThemeOverviews(overviewResult.value.data);
			setOverviewMeta(overviewResult.value.meta);
			setFoundationState('ready');
			setStatusText('基础数据已更新');
		} else {
			setFoundationState('error');
			setStatusText(overviewResult.reason instanceof Error ? overviewResult.reason.message : '题材快照失败');
		}
		if (sourceResult.status === 'fulfilled') {
			setSources(sourceResult.value.sources);
		}
		if (newsResult.status === 'fulfilled') {
			setNews(newsResult.value.data);
		}
	}, [config]);

	const loadHistorySymbols = useCallback(async (symbols: string[]) => {
		if (!config) {
			return;
		}
		const uniqueSymbols = [...new Set(symbols.filter(Boolean))];
		const waits: Promise<void>[] = [];
		const pending: string[] = [];
		for (const symbol of uniqueSymbols) {
			if (leadershipHistoriesRef.current[symbol]?.length) {
				continue;
			}
			const existing = historyFlightsRef.current.get(symbol);
			if (existing) {
				waits.push(existing);
			} else {
				pending.push(symbol);
			}
		}
		if (pending.length) {
			setHistoryLoadingSymbols((current) => new Set([...current, ...pending]));
			setHistoryErrorSymbols((current) => {
				const next = new Set(current);
				pending.forEach((symbol) => next.delete(symbol));
				return next;
			});
			let batchPromise!: Promise<void>;
			batchPromise = requestJSON<{ data: KLineLookup; errors?: Record<string, string> }>(
				config,
				`/api/v1/quotes/kline/batch?symbols=${encodeURIComponent(pending.join(','))}&period=day&limit=40`,
			)
				.then((payload) => {
					const successful = pending.filter((symbol) => Boolean(payload.data[symbol]?.length));
					const failed = pending.filter((symbol) => !payload.data[symbol]?.length);
					setLeadershipHistories((current) => {
						const next = { ...current, ...payload.data };
						leadershipHistoriesRef.current = next;
						return next;
					});
					setHistoryErrorSymbols((current) => {
						const next = new Set(current);
						successful.forEach((symbol) => next.delete(symbol));
						failed.forEach((symbol) => next.add(symbol));
						Object.keys(payload.errors || {}).forEach((symbol) => next.add(symbol));
						return next;
					});
				})
				.catch(() => {
					setHistoryErrorSymbols((current) => new Set([...current, ...pending]));
				})
				.finally(() => {
					setHistoryLoadingSymbols((current) => {
						const next = new Set(current);
						pending.forEach((symbol) => next.delete(symbol));
						return next;
					});
					pending.forEach((symbol) => {
						if (historyFlightsRef.current.get(symbol) === batchPromise) {
							historyFlightsRef.current.delete(symbol);
						}
					});
				});
			pending.forEach((symbol) => historyFlightsRef.current.set(symbol, batchPromise));
			waits.push(batchPromise);
		}
		await Promise.allSettled(waits);
	}, [config]);

	useEffect(() => {
		if (workspaceMode === 'themes') {
			void loadFoundation();
		}
	}, [loadFoundation, workspaceMode]);

	useEffect(() => {
		if (rankedThemes.length && !rankedThemes.some((theme) => theme.theme === activeTheme)) {
			setActiveTheme(rankedThemes[0].theme);
		}
	}, [activeTheme, rankedThemes]);

	useEffect(() => {
		const timer = window.setTimeout(() => {
			setDebouncedStockQuery(stockQuery.trim());
			setStockPage(1);
		}, 300);
		return () => window.clearTimeout(timer);
	}, [stockQuery]);

	const loadActiveTheme = useCallback(async () => {
		if (!config || !activeTheme || workspaceMode !== 'themes') {
			return;
		}
		const requestID = ++themeRequestID.current;
		setThemeState('loading');
		setSelectedSymbol('');
		setHasManualStockSelection(false);
		setLiveQuotes({});
		try {
			const params = new URLSearchParams({
				theme: activeTheme,
				page: String(stockPage),
				page_size: '20',
				node: selectedNode,
				lane: stockLane,
				sort: stockSort,
			});
			if (activeSnapshotID) {
				params.set('snapshot_id', activeSnapshotID);
			}
			if (debouncedStockQuery) {
				params.set('q', debouncedStockQuery);
			}
			const payload = await requestJSON<{ data: ThemeScreenData }>(
				config,
				`/api/v1/themes/screen?${params.toString()}`,
			);
			if (requestID !== themeRequestID.current) {
				return;
			}
			setSectorMap(payload.data.map);
			setStockPagination(payload.data.pagination);
			setThemeState('ready');
		} catch (error) {
			if (requestID !== themeRequestID.current) {
				return;
			}
			setThemeState('error');
			setSectorMap(null);
			setStockPagination(emptyStockPagination());
			setStatusText(error instanceof Error ? error.message : '题材成分加载失败');
		}
	}, [activeSnapshotID, activeTheme, config, debouncedStockQuery, selectedNode, stockLane, stockPage, stockSort, workspaceMode]);

	useEffect(() => {
		void loadActiveTheme();
	}, [loadActiveTheme]);

	useEffect(() => {
		if (!config || workspaceMode !== 'themes' || !rankedThemes.length) {
			priorityPrefetchPromiseRef.current = null;
			return;
		}
		let cancelled = false;
		const priorityThemes = rankedThemes.slice(0, 3);
		const prefetchPromise = (async () => {
			const screens = await Promise.allSettled(priorityThemes.map((theme) => {
				const params = new URLSearchParams({
					theme: theme.theme,
					page: '1',
					page_size: '5',
					node: 'all',
					lane: 'all',
					sort: 'rank_score',
				});
				if (theme.snapshot_id) {
					params.set('snapshot_id', theme.snapshot_id);
				}
				return requestJSON<{ data: ThemeScreenData }>(config, `/api/v1/themes/screen?${params.toString()}`);
			}));
			if (cancelled) {
				return;
			}
			const symbols = screens.flatMap((result) => result.status === 'fulfilled'
				? buildThemeStocks(result.value.data.map).slice(0, 5).map((stock) => stock.symbol)
				: []);
			await loadHistorySymbols(symbols);
		})();
		priorityPrefetchPromiseRef.current = prefetchPromise;
		return () => {
			cancelled = true;
		};
	}, [config, loadHistorySymbols, rankedThemes, workspaceMode]);

	useEffect(() => {
		if (!historyKey || workspaceMode !== 'themes') {
			return;
		}
		let cancelled = false;
		const prioritySymbols = historySymbols.slice(0, 5);
		const remainingSymbols = historySymbols.slice(5);
		void (async () => {
			await loadHistorySymbols(prioritySymbols);
			if (cancelled) return;
			await priorityPrefetchPromiseRef.current;
			if (cancelled) return;
			await loadHistorySymbols(remainingSymbols);
		})();
		return () => {
			cancelled = true;
		};
	}, [historyKey, historySymbols, loadHistorySymbols, workspaceMode]);

	useEffect(() => {
		if (!historySymbols.length || workspaceMode !== 'themes') {
			setHistoryState('idle');
			return;
		}
		const failedCount = historySymbols.filter((symbol) => historyErrorSymbols.has(symbol)).length;
		const loadingCount = historySymbols.filter((symbol) => historyLoadingSymbols.has(symbol)).length;
		if (historyReadyCount >= historySymbols.length) {
			setHistoryState('ready');
		} else if (loadingCount > 0 || historyReadyCount > 0 || failedCount < historySymbols.length) {
			setHistoryState('loading');
		} else {
			setHistoryState('error');
		}
	}, [historyErrorSymbols, historyLoadingSymbols, historyReadyCount, historySymbols, workspaceMode]);

	useEffect(() => {
		if (!visibleStocks.length) {
			setSelectedSymbol('');
			return;
		}
		if (!visibleStocks.some((stock) => stock.symbol === selectedSymbol)) {
			setSelectedSymbol(visibleStocks[0].symbol);
		}
	}, [selectedSymbol, visibleStocks]);

	useEffect(() => {
		if (historyState === 'ready' && !hasManualStockSelection && visibleStocks.length) {
			setSelectedSymbol(visibleStocks[0].symbol);
		}
	}, [hasManualStockSelection, historyState, visibleStocks]);

	useEffect(() => {
		if (!config || !selectedStock || workspaceMode !== 'themes') {
			setKLines([]);
			return;
		}
		let cancelled = false;
		setKlineState('loading');
		requestJSON<{ data: KLine[] }>(
			config,
			`/api/v1/quotes/kline?symbol=${encodeURIComponent(selectedStock.symbol)}&period=day&limit=60`,
		)
			.then((payload) => {
				if (!cancelled) {
					setKLines(payload.data);
					setKlineState('ready');
				}
			})
			.catch(() => {
				if (!cancelled) {
					setKLines([]);
					setKlineState('error');
				}
			});
		return () => {
			cancelled = true;
		};
	}, [config, selectedStock?.symbol, workspaceMode]);

	useEffect(() => {
		if (!config || !streamKey || workspaceMode !== 'themes') {
			setStreamStatus('实时流待命');
			return;
		}
		const socket = new WebSocket(buildStreamUrl(config, streamSymbols, 3000));
		socket.onopen = () => setStreamStatus('实时行情已连接');
		socket.onclose = () => setStreamStatus('实时行情已断开');
		socket.onerror = () => setStreamStatus('实时行情异常');
		socket.onmessage = (event) => {
			const message = JSON.parse(event.data) as StreamMessage;
			if (message.type === 'quotes' && message.quotes) {
				setLiveQuotes((current) => mergeQuotes(current, message.quotes || []));
				setStreamStatus('实时行情更新中');
			}
			if (message.type === 'error') {
				setStreamStatus(message.error || '实时行情异常');
			}
		};
		return () => socket.close();
	}, [config, streamKey, workspaceMode]);

	const loadLimitUpLadder = useCallback(async () => {
		if (!config) {
			return;
		}
		setLimitUpState('loading');
		setLimitUpError('');
		try {
			const payload = await requestJSON<{ data: LimitUpLadderData }>(config, '/api/v1/short-term/limit-up-ladder');
			setLimitUpData(payload.data);
			setLimitUpState('ready');
		} catch (error) {
			setLimitUpState('error');
			setLimitUpError(error instanceof Error ? error.message : '短线连板数据加载失败');
		}
	}, [config]);

	const loadMarketEmotion = useCallback(async () => {
		if (!config) return;
		setMarketEmotionState('loading');
		setMarketEmotionError('');
		try {
			const payload = await requestJSON<{ data: MarketEmotionHistory }>(config, '/api/v1/short-term/emotion-history');
			setMarketEmotionData(payload.data);
			setMarketEmotionState('ready');
		} catch (error) {
			setMarketEmotionState('error');
			setMarketEmotionError(error instanceof Error ? error.message : '市场情绪历史加载失败');
		}
	}, [config]);

	const refreshLimitUpWorkspace = useCallback(() => {
		void loadLimitUpLadder();
		void loadMarketEmotion();
	}, [loadLimitUpLadder, loadMarketEmotion]);

	useEffect(() => {
		if (workspaceMode === 'limit-up') {
			refreshLimitUpWorkspace();
		}
	}, [refreshLimitUpWorkspace, workspaceMode]);

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
		if (workspaceMode === 'ai') {
			setAIRefreshKey((current) => current + 1);
			return;
		}
		void loadFoundation();
		void loadActiveTheme();
	};

	const switchWorkspace = (mode: WorkspaceMode) => {
		setWorkspaceMode(mode);
		window.history.replaceState(null, '', mode === 'limit-up' ? '#limit-up' : mode === 'mastery' ? '#mastery' : mode === 'reviews' ? '#reviews' : mode === 'stock-ai' ? '#stock-ai' : mode === 'ai' ? '#ai' : '#themes');
	};

	const askMasteryAI = (traderName: string) => {
		setAIPrefill(`请基于本地游资心法知识库，系统梳理${traderName}的核心交易理念、适用市场环境、选股与买卖规则、仓位风控，并指出资料中可能存在的事后归因、占位或不可验证之处。`);
		switchWorkspace('ai');
	};

	const askStockAnalysisAI = (analysis: StockAIAnalysis) => {
		setAIPrefill(`请继续推演 ${analysis.name}（${analysis.symbol}）的个股AI分析。当前画像：${analysis.profile.type_label} / ${analysis.profile.price_phase} / ${analysis.profile.market_role}；综合评分 ${analysis.scorecard.overall}（${analysis.scorecard.direction}）；趋势得分 ${analysis.trend.score}；短线状态 ${analysis.short_term.state}；隔日预期 ${analysis.next_day.bias}；计划止损 ${analysis.risk_control.stop_price}；当前动作：${analysis.action_plan.current_action}。请重点挑战现有结论，分别给出支持证据、反对证据、隔日四种情景、最优验证路径和失效条件，不要编造实时数据。`);
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
		setStockPagination(emptyStockPagination());
		setActiveTheme(theme);
	};

	const currentLoadState = workspaceMode === 'limit-up' ? limitUpState : workspaceMode === 'mastery' || workspaceMode === 'reviews' || workspaceMode === 'stock-ai' || workspaceMode === 'ai' ? 'ready' : foundationState;
	const currentStatusText = workspaceMode === 'limit-up'
		? limitUpState === 'loading' ? '同步连板梯队' : limitUpState === 'error' ? '连板数据异常' : '连板结构已更新'
		: workspaceMode === 'mastery' ? '游资心法库已连接' : workspaceMode === 'reviews' ? '复盘资料库已连接' : workspaceMode === 'stock-ai' ? '个股分析引擎已连接' : workspaceMode === 'ai' ? 'AI 助手已连接' : statusText;
	const themeSourceStatus = overviewMeta?.source === 'duanxianxia:kaipanla'
		? overviewMeta.carry_forward ? '沿用 ' + (overviewMeta.trade_date || '上一交易日') + ' 开盘啦' : (overviewMeta.trade_date || '当日') + ' 开盘啦'
		: '本地趋势题材';
	const currentSubStatus = workspaceMode === 'limit-up'
		? limitUpData ? `${limitUpData.current.trade_date} · ${limitUpData.session_status} · ${limitUpData.meta.source.includes('duanxianxia') ? '开盘啦涨停池' : '东方财富兜底'} · ${limitUpData.concept_status === 'ready' ? '题材已归因' : '题材降级'}` : '开盘啦涨停池优先'
		: workspaceMode === 'mastery' ? 'GitHub 原始资料 · 每日缓存 · Hermes 本地知识库' : workspaceMode === 'reviews' ? '雪球 · 淘股吧 · 微信公众号' : workspaceMode === 'stock-ai' ? '多周期评分 · 基准超额 · 隔日情景 · 动态风控' : workspaceMode === 'ai' ? '本机 Hermes AI 对话' : themeSourceStatus + ' · ' + streamStatus;
	const topbarTitle = workspaceMode === 'themes' ? '趋势题材雷达' : workspaceMode === 'limit-up' ? '短线连板雷达' : workspaceMode === 'mastery' ? '游资心法库' : workspaceMode === 'reviews' ? '大V复盘日记' : workspaceMode === 'stock-ai' ? '个股 AI 分析' : 'AI 对话';
	const topbarDescription = workspaceMode === 'themes' ? '炒作主线、趋势强度、个股梯队与日 K 联动工作台' : workspaceMode === 'limit-up' ? '连板高度、炒作概念与晋级结构工作台' : workspaceMode === 'mastery' ? '阅读不同游资的交易经验，并由 Hermes 按原文辅助研读' : workspaceMode === 'reviews' ? '多平台复盘内容、作者观点与原文归档工作台' : workspaceMode === 'stock-ai' ? '多周期评分、隔日情景推演与账户级风控执行工作台' : '像 Codex 一样持续协作、拆解问题并形成可执行结果';

	return (
		<main className={`workspace-frame ${sidebarExpanded ? 'sidebar-expanded' : 'sidebar-collapsed'}`}>
			<aside className="app-sidebar" aria-label="功能导航">
				<div className="sidebar-brand"><div className="sidebar-logo"><img src={`${import.meta.env.BASE_URL}easy-stock-mark.svg`} alt="easy-stock" /></div>{sidebarExpanded && <div><strong>easy-stock</strong><span>AI STOCK LAB</span></div>}</div>
				<nav>
					<button type="button" className={workspaceMode === 'themes' ? 'active' : ''} onClick={() => switchWorkspace('themes')} title="趋势题材"><LayoutDashboard size={18} /><span>趋势题材</span></button>
					<button type="button" className={workspaceMode === 'limit-up' ? 'active' : ''} onClick={() => switchWorkspace('limit-up')} title="短线连板"><Flame size={18} /><span>短线连板</span></button>
					<button type="button" className={workspaceMode === 'mastery' ? 'active' : ''} onClick={() => switchWorkspace('mastery')} title="游资心法"><BookMarked size={18} /><span>游资心法</span></button>
					<button type="button" className={workspaceMode === 'reviews' ? 'active' : ''} onClick={() => switchWorkspace('reviews')} title="大V复盘日记"><BookOpen size={18} /><span>大V复盘日记</span></button>
					<button type="button" className={workspaceMode === 'stock-ai' ? 'active' : ''} onClick={() => switchWorkspace('stock-ai')} title="个股AI分析"><BrainCircuit size={18} /><span>个股AI分析</span></button>
					<button type="button" className={workspaceMode === 'ai' ? 'active' : ''} onClick={() => switchWorkspace('ai')} title="AI 对话"><Bot size={18} /><span>AI 对话</span></button>
				</nav>
				<div className="sidebar-guidance">{sidebarExpanded && <><strong>{workspaceMode === 'ai' ? '对话模型' : '数据口径'}</strong><span>{workspaceMode === 'themes' ? '概念共振与趋势归因' : workspaceMode === 'limit-up' ? '封板梯队与概念归因' : workspaceMode === 'mastery' ? '上游原文缓存与 Hermes 知识同步' : workspaceMode === 'reviews' ? '多平台原文与本地归档' : workspaceMode === 'stock-ai' ? '300日趋势 · 基准强弱 · 情景风控' : '复用系统设置中的模型连接'}</span></>}</div>
				<button type="button" className="sidebar-settings" onClick={() => setSettingsOpen(true)} aria-label="打开系统设置" title="系统设置"><Settings size={17} />{sidebarExpanded && <span>系统设置</span>}</button>
				<button type="button" className="sidebar-toggle" onClick={() => setSidebarExpanded((value) => !value)} aria-label={sidebarExpanded ? '收起侧边栏' : '展开侧边栏'}>{sidebarExpanded ? <PanelLeftClose size={17} /> : <PanelLeftOpen size={17} />} {sidebarExpanded && <span>收起侧栏</span>}</button>
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
					{workspaceMode === 'stock-ai' ? <>
						<button type="button" className={stockAIWorkspaceMode === 'analysis' ? 'active' : ''} onClick={() => setStockAIWorkspaceMode('analysis')}><BrainCircuit size={16} aria-hidden="true" />个股分析</button>
						<button type="button" className={stockAIWorkspaceMode === 'expectation' ? 'active' : ''} onClick={() => setStockAIWorkspaceMode('expectation')}><Target size={16} aria-hidden="true" />隔日预期</button>
						<button type="button" className={stockAIWorkspaceMode === 'risk' ? 'active' : ''} onClick={() => setStockAIWorkspaceMode('risk')}><ShieldCheck size={16} aria-hidden="true" />风控执行</button>
					</> : <>
						<button type="button" className="active">{workspaceMode === 'mastery' ? <BookMarked size={16} aria-hidden="true" /> : workspaceMode === 'reviews' ? <BookOpen size={16} aria-hidden="true" /> : workspaceMode === 'ai' ? <Bot size={16} aria-hidden="true" /> : <Flame size={16} aria-hidden="true" />}{workspaceMode === 'themes' ? '趋势题材' : workspaceMode === 'limit-up' ? '短线连板' : workspaceMode === 'mastery' ? '游资心法' : workspaceMode === 'reviews' ? '复盘日记' : 'AI 对话'}</button>
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
				</div>
			</header>

			{workspaceMode === 'themes' ? <>
			<section className="market-strip" aria-label="市场概览">
				<div><Activity size={16} aria-hidden="true" /><span>主线平均热度</span><strong>{marketPulse.average || '--'}</strong></div>
				<div><Flame size={16} aria-hidden="true" /><span>活跃主线</span><strong>{marketPulse.active}</strong></div>
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
							{!sources.length && <p>数据源状态加载中…</p>}
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
							<strong>按{themeStrengthWindow === 'daily' ? '当日' : '5日'}强度排序</strong>
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
							const sourceLabel = themeSourceLabel(theme);
							return (
								<button
									type="button"
									className={`theme-item ${theme.theme === activeTheme ? 'active' : ''}`}
									key={theme.theme}
									onClick={() => activateTheme(theme.theme)}
								>
									<span className="theme-rank">{String(index + 1).padStart(2, '0')}</span>
									<span className="theme-copy">
										<span className="theme-name-line">
											<strong>{theme.name}</strong>
											{sourceLabel && <em className={theme.provisional ? 'provisional' : 'primary'}>{sourceLabel}</em>}
										</span>
										<small>{emotion.stage} · {theme.leaders?.[0] || theme.top_node || '等待主线证据'}</small>
										<span className="theme-meter"><i style={{ width: `${emotion.score}%` }} /></span>
									</span>
									<span className={`emotion-score ${emotion.tone}`}>{emotion.score}</span>
								</button>
							);
						})}
						{foundationState === 'loading' && <RailSkeleton />}
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
							全部关联 <span>{stockPagination.total}</span>
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
							</div>
						</div>
						<div className="stock-controls">
							<label><span>涨跌幅制度</span><select value={stockLane} onChange={(event) => { setStockLane(event.target.value as ThemeScreenLane); setStockPage(1); }}><option value="all">全部</option><option value="10cm">10cm</option><option value="20cm">20cm</option><option value="30cm">30cm</option></select></label>
							<label><span>排序</span><select value={stockSort} onChange={(event) => { setStockSort(event.target.value as ThemeScreenSort); setStockPage(1); }}><option value="rank_score">全池领导力</option><option value="change_percent">当日涨幅</option><option value="amount">成交额</option><option value="limit_up_streak">连板高度</option></select></label>
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
												setHasManualStockSelection(true);
												setSelectedSymbol(stock.symbol);
											}}
										>
											<td><RoleBadge role={stock.role} regime={stock.limit_regime} /></td>
											<td><strong>{stock.name}</strong><small>{stock.symbol}{stock.live ? ' · 实时' : ''}</small></td>
											<td>{metricsReady ? <StateBadge state={stock.state} /> : <MetricPending failed={metricsFailed} />}</td>
											<td>{metricsReady ? <ScoreCell value={stock.leader_score} /> : <MetricPending failed={metricsFailed} />}</td>
											<td>{metricsReady ? <ScoreCell value={stock.tradability_score} /> : <MetricPending failed={metricsFailed} />}</td>
											<td className={metricsReady ? toneForValue(stock.metrics.return_5d) : undefined}>{metricsReady ? formatPercent(stock.metrics.return_5d) : <MetricPending failed={metricsFailed} />}</td>
											<td className={toneForValue(stock.change_percent)}>{formatPercent(stock.change_percent)}</td>
											<td><span className="node-tags">{stock.nodes.slice(0, 2).join(' / ')}</span></td>
										</tr>
									})}
									{themeState === 'loading' && <TableSkeleton />}
								</tbody>
							</table>
							{themeState === 'error' && <EmptyState icon={<Server size={22} />} title="主线成分加载失败" detail="保留趋势总览，刷新后重试关联个股数据。" />}
							{themeState === 'ready' && !visibleStocks.length && <EmptyState icon={<Search size={22} />} title="没有匹配个股" detail="更换细分节点或清空搜索条件。" />}
						</div>
						<div className="stock-pagination" aria-label="题材个股分页">
							<span>共 {stockPagination.total} 只 · 每页 {stockPagination.page_size} 只</span>
							<div>
								<button type="button" disabled={stockPage <= 1 || themeState === 'loading'} onClick={() => setStockPage((current) => Math.max(1, current - 1))}>上一页</button>
								<strong>{stockPagination.page}/{stockPagination.total_pages || 1}</strong>
								<button type="button" disabled={!stockPagination.has_more || themeState === 'loading'} onClick={() => setStockPage((current) => current + 1)}>下一页</button>
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
									<strong>{formatNumber(selectedStock.price)}</strong>
									<span>{formatPercent(selectedStock.change_percent)}</span>
								</div>
							)}
						</div>
						<CandlestickChart lines={kLines} state={klineState} />
						{selectedStock && (selectedHistoryReady
							? <StockSnapshot stock={selectedStock} />
							: <MetricLoadingPanel failed={selectedHistoryFailed} />)}
					</section>

					<section className="detail-panel identity-panel">
						<div className="panel-label"><Target size={16} aria-hidden="true" />龙头身份模型</div>
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
							{news.slice(0, 7).map((item) => (
								<a href={item.url} target="_blank" rel="noreferrer" key={`${item.id}-${item.published_at}`}>
									<time>{formatTime(item.published_at)}</time><span>{item.title}</span><ChevronRight size={14} aria-hidden="true" />
								</a>
							))}
						</div>
					</section>
				</aside>
			</div>
			</> : workspaceMode === 'limit-up' ? <LimitUpWorkspace data={limitUpData} state={limitUpState} error={limitUpError} emotionData={marketEmotionData} emotionState={marketEmotionState} emotionError={marketEmotionError} onRefresh={refreshLimitUpWorkspace} /> : workspaceMode === 'mastery' ? <TradingMastery config={config} refreshKey={masteryRefreshKey} onAskAI={askMasteryAI} /> : workspaceMode === 'reviews' ? <ReviewDiary config={config} refreshKey={reviewRefreshKey} /> : workspaceMode === 'stock-ai' ? <StockAIAnalysisWorkspace config={config} refreshKey={stockAIRefreshKey} mode={stockAIWorkspaceMode} onAskAI={askStockAnalysisAI} onOpenSettings={() => setSettingsOpen(true)} /> : <AIChatWorkspace config={config} refreshKey={aiRefreshKey} initialPrompt={aiPrefill} onInitialPromptConsumed={() => setAIPrefill('')} onOpenSettings={() => setSettingsOpen(true)} />}

			<footer className="data-footer">
				<div><Wifi size={15} aria-hidden="true" /><span>{config?.backendUrl || '连接本地数据服务中'}</span></div>
				<div><Radio size={15} aria-hidden="true" /><span>{workspaceMode === 'themes' ? '题材与龙一至龙五：开盘啦 · 实时行情：新浪 · K线与领导力：东方财富/新浪' : workspaceMode === 'limit-up' ? '当日涨停池与逐股题材：开盘啦优先 · 历史梯队、缺失股票与行情字段：东方财富补充 · 默认剔除ST' : workspaceMode === 'mastery' ? '来源：trading-mastery/游资心法 · 每日缓存 · 同步至 Hermes Skill 与本地记忆索引' : workspaceMode === 'reviews' ? '复盘文章：本地 SQLite 归档 · 原文观点不代表系统结论' : workspaceMode === 'stock-ai' ? '行情与K线：东方财富/新浪 · 涨停与题材：开盘啦/东方财富 · AI只基于结构化证据总结' : '模型请求由本地后端转发 · API Key 不会暴露给页面 · 对话历史保存在当前设备'}</span></div>
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
	return <span className={`role-badge ${roleClass[role]}`}>{regime && <em>{regime}</em>}{role}</span>;
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

function themeSourceLabel(theme: ThemeOverview) {
	if (theme.provisional) return '趋势新增';
	if (theme.source === 'duanxianxia:kaipanla') return theme.carry_forward ? '' : '开盘啦';
	if (theme.source === 'local-fallback') return '本地补充';
	return '本地趋势';
}

function kaipanlaLeaderRank(role?: string) {
	if (!role?.startsWith('龙')) return 0;
	const value = role.slice(1).trim();
	const chineseRanks: Record<string, number> = { 一: 1, 二: 2, 三: 3, 四: 4, 五: 5 };
	const rank = chineseRanks[value] || Number(value);
	return Number.isInteger(rank) && rank >= 1 && rank <= 5 ? rank : 0;
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

function historyStatusLabel(state: LoadState, ready: number, total: number) {
	switch (state) {
		case 'loading': return total > 0 ? `多日指标 ${ready}/${total}` : '准备多日指标';
		case 'ready': return '多日身份已计算';
		case 'error': return '多日数据降级';
		case 'idle':
		default: return '等待多日数据';
	}
}
