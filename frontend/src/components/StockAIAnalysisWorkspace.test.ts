import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

// node 环境无 jsdom，沿用本仓测试惯例：直接读源码锁定结构回归。
const source = readFileSync(new URL('./StockAIAnalysisWorkspace.tsx', import.meta.url), 'utf8');

describe('个股 AI 分析工作台结构护栏', () => {
	it('分析结果内嵌日K线面板，且K线请求带缓存 TTL（防闪烁）', () => {
		expect(source).toContain('function StockAIKLinePanel');
		expect(source).toContain('<StockAIKLinePanel config={config} symbol={analysis.symbol} />');
		expect(source).toContain('/api/v1/quotes/kline');
		expect(source).toMatch(/KLINE_CACHE_TTL_MS = \d+/);
		// 缓存过期时必须先渲染旧数据再静默刷新，不允许清空成占位符。
		expect(source).toMatch(/setLines\(cached\.lines\);\s*setState\('ready'\)/);
	});

	it('研究任务运行中时，refreshKey 不叠加新研究任务', () => {
		expect(source).toMatch(/if \(research\.job && isResearchRunning\(research\.job\)\) return;/);
	});

	it('轮询返回相同分析内容时不更换 analysis 对象引用（避免结果子树重渲染闪烁）', () => {
		expect(source).toMatch(/current\.analysis_id !== next\.analysis_id \|\| current\.generated_at !== next\.generated_at/);
	});
});
