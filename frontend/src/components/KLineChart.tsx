import { KLine } from '../lib/backend';
import { type MouseEvent, useState } from 'react';

type Props = {
	lines: KLine[];
	symbol?: string;
	state?: 'loading' | 'ready' | 'error' | 'idle';
	mode?: 'intraday' | 'daily';
	periodLabel?: string;
};

function formatTime(value: string, mode: 'intraday' | 'daily', periodLabel: string) {
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return value.slice(0, 16);
	if (mode === 'intraday' && periodLabel === '5日') return `${date.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' })} ${date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}`;
	if (mode === 'intraday') return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
	return date.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' });
}

function formatLongDate(value: string) {
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return value.slice(0, 16);
	return date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
}

function formatVolume(value: number) {
	if (value >= 100_000_000) return `${(value / 100_000_000).toFixed(1)}亿`;
	if (value >= 10_000) return `${(value / 10_000).toFixed(0)}万`;
	return value.toFixed(0);
}

function formatAmount(value: number) {
	if (!Number.isFinite(value) || value <= 0) return '--';
	if (value >= 100_000_000) return `${(value / 100_000_000).toFixed(2)}亿`;
	if (value >= 10_000) return `${(value / 10_000).toFixed(2)}万`;
	return value.toFixed(0);
}

function formatSignedPercent(value: number) {
	if (!Number.isFinite(value)) return '--';
	return `${value > 0 ? '+' : ''}${value.toFixed(2)}%`;
}

function formatAmplitude(value: number) {
	if (!Number.isFinite(value)) return '--';
	return `${value.toFixed(2)}%`;
}

function intradayLimitPercent(symbol = '') {
	const [rawCode, rawMarket] = symbol.toUpperCase().split('.');
	const code = rawCode || '';
	const market = rawMarket || '';
	if (market === 'BJ' || /^(4|8|92)/.test(code)) return 30;
	if (/^(300|301|302|303|688|689)/.test(code)) return 20;
	return 10;
}

