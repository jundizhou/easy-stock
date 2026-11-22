import { Building2 } from 'lucide-react';
import type { StockAIAnalysis } from '../lib/backend';
import { formatCompactAmount, signedPercent } from '../lib/stock-analysis';

function financialAmount(value?: number | null) {
	if (value == null) return '未提供';
	if (value === 0) return '0';
	return (value < 0 ? '−' : '') + formatCompactAmount(Math.abs(value));
}

function financialRatio(value?: number | null) {
	return value == null ? '未提供' : `${value.toFixed(1)}%`;
}

function financialGrowth(value?: number | null) {
	return value == null ? '同比未提供' : `同比 ${signedPercent(value)}`;
}

function growthTone(value?: number | null) {
	return value == null || value === 0 ? '' : value > 0 ? 'positive' : 'negative';
}

export function StockFundamentalPanel({ analysis }: { analysis: Pick<StockAIAnalysis, 'symbol' | 'fundamental'> }) {
	const item = analysis.fundamental;
	const hk = analysis.symbol.endsWith('.HK');
	const reportCurrency = item?.currency === 'CNY' ? '人民币' : item?.currency === 'HKD' ? '港元' : item?.currency || '';
	return <section className="stock-ai-panel stock-ai-fundamental-panel">
		<header><div><span>公司质量</span><h3>基本面 · 最新财报</h3></div><Building2 size={19} /></header>
		{item?.available ? <>
			<div className="stock-ai-fundamental-summary"><strong>{item.score} · {item.quality}</strong><span>{item.report_name || item.report_date} · 收益持续性{item.sustainability || '待确认'}{reportCurrency ? ` · 以${reportCurrency}列示` : ''}</span><p>{item.summary}</p>{(item.sustainability_flags || []).map((flag) => <small key={flag} className="stock-ai-fundamental-warning">{flag}</small>)}</div>
			<div className="stock-ai-fundamental-metrics">
				<FundamentalMetric label={hk ? '营运收入' : '营业总收入'} value={financialAmount(item.revenue)} detail={financialGrowth(item.revenue_yoy)} tone={growthTone(item.revenue_yoy)} />
				<FundamentalMetric label={hk ? '股东应占溢利' : '归母净利润'} value={financialAmount(item.net_profit)} detail={financialGrowth(item.net_profit_yoy)} tone={growthTone(item.net_profit_yoy)} />
				{hk ? <FundamentalMetric label="扣非净利润及一次性损益占比" value="不适用" detail="港股财报未采用此 A 股口径" /> : <>
					<FundamentalMetric label="扣非净利润" value={financialAmount(item.recurring_net_profit_available ? item.recurring_net_profit : null)} detail={financialGrowth(item.recurring_net_profit_available ? item.recurring_net_profit_yoy : null)} tone={item.recurring_net_profit_available ? growthTone(item.recurring_net_profit_yoy) : ''} />
					<FundamentalMetric label="一次性损益占比" value={financialRatio(item.recurring_net_profit_available ? item.non_recurring_profit_ratio : null)} detail="占归母净利润绝对值" tone={item.recurring_net_profit_available && (item.non_recurring_profit_ratio ?? 0) >= 30 ? 'negative' : ''} />
				</>}
				<FundamentalMetric label="ROE" value={financialRatio(item.roe)} detail={`EPS ${item.eps?.toFixed(2) ?? '未提供'}`} />
				<FundamentalMetric label="毛利率" value={financialRatio(item.gross_margin)} detail={`负债率 ${financialRatio(item.debt_ratio)}`} />
			</div>
		</> : <div className="stock-ai-panel-empty">最新F10财务数据暂不可用</div>}
	</section>;
}

function FundamentalMetric({ label, value, detail, tone = '' }: { label: string; value: string; detail: string; tone?: string }) {
	return <article><span>{label}</span><strong className={tone}>{value}</strong><small>{detail}</small></article>;
}
