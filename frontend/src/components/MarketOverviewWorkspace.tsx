import {
	Activity,
	AlertTriangle,
	BarChart3,
	Bot,
	Building2,
	ChartCandlestick,
	ChevronRight,
	Clock3,
	Database,
	FileSearch,
	Landmark,
	LoaderCircle,
	Megaphone,
	Newspaper,
	RefreshCw,
	SearchCheck,
	Sparkles,
	TrendingUp,
} from 'lucide-react';
import type { ReactNode } from 'react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import type {
	BackendConfig,
	MarketBillboardDetail,
	MarketBillboardItem,
	MarketFundFlow,
	MarketIndexSeries,
	MarketIndexSnapshot,
	MarketIndustryMomentum,
	MarketResearchItem,
	NewsItem,
	SourceHealth,
	SourceMeta,
	ThemeOverview,
} from '../lib/backend';
import { requestJSON } from '../lib/backend';
import {
	type MarketOverviewView,
	buildMarketModulePrompt,
	buildMarketPulsePrompt,
	findMarketOverviewModule,
	marketOverviewGroups,
	resolveMarketOverviewView,
} from '../lib/market-overview';
import {
	BillboardView,
	type BillboardDetailEntry,
	CoreIndexView,
	FundFlowView,
	IndustryMomentumView,
	ModuleState,
	ResearchView,
} from './market/MarketDataViews';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
	onAskAI: (prompt: string) => void;
};

type LoadState = 'idle' | 'loading' | 'ready' | 'error';
const moduleIcons = {
	pulse: Newspaper,
	'core-indexes': ChartCandlestick,
	'industry-momentum': TrendingUp,
	'industry-flow': Building2,
	'theme-flow': Sparkles,
	'stock-flow': Activity,
	billboard: Landmark,
	announcements: Megaphone,
	'institution-reports': FileSearch,
	'industry-research': SearchCheck,
} satisfies Record<MarketOverviewView, typeof Newspaper>;

