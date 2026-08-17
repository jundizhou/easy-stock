import {
	Activity,
	AlertTriangle,
	ArrowDown,
	ArrowDownUp,
	ArrowUp,
	Building2,
	CalendarDays,
	ChevronDown,
	ExternalLink,
	FileText,
	Landmark,
	LoaderCircle,
	Search,
	TrendingUp,
	WalletCards,
} from 'lucide-react';
import { type MouseEvent as ReactMouseEvent, useEffect, useMemo, useState } from 'react';
import type {
	MarketBillboardItem,
	MarketBillboardDetail,
	MarketBillboardSeat,
	MarketFundFlow,
	MarketIndexSeries,
	MarketIndexSnapshot,
	MarketIndustryMomentum,
	MarketMarginPoint,
	MarketResearchItem,
	SourceMeta,
} from '../../lib/backend';
import { classifyBillboardSeat } from '../../lib/billboard';

type DataState = 'idle' | 'loading' | 'ready' | 'error';
type SortDirection = 'asc' | 'desc';
type SortState = { key: string; direction: SortDirection };

export type BillboardDetailEntry = {
	state: DataState;
	detail?: MarketBillboardDetail;
	error?: string;
};

export function ModuleState({ state, error, children }: { state: DataState; error: string; children: React.ReactNode }) {
	if (state === 'loading') return <div className="market-module-loading" aria-label="行情数据加载中">{Array.from({ length: 10 }, (_, index) => <i key={index} />)}</div>;
	if (state === 'error') return <div className="market-module-empty"><AlertTriangle size={24} /><strong>数据加载失败</strong><span>{error || '请稍后刷新重试'}</span></div>;
	return <>{children}</>;
}

export function SourceNotice({ meta }: { meta: SourceMeta | null }) {
	if (!meta) return null;
	return <div className={`market-source-notice ${meta.stale ? 'stale' : ''}`}>
		<span>来源 {meta.source} · 抓取 {formatDateTime(meta.fetched_at)}</span>
		{meta.fallback_reason && <em>{meta.fallback_reason}</em>}
	</div>;
}

export function CoreIndexView({ indexes, selectedID, onSelect, series, seriesLoading, meta }: {
	indexes: MarketIndexSnapshot[];
	selectedID: string;
	onSelect: (id: string) => void;
	series: MarketIndexSeries | null;
	seriesLoading: boolean;
	meta: SourceMeta | null;
}) {
	const selected = indexes.find((item) => item.id === selectedID) || indexes[0];
	const lines = series?.lines || [];
	const first = lines[0]?.close || 0;
	const latest = lines.at(-1)?.close || selected?.price || 0;
	const returnPercent = first ? (latest / first - 1) * 100 : 0;
	const high = lines.length ? Math.max(...lines.map((line) => line.high)) : 0;
	const low = lines.length ? Math.min(...lines.map((line) => line.low)) : 0;
	return <div className="market-data-view">
		<SourceNotice meta={meta} />
		<div className="market-index-selector">{indexes.map((item) => <button type="button" className={item.id === selected?.id ? 'active' : ''} key={item.id} onClick={() => onSelect(item.id)}>
			<span>{item.name}</span><strong>{formatPrice(item.price)}</strong><em className={toneClass(item.change_percent)}>{formatPercent(item.change_percent)}</em>
		</button>)}</div>
		{selected ? <section className="market-index-detail">
			<header><div><span>{selected.region} · {selected.market}</span><h3>{selected.name}</h3><small>最近 {lines.length || '--'} 个交易周期 · {statusLabel(selected.status)}</small></div><div><strong>{formatPrice(selected.price)}</strong><em className={toneClass(selected.change_percent)}>{formatPercent(selected.change_percent)}</em></div></header>
			<div className="market-index-chart-wrap">
				{seriesLoading ? <div className="market-chart-loading">走势图加载中…</div> : <IndexLineChart lines={lines} />}
				<aside>
					<MiniStat label="区间收益" value={formatPercent(returnPercent)} tone={toneClass(returnPercent)} />
					<MiniStat label="区间高点" value={formatPrice(high)} />
					<MiniStat label="区间低点" value={formatPrice(low)} />
					<MiniStat label="行情时间" value={formatDateTime(series?.index.trade_time || selected.trade_time)} />
				</aside>
			</div>
			<div className="market-kline-table"><header><span>日期</span><span>开盘</span><span>最高</span><span>最低</span><span>收盘</span><span>涨跌</span></header>{lines.slice(-8).reverse().map((line) => <article key={line.time}><span>{formatDate(line.time)}</span><span>{formatPrice(line.open)}</span><span>{formatPrice(line.high)}</span><span>{formatPrice(line.low)}</span><strong>{formatPrice(line.close)}</strong><em className={toneClass(line.change_percent || 0)}>{formatPercent(line.change_percent || 0)}</em></article>)}</div>
		</section> : <EmptyData title="暂无核心指数" detail="等待指数目录恢复。" />}
	</div>;
}

