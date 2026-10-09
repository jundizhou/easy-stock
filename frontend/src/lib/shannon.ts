import { requestJSON, type BackendConfig } from './backend';

/** Brian Shannon 六层规则 · 前端类型（与 Go 侧 shannon.ScanResponse 同构）。 */

export type SwingPoint = [number, number];

export type ShannonTrend = {
	state: string;
	higher_highs: boolean;
	higher_lows: boolean;
	lower_highs: boolean;
	lower_lows: boolean;
	recent_highs: number[];
	recent_lows: number[];
	swing_highs_idx: SwingPoint[];
	swing_lows_idx: SwingPoint[];
	last_swing_low?: number;
	last_swing_high?: number;
	sources: string[];
};

export type ShannonPolarity = {
	polarity: string;
	ma5?: number;
	ma5_prev?: number;
	slope_pct?: number;
	price?: number;
	price_above_ma5: boolean;
	allow_long: boolean;
	allow_short: boolean;
	is_filter_blocking: boolean;
	sources: string[];
};

export type ShannonAlignment = {
	alignment: string;
	score: number;
	monthly: string;
	weekly: string;
	daily: string;
	ups: number;
	downs: number;
	allow_full_size: boolean;
	position_scale: number;
	sources: string[];
};

export type ShannonLight = {
	light: string;
	reasons: string[];
	posture: { action?: string; max_new?: boolean; scale?: number };
	reclaim_level?: number;
	sources: string[];
};

export type ShannonEntry = {
	valid: boolean;
	blocked: boolean;
	reason?: string;
	support?: number;
	pull_low?: number;
	trigger?: number;
	stop?: number;
	targets: number[];
	risk?: number;
	reward?: number;
	rr_ratio?: number;
	structure_ok: boolean;
	chase_blocked: boolean;
	awaiting_breakout: boolean;
	pulled_back: boolean;
	pullback_pct?: number;
	steps: string[];
	prohibitions: string[];
	sources: string[];
};

export type ShannonSizing = {
	equity: number;
	risk_pct: number;
	risk_amount: number;
	per_share_risk?: number;
	raw_shares: number;
	cap_shares?: number;
	binding_constraint: string;
	total_shares: number;
	board_lot: number;
	notional: number;
	position_pct: number;
	risk_pct_actual: number;
	raw_position_pct: number;
	position_cap_pct: number;
	insufficient_capital: boolean;
	tranches: number[];
	tranche_pcts: number[];
	scale_applied: number;
	stop_rules: string[];
	partial_exit: string[];
	sources: string[];
};

export type ShannonResult = {
	code: string;
	name?: string;
	as_of: number;
	close?: number;
	score: number;
	trend: ShannonTrend;
	polarity: ShannonPolarity;
	alignment: ShannonAlignment;
	light: ShannonLight;
	entry: ShannonEntry;
	sizing: ShannonSizing;
	sources: string[];
	needs_backtest: boolean;
	bucket?: string;
};

export type ShannonPlanPick = {
	code: string;
	name?: string;
	score: number;
	pos_pct: number;
	shares: number;
	notional: number;
	risk_pct: number;
};

export type ShannonPlan = {
	equity: number;
	candidates: number;
	picked: ShannonPlanPick[];
	total_position_pct: number;
	total_risk_pct: number;
	total_notional: number;
	cash_pct: number;
	max_positions: number;
	max_risk_pct: number;
	max_single_pct: number;
	stopped_by?: string;
};

export type ShannonScanResponse = {
	generated_at?: string;
	universe?: string;
	count: number;
	results: ShannonResult[];
	plan: ShannonPlan;
	errors?: Array<{ code: string; error: string }>;
};

export type ShannonScanRequest = {
	codes?: string[];
	universe?: string;
	board?: string;
	limit?: number;
	equity: number;
	workers?: number;
};

export type ShannonAnalyzeResponse = ShannonResult;

/** 分组中文标签（与 Go BucketLabel 对齐）。 */
export const BUCKET_LABEL: Record<string, string> = {
	ENTRY: '✅ 可入场',
	WATCH: '👀 观察',
	LOW_RR: '⚠️ 风险回报不足',
	CHASE: '🚫 追高禁区',
	NO_STRUCT: '— 无结构',
	SIZE_ZERO: '⛔ 仓位被归零',
	L2_BLOCK: '🚷 5日线拦截',
	RED: '🛑 红灯',
	OTHER: '· 其他',
	ERROR: '❗ 异常',
};

/** 灯色中文。 */
export const LIGHT_LABEL: Record<string, string> = {
	GREEN: '🟢 绿灯',
	YELLOW: '🟡 黄灯',
	RED: '🔴 红灯',
};

export const TREND_LABEL: Record<string, string> = {
	UPTREND: '上升趋势',
	DOWNTREND: '下降趋势',
	TRANSITION: '转换中',
	RANGE: '区间震荡',
};

export const ALIGN_LABEL: Record<string, string> = {
	ALIGNED_BULL: '多周期共振看多',
	BULL_LEAN: '多数看多',
	ALIGNED_BEAR: '多周期共振看空',
	BEAR_LEAN: '多数看空',
	CONFLICT: '长短冲突',
	NEUTRAL: '中性分歧',
};

export function fetchShannonScan(config: BackendConfig, request: ShannonScanRequest, init?: RequestInit): Promise<ShannonScanResponse> {
	return requestJSON<{ data: ShannonScanResponse }>(config, '/api/v1/strategy/shannon/scan', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(request),
		...init,
	}).then((payload) => payload.data);
}

export function fetchShannonAnalyze(config: BackendConfig, code: string, equity: number, init?: RequestInit): Promise<ShannonAnalyzeResponse> {
	return requestJSON<{ data: ShannonAnalyzeResponse }>(config, '/api/v1/strategy/shannon/analyze', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ code, equity }),
		...init,
	}).then((payload) => payload.data);
}

export function fetchShannonBoards(config: BackendConfig, init?: RequestInit): Promise<string[]> {
	return requestJSON<{ data: string[] }>(config, '/api/v1/strategy/shannon/boards', init).then((payload) => payload.data);
}
