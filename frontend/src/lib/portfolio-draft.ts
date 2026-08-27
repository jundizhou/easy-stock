import { PortfolioHolding, PortfolioTraderProfile } from './backend';

export type PortfolioDraftHolding = {
	symbol: string;
	name: string;
	weight: number;
	costPrice: string;
};

export type PortfolioDraft = {
	profile: PortfolioTraderProfile;
	holdings: PortfolioDraftHolding[];
};

export const portfolioDraftStorageKey = 'easy-stock.portfolio-inspection-draft.v1';
export const portfolioDraftChangedEvent = 'easy-stock:portfolio-draft-changed';
export const maxPortfolioHoldings = 10;

export const portfolioProfiles: Array<{ id: PortfolioTraderProfile; label: string; description: string; constraint: string }> = [
	{ id: 'aggressive', label: '激进', description: '短线机会与弹性优先', constraint: '单票参考上限 45%' },
	{ id: 'balanced', label: '均衡', description: '收益与回撤保持平衡', constraint: '单票参考上限 35%' },
	{ id: 'steady', label: '稳重', description: '本金保护与趋势确认优先', constraint: '单票参考上限 25%' },
];

export function readPortfolioDraft(): PortfolioDraft {
	try {
		const parsed = JSON.parse(window.localStorage.getItem(portfolioDraftStorageKey) || '{}');
		const profile = portfolioProfiles.some((item) => item.id === parsed.profile) ? parsed.profile as PortfolioTraderProfile : 'balanced';
		const holdings = Array.isArray(parsed.holdings)
			? parsed.holdings.filter(isDraftHolding).slice(0, maxPortfolioHoldings)
			: [];
		return { profile, holdings };
	} catch {
		return { profile: 'balanced', holdings: [] };
	}
}

export function writePortfolioDraft(draft: PortfolioDraft) {
	const normalized = {
		profile: portfolioProfiles.some((item) => item.id === draft.profile) ? draft.profile : 'balanced',
		holdings: draft.holdings.filter(isDraftHolding).slice(0, maxPortfolioHoldings),
	};
	window.localStorage.setItem(portfolioDraftStorageKey, JSON.stringify(normalized));
	window.dispatchEvent(new CustomEvent(portfolioDraftChangedEvent, { detail: normalized }));
}

export function portfolioDraftToHoldings(holdings: PortfolioDraftHolding[]): PortfolioHolding[] {
	return holdings.map((item) => ({
		symbol: item.symbol,
		name: item.name,
		weight_percent: item.weight,
		...(Number(item.costPrice) > 0 ? { cost_price: Number(item.costPrice) } : {}),
	}));
}

function isDraftHolding(value: unknown): value is PortfolioDraftHolding {
	if (!value || typeof value !== 'object') return false;
	const item = value as Partial<PortfolioDraftHolding>;
	return typeof item.symbol === 'string' && item.symbol.length > 0
		&& typeof item.name === 'string'
		&& Number.isFinite(Number(item.weight)) && Number(item.weight) > 0;
}
