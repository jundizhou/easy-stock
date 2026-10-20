import {
	AlertTriangle,
	BookMarked,
	BookOpen,
	ChevronDown,
	ChevronUp,
	Database,
	Flame,
	LoaderCircle,
	RefreshCw,
	Search,
	Tags,
} from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type {
	BackendConfig,
	ThemeLibraryGroup,
	ThemeLibraryLibrary,
	ThemeLibraryLiveQuote,
	ThemeLibraryStock,
	ThemeLibraryTheme,
} from '../lib/backend';
import { requestJSON } from '../lib/backend';
import './theme-library.css';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
};

type LoadState = 'idle' | 'loading' | 'ready' | 'error';
type SortKey = 'hot' | 'change' | 'limit' | 'stocks' | 'updated';

const SORT_OPTIONS: Array<{ id: SortKey; label: string }> = [
	{ id: 'hot', label: '热度' },
	{ id: 'change', label: '涨跌幅' },
	{ id: 'limit', label: '涨停数' },
	{ id: 'stocks', label: '成分数' },
	{ id: 'updated', label: '更新时间' },
];

export function ThemeLibraryWorkspace({ config, refreshKey }: Props) {
	const [library, setLibrary] = useState<ThemeLibraryLibrary | null>(null);
	const [state, setState] = useState<LoadState>('idle');
	const [error, setError] = useState('');
	const [query, setQuery] = useState('');
	const [sort, setSort] = useState<SortKey>('hot');
	const [selectedID, setSelectedID] = useState('');
	const [detail, setDetail] = useState<ThemeLibraryTheme | null>(null);
	const [live, setLive] = useState<Record<string, ThemeLibraryLiveQuote>>({});
	const [detailState, setDetailState] = useState<LoadState>('idle');
	const [detailError, setDetailError] = useState('');
	const [detailRetry, setDetailRetry] = useState(0);
	const [memberQuery, setMemberQuery] = useState('');
	const [introOpen, setIntroOpen] = useState(false);
	const listRequestID = useRef(0);

	const loadLibrary = useCallback(async () => {
		const requestID = listRequestID.current + 1;
		listRequestID.current = requestID;
		if (!config) {
			setState('error');
			setError('后端尚未连接');
			return;
		}
		setState('loading');
		setError('');
		try {
			const params = new URLSearchParams({ limit: '300', sort });
			if (query.trim()) params.set('q', query.trim());
			const payload = await requestJSON<{ data: ThemeLibraryLibrary }>(config, `/api/v1/market/theme-library?${params.toString()}`);
			if (requestID !== listRequestID.current) return;
			setLibrary(payload.data);
			setState('ready');
			setSelectedID((current) => payload.data.items.some((item) => item.source_id === current)
				? current
				: payload.data.items[0]?.source_id || '');
		} catch (requestError) {
			if (requestID !== listRequestID.current) return;
			setState('error');
			setError(errorText(requestError, '题材库加载失败'));
		}
	}, [config, query, sort]);

	useEffect(() => {
		const timer = window.setTimeout(() => { void loadLibrary(); }, 180);
		return () => window.clearTimeout(timer);
	}, [loadLibrary, refreshKey]);

	useEffect(() => {
		if (!config || !selectedID) {
			setDetail(null);
			setDetailState('idle');
			setDetailError('');
			return;
		}
		let cancelled = false;
		setDetail(null);
		setLive({});
		setDetailState('loading');
		setDetailError('');
		setIntroOpen(false);
		setMemberQuery('');
		requestJSON<{ data: ThemeLibraryTheme; live?: Record<string, ThemeLibraryLiveQuote> | null }>(config, `/api/v1/market/theme-library/${encodeURIComponent(selectedID)}`)
			.then((payload) => {
				if (cancelled) return;
				setDetail(payload.data);
				setLive(payload.live || {});
				setDetailState('ready');
			})
			.catch((requestError) => {
				if (cancelled) return;
				setDetail(null);
				setDetailState('error');
				setDetailError(errorText(requestError, '题材详情加载失败'));
			});
		return () => { cancelled = true; };
	}, [config, detailRetry, refreshKey, selectedID]);

	const items = library?.items || [];
	const selectedSummary = items.find((item) => item.source_id === selectedID) || null;
	const selected = detail || selectedSummary;
	const groups = useMemo(() => (detail?.groups || []).filter((group) => group.stocks.length > 0), [detail]);

	const distinctStockCount = useMemo(() => {
		const codes = new Set<string>();
		for (const group of detail?.groups || []) {
			for (const stock of group.stocks || []) codes.add(stock.code);
		}
		return codes.size;
	}, [detail]);

	const filteredGroups = useMemo(() => {
		const needle = memberQuery.trim().toLowerCase();
		if (!needle) return groups.map((group) => ({ group, stocks: group.stocks }));
		return groups.flatMap((group) => {
			const groupMatches = [group.name, ...(group.path || [])].some((text) => text.toLowerCase().includes(needle));
			const stocks = groupMatches
				? group.stocks
				: group.stocks.filter((stock) => matchesStock(stock, needle));
			return stocks.length ? [{ group, stocks }] : [];
		});
	}, [groups, memberQuery]);

	const filteredStockCount = useMemo(
		() => filteredGroups.reduce((total, item) => total + item.stocks.length, 0),
		[filteredGroups],
	);

	const paragraphs = useMemo(() => {
		const text = detail?.introduction || '';
		return text.split(/\n+/).map((line) => line.trim()).filter(Boolean);
	}, [detail?.introduction]);

	const retryDetail = () => setDetailRetry((current) => current + 1);

	const liveChange = (stock: ThemeLibraryStock) => live[stock.code]?.change_percent ?? stock.change_percent;

	return (
		<div className="theme-library-workspace">
			<section className="theme-library-controls" aria-label="题材库筛选">
				<label className="theme-library-search">
					<Search size={14} aria-hidden="true" />
					<input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索题材、细分、股票名称或代码" aria-label="搜索题材" />
				</label>
			</section>

			{library ? (
				<p className="theme-library-notice">
					<em>{library.stats.themes} 题材 · {library.stats.groups} 细分 · {library.stats.relations} 条归属 · {library.stats.stocks} 只去重标的</em>
				</p>
			) : null}

			<div className="theme-library-body">
				<aside className="market-margin-panel theme-library-list-panel" aria-label="题材列表">
					<header>
						<div>
							<h3>题材列表</h3>
							<small>{state === 'loading' && items.length ? '正在更新…' : `${library?.stats.themes ?? items.length} 个题材 · ${library?.stats.groups ?? '--'} 个细分`}</small>
						</div>
						{state === 'loading' ? <LoaderCircle className="spin" size={16} aria-label="加载中" /> : <span className="theme-library-list-count">{library?.total ?? items.length}</span>}
					</header>
					<div className="theme-library-list-sort">
						<div className="theme-library-sort" role="group" aria-label="题材排序">
							{SORT_OPTIONS.map((option) => (
								<button key={option.id} type="button" className={sort === option.id ? 'active' : ''} aria-pressed={sort === option.id} onClick={() => setSort(option.id)}>{option.label}</button>
							))}
						</div>
					</div>
					<div className="theme-library-list">
						{state !== 'error' && items.map((theme, index) => (
							<button
								key={theme.source_id}
								type="button"
								className={`theme-library-item ${selectedID === theme.source_id ? 'active' : ''}`}
								onClick={() => setSelectedID(theme.source_id)}
								aria-pressed={selectedID === theme.source_id}
								title={theme.description || theme.name}
							>
								<span className="theme-library-rank">{String(index + 1).padStart(2, '0')}</span>
								<span className="theme-library-item-main">
									<span className="theme-library-item-title">
										<strong>{theme.name}</strong>
										<em className={toneClass(theme.change_percent)}>{formatPercent(theme.change_percent)}</em>
									</span>
									<span className="theme-library-item-meta">
										<span>{theme.stock_count} 标的</span>
										<span>{theme.group_count} 细分</span>
										{theme.limit_up_count > 0 ? <span className="limit-up">{theme.limit_up_count} 涨停</span> : null}
										<span>{theme.rising_count}:{theme.falling_count} 涨跌</span>
										<span className="hot"><Flame size={11} aria-hidden="true" />{formatCompact(theme.hot)}</span>
									</span>
								</span>
							</button>
						))}
						{state === 'loading' && !items.length ? Array.from({ length: 9 }, (_, index) => <i className="theme-library-skeleton" key={index} />) : null}
						{state === 'error' ? (
							<div className="theme-library-state">
								<AlertTriangle size={22} aria-hidden="true" />
								<strong>题材列表加载失败</strong>
								<span>{error || '请稍后重试'}</span>
								<button type="button" className="theme-library-retry" onClick={() => void loadLibrary()}><RefreshCw size={14} aria-hidden="true" />重试</button>
							</div>
						) : null}
						{state === 'ready' && !items.length ? (
							<div className="theme-library-state"><Search size={22} aria-hidden="true" /><strong>没有匹配的题材</strong><span>换个题材名、细分名或股票代码试试</span></div>
						) : null}
						{library && !library.available && state !== 'loading' ? (
							<div className="theme-library-state"><Database size={22} aria-hidden="true" /><strong>题材库尚未导入</strong><span>{library.message || '请先运行题材库导入命令'}</span></div>
						) : null}
					</div>
				</aside>

				<main className="theme-library-detail-panel" aria-label="题材详情">
					{!selected ? (
						<div className="theme-library-detail-empty market-margin-panel">
							<BookMarked size={28} aria-hidden="true" />
							<strong>{library && !library.available ? '题材库尚未导入' : state === 'error' ? '题材库加载失败' : state === 'loading' ? '正在加载题材库…' : '选择一个题材查看产业链'}</strong>
							<span>{library && !library.available ? (library.message || '请先运行题材库导入命令') : state === 'error' ? (error || '请稍后重试') : '左侧列表按热度排序，支持搜索题材、细分和股票代码。'}</span>
						</div>
					) : (
						<>
							<header className="theme-library-detail-head">
								<div className="theme-library-detail-title">
									<span>题材详情 · {selected.source_id}</span>
									<h2>{selected.name}<em className={toneClass(selected.change_percent)}>{formatPercent(selected.change_percent)}</em></h2>
									<div className="theme-library-detail-stats">
										<span className="theme-library-stat-total"><b>{selected.stock_count}</b> 只标的</span>
										<span className="theme-library-stat-group"><b>{detail?.group_count || selected.group_count}</b> 个细分</span>
										<span className="theme-library-stat-relation"><b>{detail?.relation_count ?? selected.relation_count}</b> 条归属</span>
										<span className="theme-library-stat-limit-up"><b>{selected.limit_up_count}</b> 涨停</span>
										<span className="theme-library-stat-ratio">涨跌比 <b><span className="up">{selected.rising_count}</span>:<span className="down">{selected.falling_count}</span></b></span>
										<span className="theme-library-stat-hot">热度 <b>{formatCompact(selected.hot)}</b></span>
									</div>
								</div>
								{paragraphs.length ? (
									<button
										type="button"
										className={`theme-library-intro-toggle ${introOpen ? 'active' : ''}`}
										onClick={() => setIntroOpen((value) => !value)}
										aria-expanded={introOpen}
									>
										<BookOpen size={14} aria-hidden="true" />
										题材介绍
										{introOpen ? <ChevronUp size={14} aria-hidden="true" /> : <ChevronDown size={14} aria-hidden="true" />}
									</button>
								) : null}
							</header>

							{detailState === 'error' ? (
								<section className="market-margin-panel theme-library-detail-error">
									<AlertTriangle size={22} aria-hidden="true" />
									<strong>题材详情加载失败</strong>
									<span>{detailError || '请稍后重试'}</span>
									<button type="button" className="theme-library-retry" onClick={retryDetail}><RefreshCw size={14} aria-hidden="true" />重试</button>
								</section>
							) : detailState === 'loading' && !detail ? (
								<section className="market-margin-panel theme-library-detail-loading"><LoaderCircle className="spin" size={18} aria-hidden="true" /><span>正在读取产业链与成员…</span></section>
							) : detail ? (
								<>
									{introOpen && paragraphs.length ? (
										<section className="market-margin-panel theme-library-intro">
											<header>
												<div><h3>题材介绍</h3></div>
												<button type="button" className="theme-library-text-button" onClick={() => setIntroOpen(false)}>
													<ChevronUp size={14} aria-hidden="true" />
													收起
												</button>
											</header>
											<div className="theme-library-intro-body">
												{paragraphs.map((paragraph, index) => (
													isHeading(paragraph)
														? <h4 key={index}>{paragraph.replace(/[：:]$/, '')}</h4>
														: <p key={index}>{paragraph}</p>
												))}
											</div>
										</section>
									) : null}

									<section className="market-margin-panel theme-library-members">
										<header>
											<div>
												<h3>产业链与标的</h3>
												<small>
													{memberQuery
														? `${filteredGroups.length} 个细分 · ${filteredStockCount} 只匹配`
														: `${groups.length} 个细分 · ${detail.relation_count} 条归属 · 去重 ${distinctStockCount} 只`}
												</small>
											</div>
											<label className="theme-library-member-filter">
												<Search size={13} aria-hidden="true" />
												<input value={memberQuery} onChange={(event) => setMemberQuery(event.target.value)} placeholder="筛选细分或标的" aria-label="在题材内筛选细分或标的" />
											</label>
										</header>
										<div className="theme-library-matrix">
											{filteredGroups.map(({ group, stocks }) => (
												<article className="theme-library-group-row" key={group.source_id}>
													<div className="theme-library-group-info">
														<strong>{groupLabel(group)}</strong>
														<div className="theme-library-group-meta">
															<em className={toneClass(group.change_percent)}>{formatPercent(group.change_percent)}</em>
															<span>{group.stocks.length} 只</span>
															{group.limit_up_count > 0 ? <span className="limit-up">{group.limit_up_count} 涨停</span> : null}
														</div>
													</div>
													<div className="theme-library-group-stocks">
														{stocks.map((stock, index) => (
															<article className={`theme-library-stock-card ${stock.is_limit_up ? 'limit-up' : ''}`} key={`${stock.code}-${index}`} title={stockTitle(stock)}>
																<strong>{stock.name}</strong>
																<em className={toneClass(liveChange(stock))}>{formatPercent(liveChange(stock))}</em>
																<small>{stock.code}</small>
															</article>
														))}
													</div>
												</article>
											))}
											{!filteredGroups.length ? (
												<div className="theme-library-state"><Tags size={22} aria-hidden="true" /><strong>没有匹配的细分或标的</strong><span>换个股票名称、代码或细分名称试试</span></div>
											) : null}
										</div>
									</section>
								</>
							) : null}
						</>
					)}
				</main>
			</div>
		</div>
	);
}

