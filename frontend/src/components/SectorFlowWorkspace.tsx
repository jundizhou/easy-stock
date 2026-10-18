import { Check, LoaderCircle, Search, Sparkles, TrendingUp, X } from 'lucide-react';
import { type MouseEvent as ReactMouseEvent, useCallback, useEffect, useMemo, useState } from 'react';
import type { BackendConfig, SectorFlowRank, SectorFlowSeries, SourceMeta } from '../lib/backend';
import { requestJSON } from '../lib/backend';
import {
	MAX_SECTOR_FLOW_SERIES,
	SECTOR_FLOW_DIMENSIONS,
	SECTOR_FLOW_SORT_OPTIONS,
	buildSectorFlowPrompt,
	formatSectorFlowAmount,
	formatSectorFlowAxis,
	formatSectorFlowPercent,
	readSectorFlowSelection,
	sectorFlowSeriesColor,
	sectorFlowTone,
	shortSectorFlowName,
	toggleSectorFlowSelection,
	visibleSectorFlowRanks,
	writeSectorFlowSelection,
	type SectorFlowDimension,
	type SectorFlowSortKey,
} from '../lib/sector-flow';
import './sector-flow.css';

type LoadState = 'idle' | 'loading' | 'ready' | 'error';

const RANK_LIMIT = 200;

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
	const [sortKey, setSortKey] = useState<SectorFlowSortKey>('main');
	const [selectedOnly, setSelectedOnly] = useState(false);
	const [selection, setSelection] = useState<string[]>(() => readSectorFlowSelection(typeof window === 'undefined' ? null : window.localStorage));
	const [seriesByCode, setSeriesByCode] = useState<Record<string, SectorFlowSeries>>({});
	const [seriesState, setSeriesState] = useState<LoadState>('idle');
	const [seriesError, setSeriesError] = useState('');
	const [seriesNotice, setSeriesNotice] = useState('');
	const [hoverIndex, setHoverIndex] = useState<number | null>(null);

	useEffect(() => {
		writeSectorFlowSelection(typeof window === 'undefined' ? null : window.localStorage, selection);
	}, [selection]);

	useEffect(() => {
		if (!config) return;
		let cancelled = false;
		setState('loading');
		setError('');
		requestJSON<{ data: SectorFlowRank[]; meta: SourceMeta }>(config, `/api/v1/market/sector-flows?dimension=${dimension}&limit=${RANK_LIMIT}`)
			.then((payload) => {
				if (cancelled) return;
				setRanks(payload.data || []);
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
	}, [config, selection, refreshKey]);

	const toggleSelection = useCallback((code: string) => {
		const next = toggleSectorFlowSelection(selection, code);
		if (next.limited) {
			setSeriesNotice(`最多同时对比 ${MAX_SECTOR_FLOW_SERIES} 个板块，请先移除一个`);
			return;
		}
		setSeriesNotice('');
		setSelection(next.selection);
	}, [selection]);

	const visibleRanks = useMemo(
		() => visibleSectorFlowRanks(ranks, { query, selectedOnly, selection, sortKey }),
		[ranks, query, selectedOnly, selection, sortKey],
	);

	const ratioExtent = useMemo(() => {
		const values = visibleRanks.map((item) => Math.abs(item.main_net_inflow_ratio));
		return values.length ? Math.max(...values, 1) : 1;
	}, [visibleRanks]);

	const chartSeries = useMemo(() => selection.map((code) => seriesByCode[code]).filter((item): item is SectorFlowSeries => Boolean(item)), [selection, seriesByCode]);
	const tradeDate = chartSeries[0]?.trade_date || '';

	const summary = useMemo(() => {
		const inflow = ranks.filter((item) => item.main_net_inflow > 0).length;
		const total = ranks.reduce((sum, item) => sum + item.main_net_inflow, 0);
		return { inflow, outflow: ranks.length - inflow, total, leader: ranks[0] || null };
	}, [ranks]);

	const askAI = () => {
		onAskAI(buildSectorFlowPrompt({ dimension, ranks, series: chartSeries, meta, time: new Date().toLocaleString('zh-CN') }));
	};

	return (
		<div className="sector-flow-workspace">
			<section className="sector-flow-toolbar" aria-label="板块资金流工具条">
				<div className="sector-flow-dimensions" role="group" aria-label="板块维度">
					{SECTOR_FLOW_DIMENSIONS.map((item) => (
						<button key={item.id} type="button" className={dimension === item.id ? 'active' : ''} onClick={() => setDimension(item.id)} title={item.hint}>{item.label}</button>
					))}
				</div>
				<div className="sector-flow-toolbar-meta">
					<span>{tradeDate ? `${tradeDate} 数据盘中实时更新` : '数据盘中实时更新'}</span>
					<span>已选 {selection.length}/{MAX_SECTOR_FLOW_SERIES}</span>
				</div>
				<div className="sector-flow-toolbar-actions">
					<button type="button" className="ghost" onClick={() => setSelection([])} disabled={!selection.length}><X size={15} aria-hidden="true" />清空曲线</button>
					<button type="button" onClick={askAI} disabled={!ranks.length}><Sparkles size={15} aria-hidden="true" />问 AI</button>
				</div>
			</section>

			<div className="sector-flow-body">
				<section className="sector-flow-ranking" aria-label="资金净流入排名">
					<header>
						<div>
							<h3>资金净流入排名</h3>
							<small>{meta?.source === 'eastmoney:bkzj' ? '东方财富板块资金流 · 主力净流入 · 点击表头切换排序' : '等待数据来源'}</small>
						</div>
						<div className="sector-flow-ranking-controls">
							<label>
								<Search size={14} aria-hidden="true" />
								<input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={`搜索${dimension === 'concept' ? '概念' : '行业'}名称`} aria-label="搜索板块名称" />
							</label>
							<button type="button" className={selectedOnly ? 'active' : ''} onClick={() => setSelectedOnly((value) => !value)}><Check size={14} aria-hidden="true" />已选中</button>
						</div>
					</header>

					<div className="sector-flow-summary">
						<div><span>净流入板块</span><strong className="up">{summary.inflow}</strong></div>
						<div><span>净流出板块</span><strong className="down">{summary.outflow}</strong></div>
						<div><span>合计净流入</span><strong className={sectorFlowTone(summary.total)}>{formatSectorFlowAmount(summary.total)}</strong></div>
						<div><span>榜首板块</span><strong className="is-text">{summary.leader?.name || '--'}</strong></div>
					</div>

					{state === 'error' ? <div className="sector-flow-empty"><strong>数据加载失败</strong><span>{error || '请稍后重试'}</span></div> : (
						<div className="sector-flow-table" role="table" aria-label="板块资金流排名">
							<div className="sector-flow-table-head" role="row">
								<span role="columnheader">板块</span>
								<SortHeader label="净流入占比" />
								{SECTOR_FLOW_SORT_OPTIONS.map((option) => (
									<SortHeader key={option.key} label={option.label} title={option.hint} active={sortKey === option.key} onClick={() => setSortKey(option.key)} />
								))}
							</div>
							<div className="sector-flow-table-body" role="rowgroup">
								{state === 'loading' && !ranks.length ? Array.from({ length: 10 }, (_, index) => <i key={index} className="sector-flow-skeleton" />) : null}
								{visibleRanks.map((item) => {
									const selected = selection.includes(item.code);
									return (
										<button
											key={item.code}
											type="button"
											role="row"
											className={`sector-flow-row ${selected ? 'selected' : ''}`}
											onClick={() => toggleSelection(item.code)}
											title={selected ? '点击移除分时曲线' : '点击添加分时曲线'}
										>
											<span className="sector-flow-name" role="cell">
												<b style={selected ? { borderColor: sectorFlowSeriesColor(selection.indexOf(item.code)) } : undefined}>{selected ? selection.indexOf(item.code) + 1 : ''}</b>
												<em>{item.name}</em>
											</span>
											<span className="sector-flow-ratio" role="cell">
												<i className={item.main_net_inflow_ratio > 0 ? 'up' : 'down'} style={{ width: `${Math.min(100, Math.abs(item.main_net_inflow_ratio) / ratioExtent * 100)}%` }} />
												<small>{formatSectorFlowPercent(item.main_net_inflow_ratio)}</small>
											</span>
											<span className={sectorFlowTone(item.main_net_inflow)} role="cell">{formatSectorFlowAmount(item.main_net_inflow)}</span>
											<span className={sectorFlowTone(item.five_day_main_net_inflow)} role="cell">{formatSectorFlowAmount(item.five_day_main_net_inflow)}</span>
											<span className={sectorFlowTone(item.ten_day_main_net_inflow)} role="cell">{formatSectorFlowAmount(item.ten_day_main_net_inflow)}</span>
											<span className={sectorFlowTone(item.change_percent)} role="cell">{formatSectorFlowPercent(item.change_percent)}</span>
										</button>
									);
								})}
								{state !== 'loading' && !visibleRanks.length ? <div className="sector-flow-empty"><strong>没有匹配的板块</strong><span>{selectedOnly ? '先在上方榜单里选中板块，再切回已选中视图' : '换个关键词试试'}</span></div> : null}
							</div>
						</div>
					)}
					{meta?.fallback_reason ? <p className="sector-flow-note">{meta.fallback_reason}</p> : null}
				</section>

				<section className="sector-flow-chart-panel" aria-label="分时净流入曲线">
					<header>
						<div>
							<h3>分时净流入曲线</h3>
							<small>点击左侧列表添加或删除曲线 · 曲线末端为当日累计主力净流入</small>
						</div>
					</header>
					{seriesNotice ? <p className="sector-flow-note">{seriesNotice}</p> : null}
					{seriesState === 'error' ? <div className="sector-flow-empty"><strong>分时资金加载失败</strong><span>{seriesError || '请稍后重试'}</span></div> : null}
					{seriesState === 'loading' && !chartSeries.length ? <div className="sector-flow-chart-loading"><LoaderCircle size={18} aria-hidden="true" />正在加载分时资金…</div> : null}
					{!selection.length ? <div className="sector-flow-empty"><TrendingUp size={22} aria-hidden="true" /><strong>还没有选择板块</strong><span>在左侧榜单点击板块名称，即可在这里叠加分时净流入曲线</span></div> : null}
					{chartSeries.length ? (
						<>
							<SectorFlowChart series={chartSeries} hoverIndex={hoverIndex} onHover={setHoverIndex} />
							<div className="sector-flow-legend">
								{chartSeries.map((item, index) => {
									const point = hoverIndex === null ? item.points.at(-1) : item.points[Math.min(hoverIndex, item.points.length - 1)];
									return (
										<span key={item.code} className={sectorFlowTone(point?.main || 0)}>
											<i style={{ background: sectorFlowSeriesColor(index) }} />
											{shortSectorFlowName(item.name)}
											<b>{formatSectorFlowAmount(point?.main || 0)}</b>
											<button type="button" onClick={() => toggleSelection(item.code)} aria-label={`移除 ${item.name}`} title="移除该曲线"><X size={12} aria-hidden="true" /></button>
										</span>
									);
								})}
							</div>
						</>
					) : null}
				</section>
			</div>
		</div>
	);
}

function SortHeader({ label, title, active = false, onClick }: { label: string; title?: string; active?: boolean; onClick?: () => void }) {
	return (
		<span role="columnheader">
			{onClick ? (
				<button type="button" className={active ? 'active' : ''} onClick={onClick} title={title || `按${label}排序`}>{label}</button>
			) : label}
		</span>
	);
}

export type SectorFlowChartProps = {
	series: SectorFlowSeries[];
	hoverIndex: number | null;
	onHover: (index: number | null) => void;
};

// 导出仅用于测试：曲线绘制与末端标签布局可以脱离网络数据单独渲染验证。
export function SectorFlowChart({ series, hoverIndex, onHover }: SectorFlowChartProps) {
	const times = useMemo(() => {
		const seen = new Set<string>();
		series.forEach((item) => item.points.forEach((point) => seen.add(point.time)));
		return [...seen].sort();
	}, [series]);

	const timeIndex = useMemo(() => {
		const map = new Map<string, number>();
		times.forEach((time, index) => map.set(time, index));
		return map;
	}, [times]);

	const extent = useMemo(() => {
		let min = 0;
		let max = 0;
		series.forEach((item) => item.points.forEach((point) => {
			min = Math.min(min, point.main);
			max = Math.max(max, point.main);
		}));
		if (min === max) {
			return { min: min - 1, max: max + 1 };
		}
		const padding = (max - min) * 0.08;
		return { min: min - padding, max: max + padding };
	}, [series]);

	const geometry = { width: 980, height: 380, left: 76, right: 158, top: 18, bottom: 34 };
	const plotWidth = geometry.width - geometry.left - geometry.right;
	const plotHeight = geometry.height - geometry.top - geometry.bottom;
	const positionX = (index: number) => geometry.left + (times.length <= 1 ? plotWidth / 2 : index / (times.length - 1) * plotWidth);
	const positionY = (value: number) => geometry.top + (extent.max - value) / (extent.max - extent.min) * plotHeight;

	const paths = series.map((item, index) => ({
		item,
		color: sectorFlowSeriesColor(index),
		d: item.points.map((point, pointIndex) => {
			const index0 = timeIndex.get(point.time) ?? pointIndex;
			return `${pointIndex ? 'L' : 'M'} ${positionX(index0).toFixed(2)} ${positionY(point.main).toFixed(2)}`;
		}).join(' '),
	}));

	const ticks = useMemo(() => {
		const steps = 5;
		return Array.from({ length: steps + 1 }, (_, index) => extent.min + (extent.max - extent.min) * index / steps);
	}, [extent]);

	const timeTicks = useMemo(() => ['09:30', '10:30', '11:30', '13:30', '14:30', '15:00'].filter((time) => timeIndex.has(time)), [timeIndex]);
	const lastIndex = times.length ? times.length - 1 : 0;
	const activeIndex = hoverIndex === null ? lastIndex : Math.min(Math.max(hoverIndex, 0), lastIndex);
	const labels = paths
		.map((entry) => ({ ...entry, value: entry.item.points.at(-1)?.main ?? 0, y: positionY(entry.item.points.at(-1)?.main ?? 0) }))
		.sort((left, right) => left.y - right.y)
		.reduce<Array<{ item: SectorFlowSeries; color: string; value: number; y: number }>>((accumulator, entry) => {
			const previous = accumulator[accumulator.length - 1];
			const y = previous && entry.y - previous.y < 16 ? previous.y + 16 : entry.y;
			accumulator.push({ ...entry, y });
			return accumulator;
		}, []);

	const handleMove = (event: ReactMouseEvent<SVGRectElement>) => {
		const rect = event.currentTarget.getBoundingClientRect();
		if (!rect.width || !times.length) return;
		const ratio = (event.clientX - rect.left) / rect.width;
		onHover(Math.min(Math.max(Math.round(ratio * lastIndex), 0), lastIndex));
	};

	return (
		<div className="sector-flow-chart-wrap">
			<svg className="sector-flow-chart" viewBox={`0 0 ${geometry.width} ${geometry.height}`} role="img" aria-label="板块分时累计净流入曲线">
				{ticks.map((value, index) => (
					<g key={index}>
						<line className={value === 0 ? 'zero' : 'grid'} x1={geometry.left} x2={geometry.left + plotWidth} y1={positionY(value)} y2={positionY(value)} />
						<text className="axis" x={geometry.left - 8} y={positionY(value) + 4} textAnchor="end">{formatSectorFlowAxis(value)}</text>
					</g>
				))}
				{timeTicks.map((time) => {
					const index = timeIndex.get(time) ?? 0;
					return <line key={time} className="grid vertical" x1={positionX(index)} x2={positionX(index)} y1={geometry.top} y2={geometry.top + plotHeight} />;
				})}
				{paths.map((entry) => <path key={entry.item.code} d={entry.d} fill="none" stroke={entry.color} strokeWidth={1.8} strokeLinejoin="round" strokeLinecap="round" />)}
				{timeTicks.map((time) => {
					const index = timeIndex.get(time) ?? 0;
					return <text key={time} className="axis" x={positionX(index)} y={geometry.height - 12} textAnchor="middle">{time}</text>;
				})}
				<line className="hover-line" x1={positionX(activeIndex)} x2={positionX(activeIndex)} y1={geometry.top} y2={geometry.top + plotHeight} />
				{paths.map((entry) => {
					const point = entry.item.points[Math.min(activeIndex, entry.item.points.length - 1)];
					if (!point) return null;
					return <circle key={entry.item.code} cx={positionX(activeIndex)} cy={positionY(point.main)} r={3} fill={entry.color} />;
				})}
				<text className="axis strong" x={positionX(activeIndex)} y={geometry.top - 4} textAnchor="middle">{times[activeIndex] || ''}</text>
				{labels.map((entry) => (
					<g key={entry.item.code}>
						<line x1={geometry.left + plotWidth + 2} x2={geometry.left + plotWidth + 10} y1={positionY(entry.item.points.at(-1)?.main ?? 0)} y2={entry.y} stroke={entry.color} strokeWidth={1} />
						<text className="series-label" x={geometry.left + plotWidth + 13} y={entry.y + 4} fill={entry.color}>{`${shortSectorFlowName(entry.item.name)} ${formatSectorFlowAmount(entry.value)}`}</text>
					</g>
				))}
				<rect
					x={geometry.left}
					y={geometry.top}
					width={plotWidth}
					height={plotHeight}
					fill="transparent"
					onMouseMove={handleMove}
					onMouseLeave={() => onHover(null)}
				/>
			</svg>
		</div>
	);
}

function errorMessage(reason: unknown, fallback: string) {
	if (reason instanceof Error && reason.message) return `${fallback}：${reason.message}`;
	if (typeof reason === 'string' && reason) return `${fallback}：${reason}`;
	return fallback;
}
