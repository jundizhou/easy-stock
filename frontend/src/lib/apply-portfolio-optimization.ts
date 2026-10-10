import type { BackendConfig, PortfolioHolding } from './backend';
import { maxPortfolioHoldings, readPortfolioPlans, writePortfolioDraft } from './portfolio-draft';
import { loadPortfolioQuotes } from './use-portfolio-quotes';

// Fetch all costs before a single write. The plan ID comes from the source
// inspection, never from the current tab; historical reports stay immutable.
export async function applyPortfolioOptimization(config: BackendConfig, planId: string, target: PortfolioHolding[], signal?: AbortSignal) {
	const original = readPortfolioPlans().plans.find((plan) => plan.id === planId);
	if (!planId || !original) throw new Error('原持仓方案不存在或已删除，无法应用');
	if (!target.length || target.length > maxPortfolioHoldings || new Set(target.map((holding) => holding.symbol)).size !== target.length
		|| target.some((holding) => !holding.symbol || !Number.isInteger(holding.weight_percent) || holding.weight_percent <= 0)
		|| target.reduce((total, holding) => total + holding.weight_percent, 0) > 100) throw new Error('优化持仓比例无效，请重新优化');
	const quotes = await loadPortfolioQuotes(config, target.map((holding) => holding.symbol), signal);
	signal?.throwIfAborted();
	const missing = target.filter((holding) => !quotes[holding.symbol] || quotes[holding.symbol].meta?.stale);
	if (missing.length) throw new Error(`${missing.map((holding) => holding.name || holding.symbol).join('、')} 未取得有效现价，原方案未修改，请重试`);
	const current = readPortfolioPlans().plans.find((plan) => plan.id === planId);
	if (!current) throw new Error('原持仓方案已被删除，无法应用');
	if (JSON.stringify(current.draft) !== JSON.stringify(original.draft)) throw new Error('原方案在获取行情期间发生修改，请重新应用');
	const holdings = target.map((holding) => {
		const quote = quotes[holding.symbol];
		return { symbol: holding.symbol, name: holding.name || holding.symbol, weight: holding.weight_percent,
			costPrice: String(quote.price), entryPrice: quote.price, entryPriceTime: quote.trade_time || quote.meta?.fetched_at };
	});
	return writePortfolioDraft({ ...current.draft, holdings }, planId);
}