function matchesStock(stock: ThemeLibraryStock, needle: string) {
	return stock.name.toLowerCase().includes(needle)
		|| stock.code.toLowerCase().includes(needle)
		|| (stock.tags || []).some((tag) => tag.toLowerCase().includes(needle));
}

function stockTitle(stock: ThemeLibraryStock) {
	return [
		`${stock.name} ${stock.code}`,
		formatPercent(stock.change_percent),
		formatPrice(stock.price) !== '--' ? `现价 ${formatPrice(stock.price)}` : '',
		stock.is_limit_up ? '涨停' : '',
		...(stock.tags || []),
		stock.description || '',
	].filter(Boolean).join(' · ');
}

function groupLabel(group: ThemeLibraryGroup) {
	const leaf = group.path?.length ? group.path[group.path.length - 1] : group.name;
	return leaf || group.name;
}

function isHeading(line: string) {
	if (line.length > 32) return false;
	return /[：:]$/.test(line) || /^[一二三四五六七八九十]+、/.test(line) || /^(题材相关新闻|题材基本面介绍|投资逻辑|产业链|核心逻辑|风险提示)/.test(line);
}

function toneClass(value: number) {
	if (value > 0) return 'up';
	if (value < 0) return 'down';
	return 'flat';
}

function formatPercent(value: number) {
	if (!Number.isFinite(value)) return '--';
	return `${value > 0 ? '+' : ''}${value.toFixed(2)}%`;
}

function formatPrice(value: number) {
	if (!Number.isFinite(value) || value <= 0) return '--';
	return value.toFixed(2);
}

function formatCompact(value: number) {
	if (!Number.isFinite(value)) return '--';
	if (value >= 10000) return `${(value / 10000).toFixed(1)}万`;
	if (value >= 1000) return `${(value / 1000).toFixed(1)}k`;
	return String(Math.round(value));
}

function errorText(error: unknown, fallback: string) {
	if (error instanceof Error && error.message.trim()) return error.message;
	return fallback;
}
