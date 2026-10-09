import { KLine, SourceMeta } from './backend';

export type ThemeIndexSeries = {
	theme: string; name: string; method: 'provider-index' | 'equal-weight';
	index_code?: string; index_name?: string; base_value?: number; base_date?: string;
	lines: KLine[]; warnings: string[]; meta: SourceMeta;
	quality: { constituents: number; sampled: number; loaded: number; history_coverage: number; minimum_coverage: number; sampling_error_percent: number; sampling_estimate_available: boolean; estimated_extrema: boolean; skipped_days: number };
};
export type ThemeIndexPeriod = 'day' | 'week' | 'month';
const exchangeDateFormat = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' });

// Use exchange-local dates and Monday-start weeks, including across year ends.
function groupKey(time: string, period: ThemeIndexPeriod): string {
	const date = new Date(time);
	const day = exchangeDateFormat.format(date);
	if (period === 'day') return day;
	if (period === 'month') return day.slice(0, 7);
	const utc = new Date(`${day}T00:00:00Z`);
	utc.setUTCDate(utc.getUTCDate() - (utc.getUTCDay() + 6) % 7);
	return utc.toISOString().slice(0, 10);
}

export function aggregateThemeIndex(lines: KLine[], period: ThemeIndexPeriod): KLine[] {
	const sorted = [...lines].sort((a, b) => Date.parse(a.time) - Date.parse(b.time));
	if (period === 'day') return sorted;
	const groups = new Map<string, KLine>();
	for (const [index, line] of sorted.entries()) {
		const key = groupKey(line.time, period);
		const previous = groups.get(key);
		if (!previous) {
			const base = line.previous_close || sorted[index - 1]?.close || (line.change_percent != null && line.change_percent > -100 ? line.close / (1 + line.change_percent / 100) : undefined);
			groups.set(key, { ...line, previous_close: base }); continue;
		}
		previous.high = Math.max(previous.high, line.high);
		previous.low = Math.min(previous.low, line.low);
		previous.close = line.close;
		previous.time = line.time;
		previous.volume += line.volume;
		previous.amount += line.amount;
		previous.change_percent = previous.previous_close && previous.previous_close > 0 ? (previous.close / previous.previous_close - 1) * 100 : undefined;
	}
	return [...groups.values()];
}
