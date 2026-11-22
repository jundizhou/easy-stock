import type { SourceHealth } from './backend';

export function sourceHealthSummary(sources: SourceHealth[]) {
	const enabled = sources.filter((source) => source.enabled !== false && source.status !== 'disabled');
	const healthy = enabled.filter((source) => source.ok).length;
	const partial = enabled.filter((source) => source.status === 'partial');
	const failed = enabled.filter(isSourceFailure);
	return { total: enabled.length, healthy, partial, failed, unknown: enabled.length - healthy - partial.length - failed.length };
}

export function sourceSummaryLabel(summary: ReturnType<typeof sourceHealthSummary>): string {
	const parts = [`${summary.healthy} 通过`];
	if (summary.partial.length) parts.push(`${summary.partial.length} 部分可用`);
	if (summary.failed.length) parts.push(`${summary.failed.length} 异常`);
	if (summary.unknown) parts.push(`${summary.unknown} 未检测`);
	return parts.join(' · ');
}

export function isSourceFailure(source: SourceHealth): boolean {
	return source.enabled !== false && source.status !== 'disabled' && source.status !== 'unknown' && source.status !== 'partial' && !source.ok;
}

export function sourceStatusLabel(source: SourceHealth): string {
	if (source.enabled === false || source.status === 'disabled') return '未启用（预留）';
	if (source.status === 'unknown') return '尚未检测';
	if (source.status === 'partial') return '部分可用';
	if (source.ok) return '探测通过';
	return source.message === 'requires token' ? '需要 Token' : '探测失败';
}
