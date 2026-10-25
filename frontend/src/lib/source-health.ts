import type { SourceHealth } from './backend';

export function sourceHealthSummary(sources: SourceHealth[]) {
	const enabled = sources.filter((source) => source.enabled !== false && source.status !== 'disabled');
	const healthy = enabled.filter((source) => source.ok).length;
	const failed = enabled.filter(isSourceFailure);
	return { total: enabled.length, healthy, failed, unknown: enabled.length - healthy - failed.length };
}

export function isSourceFailure(source: SourceHealth): boolean {
	return source.enabled !== false && source.status !== 'disabled' && source.status !== 'unknown' && !source.ok;
}

export function sourceStatusLabel(source: SourceHealth): string {
	if (source.enabled === false || source.status === 'disabled') return '未启用（预留）';
	if (source.status === 'unknown') return '尚未检测';
	if (source.ok) return '探测通过';
	return source.message === 'requires token' ? '需要 Token' : source.message || '探测失败';
}
