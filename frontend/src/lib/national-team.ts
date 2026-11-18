export function etfMoney(value: number | null | undefined): string {
	return value == null || !Number.isFinite(value) ? '--' : `${value > 0 ? '+' : value < 0 ? '−' : ''}${(Math.abs(value) / 1e8).toFixed(2)} 亿`;
}
export function etfTime(value: string | null | undefined, date = false): string {
	if (!value || !Number.isFinite(Date.parse(value))) return '--';
	return new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', ...(date ? { year: 'numeric', month: '2-digit', day: '2-digit' } as const : {}), hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(value));
}
export type ETFChartPoint = { time: string; net: number | null; coverage?: string };

// A missing source observation breaks the path instead of drawing over it.
export function etfChartSegments(points: ETFChartPoint[]): number[][] {
	const result: number[][] = [];
	let current: number[] = [];
	points.forEach((point, index) => {
		if (point.net == null || !Number.isFinite(point.net)) {
			if (current.length) result.push(current);
			current = [];
		} else current.push(index);
	});
	if (current.length) result.push(current);
	return result;
}
