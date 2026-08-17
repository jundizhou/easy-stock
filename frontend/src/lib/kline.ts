import { KLine } from './backend';

const chinaDateFormatter = new Intl.DateTimeFormat('en-US', {
	timeZone: 'Asia/Shanghai',
	year: 'numeric',
	month: '2-digit',
	day: '2-digit',
});

function tradingDate(value: string) {
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return '';
	const parts = chinaDateFormatter.formatToParts(date);
	const year = parts.find((part) => part.type === 'year')?.value;
	const month = parts.find((part) => part.type === 'month')?.value;
	const day = parts.find((part) => part.type === 'day')?.value;
	return year && month && day ? `${year}-${month}-${day}` : '';
}

export function latestTradingDayKLines(lines: KLine[]) {
	if (!lines.length) return lines;

	let latestTimestamp = Number.NEGATIVE_INFINITY;
	let latestDate = '';
	for (const line of lines) {
		const timestamp = new Date(line.time).getTime();
		if (!Number.isNaN(timestamp) && timestamp > latestTimestamp) {
			latestTimestamp = timestamp;
			latestDate = tradingDate(line.time);
		}
	}
	if (!latestDate) return lines;

	let previousTimestamp = Number.NEGATIVE_INFINITY;
	let previousClose = 0;
	for (const line of lines) {
		const timestamp = new Date(line.time).getTime();
		if (
			tradingDate(line.time) !== latestDate
			&& !Number.isNaN(timestamp)
			&& timestamp < latestTimestamp
			&& timestamp > previousTimestamp
			&& line.close > 0
		) {
			previousTimestamp = timestamp;
			previousClose = line.close;
		}
	}

	const filtered = lines
		.filter((line) => tradingDate(line.time) === latestDate)
		.map((line) => line.previous_close && line.previous_close > 0
			? line
			: previousClose > 0 ? { ...line, previous_close: previousClose } : line);
	return filtered.length ? filtered : lines;
}