export function IndustryMomentumView({ items, meta }: { items: MarketIndustryMomentum[]; meta: SourceMeta | null }) {
	const [query, setQuery] = useState('');
	const [sortState, setSortState] = useState<SortState>({ key: 'score', direction: 'desc' });
	const flowAvailable = hasField(meta, 'main_net_inflow');
	const breadthAvailable = hasField(meta, 'rising_count') && hasField(meta, 'falling_count');
	const leaderAvailable = hasField(meta, 'leader_name');
	const visible = useMemo(() => items.filter((item) => !query || `${item.name}${item.leader_name || ''}`.toLowerCase().includes(query.toLowerCase())).sort((a, b) => {
		const left = industrySortValue(a, sortState.key);
		const right = industrySortValue(b, sortState.key);
		return compareSortValues(left, right, sortState.direction);
	}), [items, query, sortState]);
	const toggleSort = (key: string, available = true, defaultDirection: SortDirection = 'desc') => {
		if (!available) return;
		setSortState((current) => nextSortState(current, key, defaultDirection));
	};
	return <div className="market-data-view">
		<SourceNotice meta={meta} />
		<MarketFilter query={query} onQuery={setQuery}><span className="market-sort-hint">点击列名排序</span></MarketFilter>
		{visible.length ? <div className="market-data-table momentum"><header>
			<SortButton label="行业 / 领涨" active={sortState.key === 'name'} direction={sortState.direction} onClick={() => toggleSort('name', true, 'asc')} />
			<SortButton label="动能" active={sortState.key === 'score'} direction={sortState.direction} onClick={() => toggleSort('score')} />
			<SortButton label="当日" active={sortState.key === 'change_percent'} direction={sortState.direction} disabled={!hasField(meta, 'change_percent')} onClick={() => toggleSort('change_percent', hasField(meta, 'change_percent'))} />
			<SortButton label="5 日" active={sortState.key === 'five_day_change_percent'} direction={sortState.direction} disabled={!hasField(meta, 'five_day_change_percent')} onClick={() => toggleSort('five_day_change_percent', hasField(meta, 'five_day_change_percent'))} />
			<SortButton label="20 日" active={sortState.key === 'twenty_day_change_percent'} direction={sortState.direction} disabled={!hasField(meta, 'twenty_day_change_percent')} onClick={() => toggleSort('twenty_day_change_percent', hasField(meta, 'twenty_day_change_percent'))} />
			<SortButton label="涨跌家数" active={sortState.key === 'breadth'} direction={sortState.direction} disabled={!breadthAvailable} onClick={() => toggleSort('breadth', breadthAvailable)} />
			<SortButton label="主力净流入" active={sortState.key === 'main_net_inflow'} direction={sortState.direction} disabled={!flowAvailable} onClick={() => toggleSort('main_net_inflow', flowAvailable)} />
		</header>{visible.map((item, index) => <article key={item.code}>
			<span><i>{String(index + 1).padStart(2, '0')}</i><span><strong>{item.name}</strong><small>{leaderAvailable ? <>{item.leader_name || '暂无领涨标的'} {item.leader_name && formatPercent(item.leader_change_percent)}</> : '数据源未提供领涨标的'}</small></span></span>
			<span><b style={{ width: `${Math.max(3, item.score)}%` }} /><strong>{item.score.toFixed(1)}</strong></span>
			<em className={availableTone(item.change_percent, hasField(meta, 'change_percent'))}>{formatAvailable(item.change_percent, hasField(meta, 'change_percent'), formatPercent)}</em>
			<em className={availableTone(item.five_day_change_percent, hasField(meta, 'five_day_change_percent'))}>{formatAvailable(item.five_day_change_percent, hasField(meta, 'five_day_change_percent'), formatPercent)}</em>
			<em className={availableTone(item.twenty_day_change_percent, hasField(meta, 'twenty_day_change_percent'))}>{formatAvailable(item.twenty_day_change_percent, hasField(meta, 'twenty_day_change_percent'), formatPercent)}</em>
			<span>{breadthAvailable ? `${item.rising_count} / ${item.falling_count}` : '--'}</span>
			<strong className={availableTone(item.main_net_inflow, flowAvailable)}>{formatAvailable(item.main_net_inflow, flowAvailable, formatMoney)}</strong>
		</article>)}</div> : <EmptyData title="没有匹配行业" detail="调整搜索条件或刷新行情。" />}
	</div>;
}

export function FundFlowView({ items, dimension, meta }: {
	items: MarketFundFlow[];
	dimension: 'industry' | 'theme' | 'stock';
	meta: SourceMeta | null;
}) {
	const [query, setQuery] = useState('');
	const visible = items.filter((item) => !query || `${item.name}${item.code}${item.symbol || ''}${item.leader_name || ''}`.toLowerCase().includes(query.toLowerCase()));
	const flowValues = items.map((item) => primaryNetInflow(item, meta));
	const topFlowItem = items.reduce<MarketFundFlow | null>((best, item) => !best || primaryNetInflow(item, meta) > primaryNetInflow(best, meta) ? item : best, null);
	const topFlow = topFlowItem ? primaryNetInflow(topFlowItem, meta) : 0;
	return <div className="market-data-view">
		<SourceNotice meta={meta} />
		<MarketFilter query={query} onQuery={setQuery}><span className="market-sort-hint">点击列名排序</span></MarketFilter>
		<section className="market-flow-summary">
			<SummaryMetric icon={<TrendingUp size={17} />} label="净流入项目" value={String(flowValues.filter((value) => value > 0).length)} detail={`共 ${items.length} 项`} tone="up" />
			<SummaryMetric icon={<Building2 size={17} />} label="榜首净流入" value={topFlowItem ? formatMoney(topFlow) : '--'} detail={topFlowItem?.name || '等待数据'} tone={toneClass(topFlow)} />
			<SummaryMetric icon={<Activity size={17} />} label="口径" value={dimension === 'industry' ? '行业' : dimension === 'theme' ? '题材' : '个股'} detail={fundFlowSourceLabel(meta)} />
		</section>
		{visible.length ? dimension === 'stock'
			? <StockFundFlowTable items={visible} meta={meta} />
			: <SectorFundFlowTable items={visible} meta={meta} />
			: <EmptyData title="没有匹配资金记录" detail="调整搜索条件或刷新资金榜。" />}
	</div>;
}

