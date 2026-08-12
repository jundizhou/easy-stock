import { KLine } from '../lib/backend';

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

function intradayLimitPercent(symbol = '') {
	const [rawCode, rawMarket] = symbol.toUpperCase().split('.');
	const code = rawCode || '';
	const market = rawMarket || '';
	if (market === 'BJ' || /^(4|8|92)/.test(code)) return 30;
	if (/^(300|301|302|303|688|689)/.test(code)) return 20;
	return 10;
}

export function KLineChart({ lines, symbol, state = 'ready', mode = 'daily', periodLabel = '日K' }: Props) {
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
							const change = percentChange(line.close);
							return <g key={`${line.time}-${index}`} className={line.close >= line.open ? 'kline-up' : 'kline-down'}><title>{`${formatLongDate(line.time)} 收 ${line.close.toFixed(2)}（${change >= 0 ? '+' : ''}${change.toFixed(2)}%）成交量 ${formatVolume(line.volume)}`}</title><circle className="kline-close-point" cx={x} cy={priceY(line.close)} r={Math.max(1.5, Math.min(3, step / 3))} /><rect className="kline-volume" x={x - bodyWidth / 2} y={barTop} width={bodyWidth} height={volumeBottom - barTop} /></g>;
						})}
					</>
				) : sorted.map((line, index) => {
					const x = left + index * step + step / 2;
					const rising = line.close >= line.open;
					const bodyTop = priceY(Math.max(line.open, line.close));
					const bodyBottom = priceY(Math.min(line.open, line.close));
					const barTop = volumeY(line.volume);
					const change = percentChange(line.close);
					return <g className={rising ? 'kline-up' : 'kline-down'} key={`${line.time}-${index}`}><title>{`${formatLongDate(line.time)} 开 ${line.open.toFixed(2)} 高 ${line.high.toFixed(2)} 低 ${line.low.toFixed(2)} 收 ${line.close.toFixed(2)}（${change >= 0 ? '+' : ''}${change.toFixed(2)}%）成交量 ${formatVolume(line.volume)}`}</title><line className="kline-wick" x1={x} x2={x} y1={priceY(line.high)} y2={priceY(line.low)} /><rect className="kline-body" x={x - bodyWidth / 2} y={bodyTop} width={bodyWidth} height={Math.max(bodyBottom - bodyTop, 1.5)} /><rect className="kline-volume" x={x - bodyWidth / 2} y={barTop} width={bodyWidth} height={volumeBottom - barTop} /></g>;
				})}
				{labelIndexes.map((index) => {
					const line = sorted[index];
					const x = left + index * step + step / 2;
					return <text className="kline-date-label" x={x} y={height - 15} textAnchor={index === 0 ? 'start' : index === sorted.length - 1 ? 'end' : 'middle'} key={line.time}>{formatTime(line.time, mode, periodLabel)}</text>;
				})}
			</svg>
			<div className="kline-chart-legend"><span className="zero-dot" />0%基准：{referencePrice.toFixed(2)}{mode === 'intraday' && <span className="limit-note">涨跌幅范围 ±{percentRange}%</span>} <span className="volume-note">左侧涨跌幅 · 右侧价格 · 柱体为成交量</span>{mode === 'intraday' && <><span className="close-dot" />收盘价 <span className="average-dot" />20点均线</>}</div>
		</div>
	);
}
