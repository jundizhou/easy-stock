import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
	Activity,
	BarChart3,
	Clock3,
	Flame,
	LoaderCircle,
	Newspaper,
	Plus,
	RefreshCw,
	Sparkles,
	Target,
	TrendingUp,
	X,
} from 'lucide-react';
import {
	BackendConfig,
	CatalystItem,
	CatalystMeta,
	HotStockRankData,
	KLine,
	LimitUpLadderData,
	LimitUpLadderStock,
	MarketEmotionHistory,
	MarketIndexSeries,
	MarketIndexSnapshot,
	NewsItem,
	Quote,
	SourceMeta,
	StockAIAnalysis,
	StockDirectoryData,
	StockDirectoryEntry,
	requestJSON,
} from '../lib/backend';
import { resolveWatchlistInput } from '../lib/watchlist';
import {
	brokenRate,
	catalystEmptyReason,
	catalystHorizonLabel,
	catalystImpactLabel,
	catalystStrengthLabel,
	catalystTone,
	elapsedTradingRatio,
	formatAmount,
	formatAmountShort,
	formatClock,
	hotRankStatus,
	projectVolume,
	summarizeTelegraph,
	toneForValue,
	volumeBarHeights,
} from '../lib/workbench';

type LoadState = 'idle' | 'loading' | 'ready' | 'error';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
	onOpenStockAnalysis: (analysis: StockAIAnalysis) => void;
};

/** 自选股实时行情的独立存储键（与「自选股日报」的配置完全解耦）。 */
const QUOTE_SYMBOLS_KEY = 'easy-stock.realtime-quote-symbols';
/** 指数快照的刷新节奏（行情类 30 秒）。 */
const QUOTE_REFRESH_MS = 30_000;
const QUOTE_REFRESH_IDLE_MS = 120_000;

// 交易时段内 30 秒，收盘后降到 120 秒：借鉴 dsh-xueqiu 的"交易时段智能刷新"，
// 夜间/周末既减少上游压力，也让长期挂机的工作台不再空转打请求。
// 边界刻意放宽（9:10 前 / 15:20 后视为休市），覆盖集合竞价与尾盘延后。
function quoteRefreshInterval(now = new Date()): number {
	const minutes = now.getHours() * 60 + now.getMinutes();
	const weekday = now.getDay() >= 1 && now.getDay() <= 5;
	const inSession = weekday && ((minutes >= 9 * 60 + 10 && minutes <= 11 * 60 + 35) || (minutes >= 12 * 60 + 55 && minutes <= 15 * 60 + 20));
	return inSession ? QUOTE_REFRESH_MS : QUOTE_REFRESH_IDLE_MS;
}
/** 财联社电报的刷新节奏（60 秒）。 */
const NEWS_REFRESH_MS = 60_000;
/** 情绪催化的刷新节奏（5 分钟）。AI 精筛耗时远高于普通行情，不宜高频。 */
const CATALYST_REFRESH_MS = 300_000;

const readStoredQuoteSymbols = (): string[] => {
	if (typeof window === 'undefined') return [];
	try {
		const raw = window.localStorage.getItem(QUOTE_SYMBOLS_KEY);
		if (!raw) return [];
		const parsed = JSON.parse(raw);
		return Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === 'string' && item.length > 0) : [];
	} catch {
		return [];
	}
};

const writeStoredQuoteSymbols = (symbols: string[]) => {
	if (typeof window === 'undefined') return;
	try {
		window.localStorage.setItem(QUOTE_SYMBOLS_KEY, JSON.stringify(symbols));
	} catch { /* 隐私模式等场景静默降级 */ }
};

const formatPrice = (value?: number) => (typeof value === 'number' && Number.isFinite(value) && value > 0 ? value.toFixed(2) : '--');
const formatPercent = (value?: number) => (typeof value === 'number' && Number.isFinite(value) ? `${value >= 0 ? '+' : ''}${value.toFixed(2)}%` : '--');
const formatSignedAmount = (value: number) => {
	if (!Number.isFinite(value) || value === 0) return '--';
	const sign = value > 0 ? '+' : '-';
	return `${sign}${formatAmountShort(Math.abs(value))}`;
};

