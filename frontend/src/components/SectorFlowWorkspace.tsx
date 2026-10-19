import { Activity, AlertTriangle, ArrowDown, ArrowDownUp, ArrowUp, Check, LoaderCircle, RefreshCw, Search, Sparkles, TrendingDown, TrendingUp, Trophy, X } from 'lucide-react';
import { type MouseEvent as ReactMouseEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { BackendConfig, SectorFlowRank, SectorFlowSeries, SectorPriceSeries, SourceMeta } from '../lib/backend';
import { requestJSON } from '../lib/backend';
import {
	MAX_SECTOR_FLOW_SERIES,
	SECTOR_FLOW_AXIS_GUTTER,
	SECTOR_FLOW_CHART_MODES,
	SECTOR_FLOW_DIMENSIONS,
	SECTOR_FLOW_EMPTY_AXIS_EXTENT,
	SECTOR_FLOW_PERIODS,
	SECTOR_PRICE_EMPTY_AXIS_EXTENT,
	buildSectorFlowPrompt,
	defaultSectorFlowSelection,
	estimateSectorFlowLabelWidth,
	formatSectorFlowAmount,
	formatSectorFlowAxis,
	formatSectorFlowPercent,
	formatSectorFlowPriceAxis,
	readSectorFlowSelection,
	sectorFlowAxisScale,
	sectorFlowFundCurves,
	sectorFlowLabelGutter,
	sectorFlowNetValue,
	sectorFlowPriceCurves,
	sectorFlowRatioValue,
	sectorFlowSeriesColor,
	sectorFlowTone,
	shortSectorFlowName,
	toggleSectorFlowSelection,
	visibleSectorFlowRanks,
	writeSectorFlowSelection,
	type SectorFlowChartMode,
	type SectorFlowCurve,
	type SectorFlowDimension,
	type SectorFlowPeriod,
	type SectorFlowSortDirection,
	type SectorFlowSortKey,
} from '../lib/sector-flow';
import './sector-flow.css';

type LoadState = 'idle' | 'loading' | 'ready' | 'error';

// 后端上限 600，覆盖东财概念（约 508 个）与行业（约 496 个）全量板块，
// 否则只取前 200 条会让净流出板块几乎全部落在榜单之外。
const RANK_LIMIT = 600;

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
	onAskAI: (prompt: string) => void;
};

