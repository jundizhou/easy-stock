import { CalendarDays, ChevronLeft, ChevronRight, RefreshCw } from 'lucide-react';
import type { ReviewDailySummaryHistoryItem } from '../lib/backend';

type Props = {
	items: ReviewDailySummaryHistoryItem[];
	total: number;
	offset: number;
	pageSize: number;
	selectedDate: string;
	summary: ReviewDailySummaryHistoryItem | null;
	loading: boolean;
	error: string;
	onSelect: (date: string) => void;
	onPage: (offset: number) => void;
	onRetry: () => void;
};

export function ReviewSummaryHistory({ items, total, offset, pageSize, selectedDate, summary, loading, error, onSelect, onPage, onRetry }: Props) {
	const options = summary && !items.some((item) => item.trade_date === summary.trade_date) ? [summary, ...items] : items;
	return <section className="review-summary-history" aria-label="历史复盘">
		<div className="review-summary-history-controls">
			<label><CalendarDays size={17} /><strong>历史复盘</strong><select aria-label="选择复盘日期" value={selectedDate} onChange={(event) => onSelect(event.target.value)}>
				<option value="">最近复盘</option>
				{selectedDate && !options.some((item) => item.trade_date === selectedDate) && <option value={selectedDate}>{selectedDate}</option>}
				{options.map((item) => <option key={item.trade_date} value={item.trade_date}>{item.trade_date} · {item.author_count} 位作者 / {item.article_count} 篇文章</option>)}
			</select></label>
			<div className="review-summary-history-pages"><span>{loading ? '正在读取…' : `共 ${total} 天`}</span><button type="button" aria-label="较新的复盘日期" disabled={loading || offset === 0} onClick={() => onPage(Math.max(0, offset - pageSize))}><ChevronLeft size={16} /></button><button type="button" aria-label="更早的复盘日期" disabled={loading || offset + pageSize >= total} onClick={() => onPage(offset + pageSize)}><ChevronRight size={16} /></button></div>
		</div>
		<p>{summary ? `当前查看 ${summary.trade_date} 的已保存复盘。` : '生成后的复盘会保存在本机，可按日期回看。'} 每个日期保留最近一次生成结果。</p>
		{error && <div className="review-summary-history-error" role="alert"><span>{error}</span><button type="button" onClick={onRetry}><RefreshCw size={14} />重试</button></div>}
	</section>;
}
