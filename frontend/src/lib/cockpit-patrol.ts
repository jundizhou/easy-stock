import { BackendConfig, requestJSON } from './backend';

export type PatrolPhase = 'pre_open' | 'intraday' | 'post_close' | 'rest';

export type PatrolStatus = {
	phase: PatrolPhase;
	session_date: string;
	trading_day: boolean;
	next_run_at?: string;
	next_run_label?: string;
	running: boolean;
	ai_available: boolean;
};

export type PatrolIndexQuote = { name: string; code: string; price: number; change_percent: number; trade_time?: string };
export type PatrolEmotion = {
	trade_date: string; score: number; phase: string; limit_up: number; limit_down: number; broken: number; final_break_rate: number; max_streak: number; first_board: number;
};
export type PatrolIntraday = {
	status: string; breadth: string; summary: string; risk_score: number; limit_up: number; max_streak: number; previous_max_streak: number; high_down_rate: number; high_weak_rate: number;
};
export type PatrolLimitUp = {
	trade_date: string; session_status: string; limit_up: number; board_count: number; first_board: number; max_streak: number; reopened_count: number;
};
export type PatrolFlowItem = { name: string; net_inflow: number; change_percent: number };
export type PatrolFlowBoard = { inflow: PatrolFlowItem[]; outflow: PatrolFlowItem[] };
export type PatrolNews = { title: string; published_at?: string };
export type PatrolSource = { name: string; ok: boolean; error?: string };

export type PatrolFacts = {
	overseas?: PatrolIndexQuote[];
	cn_indexes?: PatrolIndexQuote[];
	emotion?: PatrolEmotion;
	intraday?: PatrolIntraday;
	limit_up?: PatrolLimitUp;
	industry_flows?: PatrolFlowBoard;
	concept_flows?: PatrolFlowBoard;
	// 题材涨跌的取样口径：按涨跌幅排序的领涨/领跌各若干条，比资金榜样本更大。
	concept_moves?: PatrolFlowBoard;
	news?: PatrolNews[];
	sources: PatrolSource[];
};

export type PatrolBrief = {
	id: string;
	key: string;
	phase: Exclude<PatrolPhase, 'rest'>;
	session_date: string;
	label: string;
	trigger: 'scheduled' | 'manual' | string;
	status: 'running' | 'ready' | 'failed' | string;
	narrative_status: 'ready' | 'unavailable' | 'failed' | '' | string;
	caveat?: string;
	narrative?: string;
	unverified_numbers?: string[];
	error?: string;
	facts: PatrolFacts;
	started_at: string;
	finished_at?: string;
};

export type PatrolLiveFacts = {
	phase: PatrolPhase;
	session_date: string;
	trading_day: boolean;
	collected_at: string;
	facts: PatrolFacts;
};

export const patrolPhases: Array<{ phase: Exclude<PatrolPhase, 'rest'>; label: string }> = [
	{ phase: 'pre_open', label: '盘前' },
	{ phase: 'intraday', label: '盘中' },
	{ phase: 'post_close', label: '盘后' },
];

export function patrolPhaseLabel(phase: string): string {
	return patrolPhases.find((item) => item.phase === phase)?.label ?? '休市';
}

export function clockText(iso?: string): string {
	if (!iso) return '--';
	const date = new Date(iso);
	if (Number.isNaN(date.getTime())) return '--';
	return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false });
}

// 同一阶段最近一次巡查：优先已完成的记录，运行中的记录只在没有完成结果时才显示。
export function latestBriefFor(briefs: PatrolBrief[], phase: string): PatrolBrief | null {
	const matches = briefs.filter((brief) => brief.phase === phase);
	return matches.find((brief) => brief.status === 'ready') ?? matches[0] ?? null;
}

export function sessionIntradayBriefs(briefs: PatrolBrief[], sessionDate: string): PatrolBrief[] {
	return briefs
		.filter((brief) => brief.phase === 'intraday' && brief.session_date === sessionDate)
		.sort((a, b) => a.started_at.localeCompare(b.started_at));
}

// 盘中时间线默认只展示最近若干次：手动巡查点得多了会把整行撑满，但历史仍可展开查看。
export const TIMELINE_FOLD_LIMIT = 8;