export function SectorFlowWorkspace({ config, refreshKey, onAskAI }: Props) {
	const [dimension, setDimension] = useState<SectorFlowDimension>('concept');
	const [ranks, setRanks] = useState<SectorFlowRank[]>([]);
	const [meta, setMeta] = useState<SourceMeta | null>(null);
	const [state, setState] = useState<LoadState>('idle');
	const [error, setError] = useState('');
	const [query, setQuery] = useState('');
	const [period, setPeriod] = useState<SectorFlowPeriod>('main');
	const [sortKey, setSortKey] = useState<SectorFlowSortKey>('main');
	const [sortDirection, setSortDirection] = useState<SectorFlowSortDirection>('desc');
	const [selectedOnly, setSelectedOnly] = useState(false);
	// 曲线区口径：资金曲线（亿元）与板块价格分时（%）量纲不同，用切换而不是同图叠加。
	const [chartMode, setChartMode] = useState<SectorFlowChartMode>('fund');
	// 曲线失败时的就地重试：上游是按时间窗放行的，等一会儿再点比重启页面方便。
	const [retryKey, setRetryKey] = useState(0);
	const [selectionByDimension, setSelectionByDimension] = useState<Record<SectorFlowDimension, string[] | null>>(() => {
		const storage = typeof window === 'undefined' ? null : window.localStorage;
		return { concept: readSectorFlowSelection(storage, 'concept'), industry: readSectorFlowSelection(storage, 'industry') };
	});
	const selection = useMemo(() => selectionByDimension[dimension] ?? [], [selectionByDimension, dimension]);
	const [seriesByCode, setSeriesByCode] = useState<Record<string, SectorFlowSeries>>({});
	const [seriesState, setSeriesState] = useState<LoadState>('idle');
	const [seriesError, setSeriesError] = useState('');
	const [seriesNotice, setSeriesNotice] = useState('');
	const [hoverIndex, setHoverIndex] = useState<number | null>(null);
	// 点选聚焦：某一条曲线选中后其余曲线与末端标签半透明（与线上一致），再点一次或点空白取消。
	const [focusCode, setFocusCode] = useState<string | null>(null);
	// 板块价格分时（东财分时行情）与分时资金流是两条独立的上游，状态也各自独立：
	// 一边失败不会把另一边的曲线清掉。
	const [priceByCode, setPriceByCode] = useState<Record<string, SectorPriceSeries>>({});
	const [priceState, setPriceState] = useState<LoadState>('idle');
	const [priceError, setPriceError] = useState('');
	const [priceNotice, setPriceNotice] = useState('');
	const [priceHoverIndex, setPriceHoverIndex] = useState<number | null>(null);

	// 只持久化手动选择；未调整过的默认项在下次打开页面时从最新榜单重新选取。
	const setSelection = useCallback((next: string[]) => {
		setSelectionByDimension((current) => ({ ...current, [dimension]: next }));
		writeSectorFlowSelection(typeof window === 'undefined' ? null : window.localStorage, dimension, next);
	}, [dimension]);

	const selectDimension = useCallback((next: SectorFlowDimension) => {
		if (next === dimension) return;
		setDimension(next);
		setRanks([]);
		setMeta(null);
		setHoverIndex(null);
		setPriceHoverIndex(null);
		setFocusCode(null);
	}, [dimension]);

	useEffect(() => {
		if (!config) return;
		let cancelled = false;
		setState('loading');
		setError('');
		requestJSON<{ data: SectorFlowRank[]; meta: SourceMeta }>(config, `/api/v1/market/sector-flows?dimension=${dimension}&limit=${RANK_LIMIT}`)
			.then((payload) => {
				if (cancelled) return;
				const nextRanks = payload.data || [];
				setRanks(nextRanks);
				if (nextRanks.length) {
					setSelectionByDimension((current) => current[dimension] !== null
						? current
						: { ...current, [dimension]: defaultSectorFlowSelection(nextRanks) });
				}
				setMeta(payload.meta);
				setState('ready');
			})
			.catch((reason) => {
				if (cancelled) return;
				setError(errorMessage(reason, '板块资金榜加载失败'));
				setState('error');
			});
		return () => {
			cancelled = true;
		};
	}, [config, dimension, refreshKey]);

	useEffect(() => {
		if (!config) return;
		const codes = selection.slice(0, MAX_SECTOR_FLOW_SERIES);
		if (!codes.length) {
			setSeriesByCode({});
			setSeriesState('idle');
			setSeriesError('');
			setSeriesNotice('');
			setHoverIndex(null);
			return;
		}
		let cancelled = false;
		setSeriesState('loading');
		setSeriesError('');
		requestJSON<{ data: SectorFlowSeries[]; meta: SourceMeta }>(config, `/api/v1/market/sector-flow-intraday?codes=${codes.join(',')}`)
			.then((payload) => {
				if (cancelled) return;
				const next: Record<string, SectorFlowSeries> = {};
				(payload.data || []).forEach((item) => {
					next[item.code] = item;
				});
				// 与上一次结果合并：个别板块本次失败时仍保留它之前的曲线。
				setSeriesByCode((current) => ({ ...current, ...next }));
				setSeriesNotice(payload.meta?.fallback_reason || '');
				setSeriesState('ready');
			})
			.catch((reason) => {
				if (cancelled) return;
				// 刷新失败时保留上一次的曲线，避免盘中短暂抖动把已选对比清空。
				setSeriesError(errorMessage(reason, '板块分时资金加载失败'));
				setSeriesState('error');
			});
		return () => {
			cancelled = true;
		};
	}, [config, selection, refreshKey, retryKey]);

	useEffect(() => {
		if (!config) return;
		const codes = selection.slice(0, MAX_SECTOR_FLOW_SERIES);
		if (!codes.length) {
			setPriceByCode({});
			setPriceState('idle');
			setPriceError('');
			setPriceNotice('');
			setPriceHoverIndex(null);
			return;
		}
		let cancelled = false;
		setPriceState('loading');
		setPriceError('');
		requestJSON<{ data: SectorPriceSeries[]; meta: SourceMeta }>(config, `/api/v1/market/sector-price-intraday?codes=${codes.join(',')}`)
			.then((payload) => {
				if (cancelled) return;
				const next: Record<string, SectorPriceSeries> = {};
				(payload.data || []).forEach((item) => {
					next[item.code] = item;
				});
				setPriceByCode((current) => ({ ...current, ...next }));
				setPriceNotice(payload.meta?.fallback_reason || '');
				setPriceState('ready');
			})
			.catch((reason) => {
				if (cancelled) return;
				setPriceError(errorMessage(reason, '板块分时价格加载失败'));
				setPriceState('error');
			});
		return () => {
			cancelled = true;
		};
	}, [config, selection, refreshKey, retryKey]);

	const toggleSelection = useCallback((code: string) => {
		const next = toggleSectorFlowSelection(selection, code);
		if (next.limited) {
			setSeriesNotice(`最多同时对比 ${MAX_SECTOR_FLOW_SERIES} 个板块，请先移除一个`);
			return;
		}
		setSeriesNotice('');
		setSelection(next.selection);
	}, [selection, setSelection]);

	const visibleRanks = useMemo(
		() => visibleSectorFlowRanks(ranks, { query, selectedOnly, selection, sortKey, direction: sortDirection, period }),
		[ranks, query, selectedOnly, selection, sortKey, sortDirection, period],
	);

	// 点击表头：先切排序字段（默认从大到小），再次点击同一字段切换升/降序，升序即可看净流出榜。
	const changeSort = useCallback((key: SectorFlowSortKey) => {
		if (key === sortKey) {
			setSortDirection((current) => (current === 'desc' ? 'asc' : 'desc'));
			return;
		}
		setSortKey(key);
		setSortDirection('desc');
	}, [sortKey]);

	// 资金口径滑动按钮：切换列表资金列（当日/5日/10日），并默认按该口径排序，排序方向保持用户当前选择。
	// 占比排序（'ratio'）本身也跟随口径，切换后保持按占比排，不跳回金额排序。
	const selectPeriod = useCallback((next: SectorFlowPeriod) => {
		setPeriod(next);
		setSortKey((current) => (current === 'ratio' ? 'ratio' : next));
	}, []);

	const periodOption = SECTOR_FLOW_PERIODS.find((item) => item.id === period) || SECTOR_FLOW_PERIODS[0];
	const periodIndex = Math.max(0, SECTOR_FLOW_PERIODS.findIndex((item) => item.id === period));
	const dimensionIndex = Math.max(0, SECTOR_FLOW_DIMENSIONS.findIndex((item) => item.id === dimension));
	const ratioLabel = period === 'main' ? '净流入占比' : `${periodOption.label}净占比`;
	const directionHint = sortDirection === 'desc' ? '升序（净流出榜）' : '降序（净流入榜）';
	const moneyTitle = sortKey === period
		? `${periodOption.hint} · 再点一次切换为${directionHint}`
		: `${periodOption.hint} · 点击按${periodOption.label}净流入排队`;
	const ratioTitle = sortKey === 'ratio'
		? `${periodOption.hint}主力净占比 · 再点一次切换为${sortDirection === 'desc' ? '升序' : '降序'}`
		: `按${periodOption.label}主力净占比排队 · 点击切换排序`;
	const changeTitle = sortKey === 'change'
		? `按当日涨跌幅排序 · 再点一次切换为${sortDirection === 'desc' ? '升序' : '降序'}`
		: '按当日涨跌幅排序 · 点击按涨幅排队';

	const ratioExtent = useMemo(() => {
		const values = visibleRanks.map((item) => Math.abs(sectorFlowRatioValue(item, period)));
		return values.length ? Math.max(...values, 1) : 1;
	}, [visibleRanks, period]);

	const chartSeries = useMemo(() => selection.map((code) => seriesByCode[code]).filter((item): item is SectorFlowSeries => Boolean(item)), [selection, seriesByCode]);
	const tradeDate = chartSeries[0]?.trade_date || '';
	const fundCurves = useMemo(() => sectorFlowFundCurves(chartSeries), [chartSeries]);
	const priceSeries = useMemo(() => selection.map((code) => priceByCode[code]).filter((item): item is SectorPriceSeries => Boolean(item)), [selection, priceByCode]);
	const priceCurves = useMemo(() => sectorFlowPriceCurves(priceSeries), [priceSeries]);

	// 两种口径的差异只集中在这一处：曲线数据、纵轴格式、空态量级与文案。
	// 两条上游的状态彼此独立，切换口径只是换一套数据与格式化，不会互相清空。
	const chartModeIndex = Math.max(0, SECTOR_FLOW_CHART_MODES.findIndex((item) => item.id === chartMode));
	// 被聚焦的板块从选中列表里移除后自动回到「全部实色」，不用额外的清理 effect。
	const activeFocusCode = focusCode && selection.includes(focusCode) ? focusCode : null;
	const chartView = chartMode === 'fund'
		? {
			curves: fundCurves,
			state: seriesState,
			error: seriesError,
			notice: seriesNotice,
			hoverIndex,
			onHover: setHoverIndex,
			focusCode: activeFocusCode,
			onFocus: setFocusCode,
			formatAxis: formatSectorFlowAxis,
			formatValue: formatSectorFlowAmount,
			emptyExtent: SECTOR_FLOW_EMPTY_AXIS_EXTENT,
			ariaLabel: '板块分时累计净流入曲线',
			valueHint: '当日累计主力净流入',
			errorTitle: '分时资金加载失败',
			errorHint: '东方财富分时资金接口这次不可达，本地采样曲线暂时也没有可用数据；稍后重试即可。',
			loadingText: '正在加载分时资金…',
			emptyTitle: '暂时取不到分时资金',
			footer: '单位：亿元 · 曲线为当日累计主力净流入，鼠标移入查看任一时点 · 上游不可用时自动改用本地采样（每 3 分钟）。',
		}
		: {
			curves: priceCurves,
			state: priceState,
			error: priceError,
			notice: priceNotice,
			hoverIndex: priceHoverIndex,
			onHover: setPriceHoverIndex,
			focusCode: activeFocusCode,
			onFocus: setFocusCode,
			formatAxis: formatSectorFlowPriceAxis,
			formatValue: formatSectorFlowPercent,
			emptyExtent: SECTOR_PRICE_EMPTY_AXIS_EXTENT,
			ariaLabel: '板块价格分时曲线',
			valueHint: '分时涨跌幅',
			errorTitle: '分时价格加载失败',
			errorHint: '东方财富分时行情接口这次不可达，本地采样曲线暂时也没有可用数据；稍后重试即可。',
			loadingText: '正在加载分时价格…',
			emptyTitle: '暂时取不到分时价格',
			footer: '单位：% · 曲线为板块分时涨跌幅（相对昨收），鼠标移入查看任一时点 · 上游不可用时自动改用本地采样（每 3 分钟）。',
		};

	const summary = useMemo(() => {
		const inflow = ranks.filter((item) => item.main_net_inflow > 0).length;
		const total = ranks.reduce((sum, item) => sum + item.main_net_inflow, 0);
		return { inflow, outflow: ranks.length - inflow, total };
	}, [ranks]);

	// 榜首板块按当前资金口径取净流入最大者（与榜单当前排序、搜索无关），切到 5 日 / 10 日时同步变化。
	const topBoard = useMemo(
		() => ranks.reduce<SectorFlowRank | null>((best, item) => (!best || sectorFlowNetValue(item, period) > sectorFlowNetValue(best, period) ? item : best), null),
		[ranks, period],
	);

	const askAI = () => {
		onAskAI(buildSectorFlowPrompt({ dimension, ranks, series: chartSeries, meta, time: new Date().toLocaleString('zh-CN') }));
	};

	return (
		<div className="sector-flow-workspace">
			<section className="sector-flow-control-panel" aria-label="资金流向筛选与操作">
				<div className="sector-flow-tabs" role="group" aria-label="板块维度">
					<i className="sector-flow-tabs-thumb" style={{ transform: `translateX(${dimensionIndex * 100}%)` }} aria-hidden="true" />
					{SECTOR_FLOW_DIMENSIONS.map((item) => (
						<button key={item.id} type="button" className={dimension === item.id ? 'active' : ''} onClick={() => selectDimension(item.id)} title={item.hint} aria-pressed={dimension === item.id}>{item.label}</button>
					))}
				</div>
				<div className="sector-flow-tabs sector-flow-periods" role="group" aria-label="资金口径">
					<i className="sector-flow-period-thumb" style={{ transform: `translateX(${periodIndex * 100}%)` }} aria-hidden="true" />
					{SECTOR_FLOW_PERIODS.map((item) => (
						<button key={item.id} type="button" className={period === item.id ? 'active' : ''} onClick={() => selectPeriod(item.id)} title={`查看${item.hint}`} aria-pressed={period === item.id}>{item.label}</button>
					))}
				</div>
				<label className="sector-flow-search">
					<Search size={14} aria-hidden="true" />
					<input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={`搜索名称或代码，如${dimension === 'concept' ? '半导体' : '银行'}`} aria-label="搜索板块名称" />
				</label>
				<button type="button" className={`sector-flow-flag ${selectedOnly ? 'active' : ''}`} aria-pressed={selectedOnly} onClick={() => setSelectedOnly((value) => !value)}><Check size={14} aria-hidden="true" />已选中</button>
				<div className="sector-flow-actions">
					<button type="button" className="sector-flow-button" onClick={() => setSelection([])} disabled={!selection.length}><X size={16} aria-hidden="true" />清空曲线</button>
					<button type="button" className="sector-flow-button primary" onClick={askAI} disabled={!ranks.length}><Sparkles size={16} aria-hidden="true" />问 AI</button>
				</div>
			</section>

			<section className="market-flow-summary sector-flow-summary" aria-label="板块资金概览">
				<article><i><Trophy size={17} aria-hidden="true" /></i><span><small>榜首板块</small><strong className="is-text">{topBoard?.name || '--'}</strong><em>{topBoard ? `${periodOption.label}净流入 ${formatSectorFlowAmount(sectorFlowNetValue(topBoard, period))}` : '等待资金榜单'}</em></span></article>
				<article><i><TrendingUp size={17} aria-hidden="true" /></i><span><small>净流入板块</small><strong className="up">{summary.inflow}</strong><em>主力净流入为正</em></span></article>
				<article><i><TrendingDown size={17} aria-hidden="true" /></i><span><small>净流出板块</small><strong className="down">{summary.outflow}</strong><em>主力净流入为负</em></span></article>
				<article><i><Activity size={17} aria-hidden="true" /></i><span><small>合计净流入</small><strong className={sectorFlowTone(summary.total)}>{formatSectorFlowAmount(summary.total)}</strong><em>主力净流入 = 超大单 + 大单</em></span></article>
			</section>

			<div className="sector-flow-body">
				<section className="market-margin-panel sector-flow-ranking" aria-label="板块资金流">
					<header>
						<div>
							<span>SECTOR RANKING</span>
							<h3>板块资金流</h3>
							<small>{meta ? '点击表头排序，点击板块叠加资金 / 价格曲线' : '等待数据来源'}</small>
						</div>
						<em className="sector-flow-head-pill">已选 {selection.length}/{MAX_SECTOR_FLOW_SERIES}</em>
					</header>

					{state === 'error' ? <div className="sector-flow-state"><AlertTriangle size={22} aria-hidden="true" /><strong>数据加载失败</strong><span>{error || '请稍后重试'}</span></div> : (
						<div className="sector-flow-table" role="table" aria-label="板块资金流排名">
							<div className="sector-flow-table-head" role="row">
								<span role="columnheader">板块名称</span>
								<SortHeader
									label={ratioLabel}
									title={ratioTitle}
									active={sortKey === 'ratio'}
									direction={sortKey === 'ratio' ? sortDirection : undefined}
									onClick={() => changeSort('ratio')}
								/>
								<SortHeader
									label={periodOption.label}
									title={moneyTitle}
									active={sortKey === period}
									direction={sortKey === period ? sortDirection : undefined}
									onClick={() => changeSort(period)}
								/>
								<SortHeader
									label="涨幅"
									title={changeTitle}
									active={sortKey === 'change'}
									direction={sortKey === 'change' ? sortDirection : undefined}
									onClick={() => changeSort('change')}
								/>
							</div>
							<div className="sector-flow-table-body" role="rowgroup">
								{state === 'loading' && !ranks.length ? Array.from({ length: 10 }, (_, index) => <i key={index} className="sector-flow-skeleton" />) : null}
								{visibleRanks.map((item, index) => {
									const selected = selection.includes(item.code);
									const order = selection.indexOf(item.code);
									const ratio = sectorFlowRatioValue(item, period);
									const net = sectorFlowNetValue(item, period);
									return (
										<button
											key={item.code}
											type="button"
											role="row"
											className={`sector-flow-row ${selected ? 'selected' : ''}`}
											onClick={() => toggleSelection(item.code)}
											aria-selected={selected}
											title={selected ? '点击移除曲线' : '点击添加曲线'}
										>
											<span className="sector-flow-cell-name" role="cell">
												<i className="sector-flow-rank">{String(index + 1).padStart(2, '0')}</i>
												<i className="sector-flow-mark" style={selected ? { borderColor: sectorFlowSeriesColor(order), background: sectorFlowSeriesColor(order) } : undefined} aria-hidden="true" />
												<span>
													<strong>{item.name}</strong>
													<small>{item.code}</small>
												</span>
											</span>
											<span className="sector-flow-cell-ratio" role="cell">
												<b className={ratio > 0 ? 'up' : ratio < 0 ? 'down' : 'flat'} style={{ width: `${Math.min(100, Math.abs(ratio) / ratioExtent * 100)}%` }} />
												<strong>{formatSectorFlowPercent(ratio)}</strong>
											</span>
											<strong className={sectorFlowTone(net)} role="cell">{formatSectorFlowAmount(net)}</strong>
											<em className={sectorFlowTone(item.change_percent)} role="cell">{formatSectorFlowPercent(item.change_percent)}</em>
										</button>
									);
								})}
								{state !== 'loading' && !visibleRanks.length ? <div className="sector-flow-state"><TrendingUp size={22} aria-hidden="true" /><strong>没有匹配的板块</strong><span>{selectedOnly ? '先在上方榜单里选中板块，再切回已选中视图' : '换个关键词试试'}</span></div> : null}
							</div>
						</div>
					)}
					{meta?.fallback_reason ? <footer>{meta.fallback_reason}</footer> : null}
				</section>

				<section className="market-margin-panel sector-flow-chart-panel" aria-label={chartMode === 'fund' ? '资金曲线' : '板块价格分时曲线'}>
					<header>
						<div>
							<span>{chartMode === 'fund' ? 'INTRADAY NET INFLOW' : 'INTRADAY PRICE TREND'}</span>
							<h3>{chartMode === 'fund' ? '资金曲线' : '板块价格分时'}</h3>
							<small>{chartMode === 'fund' ? '点击左侧列表添加或删除曲线 · 末端为当日累计主力净流入' : '同一批板块的分时涨跌幅（相对昨收） · 末端为最新涨跌幅'}</small>
						</div>
						<div className="sector-flow-chart-head">
							<div className="sector-flow-tabs sector-flow-chart-tabs" role="group" aria-label="曲线口径">
								<i className="sector-flow-tabs-thumb" style={{ transform: `translateX(${chartModeIndex * 100}%)` }} aria-hidden="true" />
								{SECTOR_FLOW_CHART_MODES.map((item) => (
									<button key={item.id} type="button" className={chartMode === item.id ? 'active' : ''} onClick={() => setChartMode(item.id)} title={item.hint} aria-pressed={chartMode === item.id}>{item.label}</button>
								))}
							</div>
							{chartView.curves.length ? (
								<div className="market-margin-legend sector-flow-legend">
									{chartView.curves.map((item, index) => {
										const point = chartView.hoverIndex === null ? item.points.at(-1) : item.points[Math.min(chartView.hoverIndex, item.points.length - 1)];
										const value = point?.value || 0;
										const focused = chartView.focusCode === item.code;
										return (
											<span key={item.code} className={`sector-flow-legend-item${focused ? ' focused' : ''}`} style={{ color: sectorFlowSeriesColor(index) }} title={`${item.name} · ${chartView.valueHint}`}>
												{/* 板块名可点：点一下只看这一条（其余曲线与末端标签半透明），再点一次恢复。 */}
												<button type="button" className="sector-flow-legend-focus" onClick={() => setFocusCode((current) => (current === item.code ? null : item.code))} aria-pressed={focused} title={focused ? `取消只看 ${item.name}` : `只看 ${item.name}，其余曲线半透明`}>
													{shortSectorFlowName(item.name)}
													<strong className={sectorFlowTone(value)}>{chartView.formatValue(value)}</strong>
												</button>
												<button type="button" className="sector-flow-legend-remove" onClick={() => toggleSelection(item.code)} aria-label={`移除 ${item.name}`} title="移除该曲线"><X size={12} aria-hidden="true" /></button>
											</span>
										);
									})}
								</div>
							) : <em className="sector-flow-head-pill">{selection.length ? `${selection.length} 个板块` : '等待选择'}</em>}
						</div>
					</header>
					<div className="sector-flow-chart-body">
						{chartView.notice ? <p className="sector-flow-notice">{chartView.notice}</p> : null}
						{chartView.state === 'error' ? <p className="sector-flow-notice">{chartView.errorHint}</p> : null}
						{chartView.state === 'error' && chartView.curves.length ? <p className="sector-flow-notice">{chartView.error || '部分板块加载失败'}</p> : null}
						<div className="sector-flow-chart-stage">
							<SectorFlowChart
								curves={chartView.curves}
								formatAxis={chartView.formatAxis}
								formatValue={chartView.formatValue}
								emptyExtent={chartView.emptyExtent}
								ariaLabel={chartView.ariaLabel}
								hoverIndex={chartView.hoverIndex}
								onHover={chartView.onHover}
								focusCode={chartView.focusCode}
								onFocus={chartView.onFocus}
							/>
							{/* 只有一条曲线都画不出来时才用失败态盖住整块：已经拿到的曲线不能被后一次失败清掉。 */}
							{chartView.state === 'error' && !chartView.curves.length ? (
								<div className="sector-flow-state">
									<AlertTriangle size={22} aria-hidden="true" />
									<strong>{chartView.errorTitle}</strong>
									<span>{chartView.error || '请稍后重试'}</span>
									<button type="button" className="sector-flow-button" onClick={() => setRetryKey((value) => value + 1)} disabled={!config}><RefreshCw size={14} aria-hidden="true" />重试</button>
								</div>
							) : null}
							{chartView.state === 'loading' && !chartView.curves.length ? <div className="market-chart-loading"><span className="sector-flow-loading"><LoaderCircle className="spin" size={16} aria-hidden="true" />{chartView.loadingText}</span></div> : null}
							{!selection.length ? <div className="sector-flow-state"><TrendingUp size={22} aria-hidden="true" /><strong>还没有选择板块</strong><span>在左侧榜单点击板块名称，这里就能画出它们的分时曲线</span></div> : null}
							{selection.length && !chartView.curves.length && chartView.state === 'ready' ? <div className="sector-flow-state"><AlertTriangle size={22} aria-hidden="true" /><strong>{chartView.emptyTitle}</strong><span>该板块当日数据尚未生成，稍后刷新或换一个板块试试</span></div> : null}
						</div>
					</div>
					<footer>{chartView.footer}</footer>
				</section>
			</div>
		</div>
	);
}