export function MarketOverviewWorkspace({ config, refreshKey, onAskAI }: Props) {
	const [activeView, setActiveView] = useState<MarketOverviewView>(() => resolveMarketOverviewView(window.location.hash));
	const [news, setNews] = useState<NewsItem[]>([]);
	const [themes, setThemes] = useState<ThemeOverview[]>([]);
	const [themeMeta, setThemeMeta] = useState<SourceMeta | null>(null);
	const [sources, setSources] = useState<SourceHealth[]>([]);
	const [pulseState, setPulseState] = useState<LoadState>('idle');
	const [pulseError, setPulseError] = useState('');
	const [moduleState, setModuleState] = useState<LoadState>('idle');
	const [moduleError, setModuleError] = useState('');
	const [moduleMeta, setModuleMeta] = useState<SourceMeta | null>(null);
	const [lastUpdated, setLastUpdated] = useState('');
	const [indexes, setIndexes] = useState<MarketIndexSnapshot[]>([]);
	const [selectedIndexID, setSelectedIndexID] = useState('sse');
	const [indexSeries, setIndexSeries] = useState<MarketIndexSeries | null>(null);
	const [seriesLoading, setSeriesLoading] = useState(false);
	const [industries, setIndustries] = useState<MarketIndustryMomentum[]>([]);
	const [flows, setFlows] = useState<MarketFundFlow[]>([]);
	const [billboard, setBillboard] = useState<MarketBillboardItem[]>([]);
	const [billboardDetails, setBillboardDetails] = useState<Record<string, BillboardDetailEntry>>({});
	const [tradeDate, setTradeDate] = useState('');
	const [research, setResearch] = useState<MarketResearchItem[]>([]);
	const [searchDraft, setSearchDraft] = useState('');
	const [submittedQuery, setSubmittedQuery] = useState('');
	const [announcementCategory, setAnnouncementCategory] = useState('all');
	const activeModule = useMemo(() => findMarketOverviewModule(activeView), [activeView]);

	const loadPulse = useCallback(async () => {
		if (!config) {
			setPulseState('error');
			setPulseError('后端尚未连接');
			return;
		}
		setPulseState('loading');
		setPulseError('');
		const [newsResult, themeResult, sourceResult] = await Promise.allSettled([
			requestJSON<{ data: NewsItem[] }>(config, '/api/v1/market/news?source=cls&limit=30'),
			requestJSON<{ data: ThemeOverview[]; meta: SourceMeta }>(config, '/api/v1/themes/overview'),
			requestJSON<{ sources: SourceHealth[] }>(config, '/api/v1/sources'),
		]);

		let successes = 0;
		const errors: string[] = [];
		if (newsResult.status === 'fulfilled') {
			setNews(newsResult.value.data);
			successes += 1;
		} else errors.push(errorMessage(newsResult.reason, '市场快讯加载失败'));
		if (themeResult.status === 'fulfilled') {
			setThemes(themeResult.value.data);
			setThemeMeta(themeResult.value.meta);
			successes += 1;
		} else errors.push(errorMessage(themeResult.reason, '题材快照加载失败'));
		if (sourceResult.status === 'fulfilled') {
			setSources(sourceResult.value.sources);
			successes += 1;
		} else errors.push(errorMessage(sourceResult.reason, '数据源状态加载失败'));
		setLastUpdated(new Date().toISOString());
		setPulseError(errors.join('；'));
		setPulseState(successes > 0 ? 'ready' : 'error');
	}, [config]);

	const loadModule = useCallback(async () => {
		if (!config || activeView === 'pulse') return;
		setModuleState('loading');
		setModuleError('');
		try {
			if (activeView === 'core-indexes') {
				const payload = await requestJSON<{ data: MarketIndexSnapshot[]; meta: SourceMeta }>(config, '/api/v1/market/indexes?scope=core');
				setIndexes(payload.data);
				setModuleMeta(payload.meta);
				setSelectedIndexID((current) => payload.data.some((item) => item.id === current) ? current : payload.data[0]?.id || '');
			} else if (activeView === 'industry-momentum') {
				const payload = await requestJSON<{ data: MarketIndustryMomentum[]; meta: SourceMeta }>(config, '/api/v1/market/industries?limit=80');
				setIndustries(payload.data);
				setModuleMeta(payload.meta);
			} else if (isFlowView(activeView)) {
				const dimension = flowDimension(activeView);
				const payload = await requestJSON<{ data: MarketFundFlow[]; meta: SourceMeta }>(config, `/api/v1/market/flows?dimension=${dimension}&sort=net&limit=100`);
				setFlows(payload.data);
				setModuleMeta(payload.meta);
			} else if (activeView === 'billboard') {
				const query = tradeDate ? `&trade_date=${encodeURIComponent(tradeDate)}` : '';
				const payload = await requestJSON<{ data: MarketBillboardItem[]; meta: SourceMeta }>(config, `/api/v1/market/billboard?limit=100${query}`);
				setBillboard(payload.data);
				setBillboardDetails({});
				setModuleMeta(payload.meta);
			} else {
				const params = new URLSearchParams({ limit: '80' });
				if (submittedQuery) params.set('q', submittedQuery);
				let endpoint = '/api/v1/research/announcements';
				if (activeView === 'announcements') params.set('category', announcementCategory);
				if (activeView === 'institution-reports') endpoint = '/api/v1/research/institution-reports';
				if (activeView === 'industry-research') endpoint = '/api/v1/research/industries';
				const payload = await requestJSON<{ data: MarketResearchItem[]; meta: SourceMeta }>(config, `${endpoint}?${params.toString()}`);
				setResearch(payload.data);
				setModuleMeta(payload.meta);
			}
			setLastUpdated(new Date().toISOString());
			setModuleState('ready');
		} catch (error) {
			setModuleError(errorMessage(error, `${activeModule.name}加载失败`));
			setModuleState('error');
		}
	}, [activeModule.name, activeView, announcementCategory, config, submittedQuery, tradeDate]);

	const loadBillboardDetail = useCallback(async (item: MarketBillboardItem) => {
		if (!config) return;
		const key = billboardDetailKey(item);
		setBillboardDetails((current) => ({ ...current, [key]: { state: 'loading' } }));
		const params = new URLSearchParams({ symbol: item.symbol, trade_date: item.trade_date, reason: item.reason });
		try {
			const payload = await requestJSON<{ data: MarketBillboardDetail; meta: SourceMeta }>(config, `/api/v1/market/billboard/detail?${params.toString()}`);
			setBillboardDetails((current) => ({ ...current, [key]: { state: 'ready', detail: payload.data } }));
		} catch (error) {
			setBillboardDetails((current) => ({ ...current, [key]: { state: 'error', error: errorMessage(error, '买卖五席加载失败') } }));
		}
	}, [config]);

	useEffect(() => {
		if (activeView === 'pulse') void loadPulse();
		else void loadModule();
	}, [activeView, loadModule, loadPulse, refreshKey]);

	useEffect(() => {
		if (!config || activeView !== 'core-indexes' || !selectedIndexID) return;
		let cancelled = false;
		setSeriesLoading(true);
		requestJSON<{ data: MarketIndexSeries }>(config, `/api/v1/market/index-series?id=${encodeURIComponent(selectedIndexID)}&period=day&limit=120`)
			.then((payload) => { if (!cancelled) setIndexSeries(payload.data); })
			.catch((error) => { if (!cancelled) { setIndexSeries(null); setModuleError(errorMessage(error, '指数走势加载失败')); } })
			.finally(() => { if (!cancelled) setSeriesLoading(false); });
		return () => { cancelled = true; };
	}, [activeView, config, refreshKey, selectedIndexID]);

	useEffect(() => {
		const onHashChange = () => {
			if (window.location.hash.startsWith('#market')) setActiveView(resolveMarketOverviewView(window.location.hash));
		};
		window.addEventListener('hashchange', onHashChange);
		return () => window.removeEventListener('hashchange', onHashChange);
	}, []);

	const selectView = (view: MarketOverviewView) => {
		if (view !== activeView && isResearchView(view)) {
			setSearchDraft('');
			setSubmittedQuery('');
			setAnnouncementCategory('all');
		}
		setActiveView(view);
		setModuleError('');
		window.history.replaceState(null, '', `#market/${view}`);
	};

	const activeEvidence = useMemo(() => ({
		indexes: activeView === 'core-indexes' ? indexes : undefined,
		industries: activeView === 'industry-momentum' ? industries : undefined,
		flows: isFlowView(activeView) ? flows : undefined,
		billboard: activeView === 'billboard' ? billboard : undefined,
		research: isResearchView(activeView) ? research : undefined,
		meta: moduleMeta,
	}), [activeView, billboard, flows, indexes, industries, moduleMeta, research]);

	const hasEvidence = activeView === 'pulse'
		? Boolean(news.length || themes.length)
		: Boolean(activeEvidence.indexes?.length || activeEvidence.industries?.length || activeEvidence.flows?.length || activeEvidence.billboard?.length || activeEvidence.research?.length);

	const askAI = () => {
		const asOf = formatDateTime(lastUpdated || new Date().toISOString());
		onAskAI(activeView === 'pulse' ? buildMarketPulsePrompt(news, themes, asOf) : buildMarketModulePrompt(activeView, activeEvidence, asOf));
	};

	const refresh = () => activeView === 'pulse' ? void loadPulse() : void loadModule();
	const submitResearch = () => {
		const next = searchDraft.trim();
		if (next === submittedQuery) void loadModule();
		else setSubmittedQuery(next);
	};

	return <section className="market-overview-workspace">
		<aside className="market-overview-nav" aria-label="行情总览功能">
			<header><BarChart3 size={18} /><div><span>MARKET OVERVIEW</span><strong>行情总览</strong></div></header>
			{marketOverviewGroups.map((group) => <section key={group.id}><h2>{group.name}</h2><div>{group.modules.map((module) => {
				const Icon = moduleIcons[module.id];
				return <button type="button" className={activeView === module.id ? 'active' : ''} onClick={() => selectView(module.id)} key={module.id}><Icon size={16} /><span><strong>{module.name}</strong><small>{module.description}</small></span></button>;
			})}</div></section>)}
		</aside>

		<div className="market-overview-main">
			<header className="market-overview-hero">
				<div><span>{marketOverviewGroups.find((group) => group.modules.some((module) => module.id === activeView))?.name}</span><h2>{activeModule.name}</h2><p>{activeModule.description}</p></div>
				<div><button type="button" className="market-ai-button" onClick={askAI} disabled={!hasEvidence}><Bot size={16} />交给 AI 解读</button><button type="button" className="market-refresh-button" onClick={refresh} disabled={(activeView === 'pulse' ? pulseState : moduleState) === 'loading'}>{(activeView === 'pulse' ? pulseState : moduleState) === 'loading' ? <LoaderCircle className="spin" size={16} /> : <RefreshCw size={16} />}刷新</button></div>
			</header>

			{activeView === 'pulse' ? <PulseView news={news} themes={themes} themeMeta={themeMeta} sources={sources} state={pulseState} error={pulseError} lastUpdated={lastUpdated} /> : <ModuleState state={moduleState} error={moduleError}>
				{activeView === 'core-indexes' && <CoreIndexView indexes={indexes} selectedID={selectedIndexID} onSelect={setSelectedIndexID} series={indexSeries} seriesLoading={seriesLoading} meta={moduleMeta} />}
				{activeView === 'industry-momentum' && <IndustryMomentumView items={industries} meta={moduleMeta} />}
				{isFlowView(activeView) && <FundFlowView key={activeView} items={flows} dimension={flowDimension(activeView)} meta={moduleMeta} />}
				{activeView === 'billboard' && <BillboardView items={billboard} tradeDate={tradeDate} onTradeDate={setTradeDate} meta={moduleMeta} details={billboardDetails} onLoadDetail={loadBillboardDetail} />}
				{isResearchView(activeView) && <ResearchView kind={researchKind(activeView)} items={research} queryDraft={searchDraft} onQueryDraft={setSearchDraft} onSearch={submitResearch} category={announcementCategory} onCategory={setAnnouncementCategory} meta={moduleMeta} />}
			</ModuleState>}
		</div>
	</section>;
}