export function MarginBalanceView({ items, limit, onLimit, meta }: {
	items: MarketMarginPoint[];
	limit: number;
	onLimit: (limit: number) => void;
	meta: SourceMeta | null;
}) {
	const latest = items.at(-1);
	const financingShare = latest?.margin_balance ? latest.financing_balance / latest.margin_balance * 100 : 0;
	return <div className="market-data-view market-margin-view">
		<SourceNotice meta={meta} />
		<div className="market-margin-toolbar">
			<div><strong>全市场两融余额</strong><span>沪市、深市、北交所合并口径，交易日收盘后更新</span></div>
			<nav aria-label="融资融券图表周期">{[30, 60, 120, 250].map((value) => <button type="button" className={limit === value ? 'active' : ''} onClick={() => onLimit(value)} key={value}>{value === 250 ? '近1年' : `${value}日`}</button>)}</nav>
		</div>
		<section className="market-flow-summary market-margin-summary">
			<SummaryMetric icon={<WalletCards size={17} />} label="两融余额" value={latest ? formatHundredMillion(latest.margin_balance) : '--'} detail={latest?.trade_date || '等待交易日'} />
			<SummaryMetric icon={<TrendingUp size={17} />} label="融资余额" value={latest ? formatHundredMillion(latest.financing_balance) : '--'} detail={`占两融 ${financingShare.toFixed(2)}%`} />
			<SummaryMetric icon={<Landmark size={17} />} label="融券余额" value={latest ? formatHundredMillion(latest.securities_lending_balance) : '--'} detail="按市值口径汇总" />
			<SummaryMetric icon={<Activity size={17} />} label="当日余额变化" value={latest ? formatSignedHundredMillion(latest.margin_balance_change) : '--'} detail={latest ? `融资净买入 ${formatSignedHundredMillion(latest.financing_net_buy_amount)}` : '等待数据'} tone={toneClass(latest?.margin_balance_change || 0)} />
		</section>
		<section className="market-margin-panel">
			<header><div><span>MARGIN BALANCE TREND</span><h3>融资融券余额趋势</h3></div><div className="market-margin-legend"><span className="total">两融余额</span><span className="financing">融资余额</span><span className="lending">融券余额（右轴）</span></div></header>
			<MarginBalanceChart items={items} />
			<footer>单位：亿元 · 两融余额 = 融资余额 + 融券余额；数据为交易所汇总后的东方财富历史口径。</footer>
		</section>
	</div>;
}

function MarginBalanceChart({ items }: { items: MarketMarginPoint[] }) {
	const [hoveredIndex, setHoveredIndex] = useState<number | null>(null);
	if (items.length < 2) return <div className="market-chart-loading">暂无足够的融资融券历史数据</div>;
	const points = [...items].sort((left, right) => left.trade_date.localeCompare(right.trade_date));
	const width = 960;
	const height = 380;
	const left = 74;
	const right = 74;
	const top = 25;
	const bottom = 320;
	const plotWidth = width - left - right;
	const plotHeight = bottom - top;
	const mainExtent = paddedExtent(points.flatMap((point) => [point.margin_balance, point.financing_balance]));
	const lendingExtent = paddedExtent(points.map((point) => point.securities_lending_balance));
	const x = (index: number) => left + index / (points.length - 1) * plotWidth;
	const mainY = (value: number) => top + (mainExtent.max - value) / (mainExtent.max - mainExtent.min) * plotHeight;
	const lendingY = (value: number) => top + (lendingExtent.max - value) / (lendingExtent.max - lendingExtent.min) * plotHeight;
	const linePath = (value: (point: MarketMarginPoint) => number, y: (value: number) => number) => points.map((point, index) => `${index ? 'L' : 'M'} ${x(index).toFixed(2)} ${y(value(point)).toFixed(2)}`).join(' ');
	const totalPath = linePath((point) => point.margin_balance, mainY);
	const financingPath = linePath((point) => point.financing_balance, mainY);
	const lendingPath = linePath((point) => point.securities_lending_balance, lendingY);
	const hovered = hoveredIndex == null ? null : points[hoveredIndex];
	const hoveredX = hoveredIndex == null ? null : x(hoveredIndex);
	const labelIndexes = Array.from(new Set([0, Math.floor((points.length - 1) / 4), Math.floor((points.length - 1) / 2), Math.floor((points.length - 1) * 3 / 4), points.length - 1]));
	const handleMove = (event: ReactMouseEvent<SVGRectElement>) => {
		const bounds = event.currentTarget.getBoundingClientRect();
		if (!bounds.width) return;
		const ratio = Math.max(0, Math.min(1, (event.clientX - bounds.left) / bounds.width));
		setHoveredIndex(Math.round(ratio * (points.length - 1)));
	};
	return <div className="market-margin-chart-wrap">
		<svg className="market-margin-chart" viewBox={`0 0 ${width} ${height}`} role="img" aria-label="融资融券余额折线图">
			<defs><linearGradient id="marginBalanceArea" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stopColor="#2476d2" stopOpacity=".18" /><stop offset="1" stopColor="#2476d2" stopOpacity=".01" /></linearGradient></defs>
			{Array.from({ length: 5 }, (_, index) => {
				const ratio = index / 4;
				const y = top + ratio * plotHeight;
				const mainValue = mainExtent.max - ratio * (mainExtent.max - mainExtent.min);
				const lendingValue = lendingExtent.max - ratio * (lendingExtent.max - lendingExtent.min);
				return <g key={index}><line className="market-margin-grid" x1={left} x2={width - right} y1={y} y2={y} /><text className="market-margin-axis" x={left - 10} y={y + 4} textAnchor="end">{formatAxisHundredMillion(mainValue)}</text><text className="market-margin-axis right" x={width - right + 10} y={y + 4}>{formatAxisHundredMillion(lendingValue)}</text></g>;
			})}
			<path className="market-margin-area" d={`${totalPath} L ${x(points.length - 1).toFixed(2)} ${bottom} L ${left} ${bottom} Z`} />
			<path className="market-margin-line total" d={totalPath} />
			<path className="market-margin-line financing" d={financingPath} />
			<path className="market-margin-line lending" d={lendingPath} />
			{labelIndexes.map((index) => <text className="market-margin-date" x={x(index)} y={height - 25} textAnchor={index === 0 ? 'start' : index === points.length - 1 ? 'end' : 'middle'} key={points[index].trade_date}>{formatMonthDay(points[index].trade_date)}</text>)}
			{hovered && hoveredIndex != null && hoveredX != null && <g className="market-margin-crosshair"><line x1={hoveredX} x2={hoveredX} y1={top} y2={bottom} /><circle className="total" cx={hoveredX} cy={mainY(hovered.margin_balance)} r="4" /><circle className="financing" cx={hoveredX} cy={mainY(hovered.financing_balance)} r="4" /><circle className="lending" cx={hoveredX} cy={lendingY(hovered.securities_lending_balance)} r="4" /></g>}
			<rect className="market-margin-hover-layer" x={left} y={top} width={plotWidth} height={plotHeight} onMouseMove={handleMove} onMouseLeave={() => setHoveredIndex(null)} />
		</svg>
		{hovered && hoveredIndex != null && <div className={`market-margin-tooltip ${hoveredIndex > points.length / 2 ? 'left' : 'right'}`}>
			<strong>{hovered.trade_date}</strong>
			<div><span>两融余额</span><b>{formatHundredMillion(hovered.margin_balance)}</b></div>
			<div><span>融资余额</span><b>{formatHundredMillion(hovered.financing_balance)}</b></div>
			<div><span>融券余额</span><b>{formatHundredMillion(hovered.securities_lending_balance)}</b></div>
			<div><span>余额变化</span><b className={toneClass(hovered.margin_balance_change)}>{formatSignedHundredMillion(hovered.margin_balance_change)}</b></div>
			<div><span>融资净买入</span><b className={toneClass(hovered.financing_net_buy_amount)}>{formatSignedHundredMillion(hovered.financing_net_buy_amount)}</b></div>
		</div>}
	</div>;
}

