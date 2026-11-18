import { Activity } from 'lucide-react';
import { useState } from 'react';
import { etfChartSegments, etfMoney, type ETFChartPoint } from '../../lib/national-team';

export function ETFSubscriptionChart({ points }: { points: ETFChartPoint[] }) {
	const [hover, setHover] = useState<number | null>(null);
	const values = points.map((point) => point.net != null && Number.isFinite(point.net) ? point.net / 1e8 : null);
	const available = values.filter((value): value is number => value != null);
	if (!points.length || !available.length) return <div className="market-empty-panel"><Activity size={22} /><strong>暂无可绘制的净申购历史</strong></div>;
	const width = 960, height = 220, left = 74, right = 26, top = 22, bottom = 34;
	let low = Math.min(0, ...available), high = Math.max(0, ...available);
	const padding = Math.max((high - low) * .12, .1); low -= padding; high += padding;
	const x = (index: number) => points.length === 1 ? (left + width - right) / 2 : left + index / (points.length - 1) * (width - left - right);
	const y = (value: number) => top + (high - value) / (high - low) * (height - top - bottom);
	const ticks = [low, (low + high) / 2, high];
	const index = hover != null && hover < points.length ? hover : points.length - 1;
	const selected = points[index];
	const xTicks = [...new Set([0, ...(points.length > 5 ? [Math.round((points.length - 1) / 3), Math.round((points.length - 1) * 2 / 3)] : []), points.length - 1])];
	return <><div className="national-team-chart-reading" aria-live="polite">{selected.time} <strong className={selected.net == null || selected.net === 0 ? 'flat' : selected.net > 0 ? 'up' : 'down'}>{etfMoney(selected.net)}</strong>{selected.coverage && <span>覆盖 {selected.coverage}</span>}</div><svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label="ETF近一个月净申购估算折线图" onMouseLeave={() => setHover(null)}>
		{ticks.map((tick) => <g key={tick}><line x1={left} x2={width - right} y1={y(tick)} y2={y(tick)} className="grid" /><text x={left - 12} y={y(tick) + 4} textAnchor="end">{tick.toFixed(2)} 亿</text></g>)}
		<line x1={left} x2={width - right} y1={y(0)} y2={y(0)} className="zero" />
		{etfChartSegments(points).map((segment) => <polyline key={segment[0]} points={segment.map((i) => `${x(i)},${y(values[i]!)}`).join(' ')} className="flow-line" />)}
		{points.map((point, i) => <g key={point.time}>{values[i] != null && <circle cx={x(i)} cy={y(values[i]!)} r={i === index ? 4 : 2.3} className="flow-dot" />}<rect tabIndex={0} aria-label={`${point.time} 净申购估算 ${etfMoney(point.net)}`} x={x(i) - Math.max(4, (width - left - right) / points.length / 2)} y={top} width={Math.max(8, (width - left - right) / points.length)} height={height - bottom - top} fill="transparent" onMouseEnter={() => setHover(i)} onFocus={() => setHover(i)}><title>{point.time} · {etfMoney(point.net)}{point.coverage ? ` · 覆盖 ${point.coverage}` : ''}</title></rect></g>)}
		{xTicks.map((i) => <text key={i} x={x(i)} y={height - 8} textAnchor={i === 0 ? 'start' : i === points.length - 1 ? 'end' : 'middle'}>{points[i].time.slice(5)}</text>)}
	</svg></>;
}