function billboardDetailKey(item: MarketBillboardItem) {
	return `${item.trade_date}|${item.symbol}|${item.reason}`;
}

function PulseView({ news, themes, themeMeta, sources, state, error, lastUpdated }: { news: NewsItem[]; themes: ThemeOverview[]; themeMeta: SourceMeta | null; sources: SourceHealth[]; state: LoadState; error: string; lastUpdated: string }) {
	const healthySources = sources.filter((source) => source.ok).length;
	return <div className="market-pulse-view">
		<section className="market-pulse-metrics">
			<Metric icon={<Newspaper size={17} />} label="快讯样本" value={state === 'loading' ? '--' : String(news.length)} detail="财联社最新快讯" />
			<Metric icon={<TrendingUp size={17} />} label="题材快照" value={state === 'loading' ? '--' : String(themes.length)} detail={themeMeta?.trade_date || '等待交易日'} />
			<Metric icon={<Database size={17} />} label="数据源" value={sources.length ? `${healthySources}/${sources.length}` : '--'} detail="当前健康状态" />
			<Metric icon={<Clock3 size={17} />} label="最近刷新" value={lastUpdated ? formatTime(lastUpdated) : '--'} detail={themeMeta?.stale ? '题材数据已标记陈旧' : '本机聚合时间'} />
		</section>
		{error && <div className="market-partial-warning"><AlertTriangle size={15} /><span>{error}。已展示其余可用数据。</span></div>}
		<div className="market-pulse-grid">
			<section className="market-pulse-panel market-news-panel"><header><div><span>LIVE FEED</span><h3>盘面快讯</h3></div><em>{news[0]?.meta?.source || 'CLS'}</em></header><div className="market-news-feed">{news.map((item, index) => {
				const content = <><time>{formatTime(item.published_at)}</time><span><strong>{item.title}</strong>{item.content && item.content !== item.title && <small>{item.content}</small>}</span><ChevronRight size={14} /></>;
				return item.url ? <a href={item.url} target="_blank" rel="noreferrer" key={item.id || `${item.title}-${index}`}>{content}</a> : <article key={item.id || `${item.title}-${index}`}>{content}</article>;
			})}{state === 'loading' && <LoadingRows />}{state !== 'loading' && !news.length && <EmptyPanel title="暂无市场快讯" detail="刷新后重试，或检查财联社数据源状态。" />}</div></section>
			<section className="market-pulse-panel market-theme-panel"><header><div><span>THEME SIGNALS</span><h3>题材强度</h3></div><em>{themeMeta?.source || '等待来源'}</em></header><div className="market-theme-ranking">{themes.slice(0, 12).map((theme, index) => <article key={theme.theme}><span>{String(index + 1).padStart(2, '0')}</span><div><strong>{theme.name}</strong><small>{theme.leaders?.slice(0, 2).join(' · ') || theme.top_node || '等待核心标的'}</small></div><em className={toneClass(theme.change_percent)}>{formatPercent(theme.change_percent)}</em></article>)}{state === 'loading' && <LoadingRows compact />}{state !== 'loading' && !themes.length && <EmptyPanel title="暂无题材快照" detail="当前仍可查看市场快讯，题材数据恢复后会自动补齐。" />}</div></section>
		</div>
	</div>;
}

