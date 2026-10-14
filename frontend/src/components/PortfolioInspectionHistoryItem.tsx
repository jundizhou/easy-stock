import { CheckCircle2, ChevronRight, CircleAlert, LoaderCircle, Trash2 } from 'lucide-react';
import type { PortfolioInspectionJob } from '../lib/backend';

type Props = {
	item: PortfolioInspectionJob;
	title: string;
	detail: string;
	selected: boolean;
	disabled: boolean;
	deleting: boolean;
	confirming: boolean;
	onSelect: () => void;
	onRequestDelete: () => void;
	onConfirmDelete: () => void;
	onCancelDelete: () => void;
};

export function PortfolioInspectionHistoryItem({ item, title, detail, selected, disabled, deleting, confirming, onSelect, onRequestDelete, onConfirmDelete, onCancelDelete }: Props) {
	const running = item.status === 'running';
	const deleteLabel = `删除 ${detail} 的巡检记录`;
	return <article className={`portfolio-history-item${selected ? ' active' : ''}`}>
		<button type="button" className="portfolio-history-select" disabled={disabled} aria-pressed={selected} onClick={onSelect}>
			<span>{running ? <LoaderCircle className="spin" size={14} /> : item.status === 'succeeded' ? <CheckCircle2 size={14} /> : <CircleAlert size={14} />}</span>
			<div><strong>{title}</strong><small>{detail}</small></div>
			<ChevronRight size={14} />
		</button>
		<button type="button" className="portfolio-history-delete" disabled={disabled || running} onClick={onRequestDelete} aria-label={deleteLabel} title={running ? '巡检进行中，请先停止并等待结束后再删除' : deleteLabel}>
			{deleting ? <LoaderCircle className="spin" size={14} /> : <Trash2 size={14} />}
		</button>
		{confirming && <section className="portfolio-history-confirm" role="alert" aria-label="确认删除巡检记录">
			<p>删除这条巡检记录及其评分缓存？此操作不可撤销。持仓方案、定时巡检、标的研究和独立优化快照会保留。</p>
			<div><button type="button" disabled={disabled || running} onClick={onConfirmDelete}>{deleting ? '正在删除…' : '确认删除'}</button><button type="button" disabled={deleting} onClick={onCancelDelete}>取消</button></div>
		</section>}
	</article>;
}