function paddedExtent(values: number[]) {
	const minimum = Math.min(...values);
	const maximum = Math.max(...values);
	const range = maximum - minimum;
	const padding = range > 0 ? range * 0.08 : Math.max(Math.abs(maximum) * 0.01, 1);
	return { min: minimum - padding, max: maximum + padding };
}

function SectorFundFlowTable({ items, meta }: { items: MarketFundFlow[]; meta: SourceMeta | null }) {
	const netAvailable = hasField(meta, 'net_inflow');
	const fallbackMainAvailable = hasField(meta, 'main_net_inflow');
	const [sortState, setSortState] = useState<SortState>({ key: 'net_value', direction: 'desc' });
	const sortedItems = useMemo(() => [...items].sort((a, b) => compareSortValues(sectorSortValue(a, sortState.key, meta), sectorSortValue(b, sortState.key, meta), sortState.direction)), [items, meta, sortState]);
	const toggleSort = (key: string, available = true, defaultDirection: SortDirection = 'desc') => { if (available) setSortState((current) => nextSortState(current, key, defaultDirection)); };
	return <div className="market-data-table flow sector"><header>
		<SortButton label="名称 / 代码" active={sortState.key === 'name'} direction={sortState.direction} onClick={() => toggleSort('name', true, 'asc')} />
		<SortButton label="均价" active={sortState.key === 'price'} direction={sortState.direction} disabled={!hasField(meta, 'price')} onClick={() => toggleSort('price', hasField(meta, 'price'))} />
		<SortButton label="涨跌幅" active={sortState.key === 'change_percent'} direction={sortState.direction} disabled={!hasField(meta, 'change_percent')} onClick={() => toggleSort('change_percent', hasField(meta, 'change_percent'))} />
		<SortButton label="资金流入" active={sortState.key === 'inflow'} direction={sortState.direction} disabled={!hasField(meta, 'inflow')} onClick={() => toggleSort('inflow', hasField(meta, 'inflow'))} />
		<SortButton label="资金流出" active={sortState.key === 'outflow'} direction={sortState.direction} disabled={!hasField(meta, 'outflow')} onClick={() => toggleSort('outflow', hasField(meta, 'outflow'))} />
		<SortButton label="净流入" active={sortState.key === 'net_value'} direction={sortState.direction} disabled={!netAvailable && !fallbackMainAvailable} onClick={() => toggleSort('net_value', netAvailable || fallbackMainAvailable)} />
		<SortButton label="净流入率" active={sortState.key === 'net_inflow_ratio'} direction={sortState.direction} disabled={!hasField(meta, 'net_inflow_ratio')} onClick={() => toggleSort('net_inflow_ratio', hasField(meta, 'net_inflow_ratio'))} />
		<SortButton label="领涨标的" active={sortState.key === 'leader_name'} direction={sortState.direction} disabled={!hasField(meta, 'leader_name')} onClick={() => toggleSort('leader_name', hasField(meta, 'leader_name'), 'asc')} />
	</header>{sortedItems.map((item, index) => {
		const netValue = netAvailable ? item.net_inflow : item.main_net_inflow;
		const netValueAvailable = netAvailable || fallbackMainAvailable;
		return <article key={`${item.dimension}-${item.code}`}>
			<FlowIdentity item={item} index={index} />
			<strong>{formatAvailable(item.price, hasField(meta, 'price'), formatPrice)}</strong>
			<em className={availableTone(item.change_percent, hasField(meta, 'change_percent'))}>{formatAvailable(item.change_percent, hasField(meta, 'change_percent'), formatPercent)}</em>
			<span className={availableTone(item.inflow, hasField(meta, 'inflow'))}>{formatAvailable(item.inflow, hasField(meta, 'inflow'), formatMoney)}</span>
			<span className={availableTone(-item.outflow, hasField(meta, 'outflow'))}>{formatAvailable(item.outflow, hasField(meta, 'outflow'), formatMoney)}</span>
			<strong className={availableTone(netValue, netValueAvailable)}>{formatAvailable(netValue, netValueAvailable, formatMoney)}</strong>
			<em className={availableTone(item.net_inflow_ratio, hasField(meta, 'net_inflow_ratio'))}>{formatAvailable(item.net_inflow_ratio, hasField(meta, 'net_inflow_ratio'), formatPercent)}</em>
			<span className="market-flow-leader">{hasField(meta, 'leader_name') ? <><strong>{item.leader_name || '--'}</strong><small>{item.leader_symbol || ''}{item.leader_name ? ` · ${formatPercent(item.leader_change_percent)}` : ''}</small></> : <small>--</small>}</span>
		</article>;
	})}</div>;
}

