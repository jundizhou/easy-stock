import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import type { PortfolioInspectionJob } from '../lib/backend';
import { PortfolioInspectionHistoryItem } from './PortfolioInspectionHistoryItem';

function render(status: PortfolioInspectionJob['status'], confirming = false, deleting = false) {
	return renderToStaticMarkup(<PortfolioInspectionHistoryItem item={{ id: 'record', status } as PortfolioInspectionJob} title="2 只持仓 · 稳健型" detail="2026/10/14 15:30" selected disabled={deleting} deleting={deleting} confirming={confirming} onSelect={() => {}} onRequestDelete={() => {}} onConfirmDelete={() => {}} onCancelDelete={() => {}} />);
}

describe('inspection history deletion', () => {
	it('provides separate accessible selection and delete buttons', () => {
		const html = render('succeeded');
		expect(html).toContain('aria-pressed="true"');
		expect(html).toContain('aria-label="删除 2026/10/14 15:30 的巡检记录"');
		expect(html.match(/<button\b/g)).toHaveLength(2);
		expect(html).not.toMatch(/<button\b[^>]*>(?:(?!<\/button>)[\s\S])*<button\b/);
		expect(html).not.toContain('确认删除');
	});
	it('prevents deleting running inspections', () => {
		const html = render('running', true);
		expect(html).toMatch(/class="portfolio-history-delete" disabled=""/);
		expect(html).toContain('巡检进行中，请先停止并等待结束后再删除');
		expect(html).toMatch(/<button[^>]*disabled=""[^>]*>确认删除/);
	});
	it('confirms the exact scope and prevents repeated submissions', () => {
		const html = render('failed', true, true);
		expect(html).toContain('role="alert"');
		expect(html).toContain('此操作不可撤销');
		expect(html).toContain('持仓方案、定时巡检、标的研究和独立优化快照会保留');
		expect(html).toMatch(/<button[^>]*disabled=""[^>]*>正在删除…/);
		expect(html).toMatch(/<button[^>]*disabled=""[^>]*>取消/);
	});
});
