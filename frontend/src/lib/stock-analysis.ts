import { StockAIAnalysis, StockDirectoryEntry } from './backend';

export function normalizeAnalysisSymbol(value: string) {
	return value.trim().toUpperCase().replace(/\s+/g, '');
}

export function searchStockDirectory(stocks: StockDirectoryEntry[], query: string, limit = 8) {
	const needle = normalizeStockSearchText(query);
	if (!needle || limit <= 0) return [];
	return stocks
		.map((stock) => ({ stock, score: stockDirectoryMatchScore(stock, needle) }))
		.filter((item) => item.score >= 0)
		.sort((left, right) => left.score - right.score || left.stock.code.localeCompare(right.stock.code))
		.slice(0, limit)
		.map((item) => item.stock);
}

export function resolveStockDirectorySymbol(value: string, stocks: StockDirectoryEntry[]) {
	const normalized = normalizeAnalysisSymbol(value);
	if (/^\d{6}(?:\.(?:SH|SZ|BJ))?$/.test(normalized)) return normalized;

	const needle = normalizeStockSearchText(value);
	const exactMatches = stocks.filter((stock) => {
		return normalizeStockSearchText(stock.symbol) === needle
			|| normalizeStockSearchText(stock.code) === needle
			|| normalizeStockSearchText(stock.name) === needle;
	});
	if (exactMatches.length === 1) return exactMatches[0].symbol;

	const embeddedCode = value.toUpperCase().match(/(?:^|\D)(\d{6})(?:\s*\.\s*(SH|SZ|BJ))?(?:$|\D)/);
	if (embeddedCode) return `${embeddedCode[1]}${embeddedCode[2] ? `.${embeddedCode[2]}` : ''}`;

	const matches = searchStockDirectory(stocks, value, 2);
	return matches.length === 1 ? matches[0].symbol : '';
}

function stockDirectoryMatchScore(stock: StockDirectoryEntry, needle: string) {
	const code = normalizeStockSearchText(stock.code);
	const symbol = normalizeStockSearchText(stock.symbol);
	const name = normalizeStockSearchText(stock.name);
	if (code === needle || symbol === needle) return 0;
	if (name === needle) return 1;
	if (code.startsWith(needle)) return 2;
	if (name.startsWith(needle)) return 3;
	if (symbol.startsWith(needle)) return 4;
	if (code.includes(needle)) return 5;
	if (name.includes(needle)) return 6;
	if (symbol.includes(needle)) return 7;
	if (isSubsequence(needle, name) || isSubsequence(needle, symbol)) return 8;
	return -1;
}

function normalizeStockSearchText(value: string) {
	return value.trim().toLowerCase().replace(/[\s.·_\-]+/g, '');
}

function isSubsequence(needle: string, candidate: string) {
	let index = 0;
	for (const character of candidate) {
		if (character === needle[index]) index++;
		if (index === needle.length) return true;
	}
	return false;
}

export function analysisTone(analysis: Pick<StockAIAnalysis, 'profile' | 'trend'>) {
	if (analysis.profile.primary_type === 'weak_risk' || analysis.trend.score < 32) return 'risk';
	if (analysis.profile.primary_type === 'emotion_leader') return 'hot';
	if (analysis.trend.score >= 68) return 'strong';
	return 'neutral';
}

export function formatCompactAmount(value: number) {
	if (!Number.isFinite(value) || value <= 0) return '--';
	if (value >= 100_000_000) return `${(value / 100_000_000).toFixed(value >= 1_000_000_000 ? 1 : 2)}亿`;
	if (value >= 10_000) return `${(value / 10_000).toFixed(1)}万`;
	return value.toLocaleString('zh-CN');
}

export function signedPercent(value: number) {
	if (!Number.isFinite(value)) return '--';
	return `${value > 0 ? '+' : ''}${value.toFixed(1)}%`;
}

export type PositionSizingInput = {
	accountCapital: number;
	riskPercent: number;
	entryPrice: number;
	stopPrice: number;
	maxPositionPercent: number;
};

export type PositionSizingResult = {
	shares: number;
	positionValue: number;
	positionPercent: number;
	maxLoss: number;
	allowedLoss: number;
	riskPerShare: number;
};

export function calculatePositionSizing(input: PositionSizingInput): PositionSizingResult {
	const { accountCapital, riskPercent, entryPrice, stopPrice, maxPositionPercent } = input;
	if (![accountCapital, riskPercent, entryPrice, stopPrice, maxPositionPercent].every(Number.isFinite) || accountCapital <= 0 || riskPercent <= 0 || entryPrice <= 0 || stopPrice <= 0 || entryPrice <= stopPrice || maxPositionPercent <= 0) {
		return { shares: 0, positionValue: 0, positionPercent: 0, maxLoss: 0, allowedLoss: 0, riskPerShare: 0 };
	}
	const allowedLoss = accountCapital * riskPercent / 100;
	const riskPerShare = entryPrice - stopPrice;
	const sharesByRisk = Math.floor(allowedLoss / riskPerShare / 100) * 100;
	const positionCap = accountCapital * Math.min(maxPositionPercent, 100) / 100;
	const sharesByPosition = Math.floor(positionCap / entryPrice / 100) * 100;
	const shares = Math.max(Math.min(sharesByRisk, sharesByPosition), 0);
	const positionValue = shares * entryPrice;
	return {
		shares,
		positionValue,
		positionPercent: accountCapital > 0 ? positionValue / accountCapital * 100 : 0,
		maxLoss: shares * riskPerShare,
		allowedLoss,
		riskPerShare,
	};
}