function StockFundFlowTable({ items, meta }: { items: MarketFundFlow[]; meta: SourceMeta | null }) {
	const [sortState, setSortState] = useState<SortState>({ key: 'net_inflow', direction: 'desc' });
	const sortedItems = useMemo(() => [...items].sort((a, b) => compareSortValues(stockSortValue(a, sortState.key, meta), stockSortValue(b, sortState.key, meta), sortState.direction)), [items, meta, sortState]);
	const toggleSort = (key: string, available = true, defaultDirection: SortDirection = 'desc') => { if (available) setSortState((current) => nextSortState(current, key, defaultDirection)); };
	return <div className="market-data-table flow stock"><header>
		<SortButton label="名称 / 代码" active={sortState.key === 'name'} direction={sortState.direction} onClick={() => toggleSort('name', true, 'asc')} />
		<SortButton label="价格" active={sortState.key === 'price'} direction={sortState.direction} disabled={!hasField(meta, 'price')} onClick={() => toggleSort('price', hasField(meta, 'price'))} />
		<SortButton label="涨跌幅" active={sortState.key === 'change_percent'} direction={sortState.direction} disabled={!hasField(meta, 'change_percent')} onClick={() => toggleSort('change_percent', hasField(meta, 'change_percent'))} />
		<SortButton label="总净流入" active={sortState.key === 'net_inflow'} direction={sortState.direction} disabled={!hasField(meta, 'net_inflow')} onClick={() => toggleSort('net_inflow', hasField(meta, 'net_inflow'))} />
		<SortButton label="总净流入率" active={sortState.key === 'net_inflow_ratio'} direction={sortState.direction} disabled={!hasField(meta, 'net_inflow_ratio')} onClick={() => toggleSort('net_inflow_ratio', hasField(meta, 'net_inflow_ratio'))} />
		<SortButton label="主力净流入" active={sortState.key === 'main_net_inflow'} direction={sortState.direction} disabled={!hasField(meta, 'main_net_inflow')} onClick={() => toggleSort('main_net_inflow', hasField(meta, 'main_net_inflow'))} />
		<SortButton label="主力净流入率" active={sortState.key === 'main_net_inflow_ratio'} direction={sortState.direction} disabled={!hasField(meta, 'main_net_inflow_ratio')} onClick={() => toggleSort('main_net_inflow_ratio', hasField(meta, 'main_net_inflow_ratio'))} />
		<SortButton label="散户净流入" active={sortState.key === 'retail_net_inflow'} direction={sortState.direction} disabled={!hasField(meta, 'retail_net_inflow')} onClick={() => toggleSort('retail_net_inflow', hasField(meta, 'retail_net_inflow'))} />
		<SortButton label="散户净流入率" active={sortState.key === 'retail_net_inflow_ratio'} direction={sortState.direction} disabled={!hasField(meta, 'retail_net_inflow_ratio')} onClick={() => toggleSort('retail_net_inflow_ratio', hasField(meta, 'retail_net_inflow_ratio'))} />
	</header>{sortedItems.map((item, index) => <article key={`${item.dimension}-${item.code}`}>
		<FlowIdentity item={item} index={index} />
		<strong>{formatAvailable(item.price, hasField(meta, 'price'), formatPrice)}</strong>
		<em className={availableTone(item.change_percent, hasField(meta, 'change_percent'))}>{formatAvailable(item.change_percent, hasField(meta, 'change_percent'), formatPercent)}</em>
		<strong className={availableTone(item.net_inflow, hasField(meta, 'net_inflow'))}>{formatAvailable(item.net_inflow, hasField(meta, 'net_inflow'), formatMoney)}</strong>
		<em className={availableTone(item.net_inflow_ratio, hasField(meta, 'net_inflow_ratio'))}>{formatAvailable(item.net_inflow_ratio, hasField(meta, 'net_inflow_ratio'), formatPercent)}</em>
		<strong className={availableTone(item.main_net_inflow, hasField(meta, 'main_net_inflow'))}>{formatAvailable(item.main_net_inflow, hasField(meta, 'main_net_inflow'), formatMoney)}</strong>
		<em className={availableTone(item.main_net_inflow_ratio, hasField(meta, 'main_net_inflow_ratio'))}>{formatAvailable(item.main_net_inflow_ratio, hasField(meta, 'main_net_inflow_ratio'), formatPercent)}</em>
		<strong className={availableTone(item.retail_net_inflow, hasField(meta, 'retail_net_inflow'))}>{formatAvailable(item.retail_net_inflow, hasField(meta, 'retail_net_inflow'), formatMoney)}</strong>
		<em className={availableTone(item.retail_net_inflow_ratio, hasField(meta, 'retail_net_inflow_ratio'))}>{formatAvailable(item.retail_net_inflow_ratio, hasField(meta, 'retail_net_inflow_ratio'), formatPercent)}</em>
	</article>)}</div>;
}

function FlowIdentity({ item, index }: { item: MarketFundFlow; index: number }) {
	return <span><i>{String(index + 1).padStart(2, '0')}</i><span><strong>{item.name}</strong><small>{item.symbol || item.code}</small></span></span>;
}