// 折叠时保留最近 limit 次记录；若当前选中的是更早的一次，把它一起保留，
// 避免选中项因为折叠从界面上消失、右侧正文却还是那一条。
export function foldTimeline(briefs: PatrolBrief[], limit = TIMELINE_FOLD_LIMIT, expanded = false, pinnedID = ''): { visible: PatrolBrief[]; hidden: number } {
	if (expanded || briefs.length <= limit) return { visible: briefs, hidden: 0 };
	const tail = briefs.slice(-limit);
	const keep = new Set(tail.map((brief) => brief.id));
	if (pinnedID) keep.add(pinnedID);
	const visible = briefs.filter((brief) => keep.has(brief.id));
	return { visible, hidden: briefs.length - visible.length };
}

// 后端 summary 形如「高位延续 · 低位一般」，已包含状态；界面单独展示状态，这里去掉重复前缀。
export function intradaySummary(summary: string, status: string): string {
	if (!status || !summary.startsWith(status)) return summary;
	return summary.slice(status.length).replace(/^[\s·:：-]+/, '');
}

// 简报正文常以一级标题开头，内容与面板上的记录标题重复；展示时去掉这一行。
// 只影响渲染，不改动存储的原文。
export function stripLeadingTitle(narrative: string): string {
	const match = narrative.match(/^\s*#{1,3}[^\n]*(\n|$)/);
	if (!match) return narrative;
	const rest = narrative.slice(match[0].length).replace(/^\s+/, '');
	return rest || narrative;
}

export type NarrativeNotice = { tone: 'ok' | 'warn' | 'error' | 'muted'; text: string };

// 校验规则要写在界面上，避免用户把「未提示」误解成「所有数字都已核对」。
export const narrativeVerifyRule = '校验规则：只核对叙述中的小数与百分数，按绝对值与采集数据比对；整数、日期和涨跌方向不参与判断。';

// 叙述状态必须如实告知用户：没有 AI 时只展示事实，不能假装有结论。
export function narrativeNotice(brief: PatrolBrief): NarrativeNotice {
	if (brief.status === 'running') return { tone: 'muted', text: '巡查进行中，采集完成后生成叙述' };
	if (brief.status === 'failed') return { tone: 'error', text: brief.error || '巡查失败' };
	switch (brief.narrative_status) {
		case 'ready':
			return brief.unverified_numbers?.length
				? { tone: 'warn', text: `叙述中有 ${brief.unverified_numbers.length} 个数值未在本次采集数据中找到，请人工核对` }
				: { tone: 'ok', text: '叙述中的小数与百分数均可在下方采集数据中找到' };
		case 'unavailable':
			return { tone: 'muted', text: 'AI 模型未配置，以下仅为采集到的事实，没有生成结论' };
		case 'failed':
			return { tone: 'error', text: brief.error || 'AI 叙述生成失败，以下仅为采集到的事实' };
		default:
			return { tone: 'muted', text: '无叙述' };
	}
}

export async function fetchPatrolStatus(config: BackendConfig, signal?: AbortSignal): Promise<PatrolStatus> {
	const payload = await requestJSON<{ data: PatrolStatus }>(config, '/api/v1/cockpit/status', { signal });
	return payload.data;
}

export async function fetchPatrolBriefs(config: BackendConfig, signal?: AbortSignal, limit = 40): Promise<PatrolBrief[]> {
	const payload = await requestJSON<{ data: PatrolBrief[] }>(config, `/api/v1/cockpit/briefs?limit=${limit}`, { signal });
	return payload.data || [];
}

// 实时事实：固定模块自己轮询这个接口，与巡查记录解耦，不依赖是否跑过巡查。
export async function fetchPatrolFacts(config: BackendConfig, signal?: AbortSignal): Promise<PatrolLiveFacts> {
	const payload = await requestJSON<{ data: PatrolLiveFacts }>(config, '/api/v1/cockpit/facts', { signal });
	return payload.data;
}

export async function runPatrol(config: BackendConfig, phase: Exclude<PatrolPhase, 'rest'>): Promise<PatrolBrief> {
	const payload = await requestJSON<{ data: PatrolBrief }>(config, '/api/v1/cockpit/briefs', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ phase }),
	});
	return payload.data;
}