export function KLineChart({ lines, symbol, state = 'ready', mode = 'daily', periodLabel = '日K' }: Props) {
	const [hoveredIndex, setHoveredIndex] = useState<number | null>(null);
	if (state === 'loading') return <div className="kline-chart-placeholder">正在加载{periodLabel}数据…</div>;
	if (!lines.length) return <div className="kline-chart-placeholder">{state === 'error' ? `${periodLabel}数据暂不可用，请稍后重试。` : `暂无${periodLabel}数据。`}</div>;

	const sorted = [...lines].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime());
	const width = 960;
	const height = 430;
	const left = 52;
	const right = 72;
	const chartTop = 20;
	const chartBottom = 316;
	const volumeTop = 338;
	const volumeBottom = 388;
	const plotWidth = width - left - right;
	const minPrice = Math.min(...sorted.map((line) => line.low));
	const maxPrice = Math.max(...sorted.map((line) => line.high));
	// Use the first bar as the period baseline. The percentage scale is
	// deliberately symmetric so the 0% axis always sits in the middle.
	const referencePrice = sorted[0].close > 0 ? sorted[0].close : Math.max(minPrice, 0.01);
	const percentChange = (value: number) => ((value - referencePrice) / referencePrice) * 100;
	const percentRange = mode === 'intraday' ? intradayLimitPercent(symbol) : Math.max(
		5,
		Math.ceil((Math.max(...sorted.flatMap((line) => [Math.abs(percentChange(line.high)), Math.abs(percentChange(line.low))]), 1) * 1.08) / 5) * 5,
	);
	const maxVolume = Math.max(...sorted.map((line) => line.volume), 1);
	const step = plotWidth / sorted.length;
	const bodyWidth = Math.max(2, Math.min(10, step * 0.62));
	const percentY = (value: number) => {
		const bounded = Math.max(-percentRange, Math.min(percentRange, value));
		return chartTop + ((percentRange - bounded) / (percentRange * 2)) * (chartBottom - chartTop);
	};
	const priceY = (value: number) => percentY(percentChange(value));
	const volumeY = (value: number) => volumeBottom - (value / maxVolume) * (volumeBottom - volumeTop);
	const percentTicks = Array.from({ length: 7 }, (_, index) => percentRange - (percentRange * 2 * index) / 6);
	const labelIndexes = sorted.length <= 5 ? sorted.map((_, index) => index) : [0, Math.floor((sorted.length - 1) / 2), sorted.length - 1];
	const dayKey = (value: string) => value.slice(0, 10);
	const intradayBaselines: Array<number | null> = [];
	let currentDay = '';
	let previousClose: number | null = null;
	for (const line of sorted) {
		const key = dayKey(line.time);
		if (key !== currentDay) currentDay = key;
		intradayBaselines.push(previousClose);
		previousClose = line.close;
	}
	const barBasePrice = (index: number) => {
		if (mode === 'intraday') return intradayBaselines[index] || referencePrice;
		return index > 0 && sorted[index - 1].close > 0 ? sorted[index - 1].close : referencePrice;
	};
	const barChangePercent = (index: number) => {
		const line = sorted[index];
		const base = barBasePrice(index);
		if (base > 0 && (mode === 'intraday' || index > 0)) return ((line.close - base) / base) * 100;
		return line.change_percent != null && Number.isFinite(line.change_percent) ? line.change_percent : percentChange(line.close);
	};
	const hoveredLine = hoveredIndex == null ? null : sorted[hoveredIndex];
	const hoveredPrevious = hoveredIndex != null && hoveredIndex > 0 ? sorted[hoveredIndex - 1] : null;
	const hoveredBase = hoveredIndex != null ? barBasePrice(hoveredIndex) : hoveredPrevious?.close || referencePrice;
	const dailyChangePercent = (index: number) => {
		const line = sorted[index];
		if (index > 0 && sorted[index - 1].close > 0) return ((line.close - sorted[index - 1].close) / sorted[index - 1].close) * 100;
		return line.change_percent != null && Number.isFinite(line.change_percent) ? line.change_percent : NaN;
	};
	const hoveredChange = hoveredLine && hoveredIndex != null
		? mode === 'daily' ? dailyChangePercent(hoveredIndex) : barChangePercent(hoveredIndex)
		: NaN;
	const hoveredAmplitude = hoveredLine && hoveredBase > 0 ? ((hoveredLine.high - hoveredLine.low) / hoveredBase) * 100 : NaN;
	const hoveredX = hoveredIndex == null ? null : left + hoveredIndex * step + step / 2;
	const hoveredY = hoveredLine ? priceY(hoveredLine.close) : null;
	const handleChartMove = (event: MouseEvent<SVGRectElement>) => {
		const bounds = event.currentTarget.getBoundingClientRect();
		if (!bounds.width) return;
		const plotX = left + ((event.clientX - bounds.left) / bounds.width) * plotWidth;
		setHoveredIndex(Math.max(0, Math.min(sorted.length - 1, Math.floor((plotX - left) / step))));
	};
	const linePath = sorted.map((line, index) => {
		const x = left + index * step + step / 2;
		return `${index === 0 ? 'M' : 'L'} ${x.toFixed(2)} ${priceY(line.close).toFixed(2)}`;
	}).join(' ');

	return (
		<div className={`kline-chart-wrap ${mode === 'intraday' ? 'intraday-chart' : 'daily-chart'}`}>
			<svg className="kline-chart" viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`${periodLabel}价格、涨跌幅和成交量图`}>
				{percentTicks.map((tick) => {
					const y = percentY(tick);
					const price = referencePrice * (1 + tick / 100);
					const isZero = Math.abs(tick) < 0.001;
					const percentLabel = `${tick > 0 ? '+' : ''}${tick.toFixed(tick % 1 === 0 ? 0 : 1)}%`;
					return <g key={tick}><line className={`kline-grid ${isZero ? 'kline-zero-grid' : ''}`} x1={left} x2={width - right} y1={y} y2={y} /><text className="kline-percent-axis-label" x={left - 9} y={y + 4} textAnchor="end">{percentLabel}</text><text className="kline-axis-label" x={width - right + 9} y={y + 4}>{price.toFixed(2)}</text></g>;
				})}
				<line className="kline-divider" x1={left} x2={width - right} y1={volumeTop - 12} y2={volumeTop - 12} />
				{mode === 'intraday' ? (
					<>
						<path className="kline-close-line" d={linePath} />
						{sorted.length > 1 && <path className="kline-average-line" d={sorted.map((line, index) => {
							const start = Math.max(0, index - 19);
							const average = sorted.slice(start, index + 1).reduce((sum, item) => sum + item.close, 0) / (index - start + 1);
							const x = left + index * step + step / 2;
							return `${index === 0 ? 'M' : 'L'} ${x.toFixed(2)} ${priceY(average).toFixed(2)}`;
						}).join(' ')} />}
						{sorted.map((line, index) => {
							const x = left + index * step + step / 2;
							const barTop = volumeY(line.volume);
							const change = barChangePercent(index);
							return <g key={`${line.time}-${index}`} className={line.close >= line.open ? 'kline-up' : 'kline-down'}><title>{`${formatLongDate(line.time)} 收 ${line.close.toFixed(2)}（${change >= 0 ? '+' : ''}${change.toFixed(2)}%）成交量 ${formatVolume(line.volume)}`}</title><circle className="kline-close-point" cx={x} cy={priceY(line.close)} r={Math.max(1.5, Math.min(3, step / 3))} /><rect className="kline-volume" x={x - bodyWidth / 2} y={barTop} width={bodyWidth} height={volumeBottom - barTop} /></g>;
						})}
					</>
				) : sorted.map((line, index) => {
					const x = left + index * step + step / 2;
					const rising = line.close >= line.open;
					const bodyTop = priceY(Math.max(line.open, line.close));
					const bodyBottom = priceY(Math.min(line.open, line.close));
					const barTop = volumeY(line.volume);
					const change = barChangePercent(index);
					return <g className={rising ? 'kline-up' : 'kline-down'} key={`${line.time}-${index}`}><title>{`${formatLongDate(line.time)} 开 ${line.open.toFixed(2)} 高 ${line.high.toFixed(2)} 低 ${line.low.toFixed(2)} 收 ${line.close.toFixed(2)}（${change >= 0 ? '+' : ''}${change.toFixed(2)}%）成交量 ${formatVolume(line.volume)}`}</title><line className="kline-wick" x1={x} x2={x} y1={priceY(line.high)} y2={priceY(line.low)} /><rect className="kline-body" x={x - bodyWidth / 2} y={bodyTop} width={bodyWidth} height={Math.max(bodyBottom - bodyTop, 1.5)} /><rect className="kline-volume" x={x - bodyWidth / 2} y={barTop} width={bodyWidth} height={volumeBottom - barTop} /></g>;
				})}
				{labelIndexes.map((index) => {
					const line = sorted[index];
					const x = left + index * step + step / 2;
					return <text className="kline-date-label" x={x} y={height - 15} textAnchor={index === 0 ? 'start' : index === sorted.length - 1 ? 'end' : 'middle'} key={line.time}>{formatTime(line.time, mode, periodLabel)}</text>;
				})}
				{hoveredX != null && hoveredY != null && <>
					<line className="kline-crosshair" x1={hoveredX} x2={hoveredX} y1={chartTop} y2={volumeBottom} />
					<line className="kline-crosshair kline-crosshair-horizontal" x1={left} x2={width - right} y1={hoveredY} y2={hoveredY} />
					<circle className="kline-crosshair-point" cx={hoveredX} cy={hoveredY} r="4" />
				</>}
				<rect className="kline-hover-layer" x={left} y={chartTop} width={plotWidth} height={volumeBottom - chartTop} onMouseMove={handleChartMove} onMouseLeave={() => setHoveredIndex(null)} aria-label="悬浮查看行情明细" />
			</svg>
			{hoveredLine && <div className={`kline-hover-card ${mode === 'intraday' ? 'right' : 'left'}`}>
				<strong>{mode === 'intraday' ? formatLongDate(hoveredLine.time) : formatTime(hoveredLine.time, mode, periodLabel)}</strong>
				<div><span>开盘</span><b>{hoveredLine.open.toFixed(2)}</b></div>
				<div><span>收盘</span><b className={hoveredLine.close >= hoveredLine.open ? 'up' : 'down'}>{hoveredLine.close.toFixed(2)}</b></div>
				<div><span>最高</span><b className="up">{hoveredLine.high.toFixed(2)}</b></div>
				<div><span>最低</span><b className="down">{hoveredLine.low.toFixed(2)}</b></div>
				<div><span>涨跌幅</span><b className={hoveredChange >= 0 ? 'up' : hoveredChange < 0 ? 'down' : ''}>{formatSignedPercent(hoveredChange)}</b></div>
				<div><span>振幅</span><b>{formatAmplitude(hoveredAmplitude)}</b></div>
				<div><span>成交量</span><b>{formatVolume(hoveredLine.volume)}</b></div>
				<div><span>成交额</span><b>{formatAmount(hoveredLine.amount)}</b></div>
				<div><span>换手率</span><b>{hoveredLine.turnover_rate != null && Number.isFinite(hoveredLine.turnover_rate) ? `${hoveredLine.turnover_rate.toFixed(2)}%` : '--'}</b></div>
			</div>}
			<div className="kline-chart-legend"><span className="zero-dot" />0%基准：{referencePrice.toFixed(2)}{mode === 'intraday' && <span className="limit-note">涨跌幅范围 ±{percentRange}%</span>} <span className="volume-note">左侧涨跌幅 · 右侧价格 · 柱体为成交量</span>{mode === 'intraday' && <><span className="close-dot" />收盘价 <span className="average-dot" />20点均线</>}</div>
		</div>
	);
}