export function BillboardView({ items, tradeDate, onTradeDate, meta, details, onLoadDetail }: {
	items: MarketBillboardItem[];
	tradeDate: string;
	onTradeDate: (value: string) => void;
	meta: SourceMeta | null;
	details: Record<string, BillboardDetailEntry>;
	onLoadDetail: (item: MarketBillboardItem) => void;
}) {
	const [query, setQuery] = useState('');
	const [expandedKeys, setExpandedKeys] = useState<Set<string>>(() => new Set());
	const visible = items.filter((item) => !query || `${item.name}${item.symbol}${item.reason}`.toLowerCase().includes(query.toLowerCase()));
	const netTotal = items.reduce((sum, item) => sum + item.net_amount, 0);
	useEffect(() => setExpandedKeys(new Set()), [items]);
	const toggleDetail = (item: MarketBillboardItem) => {
		const key = billboardDetailKey(item);
		const expanding = !expandedKeys.has(key);
		setExpandedKeys((current) => {
			const next = new Set(current);
			if (next.has(key)) next.delete(key);
			else next.add(key);
			return next;
		});
		if (expanding && details[key]?.state !== 'loading' && details[key]?.state !== 'ready') onLoadDetail(item);
	};
	return <div className="market-data-view">
		<SourceNotice meta={meta} />
		<div className="market-filter-bar"><label><Search size={14} /><input aria-label="搜索龙虎榜" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索股票、代码或上榜原因" /></label><label className="market-date-control"><CalendarDays size={14} /><input aria-label="龙虎榜交易日" type="date" value={tradeDate} onChange={(event) => onTradeDate(event.target.value)} /></label></div>
		<section className="market-flow-summary">
			<SummaryMetric icon={<Landmark size={17} />} label="上榜记录" value={String(items.length)} detail={meta?.trade_date || tradeDate || '最近交易日'} />
			<SummaryMetric icon={<TrendingUp size={17} />} label="合计净额" value={formatMoney(netTotal)} detail="当前列表简单合计" tone={toneClass(netTotal)} />
			<SummaryMetric icon={<Building2 size={17} />} label="机构现身" value={String(items.filter((item) => item.institution_buyers > 0).length)} detail="含机构买方记录" />
		</section>
		{visible.length ? <div className="market-billboard-list">{visible.map((item, index) => {
			const key = billboardDetailKey(item);
			const expanded = expandedKeys.has(key);
			const detailEntry = details[key];
			const detailID = `billboard-seats-${index}`;
			return <article className={expanded ? 'expanded' : ''} key={key}>
				<header className="market-billboard-card-header"><span><strong>{item.name}</strong><small>{item.symbol} · {item.trade_date}</small></span><em className={toneClass(item.change_percent)}>{formatPercent(item.change_percent)}</em></header>
				<p>{item.reason || '未提供上榜原因'}</p>{item.summary && <blockquote>{item.summary}</blockquote>}
				<footer><span>买入 <strong>{formatMoney(item.buy_amount)}</strong></span><span>卖出 <strong>{formatMoney(item.sell_amount)}</strong></span><span>净额 <strong className={toneClass(item.net_amount)}>{formatMoney(item.net_amount)}</strong></span><span>机构买方 <strong>{item.institution_buyers}</strong></span><span>换手 <strong>{formatPercent(item.turnover_rate)}</strong></span></footer>
				<button type="button" className="market-billboard-toggle" aria-expanded={expanded} aria-controls={detailID} onClick={() => toggleDetail(item)}><ChevronDown size={14} />{expanded ? '收起买卖五席' : '查看买一至买五 / 卖一至卖五'}</button>
				{expanded && <div className="market-billboard-detail" id={detailID}>
					{detailEntry?.state === 'loading' && <div className="market-billboard-detail-state"><LoaderCircle className="spin" size={17} /><span>正在读取完整买卖五席</span></div>}
					{detailEntry?.state === 'error' && <div className="market-billboard-detail-state error"><AlertTriangle size={17} /><span>{detailEntry.error || '席位明细加载失败'}</span><button type="button" onClick={() => onLoadDetail(item)}>重试</button></div>}
					{detailEntry?.state === 'ready' && detailEntry.detail && <div className="market-billboard-seat-grid">
						<BillboardSeatPanel title="买方五席" prefix="买" seats={detailEntry.detail.buy_seats} />
						<BillboardSeatPanel title="卖方五席" prefix="卖" seats={detailEntry.detail.sell_seats} />
					</div>}
				</div>}
			</article>;
		})}</div> : <EmptyData title="该交易日暂无龙虎榜" detail="留空日期可自动回溯到最近有数据的交易日。" />}
	</div>;
}

function BillboardSeatPanel({ title, prefix, seats }: { title: string; prefix: '买' | '卖'; seats: MarketBillboardSeat[] }) {
	const seatByRank = new Map(seats.map((seat) => [seat.rank, seat]));
	return <section className={`market-billboard-seat-panel ${prefix === '买' ? 'buy' : 'sell'}`}>
		<header><strong>{title}</strong><span>{prefix === '买' ? '按买入额排名' : '按卖出额排名'}</span></header>
		<div className="market-billboard-seat-head"><span>排名 / 席位</span><span>买入</span><span>卖出</span><span>净额</span></div>
		{Array.from({ length: 5 }, (_, index) => {
			const rank = index + 1;
			const seat = seatByRank.get(rank);
			const label = seat ? classifyBillboardSeat(seat) : null;
			return <article key={rank}>
				<span className="market-seat-name"><i>{prefix}{rank}</i><span><strong title={seat?.name || '数据源未提供'}>{seat?.name || '数据源未提供'}</strong>{label && <small className={`market-seat-label ${label.kind}`} title={label.note}>{label.label}</small>}</span></span>
				<SeatAmount amount={seat?.buy_amount} ratio={seat?.buy_ratio} />
				<SeatAmount amount={seat?.sell_amount} ratio={seat?.sell_ratio} />
				<strong className={seat ? toneClass(seat.net_amount) : 'flat'}>{seat ? formatMoney(seat.net_amount) : '--'}</strong>
			</article>;
		})}
	</section>;
}

