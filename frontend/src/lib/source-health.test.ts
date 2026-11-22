import { expect, it } from 'vitest';
import type { SourceHealth } from './backend';
import { sourceHealthSummary, sourceStatusLabel, sourceSummaryLabel } from './source-health';
import { buildAttentionItems, buildCockpitBriefPrompt } from './cockpit';

it('excludes reserved and untested sources from outage alerts', () => {
	const sources: SourceHealth[] = [
		{ id: 'sina', name: '新浪', category: 'quote', ok: true, enabled: true, status: 'healthy' },
		{ id: 'tushare', name: 'Tushare', category: 'daily', ok: false, enabled: false, status: 'disabled' },
		{ id: 'cls', name: '财联社', category: 'news', ok: false, enabled: true, status: 'unknown' },
	];
	expect(sourceHealthSummary(sources)).toEqual({ total: 2, healthy: 1, partial: [], failed: [], unknown: 1 });
	expect(buildAttentionItems({ sources }).some((item) => item.id === 'sources-failed')).toBe(false);
	expect(sourceStatusLabel(sources[1])).toBe('未启用（预留）');
	expect(sourceStatusLabel(sources[2])).toBe('尚未检测');
	expect(buildCockpitBriefPrompt({ sources }, [])).toContain('1/2');
});

it('keeps long connection errors out of status labels without hiding failures', () => {
	const source: SourceHealth = {
		id: 'eastmoney', name: '东方财富', category: 'kline', enabled: true, ok: false, status: 'unhealthy',
		message: '探测失败：Get "https://push2his.eastmoney.com/api/qt/stock/kline/get?fields2=' + 'f51%2C'.repeat(40) + '": EOF',
	};
	expect(sourceStatusLabel(source)).toBe('探测失败');
	expect(sourceHealthSummary([source])).toEqual({ total: 1, healthy: 0, partial: [], failed: [source], unknown: 0 });
	expect(source.message).toContain('EOF');
	expect(sourceStatusLabel({ ...source, message: 'requires token' })).toBe('需要 Token');
	expect(sourceStatusLabel({ ...source, ok: true, status: 'healthy' })).toBe('探测通过');
});

it('keeps partial EastMoney coverage visible in status, alerts and AI briefs', () => {
	const source: SourceHealth = {
		id: 'eastmoney', name: '东方财富', category: 'kline,f10', ok: false, status: 'partial',
		probe_summary: '日 K 线失败 · 公司资料通过',
	};
	const summary = sourceHealthSummary([source]);
	expect(summary).toEqual({ total: 1, healthy: 0, partial: [source], failed: [], unknown: 0 });
	expect(sourceSummaryLabel(summary)).toContain('1 部分可用');
	expect(sourceStatusLabel(source)).toBe('部分可用');
	const input = { sources: [source] };
	expect(buildAttentionItems(input)).toContainEqual(expect.objectContaining({ title: '东方财富部分可用', detail: source.probe_summary }));
	expect(buildCockpitBriefPrompt(input, [])).toContain(source.probe_summary);
});
