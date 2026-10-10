import type { PortfolioResearchRequest } from './backend';
import { portfolioDraftToHoldings, type PortfolioPlan } from './portfolio-draft';

export type ScheduleChannel = 'dingtalk' | 'feishu';
export type PortfolioScheduleConfig = {
 enabled: boolean;
 interval: number;
 unit: 'days' | 'weeks' | 'months';
 start_date: string;
 time: string;
 channels: ScheduleChannel[];
 request: PortfolioResearchRequest;
};
export type PortfolioSchedule = PortfolioScheduleConfig & {
 next_run_at: string;
 last_run_at: string;
 last_job_id?: string;
 last_error?: string;
 updated_at: string;
};

export function scheduleRequest(plan: PortfolioPlan): PortfolioResearchRequest {
 return { portfolio_plan_id: plan.id, portfolio_plan_name: plan.name, trader_profile: plan.draft.profile,
  holdings: portfolioDraftToHoldings(plan.draft.holdings), horizon: plan.draft.horizon || 'swing', research_level: plan.draft.researchLevel || 'standard' };
}

export function scheduleSnapshotMatches(left: PortfolioResearchRequest, right: PortfolioResearchRequest): boolean {
 const canonical = (value: PortfolioResearchRequest) => JSON.stringify({
  id: value.portfolio_plan_id, name: value.portfolio_plan_name, profile: value.trader_profile,
  horizon: value.horizon || 'swing', depth: value.research_level || 'standard',
  holdings: value.holdings.map((item) => [item.symbol, item.name || '', item.weight_percent, item.cost_price ?? null]),
 });
 return canonical(left) === canonical(right);
}

export function defaultSchedule(plan: PortfolioPlan, now = new Date()): PortfolioScheduleConfig {
 // Tomorrow in Beijing, even if the operating system uses another timezone.
 const tomorrow = new Date(now.getTime() + 32 * 60 * 60 * 1000).toISOString().slice(0, 10);
 return { enabled: false, interval: 3, unit: 'days', start_date: tomorrow, time: '15:30', channels: [], request: scheduleRequest(plan) };
}

export function scheduleTimeLabel(value?: string): string {
 if (!value || value.startsWith('0001-')) return '暂无';
 const date = new Date(value);
 if (Number.isNaN(date.getTime())) return '暂无';
 return date.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false });
}
