import { PortfolioHolding, PortfolioTraderProfile } from './backend';

export type PortfolioDraftHolding = {
	symbol: string;
	name: string;
	weight: number;
	costPrice: string;
};

export type PortfolioDraft = {
	profile: PortfolioTraderProfile;
	horizon?: 'short' | 'swing' | 'medium';
	researchLevel?: 'standard' | 'deep';
	holdings: PortfolioDraftHolding[];
};

export type PortfolioPlan = { id: string; name: string; draft: PortfolioDraft };
export type PortfolioPlans = { activeId: string; plans: PortfolioPlan[] };

export const portfolioPlansStorageKey = 'easy-stock.portfolio-plans.v1';
export const portfolioPlanNameLimit = 40;
export const portfolioDraftStorageKey = 'easy-stock.portfolio-inspection-draft.v1';
export const portfolioDraftChangedEvent = 'easy-stock:portfolio-draft-changed';
export const maxPortfolioHoldings = 10;

export const portfolioProfiles: Array<{ id: PortfolioTraderProfile; label: string; description: string; constraint: string }> = [
	{ id: 'aggressive', label: '激进', description: '短线机会与弹性优先', constraint: '单票参考上限 45%' },
	{ id: 'balanced', label: '均衡', description: '收益与回撤保持平衡', constraint: '单票参考上限 35%' },
	{ id: 'steady', label: '稳重', description: '本金保护与趋势确认优先', constraint: '单票参考上限 25%' },
];

// The original single draft is kept as a migration fallback. All current callers
// read and write the selected plan through the same storage API.
export function readPortfolioPlans(): PortfolioPlans {
	try {
		const parsed = JSON.parse(window.localStorage.getItem(portfolioPlansStorageKey) || 'null');
		if (parsed && Array.isArray(parsed.plans)) {
			const ids = new Set<string>();
			const plans: PortfolioPlan[] = [];
			for (const item of parsed.plans) {
				if (!item || typeof item.id !== 'string' || !item.id || ids.has(item.id) || !item.draft || typeof item.draft !== 'object') continue;
				ids.add(item.id);
				plans.push({ id: item.id, name: normalizePlanName(item.name) || `方案 ${plans.length + 1}`, draft: normalizeDraft(item.draft) });
			}
			if (plans.length) return { activeId: ids.has(parsed.activeId) ? parsed.activeId : plans[0].id, plans };
		}
	} catch { /* Recover the original draft if the plan collection is unavailable. */ }
	let legacy: unknown;
	try { legacy = JSON.parse(window.localStorage.getItem(portfolioDraftStorageKey) || 'null'); } catch { /* Start with an empty plan. */ }
	return { activeId: 'default', plans: [{ id: 'default', name: '方案 1', draft: normalizeDraft(legacy) }] };
}

export function activePortfolioPlan(state: PortfolioPlans): PortfolioPlan {
	return state.plans.find((plan) => plan.id === state.activeId) || state.plans[0];
}

export function readPortfolioDraft(): PortfolioDraft {
	return activePortfolioPlan(readPortfolioPlans()).draft;
}

export function writePortfolioDraft(draft: PortfolioDraft, planId?: string): PortfolioPlans {
	const state = readPortfolioPlans();
	const id = planId || state.activeId;
	if (!state.plans.some((plan) => plan.id === id)) throw new Error('这份持仓方案已被删除，请重新选择方案');
	return savePortfolioPlans({ ...state, plans: state.plans.map((plan) => plan.id === id ? { ...plan, draft: normalizeDraft(draft) } : plan) });
}

export function addPortfolioPlan(): PortfolioPlans {
	const state = readPortfolioPlans();
	let index = 1;
	while (state.plans.some((plan) => plan.name === `方案 ${index}`)) index++;
	const plan = { id: crypto.randomUUID(), name: `方案 ${index}`, draft: normalizeDraft(null) };
	return savePortfolioPlans({ activeId: plan.id, plans: [...state.plans, plan] });
}

export function selectPortfolioPlan(id: string): PortfolioPlans {
	const state = readPortfolioPlans();
	if (!state.plans.some((plan) => plan.id === id)) throw new Error('持仓方案不存在，请重新选择');
	return savePortfolioPlans({ ...state, activeId: id });
}

export function renamePortfolioPlan(id: string, name: string): PortfolioPlans {
	const state = readPortfolioPlans();
	const normalized = normalizePlanName(name);
	if (!normalized) throw new Error('请输入方案名称');
	return savePortfolioPlans({ ...state, plans: state.plans.map((plan) => plan.id === id ? { ...plan, name: normalized } : plan) });
}

export function removePortfolioPlan(id: string): PortfolioPlans {
	const state = readPortfolioPlans();
	if (state.plans.length <= 1) throw new Error('至少保留一份持仓方案');
	const plans = state.plans.filter((plan) => plan.id !== id);
	return savePortfolioPlans({ activeId: state.activeId === id ? plans[0].id : state.activeId, plans });
}

function savePortfolioPlans(state: PortfolioPlans): PortfolioPlans {
	// Write the entire collection atomically before notifying mounted consumers.
	// On failure callers keep the previous UI state and show a save error.
	window.localStorage.setItem(portfolioPlansStorageKey, JSON.stringify(state));
	window.dispatchEvent(new CustomEvent(portfolioDraftChangedEvent, { detail: activePortfolioPlan(state).draft }));
	return state;
}

function normalizePlanName(value: unknown): string {
	return typeof value === 'string' ? value.trim().slice(0, portfolioPlanNameLimit) : '';
}

function normalizeDraft(value: unknown): PortfolioDraft {
	const draft = value && typeof value === 'object' ? value as Partial<PortfolioDraft> : {};
	return {
		profile: portfolioProfiles.some((item) => item.id === draft.profile) ? draft.profile! : 'balanced',
		horizon: draft.horizon === 'short' || draft.horizon === 'medium' ? draft.horizon : 'swing',
		researchLevel: draft.researchLevel === 'deep' ? 'deep' : 'standard',
		holdings: Array.isArray(draft.holdings) ? draft.holdings.filter(isDraftHolding).slice(0, maxPortfolioHoldings).map((holding) => ({ ...holding, weight: Number(holding.weight), costPrice: typeof holding.costPrice === 'string' ? holding.costPrice : '' })) : [],
	};
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