function Metric({ icon, label, value, detail }: { icon: ReactNode; label: string; value: string; detail: string }) {
	return <article><i>{icon}</i><span><small>{label}</small><strong>{value}</strong><em>{detail}</em></span></article>;
}

function LoadingRows({ compact = false }: { compact?: boolean }) {
	return <div className={`market-loading-rows ${compact ? 'compact' : ''}`} aria-label="加载中">{Array.from({ length: compact ? 6 : 8 }, (_, index) => <i key={index} />)}</div>;
}

function EmptyPanel({ title, detail }: { title: string; detail: string }) {
	return <div className="market-empty-panel"><AlertTriangle size={20} /><strong>{title}</strong><span>{detail}</span></div>;
}

function isFlowView(view: MarketOverviewView): view is 'industry-flow' | 'theme-flow' | 'stock-flow' {
	return view === 'industry-flow' || view === 'theme-flow' || view === 'stock-flow';
}

function flowDimension(view: 'industry-flow' | 'theme-flow' | 'stock-flow') {
	return view === 'industry-flow' ? 'industry' : view === 'theme-flow' ? 'theme' : 'stock';
}

function isResearchView(view: MarketOverviewView): view is 'announcements' | 'institution-reports' | 'industry-research' {
	return view === 'announcements' || view === 'institution-reports' || view === 'industry-research';
}

function researchKind(view: 'announcements' | 'institution-reports' | 'industry-research') {
	return view === 'announcements' ? 'announcement' : view === 'institution-reports' ? 'stock' : 'industry';
}

function errorMessage(error: unknown, fallback: string) {
	return error instanceof Error ? error.message : fallback;
}

function formatTime(value?: string) {
	if (!value) return '--:--';
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return '--:--';
	return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
}

function formatDateTime(value: string) {
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return value;
	return date.toLocaleString('zh-CN', { hour12: false });
}

function formatPercent(value: number) {
	if (!Number.isFinite(value)) return '--';
	return `${value > 0 ? '+' : ''}${value.toFixed(2)}%`;
}

function toneClass(value: number) {
	return value > 0 ? 'up' : value < 0 ? 'down' : 'flat';
}