function SeatAmount({ amount, ratio }: { amount?: number; ratio?: number }) {
	if (amount === undefined) return <span className="market-seat-amount"><strong>--</strong></span>;
	return <span className="market-seat-amount"><strong>{formatMoney(amount)}</strong>{ratio !== undefined && ratio !== 0 && <small>{formatPercent(ratio)}</small>}</span>;
}

function billboardDetailKey(item: MarketBillboardItem) {
	return `${item.trade_date}|${item.symbol}|${item.reason}`;
}

export function ResearchView({ items, kind, queryDraft, onQueryDraft, onSearch, category, onCategory, meta }: {
	items: MarketResearchItem[];
	kind: 'announcement' | 'stock' | 'industry';
	queryDraft: string;
	onQueryDraft: (value: string) => void;
	onSearch: () => void;
	category: string;
	onCategory: (value: string) => void;
	meta: SourceMeta | null;
}) {
	return <div className="market-data-view">
		<SourceNotice meta={meta} />
		<form className="market-filter-bar" onSubmit={(event) => { event.preventDefault(); onSearch(); }}><label><Search size={14} /><input aria-label="搜索研究信号" value={queryDraft} onChange={(event) => onQueryDraft(event.target.value)} placeholder={kind === 'announcement' ? '搜索公告标题或输入股票关键词' : '搜索公司、行业、机构或观点'} /></label>{kind === 'announcement' && <select aria-label="公告分类" value={category} onChange={(event) => onCategory(event.target.value)}><option value="all">全部公告</option><option value="重大">重大事项</option><option value="业绩">业绩公告</option><option value="融资">融资公告</option><option value="风险">风险提示</option></select>}<button type="submit"><Search size={14} />检索</button></form>
		{items.length ? <div className="market-research-list">{items.map((item) => <article key={`${item.kind}-${item.id}`}>
			<div className="market-research-icon">{kind === 'announcement' ? <FileText size={18} /> : kind === 'stock' ? <Building2 size={18} /> : <TrendingUp size={18} />}</div>
			<div><header><span>{item.category || (kind === 'stock' ? '个股研报' : kind === 'industry' ? '行业研报' : '公告')}</span><time>{formatDateTime(item.published_at)}</time></header><h3>{item.title}</h3><p>{[item.stock_name || item.symbol, item.industry_name, item.organization, item.researchers].filter(Boolean).join(' · ') || '市场研究信号'}</p><footer>{item.rating && <span>评级 <strong>{item.rating}</strong>{item.previous_rating && ` / 前值 ${item.previous_rating}`}</span>}{(item.target_low || item.target_high) && <span>目标价 <strong>{formatTarget(item.target_low, item.target_high)}</strong></span>}{item.eps ? <span>预测 EPS <strong>{item.eps.toFixed(2)}</strong></span> : null}{item.pe ? <span>预测 PE <strong>{item.pe.toFixed(1)}</strong></span> : null}</footer></div>
			{item.url && <a href={item.url} target="_blank" rel="noreferrer" title="查看原文"><ExternalLink size={16} /></a>}
		</article>)}</div> : <EmptyData title="暂无匹配研究信号" detail="调整关键词或公告分类后重新检索。" />}
	</div>;
}

function MarketFilter({ query, onQuery, children }: { query: string; onQuery: (value: string) => void; children?: React.ReactNode }) {
	return <div className="market-filter-bar"><label><Search size={14} /><input value={query} onChange={(event) => onQuery(event.target.value)} placeholder="搜索名称、代码或领涨标的" /></label>{children}</div>;
}

function SummaryMetric({ icon, label, value, detail, tone = '' }: { icon: React.ReactNode; label: string; value: string; detail: string; tone?: string }) {
	return <article><i>{icon}</i><span><small>{label}</small><strong className={tone}>{value}</strong><em>{detail}</em></span></article>;
}

function MiniStat({ label, value, tone = '' }: { label: string; value: string; tone?: string }) {
	return <article><small>{label}</small><strong className={tone}>{value}</strong></article>;
}

function SortButton({ label, active, direction, disabled = false, onClick }: { label: string; active: boolean; direction: SortDirection; disabled?: boolean; onClick: () => void }) {
	const Icon = active ? direction === 'asc' ? ArrowUp : ArrowDown : ArrowDownUp;
	return <button type="button" className={`market-sort-button ${active ? 'active' : ''}`} disabled={disabled} aria-label={`${label}${disabled ? '不可排序' : active ? direction === 'asc' ? '升序' : '降序' : '排序'}`} aria-pressed={active} onClick={onClick}>
		<span>{label}</span><Icon size={11} aria-hidden="true" />
	</button>;
}

export function nextSortState(current: SortState, key: string, defaultDirection: SortDirection): SortState {
	return current.key === key ? { key, direction: current.direction === 'asc' ? 'desc' : 'asc' } : { key, direction: defaultDirection };
}

export function compareSortValues(left: string | number | null | undefined, right: string | number | null | undefined, direction: SortDirection) {
	const leftMissing = left === null || left === undefined || (typeof left === 'number' && !Number.isFinite(left)) || (typeof left === 'string' && !left.trim());
	const rightMissing = right === null || right === undefined || (typeof right === 'number' && !Number.isFinite(right)) || (typeof right === 'string' && !right.trim());
	if (leftMissing || rightMissing) {
		if (leftMissing && rightMissing) return 0;
		return leftMissing ? 1 : -1;
	}
	const multiplier = direction === 'asc' ? 1 : -1;
	if (typeof left === 'number' && typeof right === 'number') return (left - right) * multiplier;
	return String(left).localeCompare(String(right), 'zh-CN', { numeric: true, sensitivity: 'base' }) * multiplier;
}