function SortHeader({ label, title, active = false, direction, onClick }: { label: string; title?: string; active?: boolean; direction?: SectorFlowSortDirection; onClick?: () => void }) {
	const Icon = direction === 'asc' ? ArrowUp : direction === 'desc' ? ArrowDown : ArrowDownUp;
	return (
		<span role="columnheader" aria-sort={active && direction ? (direction === 'asc' ? 'ascending' : 'descending') : undefined}>
			{onClick ? (
				<button type="button" className={`market-sort-button ${active ? 'active' : ''}`} title={title || `按${label}排序`} aria-pressed={active} onClick={onClick}>
					<span>{label}</span>
					<i className="sector-flow-sort-arrow" aria-hidden="true"><Icon size={11} /></i>
				</button>
			) : label}
		</span>
	);
}

export type SectorFlowChartProps = {
	/** 曲线数据：资金曲线与价格曲线都会先归一化成「时间 → 数值」再进来。 */
	curves: SectorFlowCurve[];
	/** 纵轴刻度文案（资金用「亿」、价格用「%」）。 */
	formatAxis?: (value: number) => string;
	/** 末端标签与悬停浮层里的数值文案。 */
	formatValue?: (value: number) => string;
	/** 没有曲线时的占位纵轴量级（±extent），让空框架也有一套完整刻度。 */
	emptyExtent?: number;
	ariaLabel?: string;
	hoverIndex: number | null;
	onHover: (index: number | null) => void;
	/** 点选聚焦：只有这一条曲线是实色，其余曲线与末端标签半透明；不传就是全部实色。 */
	focusCode?: string | null;
	onFocus?: (code: string | null) => void;
};

