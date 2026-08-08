import { StockAIAnalysis } from './backend';

export function normalizeAnalysisSymbol(value: string) {
	return value.trim().toUpperCase().replace(/\s+/g, '');
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