/** 工作台首屏：市场情绪 → 指数 → 量能 → 自选/热度，右侧财联社电报。 */
export function WorkbenchWorkspace({ config, refreshKey, onOpenStockAnalysis }: Props) {
	const [emotion, setEmotion] = useState<MarketEmotionHistory | null>(null);
	const [emotionState, setEmotionState] = useState<LoadState>('idle');
	const [indexes, setIndexes] = useState<MarketIndexSnapshot[]>([]);
	const [indexLines, setIndexLines] = useState<Record<string, KLine[]>>({});
	const [indexState, setIndexState] = useState<LoadState>('idle');
	const [limitUp, setLimitUp] = useState<LimitUpLadderData | null>(null);
	const [hotRanks, setHotRanks] = useState<HotStockRankData | null>(null);
	const [hotState, setHotState] = useState<LoadState>('idle');
	const [news, setNews] = useState<NewsItem[]>([]);
	const [newsState, setNewsState] = useState<LoadState>('idle');
	const [expandedNews, setExpandedNews] = useState<Set<string>>(() => new Set());
	const [catalysts, setCatalysts] = useState<CatalystItem[]>([]);
	const [catalystMeta, setCatalystMeta] = useState<CatalystMeta | null>(null);
	const [catalystState, setCatalystState] = useState<LoadState>('idle');

	const [quoteSymbols, setQuoteSymbols] = useState<string[]>(() => readStoredQuoteSymbols());
	const [quotes, setQuotes] = useState<Record<string, Quote>>({});
	const [quoteState, setQuoteState] = useState<LoadState>('idle');
	const [quoteUpdatedAt, setQuoteUpdatedAt] = useState('');
	const [quoteInput, setQuoteInput] = useState('');
	const [analyzingSymbol, setAnalyzingSymbol] = useState('');
	const [notice, setNotice] = useState('');
	const [error, setError] = useState('');

	const directoryRef = useRef<StockDirectoryEntry[] | null>(null);
	const [lastSync, setLastSync] = useState('');

	// ── 市场情绪 + 连板梯队（同一份情绪口径，供 KPI 条使用） ──
	const loadEmotion = useCallback(async () => {
		if (!config) return;
		try {
			const [emotionData, ladderData] = await Promise.all([
				requestJSON<{ data: MarketEmotionHistory }>(config, '/api/v1/short-term/emotion-history?days=20'),
				requestJSON<{ data: LimitUpLadderData }>(config, '/api/v1/short-term/limit-up-ladder').catch(() => ({ data: null as unknown as LimitUpLadderData })),
			]);
			setEmotion(emotionData.data);
			setLimitUp(ladderData.data ?? null);
			setEmotionState('ready');
		} catch (loadError) {
			setEmotionState('error');
			setError(loadError instanceof Error ? loadError.message : '市场情绪加载失败');
		}
	}, [config]);

	// ── 指数：先取快照，再为上证/深证取日线，供量能卡计算近 20 日成交口径 ──
	const loadIndexes = useCallback(async () => {
		if (!config) return;
		try {
			const payload = await requestJSON<{ data: MarketIndexSnapshot[] }>(config, '/api/v1/market/indexes?scope=core');
			const list = payload.data ?? [];
			setIndexes(list);
			setIndexState('ready');
			// 只给 A 股四大指数画走势，避免一次打太多请求。
			// 注意：指数 id 用的是 indexCatalog 里的真实取值（创业板指为 chinext，
			// 不是 chinex），写错会静默少画一条走势。
			const preferred = ['sse', 'szse', 'chinext', 'star50'];
			const targets = preferred
				.map((id) => list.find((item) => item.id === id))
				.filter((item): item is MarketIndexSnapshot => Boolean(item))
				.slice(0, 4);
			const series = await Promise.all(targets.map(async (item) => {
				try {
					const data = await requestJSON<{ data: MarketIndexSeries }>(config, `/api/v1/market/index-series?id=${item.id}&period=day&limit=60`);
					return [item.id, data.data?.lines ?? []] as const;
				} catch {
					return [item.id, [] as KLine[]] as const;
				}
			}));
			setIndexLines(Object.fromEntries(series));
		} catch (loadError) {
			setIndexState('error');
			setError(loadError instanceof Error ? loadError.message : '指数加载失败');
		}
	}, [config]);

	const loadHotRanks = useCallback(async () => {
		if (!config) return;
		try {
			const payload = await requestJSON<{ data: HotStockRankData }>(config, '/api/v1/stocks/hot-ranks');
			setHotRanks(payload.data);
			setHotState('ready');
		} catch {
			setHotState('error');
		}
	}, [config]);

	const loadNews = useCallback(async () => {
		if (!config) return;
		try {
			const payload = await requestJSON<{ data: NewsItem[] }>(config, '/api/v1/market/news?source=cls&limit=30');
			setNews(payload.data ?? []);
			setNewsState('ready');
		} catch {
			setNewsState('error');
		}
	}, [config]);

	const loadCatalysts = useCallback(async () => {
		if (!config) return;
		try {
			// 不带 scan 参数，用后端默认窗口（120 条）；催化是稀有事件，窗口太窄会漏。
			const payload = await requestJSON<{ data: CatalystItem[]; meta: CatalystMeta }>(config, '/api/v1/market/catalysts');
			setCatalysts(payload.data ?? []);
			setCatalystMeta(payload.meta ?? null);
			setCatalystState('ready');
		} catch {
			setCatalystState('error');
		}
	}, [config]);

	const loadQuotes = useCallback(async () => {
		if (!config || quoteSymbols.length === 0) {
			setQuotes({});
			setQuoteState('ready');
			return;
		}
		try {
			const payload = await requestJSON<{ data: Quote[] }>(config, `/api/v1/quotes/realtime?symbols=${encodeURIComponent(quoteSymbols.join(','))}`);
			const next: Record<string, Quote> = {};
			for (const quote of payload.data ?? []) {
				// 后端返回的 symbol 带市场后缀（600519.SH），而列表里存的是纯代码（600519）。
				// 两种写法都建索引，否则行渲染时会查不到行情、价格恒为 "—"。
				const bare = quote.symbol.split('.')[0];
				next[quote.symbol] = quote;
				if (bare) next[bare] = quote;
			}
			setQuotes(next);
			setQuoteUpdatedAt(new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' }));
			setQuoteState('ready');
		} catch {
			setQuoteState('error');
		}
	}, [config, quoteSymbols]);

	const refreshAll = useCallback(async () => {
		setEmotionState((current) => (current === 'ready' ? current : 'loading'));
		setIndexState((current) => (current === 'ready' ? current : 'loading'));
		await Promise.all([loadEmotion(), loadIndexes(), loadHotRanks(), loadNews(), loadQuotes(), loadCatalysts()]);
		setLastSync(new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }));
	}, [loadEmotion, loadIndexes, loadHotRanks, loadNews, loadQuotes, loadCatalysts]);

	useEffect(() => { void refreshAll(); }, [refreshAll, refreshKey]);

	// 首次载入时，如果本地还没有独立的行情自选，用日报配置做一次性初始化。
	useEffect(() => {
		if (!config) return;
		if (window.localStorage.getItem(QUOTE_SYMBOLS_KEY)) return;
		let cancelled = false;
		void (async () => {
			try {
				const payload = await requestJSON<{ data: { watchlist?: string[] } }>(config, '/api/v1/daily-analysis/config');
				if (cancelled) return;
				const seed = payload.data?.watchlist ?? [];
				if (seed.length > 0) {
					setQuoteSymbols(seed);
					writeStoredQuoteSymbols(seed);
				}
			} catch { /* 初始化失败不影响使用 */ }
		})();
		return () => { cancelled = true; };
	}, [config]);

	// 行情类按交易时段自适应刷新（盘中 30 秒 / 休市 120 秒）、新闻 60 秒。
	useEffect(() => {
		if (!config) return;
		let quoteTimer: number;
		const scheduleQuoteRefresh = () => {
			quoteTimer = window.setTimeout(() => {
				void loadIndexes();
				void loadQuotes();
				void loadEmotion();
				scheduleQuoteRefresh();
			}, quoteRefreshInterval());
		};
		scheduleQuoteRefresh();
		const newsTimer = window.setInterval(() => void loadNews(), NEWS_REFRESH_MS);
		const catalystTimer = window.setInterval(() => void loadCatalysts(), CATALYST_REFRESH_MS);
		return () => {
			window.clearTimeout(quoteTimer);
			window.clearInterval(newsTimer);
			window.clearInterval(catalystTimer);
		};
	}, [config, loadIndexes, loadQuotes, loadNews, loadEmotion, loadCatalysts]);

	// ── 派生数据 ──
	const latest = emotion?.latest;
	const raw = latest?.raw;
	const intraday = emotion?.intraday;

	const amount = raw ? raw.limit_up_count : 0;
	const broken = raw ? raw.broken_count : 0;
	const breakRate = raw ? brokenRate(amount, broken) : undefined;

	const advanceCount = raw?.advance_rate ? Math.round(raw.advance_rate * 100) : undefined;
	const declineCount = advanceCount !== undefined ? Math.max(0, 100 - advanceCount) : undefined;

	/**
	 * 近 20 个交易日两市量能（上证 + 深证 日K 相加），按「旧 → 新」排列。
	 *
	 * 口径优先级：
	 *   1) 指数日K带 amount（东财源）→ 直接用真实「成交额」，单位元；
	 *   2) 只有 volume（腾讯回退源）→ 退化为「成交量」，仅作相对高低参考。
	 *
	 * 注意：东财 push2his 在本机网络下是「时好时坏」的（成功窗口约 10%~20%），
	 * 失败时后端会回退腾讯源，于是 amount 全为 0。所以这里必须按「当日是否带
	 * amount」逐根判断，并用 hasAmount 把口径透出到界面，避免把成交量当金额读。
	 */
	const volumeHistory = useMemo(() => {
		const sseLines = indexLines.sse ?? [];
		const szseLines = indexLines.szse ?? [];
		const empty = { values: [] as number[], dates: [] as string[], hasAmount: false };
		if (sseLines.length === 0 || szseLines.length === 0) return empty;

		// 按日期配对，而不是按下标配对：两个市场的日K序列未必等长。
		const szseByDay = new Map<string, KLine>();
		for (const line of szseLines) {
			const day = line.time?.slice(0, 10);
			if (day) szseByDay.set(day, line);
		}

		const values: number[] = [];
		const dates: string[] = [];
		let amountDays = 0;
		for (const sseLine of sseLines) {
			const day = sseLine.time?.slice(0, 10);
			if (!day) continue;
			const szseLine = szseByDay.get(day);
			if (!szseLine) continue;
			const sseAmount = sseLine.amount ?? 0;
			const szseAmount = szseLine.amount ?? 0;
			const hasAmount = sseAmount > 0 && szseAmount > 0;
			if (hasAmount) amountDays += 1;
			// 同一根K线内两种口径不会混用：有金额就用金额，否则两市场都用成交量。
			values.push(hasAmount ? sseAmount + szseAmount : (sseLine.volume ?? 0) + (szseLine.volume ?? 0));
			dates.push(sseLine.time ?? '');
		}
		const tailValues = values.slice(-20);
		const tailDates = dates.slice(-20);
		// 近 20 根里过半带金额，就整体按「成交额」口径展示。
		return { values: tailValues, dates: tailDates, hasAmount: amountDays * 2 > values.length };
	}, [indexLines]);

	const volumeBars = useMemo(() => volumeBarHeights(volumeHistory.values), [volumeHistory]);

	/**
	 * 上一交易日两市成交额基准。
	 *
	 * 优先用日K里的真实金额（东财源可用时）；拿不到时，用
	 * 今日成交额 / (今日成交量 / 上一交易日成交量) 反推一个估算值。
	 * 后者是近似而非实测，界面上按「约」表述。
	 */
	const previousAmount = useMemo(() => {
		const series = volumeHistory.values;
		if (series.length < 2) return undefined;
		const today = series[series.length - 1];
		const previous = series[series.length - 2];
		if (!Number.isFinite(today) || !Number.isFinite(previous) || today <= 0 || previous <= 0) return undefined;
		// 日K口径本身就是成交额时，直接取上一交易日。
		if (volumeHistory.hasAmount) return previous;
		const sse = indexes.find((item) => item.id === 'sse')?.amount ?? 0;
		const szse = indexes.find((item) => item.id === 'szse')?.amount ?? 0;
		const todayAmount = sse + szse;
		if (todayAmount <= 0) return undefined;
		// 注意：日K最后一根可能是今日盘中数据，量比仍成立。
		return todayAmount / (today / previous);
	}, [volumeHistory, indexes]);

	const volume = useMemo(() => {
		// 两市成交额 = 上证指数成交额 + 深证成指成交额（东财快照 f6，单位元）。
		const sse = indexes.find((item) => item.id === 'sse')?.amount ?? 0;
		const szse = indexes.find((item) => item.id === 'szse')?.amount ?? 0;
		const current = sse + szse;
		return projectVolume(current, previousAmount, new Date());
	}, [indexes, previousAmount]);

	const topStreakStock = useMemo<LimitUpLadderStock | null>(() => {
		const levels = limitUp?.current?.levels ?? [];
		for (const level of [...levels].sort((a, b) => b.level - a.level)) {
			if (level.stocks?.length) return level.stocks[0];
		}
		return null;
	}, [limitUp]);

	const hotRows = useMemo(() => (hotRanks?.stocks ?? []).slice(0, 20), [hotRanks]);

	/** 连板梯队里按代码索引的真实连板数据，用于给热榜补齐「状态」。 */
	const limitUpBySymbol = useMemo(() => {
		const lookup = new Map<string, { streak: number; days: number }>();
		const levels = limitUp?.current?.levels ?? [];
		for (const level of levels) {
			for (const stock of level.stocks ?? []) {
				lookup.set(stock.symbol, { streak: stock.streak ?? level.level ?? 0, days: stock.days ?? 0 });
			}
		}
		return lookup;
	}, [limitUp]);

	/**
	 * 热榜需要行情才能显示涨幅，故这里按热榜代码单独取一次实时行情。
	 * 与自选股行情分开请求，避免自选增删影响热榜展示。
	 */
	const [hotQuotes, setHotQuotes] = useState<Record<string, Quote>>({});
	const hotSymbols = useMemo(() => hotRows.map((entry) => entry.symbol).filter(Boolean), [hotRows]);

	useEffect(() => {
		if (!config || hotSymbols.length === 0) return;
		let cancelled = false;
		void (async () => {
			try {
				const payload = await requestJSON<{ data: Quote[] }>(config, `/api/v1/quotes/realtime?symbols=${encodeURIComponent(hotSymbols.join(','))}`);
				if (cancelled) return;
				const next: Record<string, Quote> = {};
				for (const quote of payload.data ?? []) next[quote.symbol] = quote;
				setHotQuotes(next);
			} catch { /* 热榜行情失败时状态列退化为「—」，不影响榜单本身 */ }
		})();
		return () => { cancelled = true; };
	}, [config, hotSymbols]);

	const quoteRows = useMemo(() => {
		return [...quoteSymbols].sort((left, right) => {
			const a = quotes[left]?.change_percent;
			const b = quotes[right]?.change_percent;
			const aValid = typeof a === 'number' && Number.isFinite(a);
			const bValid = typeof b === 'number' && Number.isFinite(b);
			if (!aValid && !bValid) return left.localeCompare(right);
			if (!aValid) return 1;
			if (!bValid) return -1;
			return (b as number) - (a as number);
		});
	}, [quoteSymbols, quotes]);

	// ── 交互 ──
	const resolveSymbolInput = async (rawInput: string): Promise<string | null> => {
		const compact = rawInput.trim().replace(/\s+/g, '').toUpperCase();
		const looksLikeCode = /^(SH|SZ|BJ)?\d{6}$/.test(compact) || /^\d{6}\.(SH|SZ|BJ)$/.test(compact);
		if (!looksLikeCode && config && !directoryRef.current) {
			try {
				const payload = await requestJSON<StockDirectoryData>(config, '/api/v1/stocks/directory');
				directoryRef.current = payload.stocks ?? [];
			} catch {
				directoryRef.current = [];
			}
		}
		const resolution = resolveWatchlistInput(rawInput, directoryRef.current ?? []);
		if (resolution.notice) {
			setNotice(resolution.notice);
			return null;
		}
		return resolution.symbol ?? null;
	};

	const addQuoteSymbol = async () => {
		const rawInput = quoteInput.trim();
		if (!rawInput) return;
		const value = await resolveSymbolInput(rawInput);
		if (!value) return;
		if (quoteSymbols.some((item) => item.toUpperCase() === value.toUpperCase())) { setNotice(`${value} 已在自选`); return; }
		if (quoteSymbols.length >= 20) { setNotice('自选最多 20 只'); return; }
		const next = [...quoteSymbols, value];
		setQuoteSymbols(next);
		writeStoredQuoteSymbols(next);
		setQuoteInput('');
		setNotice('');
	};

	const removeQuoteSymbol = (symbol: string) => {
		const next = quoteSymbols.filter((item) => item !== symbol);
		setQuoteSymbols(next);
		writeStoredQuoteSymbols(next);
	};

	const openStockAnalysis = async (symbol: string) => {
		if (!config) return;
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
	};

	const toggleNews = (key: string) => {
		setExpandedNews((current) => {
			const next = new Set(current);
			if (next.has(key)) next.delete(key);
			else next.add(key);
			return next;
		});
	};

	const syncMeta: SourceMeta | undefined = limitUp?.meta;

	return (
		<div className="wb-wrap">
			{error && <div className="portfolio-error">{error}</div>}
			{notice && <div className="portfolio-notice">{notice}</div>}

			<div className="wb-body">
				<div className="wb-main">
					{/* ── 指数 ── */}
					<section className="wb-card">
						<div className="wb-card-head">
							<h3><TrendingUp size={15} /> 核心指数</h3>
							{lastSync && <small className="wb-muted">同步于 {lastSync} · 每 30 秒刷新</small>}
						</div>
						<div className="wb-index-grid wb-index-compact">
							{indexState === 'loading' && indexes.length === 0 && <div className="wb-skeleton-row"><LoaderCircle className="spin" size={16} /> 加载指数…</div>}
							{indexes.filter((item) => ['sse', 'szse', 'chinex', 'star50'].includes(item.id)).map((item) => (
								<div className="wb-index" key={item.id}>
									<div className="wb-index-head">
										<strong>{item.name}</strong>
											</div>
									<div className={`wb-index-price ${toneForValue(item.change_percent)}`}>{formatPrice(item.price)}</div>
									<div className={`wb-index-change ${toneForValue(item.change_percent)}`}>
										{item.change >= 0 ? '+' : ''}{item.change.toFixed(2)} ({formatPercent(item.change_percent)})
									</div>
									</div>
							))}
							{indexState === 'ready' && indexes.length === 0 && <p className="wb-muted">暂无指数数据</p>}
						</div>
					</section>

					{/* ── 市场量能 ── */}
					<section className="wb-card">
						<div className="wb-card-head">
							<h3><BarChart3 size={15} /> 市场量能</h3>
							<small className="wb-muted">{indexState === 'ready' ? '上证 + 深证成交额' : '加载中'}</small>
						</div>
						<div className="wb-tape-grid wb-tape-compact">
							<div className="wb-tape">
								<span>截至 {new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}</span>
								<strong>{formatAmountShort(volume.current)}</strong>
								<small>{volume.closed ? '已收盘' : `已走完 ${(elapsedTradingRatio(new Date()) * 100).toFixed(1)}% 交易时段`}</small>
							</div>
							<div className="wb-tape">
								<span>预计全天</span>
								<strong className={toneForValue(volume.deltaRatio)}>{formatAmountShort(volume.projected)}</strong>
								<small>{volume.deltaRatio === undefined
									? '缺少上一交易日基准'
									: `约${volume.delta >= 0 ? '放量' : '缩量'} ${formatAmountShort(Math.abs(volume.delta))}（${volume.deltaRatio >= 0 ? '+' : ''}${(volume.deltaRatio * 100).toFixed(1)}%）`}</small>
							</div>
						</div>
						{volumeBars.length > 0 && (
							<div className="wb-volume-block">
								<div className="wb-volume-head">
									<span>{volumeHistory.hasAmount ? `近 ${volumeBars.length} 日两市成交额` : `近 ${volumeBars.length} 日两市成交量`}</span>
									<small className="wb-muted">柱高为相对区间最高的比例</small>
								</div>
								<div className="wb-volume-bars wb-volume-compact">
									{volumeBars.map((height, index) => {
										const isLast = index === volumeBars.length - 1;
										const value = volumeHistory.values[index];
										const tip = volumeHistory.hasAmount
											? `${volumeHistory.dates[index]?.slice(0, 10) ?? ''} 成交额 ${formatAmount(value)}`
											: `${volumeHistory.dates[index]?.slice(0, 10) ?? ''} 成交量 ${formatAmountShort(value)} 手`;
										return <i key={index} className={isLast ? 'today' : ''} style={{ height: `${height}%` }} title={tip} />;
									})}
								</div>
								
							</div>
						)}
						
					</section>

					{/* ── 市场情绪 KPI 条 ── */}
					<section className="wb-card wb-emotion-card">
						<div className="wb-card-head">
							<h3><Activity size={16} /> 市场情绪 <small>{latest?.trade_date || '--'} · {latest?.phase || '待同步'}</small></h3>
							<button type="button" className="portfolio-btn secondary" disabled={emotionState === 'loading'} onClick={() => void refreshAll()}>
								{emotionState === 'loading' ? <LoaderCircle className="spin" size={14} /> : <RefreshCw size={14} />} 刷新
							</button>
						</div>
						<div className="wb-kpi-grid">
							<div className="wb-kpi">
								<span>涨 / 跌家数</span>
								<strong><b className="up">{advanceCount ?? '--'}</b> / <b className="down">{declineCount ?? '--'}</b></strong>
								<small>按涨停家数口径归一</small>
							</div>
							<div className="wb-kpi wb-kpi-accent">
								<span>涨停 / 跌停</span>
								<strong><b className="up">{raw?.limit_up_count ?? '--'}</b> / <b className="down">{raw?.limit_down_count ?? '--'}</b></strong>
								<small>{raw?.board_count ?? 0} 只连板 · 首板 {raw?.first_board_count ?? '--'}</small>
							</div>
							<div className="wb-kpi">
								<span>炸板率</span>
								<strong>{breakRate === undefined ? '--' : `${breakRate.toFixed(1)}%`}</strong>
								<small>{broken} 只开板</small>
							</div>
							<div className="wb-kpi">
								<span>最高连板</span>
								<strong className="up">{raw?.max_streak ? `${raw.max_streak}板` : '--'}</strong>
								<small>{topStreakStock ? `${topStreakStock.name} · ${topStreakStock.streak_label || `第${topStreakStock.days}天`}` : '无连板'}</small>
							</div>
							<div className="wb-kpi">
								<span>昨涨停今溢价</span>
								<strong className={toneForValue(raw?.previous_limit_up_return)}>{raw ? formatPercent(raw.previous_limit_up_return) : '--'}</strong>
								<small className={toneForValue(raw?.previous_board_return)}>连板 {raw ? formatPercent(raw.previous_board_return) : '--'}</small>
							</div>
							<div className="wb-kpi">
								<span>情绪总分</span>
								<strong className={latest ? (latest.emotion_score >= 60 ? 'up' : latest.emotion_score <= 40 ? 'down' : undefined) : undefined}>{latest?.emotion_score ?? '--'}</strong>
								<small>热度 {latest?.scores?.heat ?? '--'} · 结构 {latest?.scores?.structure ?? '--'}</small>
							</div>
						</div>
						{intraday && (
							<div className="wb-emotion-foot">
								<span className={`wb-chip ${intraday.risk_score >= 60 ? 'danger' : intraday.risk_score >= 40 ? 'warn' : 'ok'}`}>风险 {intraday.risk_score}</span>
								<span className="wb-chip">{intraday.session_status || '盘中'}</span>
								<span className="wb-chip">{intraday.breadth || '--'}</span>
								{intraday.summary && <em className="wb-emotion-summary">{intraday.summary}</em>}
								{syncMeta?.stale && <span className="wb-chip warn">数据可能滞后</span>}
							</div>
						)}
					</section>

					{/* ── 自选股实时行情 ── */}
					<section className="wb-card">
						<div className="wb-card-head">
							<h3><Flame size={16} /> 自选股实时行情 <small>{quoteRows.length} 只 · 按涨跌幅排序</small></h3>
							<div className="wb-head-actions">
								{quoteUpdatedAt && <small className="wb-muted">更新于 {quoteUpdatedAt} · 每 30 秒</small>}
								<button type="button" className="portfolio-btn secondary" disabled={quoteState === 'loading'} onClick={() => void loadQuotes()}>
									{quoteState === 'loading' ? <LoaderCircle className="spin" size={14} /> : <RefreshCw size={14} />} 刷新
								</button>
							</div>
						</div>
						<div className="daily-add wb-quote-add">
							<input value={quoteInput} placeholder="代码或名称：300750 / 宁德时代" onChange={(event) => setQuoteInput(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); void addQuoteSymbol(); } }} />
							<button type="button" onClick={() => void addQuoteSymbol()} aria-label="添加自选"><Plus size={14} /></button>
						</div>
						{quoteRows.length === 0 ? (
							<p className="wb-muted">还没有自选。输入代码或名称即可添加（最多 20 只）。</p>
						) : (
							<div className="wb-quote-list">
								{quoteRows.map((symbol) => {
									const quote = quotes[symbol];
									return (
										<div className="wb-quote-row" key={symbol}>
											<span className="wb-quote-name"><strong>{quote?.name || '—'}</strong><small>{symbol}</small></span>
											<span className="wb-quote-price">{formatPrice(quote?.price)}</span>
											<span className={`wb-quote-change ${toneForValue(quote?.change_percent)}`}>{formatPercent(quote?.change_percent)}</span>
											<span className="wb-quote-actions">
												<button type="button" disabled={analyzingSymbol === symbol} onClick={() => void openStockAnalysis(symbol)}>
													{analyzingSymbol === symbol ? <LoaderCircle className="spin" size={12} /> : <Sparkles size={12} />} 分析
												</button>
												<button type="button" aria-label={`移除 ${symbol}`} onClick={() => removeQuoteSymbol(symbol)}><X size={12} /></button>
											</span>
										</div>
									);
								})}
							</div>
						)}
					</section>

					{/* ── 市场热度 ── */}
					<section className="wb-card">
						<div className="wb-card-head">
							<h3><Flame size={16} /> 市场热度 <small>多平台关注度</small></h3>
							{hotRanks?.sources && (
								<div className="wb-source-row">
									{hotRanks.sources.map((source) => (
										<span key={source.id} className={`wb-chip ${source.available ? 'ok' : 'warn'}`}>{source.name} {source.available ? source.count : '不可用'}</span>
									))}
								</div>
							)}
						</div>
						{hotState === 'loading' && hotRows.length === 0 ? (
							<div className="wb-skeleton-row"><LoaderCircle className="spin" size={16} /> 加载热度榜…</div>
						) : hotRows.length === 0 ? (
							<p className="wb-muted">暂无热度数据</p>
						) : (
							<div className="wb-hot-table">
								<table>
									<thead><tr><th>#</th><th>名称</th><th>状态</th><th>日内涨幅</th><th>来源</th></tr></thead>
									<tbody>
										{hotRows.map((entry, index) => {
											const streakInfo = limitUpBySymbol.get(entry.symbol);
											const quote = hotQuotes[entry.symbol];
											const status = hotRankStatus({
												limitUpStreak: streakInfo?.streak,
												limitUpDays: streakInfo?.days,
												changePercent: quote?.change_percent,
											});
											return (
												<tr key={entry.symbol}>
													<td className="wb-hot-rank">{index + 1}</td>
													<td className="wb-hot-name"><strong>{entry.name}</strong><small>{entry.code}</small></td>
													<td>{status ? <span className={`wb-chip ${status.includes('涨停') ? 'danger' : 'warn'}`}>{status}</span> : <span className="wb-muted">—</span>}</td>
													<td className={toneForValue(quote?.change_percent)}>{formatPercent(quote?.change_percent)}</td>
													<td className="wb-hot-sources">
														{entry.ranks?.ths ? <span>同花顺 #{entry.ranks.ths}</span> : null}
														{entry.ranks?.eastmoney ? <span>东财 #{entry.ranks.eastmoney}</span> : null}
													{entry.ranks?.xueqiu ? <span>雪球 #{entry.ranks.xueqiu}</span> : null}
													</td>
												</tr>
											);
										})}
									</tbody>
								</table>
							</div>
						)}
					</section>
				</div>

				{/* ── 右侧：情绪催化 + 财联社电报 ── */}
				<aside className="wb-tape-rail">
					<section className="wb-card wb-catalyst-card">
						<div className="wb-card-head">
							<h3><Target size={16} /> 情绪催化 <small>{catalysts.length} 条</small></h3>
							<button type="button" className="portfolio-btn secondary" disabled={catalystState === 'loading'} onClick={() => void loadCatalysts()}>
								{catalystState === 'loading' ? <LoaderCircle className="spin" size={14} /> : <RefreshCw size={14} />}
							</button>
						</div>
						{catalystState === 'loading' && catalysts.length === 0 ? (
							<div className="wb-skeleton-row"><LoaderCircle className="spin" size={16} /> AI 正在筛选电报…</div>
						) : catalystState === 'error' ? (
							<p className="wb-muted">催化筛选失败，<button type="button" className="wb-link" onClick={() => void loadCatalysts()}>重试</button></p>
						) : catalysts.length === 0 ? (
							<div className="wb-catalyst-empty">
								<strong>暂无够格的催化</strong>
								<span>{catalystEmptyReason(catalystMeta)}</span>
							</div>
						) : (
							<div className="wb-catalyst-list">
								{catalysts.map((item, index) => {
									const tone = catalystTone(item.impact);
									const horizon = catalystHorizonLabel(item.horizon);
									return (
										<article className={`wb-catalyst-item tone-${tone}`} key={item.id || `${item.title}-${index}`}>
											<div className="wb-catalyst-top">
												<span className={`wb-catalyst-impact ${tone}`}>{catalystImpactLabel(item.impact)}</span>
												<span className="wb-catalyst-strength" title={`强度 ${item.strength}`}>
													强度 {catalystStrengthLabel(item.strength)}
													<i style={{ width: `${Math.max(0, Math.min(100, item.strength))}%` }} />
												</span>
												{horizon && <span className="wb-catalyst-horizon">{horizon}</span>}
												<time>{formatClock(item.published_at)}</time>
											</div>
											<h4>{item.title}</h4>
											{item.why && <p className="wb-catalyst-why">{item.why}</p>}
											{(item.sectors?.length || item.stocks?.length) ? (
												<div className="wb-catalyst-tags">
													{item.sectors?.map((sector) => <span className="wb-chip" key={`s-${sector}`}>{sector}</span>)}
													{item.stocks?.map((stock) => <span className="wb-chip stock" key={`k-${stock}`}>{stock}</span>)}
												</div>
											) : null}
											{item.url && (
												<a className="wb-catalyst-link" href={item.url} target="_blank" rel="noreferrer">查看原文</a>
											)}
										</article>
									);
								})}
							</div>
						)}
						{catalystMeta?.note && catalysts.length > 0 && (
							<p className="wb-footnote">{catalystMeta.note}</p>
						)}
						{catalystMeta && catalysts.length > 0 && (
							<p className="wb-footnote">
								扫描 {catalystMeta.scanned} 条 · 进入判断 {catalystMeta.candidates} 条 · 淘汰 {catalystMeta.filtered} 条
								{catalystMeta.model_used ? ' · AI 精筛' : ' · 规则筛选'}
							</p>
						)}
					</section>

					<section className="wb-card wb-news-card">
						<div className="wb-card-head">
							<h3><Newspaper size={16} /> 财联社电报 <small>{news.length} 条</small></h3>
							<button type="button" className="portfolio-btn secondary" disabled={newsState === 'loading'} onClick={() => void loadNews()}>
								{newsState === 'loading' ? <LoaderCircle className="spin" size={14} /> : <RefreshCw size={14} />}
							</button>
						</div>
						{newsState === 'loading' && news.length === 0 ? (
							<div className="wb-skeleton-row"><LoaderCircle className="spin" size={16} /> 加载电报…</div>
						) : news.length === 0 ? (
							<p className="wb-muted">暂无电报</p>
						) : (
							<div className="wb-news-list">
								{news.map((item, index) => {
									const key = item.id || `${item.published_at ?? ''}-${index}`;
									const expanded = expandedNews.has(key);
									return (
										<article className="wb-news-item" key={key}>
											<div className="wb-news-time"><Clock3 size={12} /> {formatClock(item.published_at)}</div>
											<h4>{item.title}</h4>
											{item.content && <p>{expanded ? item.content : summarizeTelegraph(item.content)}</p>}
											{item.content && item.content.length > 90 && (
												<button type="button" className="wb-news-toggle" onClick={() => toggleNews(key)}>{expanded ? '收起' : '展开'}</button>
											)}
										</article>
									);
								})}
							</div>
						)}
					</section>
				</aside>
			</div>
		</div>
	);
}