// 曲线右端标签：板块名 + 末端数值，宽度估算与实际渲染共用这一份文案。
function curveLabelText(item: SectorFlowCurve, formatValue: (value: number) => string): string {
	return `${shortSectorFlowName(item.name)} ${formatValue(item.points.at(-1)?.value ?? 0)}`;
}

// 一个交易日的分时时间轴（09:31—11:30、13:01—15:00，共 240 分钟）：
// 还没选择板块时也先把坐标轴框架画出来，让区块看起来是「等数据的图表」而不是空白。
const SESSION_TIME_AXIS = (() => {
	const minutes: string[] = [];
	const append = (start: number, end: number) => {
		for (let minute = start; minute <= end; minute += 1) {
			minutes.push(`${String(Math.floor(minute / 60)).padStart(2, '0')}:${String(minute % 60).padStart(2, '0')}`);
		}
	};
	append(9 * 60 + 31, 11 * 60 + 30);
	append(13 * 60 + 1, 15 * 60);
	return minutes;
})();

// 导出仅用于测试：曲线绘制与末端标签布局可以脱离网络数据单独渲染验证。
export function SectorFlowChart({
	curves,
	formatAxis = formatSectorFlowAxis,
	formatValue = formatSectorFlowAmount,
	emptyExtent = SECTOR_FLOW_EMPTY_AXIS_EXTENT,
	ariaLabel = '板块分时累计净流入曲线',
	hoverIndex,
	onHover,
	focusCode = null,
	onFocus,
}: SectorFlowChartProps) {
	const [wrapRef, size] = useElementSize<HTMLDivElement>();
	const hasData = curves.length > 0;
	// 时间轴固定为一整个交易日：曲线从开盘处向右生长，标线与标签挂在「数据末端」，
	// 盘中任何时候看到的都是同一套坐标，而不是把横轴压缩到当前时刻。
	const times = useMemo(() => {
		const seen = new Set<string>(SESSION_TIME_AXIS);
		curves.forEach((item) => item.points.forEach((point) => seen.add(point.time)));
		return [...seen].sort();
	}, [curves]);

	const timeIndex = useMemo(() => {
		const map = new Map<string, number>();
		times.forEach((time, index) => map.set(time, index));
		return map;
	}, [times]);

	// 纵轴始终含 0，刻度取整到 20亿 / 40亿（或 ±1% / ±2%）这类整数；没有数据时给一套占位轴，
	// 空框架也有完整刻度数字，不会只剩一条没有数字的竖轴。
	const scale = useMemo(() => {
		if (!curves.length) return sectorFlowAxisScale(-emptyExtent, emptyExtent);
		let min = 0;
		let max = 0;
		curves.forEach((item) => item.points.forEach((point) => {
			min = Math.min(min, point.value);
			max = Math.max(max, point.value);
		}));
		return sectorFlowAxisScale(min, max);
	}, [curves, emptyExtent]);
	const extent = scale;

	// 画布尺寸跟随容器，窗口缩放时按像素重排而不是拉伸视图，文字与线宽保持清晰。
	// 左右留白：左边固定给纵轴刻度，右边按曲线末端标签的实际宽度给；没有曲线时两边等宽，空框架左右对称。
	// 高度下限压到 150px：右侧要同时放下资金曲线与价格分时，面板再矮也不会因为下限过高而变形。
	const labelTexts = curves.map((item) => curveLabelText(item, formatValue));
	const labelGutter = sectorFlowLabelGutter(labelTexts);
	const geometry = {
		width: Math.max(360, size.width || 980),
		height: Math.max(150, size.height || 380),
		left: SECTOR_FLOW_AXIS_GUTTER,
		right: labelGutter,
		top: 18,
		bottom: 34,
	};
	const plotWidth = Math.max(120, geometry.width - geometry.left - geometry.right);
	const plotHeight = Math.max(90, geometry.height - geometry.top - geometry.bottom);
	const positionX = (index: number) => geometry.left + (times.length <= 1 ? plotWidth / 2 : index / (times.length - 1) * plotWidth);
	const positionY = (value: number) => geometry.top + (extent.max - value) / (extent.max - extent.min) * plotHeight;
	// 轴线落在半像素上，1px 细线才不会被抗锯齿糊成 2px 灰边。
	const axisX = (index: number) => Math.round(positionX(index)) + 0.5;
	const axisY = (value: number) => Math.round(positionY(value)) + 0.5;

	const paths = curves.map((item, index) => ({
		item,
		color: sectorFlowSeriesColor(index),
		d: item.points.map((point, pointIndex) => {
			const index0 = timeIndex.get(point.time) ?? pointIndex;
			return `${pointIndex ? 'L' : 'M'} ${positionX(index0).toFixed(2)} ${positionY(point.value).toFixed(2)}`;
		}).join(' '),
	}));

	const ticks = scale.ticks;

	/**
	 * 时间轴刻度按可用宽度选步长：宽度够就是 10 分钟一档（与行情软件一致），
	 * 变窄后自动放宽到 15 / 20 / 30 / 60 分钟，保证相邻标签至少 56px，不会糊成一片。
	 * 首末两个时间点始终标注，坐标轴两端不会缺角。
	 */
	const tickStep = useMemo(() => {
		const spacing = plotWidth / Math.max(1, times.length - 1);
		return [10, 15, 20, 30, 60].find((step) => spacing * step >= 56) ?? 120;
	}, [plotWidth, times.length]);

	const timeTicks = useMemo(() => {
		const last = times.length - 1;
		return times.filter((time, index) => {
			if (index === 0 || index === last) return true;
			const [hour, minute] = time.split(':').map(Number);
			return (hour * 60 + minute) % tickStep === 0;
		});
	}, [times, tickStep]);
	const lastIndex = times.length ? times.length - 1 : 0;
	// 曲线画到的最后一分钟：全天坐标轴下它才是「当前时刻」，十字线、圆点与末端标签都挂在它右边。
	const dataEndIndex = useMemo(() => curves.reduce((last, item) => {
		const point = item.points.at(-1);
		return point ? Math.max(last, timeIndex.get(point.time) ?? 0) : last;
	}, 0), [curves, timeIndex]);
	const endIndex = hasData ? dataEndIndex : lastIndex;
	const activeIndex = hoverIndex === null ? endIndex : Math.min(Math.max(hoverIndex, 0), lastIndex);
	// 有曲线时十字线停在数据末端（当日此刻），鼠标移上来跟着走；没有曲线时只跟随鼠标，不显示静态竖线。
	const showCrosshair = hasData || hoverIndex !== null;
	const labels = paths
		.map((entry) => ({ ...entry, y: positionY(entry.item.points.at(-1)?.value ?? 0) }))
		.sort((left, right) => left.y - right.y)
		.reduce<Array<{ item: SectorFlowCurve; color: string; y: number }>>((accumulator, entry) => {
			const previous = accumulator[accumulator.length - 1];
			const y = previous && entry.y - previous.y < 16 ? previous.y + 16 : entry.y;
			accumulator.push({ ...entry, y });
			return accumulator;
		}, []);
	// 末端标签贴在数据末端右侧、跟着曲线一起往右走（收盘时正好落在右侧标签区），
	// 整列标签再压一道右边界：最长的标签在盘中也不会顶出画布被裁掉。
	const dataEndX = Math.round(positionX(endIndex));
	const widestLabel = labelTexts.reduce((width, text) => Math.max(width, estimateSectorFlowLabelWidth(text)), 0);
	const labelX = Math.min(dataEndX + 12, Math.round(geometry.width - 8 - widestLabel));

	const handleMove = (event: ReactMouseEvent<SVGSVGElement>) => {
		const rect = event.currentTarget.getBoundingClientRect();
		if (!rect.width || !times.length) return;
		// svg 宽度按容器铺满：先把鼠标位置换算回 viewBox 坐标，再映射到绘图区里的分钟。
		const x = (event.clientX - rect.left) * (geometry.width / rect.width);
		const ratio = (x - geometry.left) / plotWidth;
		onHover(Math.min(Math.max(Math.round(ratio * lastIndex), 0), lastIndex));
	};
	// 点选聚焦：再点一次同一根就取消，点空白（svg 背景）取消。
	const toggleFocus = (code: string) => onFocus?.(code === focusCode ? null : code);

	return (
		<div className="sector-flow-chart-wrap" ref={wrapRef}>
			<svg className="sector-flow-chart" width={geometry.width} height={geometry.height} viewBox={`0 0 ${geometry.width} ${geometry.height}`} role="img" aria-label={ariaLabel} onMouseMove={handleMove} onMouseLeave={() => onHover(null)} onClick={() => onFocus?.(null)}>
				{/* 坐标轴框架：左竖轴 + 下横轴 + 刻度短线，没有数据时就是一套空框架。 */}
				<line className="axis-rail" x1={axisX(0)} x2={axisX(0)} y1={geometry.top} y2={geometry.top + plotHeight} />
				<line className="axis-rail" x1={geometry.left} x2={geometry.left + plotWidth} y1={axisY(extent.min)} y2={axisY(extent.min)} />
				{ticks.map((value, index) => (
					<g key={index}>
						<line className={value === 0 ? 'zero' : 'grid'} x1={geometry.left} x2={geometry.left + plotWidth} y1={axisY(value)} y2={axisY(value)} />
						<line className="tick" x1={geometry.left - 5} x2={geometry.left} y1={axisY(value)} y2={axisY(value)} />
						<text className="axis" x={geometry.left - 10} y={positionY(value) + 4} textAnchor="end">{formatAxis(value)}</text>
					</g>
				))}
				{timeTicks.map((time) => {
					const index = timeIndex.get(time) ?? 0;
					return <g key={time}>
						<line className="grid vertical" x1={axisX(index)} x2={axisX(index)} y1={geometry.top} y2={geometry.top + plotHeight} />
						<line className="tick" x1={axisX(index)} x2={axisX(index)} y1={geometry.top + plotHeight} y2={geometry.top + plotHeight + 5} />
					</g>;
				})}
				{paths.map((entry) => (
					<g key={entry.item.code} className={`series-group${focusCode && entry.item.code !== focusCode ? ' dim' : ''}`}>
						<path className="series-line" d={entry.d} fill="none" stroke={entry.color} strokeWidth={1.8} strokeLinejoin="round" strokeLinecap="round" />
						{/* 曲线本身只有 1.8px 宽，点选聚焦再补一条加粗的透明命中线，细线也好点。 */}
						{onFocus ? <path className="series-hit" d={entry.d} fill="none" onClick={(event) => { event.stopPropagation(); toggleFocus(entry.item.code); }} /> : null}
					</g>
				))}
				{timeTicks.map((time) => {
					const index = timeIndex.get(time) ?? 0;
					return <text key={time} className="axis" x={positionX(index)} y={geometry.height - 10} textAnchor="middle">{time}</text>;
				})}
				{/* 十字线：有曲线时停在收盘，鼠标移上来就跟着走；没有曲线时只跟随鼠标。 */}
				{showCrosshair ? <line className="hover-line" x1={axisX(activeIndex)} x2={axisX(activeIndex)} y1={geometry.top} y2={geometry.top + plotHeight} /> : null}
				{hasData ? paths.map((entry) => {
					const point = entry.item.points[Math.min(activeIndex, entry.item.points.length - 1)];
					if (!point) return null;
					return <circle key={entry.item.code} className={focusCode && entry.item.code !== focusCode ? 'dim' : undefined} cx={positionX(activeIndex)} cy={positionY(point.value)} r={3} fill={entry.color} />;
				}) : null}
				{showCrosshair ? <text className="axis strong" x={positionX(activeIndex)} y={geometry.top - 4} textAnchor="middle">{times[activeIndex] || ''}</text> : null}
				{labels.map((entry) => (
					<g key={entry.item.code} className={`series-label-group${focusCode && entry.item.code !== focusCode ? ' dim' : ''}`} onClick={(event) => { event.stopPropagation(); toggleFocus(entry.item.code); }}>
						<line className="series-label-link" x1={dataEndX + 2} x2={labelX - 3} y1={positionY(entry.item.points.at(-1)?.value ?? 0)} y2={entry.y} stroke={entry.color} strokeWidth={1} />
						<text className="series-label" x={labelX} y={entry.y + 4} fill={entry.color}>{curveLabelText(entry.item, formatValue)}</text>
					</g>
				))}
				{/* 鼠标事件挂在整个 svg 上：曲线与末端标签要能点（点选聚焦），
				    透明的命中矩形会挡住它们，所以不再铺一层 rect。 */}
			</svg>
			{/* 悬停浮层：纵向跟随十字线的时点，横向贴近十字线，靠右侧时自动翻到左边。 */}
			{hoverIndex === null || !curves.length ? null : (
				<div className={`sector-flow-chart-tip${activeIndex > lastIndex / 2 ? ' flip' : ''}`} style={{ left: 16 + positionX(activeIndex) }}>
					<strong>{times[activeIndex] || ''}</strong>
					{curves.map((item, index) => {
						const point = item.points[Math.min(activeIndex, item.points.length - 1)];
						const value = point?.value || 0;
						return <div key={item.code}>
							<i style={{ background: sectorFlowSeriesColor(index) }} aria-hidden="true" />
							<span>{shortSectorFlowName(item.name)}</span>
							<b className={sectorFlowTone(value)}>{formatValue(value)}</b>
						</div>;
					})}
				</div>
			)}
		</div>
	);
}

// useElementSize 用 ResizeObserver 跟踪容器像素尺寸，桌面端缩放窗口时曲线按新尺寸重绘。
function useElementSize<T extends HTMLElement>() {
	const ref = useRef<T | null>(null);
	const [size, setSize] = useState({ width: 0, height: 0 });
	useEffect(() => {
		const node = ref.current;
		if (!node || typeof ResizeObserver === 'undefined') return;
		const observer = new ResizeObserver((entries) => {
			const rect = entries[0]?.contentRect;
			if (!rect) return;
			const next = { width: Math.round(rect.width), height: Math.round(rect.height) };
			setSize((current) => (current.width === next.width && current.height === next.height ? current : next));
		});
		observer.observe(node);
		return () => observer.disconnect();
	}, []);
	return [ref, size] as const;
}

function errorMessage(reason: unknown, fallback: string) {
	if (reason instanceof Error && reason.message) return `${fallback}：${reason.message}`;
	if (typeof reason === 'string' && reason) return `${fallback}：${reason}`;
	return fallback;
}
