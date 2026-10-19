import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { Newspaper } from 'lucide-react';
import { NewsAnalysisPanel } from './StockAIAnalysisWorkspace';
import type { StockAINewsAnalysis } from '../lib/backend';

function newsFixture(): StockAINewsAnalysis {
	return {
		available: true,
		window_days: 30,
		article_count: 2,
		source_count: 1,
		tone: '中性',
		summary: '近30日检到2条个股新闻，来自1个来源，关键词未指向明确催化或风险。',
		catalysts: [],
		risks: [],
		keywords: ['样本题材'],
		analysis_source: 'local-rules',
		articles: [{
			id: 'news-1',
			title: '样本新闻标题',
			content: '样本新闻摘要正文，用于核验展开区是否复用同一段内容。',
			url: 'https://example.com/news/1',
			published_at: '2026-10-16T09:30:00+08:00',
			tags: ['样本标签'],
			meta: { source: 'eastmoney', fetched_at: '2026-10-16T09:31:00+08:00', latency_ms: 12, stale: false },
		}],
	};
}

function panelMarkup(item: StockAINewsAnalysis) {
	return renderToStaticMarkup(<NewsAnalysisPanel eyebrow="个股资讯" title="近期个股新闻分析" item={item} icon={<Newspaper size={19} />} />);
}

describe('个股与题材新闻面板', () => {
	const markup = panelMarkup(newsFixture());

	it('收起态是原地展开的按钮行，不提供直接跳外站的入口', () => {
		expect(markup).toContain('stock-ai-news-head');
		expect(markup).toContain('aria-expanded="false"');
		expect(markup).toContain('样本新闻标题');
		expect(markup).not.toContain('查看原文');
		expect(markup).not.toContain('https://example.com/news/1');
	});

	it('收起态仍给出来源、时间与摘要，便于判断是否值得展开', () => {
		expect(markup).toContain('东方财富');
		expect(markup).toContain('10/16');
		expect(markup).toContain('样本新闻摘要正文');
		expect(markup).toContain('样本题材');
	});

	it('没有匹配新闻时不渲染列表行', () => {
		const empty = panelMarkup({ ...newsFixture(), article_count: 0, articles: [] });
		expect(empty).toContain('暂无匹配新闻');
		expect(empty).not.toContain('stock-ai-news-head');
	});
});