function industrySortValue(item: MarketIndustryMomentum, key: string): string | number | null {
	if (key === 'name') return item.name;
	if (key === 'breadth') return item.rising_count - item.falling_count;
	return item[key as keyof MarketIndustryMomentum] as string | number | null;
}

function sectorSortValue(item: MarketFundFlow, key: string, meta: SourceMeta | null): string | number | null {
	if (key === 'name') return item.name || item.code;
	if (key === 'net_value') return hasField(meta, 'net_inflow') ? item.net_inflow : item.main_net_inflow;
	if (key === 'leader_name') return item.leader_name || '';
	return item[key as keyof MarketFundFlow] as string | number | null;
}

function stockSortValue(item: MarketFundFlow, key: string, _meta: SourceMeta | null): string | number | null {
	if (key === 'name') return item.name || item.symbol || item.code;
	return item[key as keyof MarketFundFlow] as string | number | null;
}

function IndexLineChart({ lines }: { lines: MarketIndexSeries['lines'] }) {
	if (lines.length < 2) return <div className="market-chart-loading">暂无足够走势数据</div>;
	const values = lines.map((line) => line.close);
	const minValue = Math.min(...values);
	const maxValue = Math.max(...values);
	const range = maxValue - minValue || 1;
	const points = values.map((value, index) => `${(index / (values.length - 1)) * 100},${92 - ((value - minValue) / range) * 80}`).join(' ');
	const area = `0,100 ${points} 100,100`;
	const up = values.at(-1)! >= values[0];
	return <div className={`market-index-chart ${up ? 'up' : 'down'}`}><svg viewBox="0 0 100 100" preserveAspectRatio="none" role="img" aria-label="指数收盘走势"><defs><linearGradient id="marketArea" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stopColor="currentColor" stopOpacity=".22" /><stop offset="1" stopColor="currentColor" stopOpacity="0" /></linearGradient></defs><polygon points={area} fill="url(#marketArea)" /><polyline points={points} fill="none" stroke="currentColor" strokeWidth="1.8" vectorEffect="non-scaling-stroke" /></svg><span>{formatPrice(maxValue)}</span><span>{formatPrice(minValue)}</span></div>;
}

function EmptyData({ title, detail }: { title: string; detail: string }) {
	return <div className="market-module-empty"><AlertTriangle size={22} /><strong>{title}</strong><span>{detail}</span></div>;
}

function formatTarget(low?: number, high?: number) {
	if (low && high) return `${low.toFixed(2)} - ${high.toFixed(2)}`;
	return formatPrice(high || low || 0);
}

function formatPrice(value: number) {
	if (!Number.isFinite(value) || value === 0) return '--';
	return value >= 10_000 ? value.toLocaleString('zh-CN', { maximumFractionDigits: 1 }) : value.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function formatPercent(value: number) {
	if (!Number.isFinite(value)) return '--';
	const fractionDigits = value !== 0 && Math.abs(value) < 0.01 ? 4 : 2;
	return `${value > 0 ? '+' : ''}${value.toFixed(fractionDigits)}%`;
}

function formatMoney(value: number) {
	if (!Number.isFinite(value)) return '--';
	const absolute = Math.abs(value);
	if (absolute >= 100_000_000) return `${(value / 100_000_000).toFixed(2)}亿`;
	if (absolute >= 10_000) return `${(value / 10_000).toFixed(1)}万`;
	return value.toFixed(0);
}

function formatHundredMillion(value: number) {
	if (!Number.isFinite(value)) return '--';
	return `${(value / 100_000_000).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}亿`;
}

function formatSignedHundredMillion(value: number) {
	if (!Number.isFinite(value)) return '--';
	return `${value > 0 ? '+' : ''}${(value / 100_000_000).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}亿`;
}

function formatAxisHundredMillion(value: number) {
	return (value / 100_000_000).toLocaleString('zh-CN', { maximumFractionDigits: 0 });
}

function formatMonthDay(value: string) {
	const parts = value.slice(0, 10).split('-');
	return parts.length === 3 ? `${parts[1]}/${parts[2]}` : value;
}

function hasField(meta: SourceMeta | null, field: string) {
	return !meta?.available_fields?.length || meta.available_fields.includes(field);
}

function formatAvailable(value: number, available: boolean, formatter: (value: number) => string) {
	return available ? formatter(value) : '--';
}

function availableTone(value: number, available: boolean) {
	return available ? toneClass(value) : 'flat';
}

function primaryNetInflow(item: MarketFundFlow, meta: SourceMeta | null) {
	if (hasField(meta, 'net_inflow')) return item.net_inflow;
	if (hasField(meta, 'main_net_inflow')) return item.main_net_inflow;
	return 0;
}

function fundFlowSourceLabel(meta: SourceMeta | null) {
	if (!meta) return '等待数据来源';
	if (meta.source === 'sina:stock-money-flow') return '新浪总资金 / 主力 / 散户';
	if (meta.source.includes('sina:')) return '新浪板块资金与领涨标的';
	if (meta.source === 'eastmoney:bkzj') return '东方财富主力净流入快照';
	return '东方财富分单资金';
}

function formatDateTime(value?: string) {
	if (!value) return '--';
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return value;
	return date.toLocaleString('zh-CN', { hour12: false });
}

function formatDate(value?: string) {
	if (!value) return '--';
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return value.slice(0, 10);
	return date.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' });
}

function toneClass(value: number) {
	return value > 0 ? 'up' : value < 0 ? 'down' : 'flat';
}

function statusLabel(status: string) {
	return status === 'open' ? '交易中' : status === 'closed' ? '已收盘' : '状态未知';
}
