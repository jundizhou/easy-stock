import { ShieldAlert, X } from 'lucide-react';
import { useEffect } from 'react';
import type { KLine } from '../lib/backend';
import { KLineChart } from './KLineChart';

export type KLinePeriodKey = 'intraday' | 'five-day' | 'day' | 'week' | 'month';

type KLinePeriod = {
	key: KLinePeriodKey;
	label: string;
	apiPeriod: string;
	limit: number;
	mode: 'intraday' | 'daily';
};

export const KLINE_PERIODS: KLinePeriod[] = [
	{ key: 'intraday', label: '分时', apiPeriod: '1', limit: 240, mode: 'intraday' },
	{ key: 'five-day', label: '5日', apiPeriod: '5', limit: 240, mode: 'intraday' },
	{ key: 'day', label: '日K', apiPeriod: 'day', limit: 120, mode: 'daily' },
	{ key: 'week', label: '周K', apiPeriod: 'week', limit: 80, mode: 'daily' },
	{ key: 'month', label: '月K', apiPeriod: 'month', limit: 60, mode: 'daily' },
];

export type StockKLineTarget = {
	symbol: string;
	name: string;
	price?: number;
	change_percent?: number;
};

type Props = {
	stock: StockKLineTarget;
	contextLabel: string;
	description: string;
	lines: KLine[];
	state: 'idle' | 'loading' | 'ready' | 'error';
	error: string;
	periodKey: KLinePeriodKey;
	onPeriodChange: (period: KLinePeriodKey) => void;
	onClose: () => void;
};

export function StockKLineModal({ stock, contextLabel, description, lines, state, error, periodKey, onPeriodChange, onClose }: Props) {
	useEffect(() => {
		const onKeyDown = (event: KeyboardEvent) => { if (event.key === 'Escape') onClose(); };
		window.addEventListener('keydown', onKeyDown);
		return () => window.removeEventListener('keydown', onKeyDown);
	}, [onClose]);
	const latest = lines.length ? [...lines].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime()).at(-1) : null;
	const activePeriod = KLINE_PERIODS.find((item) => item.key === periodKey) || KLINE_PERIODS[2];
	const periodRangeLabel = activePeriod.mode === 'intraday' ? `${activePeriod.label} · ${lines.length} 个数据点` : `${lines.length} 根${activePeriod.label}`;
	const change = latest?.change_percent ?? stock.change_percent;
	return (
		<div className="kline-modal-overlay" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
			<section className="kline-modal" role="dialog" aria-modal="true" aria-label={`${stock.name} K线`}>
				<header className="kline-modal-header">
					<div><span>{contextLabel} · {activePeriod.label}</span><h2>{stock.name}<small>{stock.symbol}</small></h2><p>{description}</p></div>
					<button type="button" className="kline-modal-close" onClick={onClose} aria-label="关闭 K 线弹窗"><X size={19} /></button>
				</header>
				<div className="kline-period-tabs" role="tablist" aria-label="K线周期">
					{KLINE_PERIODS.map((period) => <button key={period.key} type="button" role="tab" aria-selected={period.key === periodKey} className={period.key === periodKey ? 'active' : ''} onClick={() => onPeriodChange(period.key)}>{period.label}</button>)}
				</div>
				<div className="kline-modal-quote">
					<div><span>最新</span><strong>{latest?.close?.toFixed(2) || stock.price?.toFixed(2) || '--'}</strong></div>
					<div><span>涨跌幅</span><strong className={change == null ? '' : change >= 0 ? 'up' : 'down'}>{formatSignedPercent(change)}</strong></div>
					<div><span>开盘</span><strong>{latest?.open?.toFixed(2) || '--'}</strong></div>
					<div><span>最高</span><strong className="up">{latest?.high?.toFixed(2) || '--'}</strong></div>
					<div><span>最低</span><strong className="down">{latest?.low?.toFixed(2) || '--'}</strong></div>
					<div><span>成交量</span><strong>{latest ? formatMoney(latest.volume) : '--'}</strong></div>
					<div><span>成交额</span><strong>{latest?.amount ? formatMoney(latest.amount) : '--'}</strong></div>
					<div><span>换手率</span><strong>{latest?.turnover_rate != null ? `${latest.turnover_rate.toFixed(2)}%` : '--'}</strong></div>
				</div>
				<div className="kline-period-summary"><span>{periodRangeLabel}</span><span>前复权行情</span></div>
				{state === 'error' && <div className="kline-modal-error"><ShieldAlert size={17} /><span>{error || `${activePeriod.label}数据加载失败`}</span></div>}
				<KLineChart lines={lines} symbol={stock.symbol} state={state} mode={activePeriod.mode} periodLabel={activePeriod.label} />
				<footer className="kline-modal-footer">鼠标悬浮图表可查看行情细节；数据源：东方财富 / 新浪，结果仅供研究参考。</footer>
			</section>
		</div>
	);
}

function formatSignedPercent(value?: number) {
	if (value == null || !Number.isFinite(value)) return '--';
	return `${value >= 0 ? '+' : ''}${value.toFixed(2)}%`;
}

function formatMoney(value: number) {
	if (!Number.isFinite(value)) return '--';
	if (Math.abs(value) >= 100_000_000) return `${(value / 100_000_000).toFixed(1)}亿`;
	if (Math.abs(value) >= 10_000) return `${(value / 10_000).toFixed(0)}万`;
	return value.toFixed(0);
}
