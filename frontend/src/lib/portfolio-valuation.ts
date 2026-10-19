import type { Quote } from './backend';
import { defaultPortfolioAssets, portfolioHoldingCost, portfolioHoldingQuantity, type PortfolioDraft, type PortfolioDraftHolding } from './portfolio-draft';

export function valuePortfolioHolding(holding: PortfolioDraftHolding, totalAssets: number, quote?: Quote) {
	const costPrice = portfolioHoldingCost(holding);
	const validAssets = Number.isFinite(totalAssets) && totalAssets > 0;
	const explicitQuantity = portfolioHoldingQuantity(holding);
	const quantity = explicitQuantity !== null ? explicitQuantity : validAssets && costPrice !== null ? Math.floor((totalAssets * holding.weight / 100) / costPrice + 1e-9) : null;
	const price = quote && Number.isFinite(quote.price) && quote.price > 0 ? quote.price : null;
	const costValue = quantity !== null && costPrice !== null ? quantity * costPrice : null;
	const marketValue = quantity !== null && price !== null ? quantity * price : null;
	const weightPercent = explicitQuantity !== null && marketValue !== null && validAssets ? marketValue / totalAssets * 100 : null;
	const profit = marketValue !== null && costValue !== null ? marketValue - costValue : null;
	const profitPercent = price !== null && costPrice !== null ? (price / costPrice - 1) * 100 : null;
	return { quantity, price, costPrice, costValue, marketValue, weightPercent, profit, profitPercent };
}

export function valuePortfolio(draft: PortfolioDraft, quotes: Record<string, Quote>) {
	const totalAssets = draft.totalAssets ?? defaultPortfolioAssets;
	const holdings = draft.holdings.map((holding) => valuePortfolioHolding(holding, totalAssets, quotes[holding.symbol]));
	const completeCost = holdings.every((holding) => holding.costValue !== null);
	const completeQuotes = holdings.every((holding) => holding.marketValue !== null);
	const costValue = completeCost ? holdings.reduce((sum, holding) => sum + holding.costValue!, 0) : null;
	const marketValue = completeQuotes ? holdings.reduce((sum, holding) => sum + holding.marketValue!, 0) : null;
	const profit = marketValue !== null && costValue !== null ? marketValue - costValue : null;
	const profitPercent = profit !== null && costValue !== null ? (costValue > 0 ? profit / costValue * 100 : 0) : null;
	const cash = costValue !== null ? totalAssets - costValue : null;
	return { holdings, costValue, marketValue, profit, profitPercent, cash };
}
