import { expect, it } from 'vitest';
import type { SourceHealth } from './backend';
import { sourceHealthSummary, sourceStatusLabel } from './source-health';
import { buildAttentionItems, buildCockpitBriefPrompt } from './cockpit';

it('excludes reserved and untested sources from outage alerts', () => {
	const sources: SourceHealth[] = [
		{ id: 'sina', name: '新浪', category: 'quote', ok: true, enabled: true, status: 'healthy' },
		{ id: 'tushare', name: 'Tushare', category: 'daily', ok: false, enabled: false, status: 'disabled' },
		{ id: 'cls', name: '财联社', category: 'news', ok: false, enabled: true, status: 'unknown' },
	];
	expect(sourceHealthSummary(sources)).toEqual({ total: 2, healthy: 1, failed: [], unknown: 1 });
	expect(buildAttentionItems({ sources }).some((item) => item.id === 'sources-failed')).toBe(false);
	expect(sourceStatusLabel(sources[1])).toBe('未启用（预留）');
	expect(sourceStatusLabel(sources[2])).toBe('尚未检测');
	expect(buildCockpitBriefPrompt({ sources }, [])).toContain('1/2');
});
