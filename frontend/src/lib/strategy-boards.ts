import { requestJSON, type BackendConfig } from './backend';

// ============ core2 中线量化 ============

export type Core2Holding = {
	code?: string;
	target_weight?: number;
	signal_score?: number;
	assumed_price?: number;
	price_basis?: string;
	notional_cny?: number;
};

export type Core2PeriodSummary = {
	period?: number;
	role?: string;
	signal_date?: string;
	execute_date?: string | null;
	status?: string;
	n_holdings?: number;
	gross_weight?: number;
	recorded_at?: string;
	file?: string;
};

export type Core2Latest = Record<string, unknown> & {
	holdings?: Core2Holding[];
};

export type Core2Paper = {
	ok?: boolean;
	paper_dir?: string;
	spec?: Record<string, unknown>;
	params_lock?: Record<string, unknown> | null;
	state?: Record<string, unknown> | null;
	periods?: Core2PeriodSummary[];
	latest?: Core2Latest | null;
	fetched_at?: string;
	error?: string;
};

export function fetchCore2Paper(config: BackendConfig, init?: RequestInit): Promise<Core2Paper> {
	return requestJSON<Core2Paper>(config, '/api/v1/strategy/core2/paper', init);
}

// ============ 拐点识别引擎 ============

export type InflectionFactor = {
	name: string;
	score: number;
	weight: number;
	detail?: string;
};

export type InflectionSignal = {
	type: string;
	status: string;
	setup?: string;
	kinds?: string[];
	scope?: string;
	score: number;
	old_anchor_symbol?: string;
	new_carrier_symbol?: string;
	factors: InflectionFactor[];
	evidence?: string[];
	risks?: string[];
};

export type InflectionCandidate = {
	candidate?: {
		symbol?: string;
		name?: string;
		kind?: string;
		scope_name?: string;
		recognition?: Record<string, number>;
		change_percent?: number;
		limit_up?: boolean;
		board_broken?: boolean;
	};
	recognition_score?: number;
	active_weakness_score?: number;
	active_strength_score?: number;
	clearance_score?: number;
	evidence?: string[];
};

export type InflectionEvaluation = {
	evaluated_at?: string;
	market_stress_score?: number;
	environment_turn_score?: number;
	candidates?: InflectionCandidate[];
	anchors?: Array<{
		kind?: string;
		selected?: InflectionCandidate;
		runner_up?: InflectionCandidate;
		clear?: boolean;
		score_gap?: number;
		evidence?: string[];
	}>;
	big?: InflectionSignal;
	small?: InflectionSignal;
	primary_signal?: string;
	warnings?: string[];
};

export type InflectionAutoResponse = {
	data?: InflectionEvaluation;
	assumptions?: string[];
	source?: {
		current_trade_date?: string;
		previous_trade_date?: string;
		limit_up_count?: number;
		max_streak?: number;
		previous_max_streak?: number;
		candidate_count?: number;
		session_status?: string;
	};
	error?: string;
};

export function fetchInflectionAuto(config: BackendConfig, init?: RequestInit): Promise<InflectionAutoResponse> {
	return requestJSON<InflectionAutoResponse>(config, '/api/v1/strategy/inflections/auto', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: '{}',
		...init,
	});
}

// ============ 双确认共振 ============

export type ResonanceAxis = {
	axis?: string;
	value?: number;
	is_hot?: boolean;
	absolute_ok?: boolean | null;
	relative_ok?: boolean | null;
	threshold?: number | null;
	rank?: number;
	total?: number;
	rank_pct?: number;
	status?: string;
	reason?: string;
};

export type ResonanceCalibrated = {
	bucket?: string;
	news?: ResonanceAxis;
	money?: ResonanceAxis;
	quadrant?: string;
	[key: string]: unknown;
};

export type ResonanceRow = {
	bucket?: string;
	news_heat?: number;
	news_count?: number;
	news_velocity?: number;
	news_stage?: string;
	news_sources?: number;
	news_sentiment?: string;
	news_samples?: string[];
	money_score?: number;
	limit_up_count?: number;
	board_count?: number;
	max_streak?: number;
	market_stage?: string;
	market_change?: number;
	leaders?: string[];
	resonance?: number;
	quadrant?: string;
};

export type ResonanceBoard = {
	ok?: boolean;
	board_file?: string;
	updated_at?: string;
	pit_audit?: {
		passed?: boolean;
		checked_at?: string;
		trade_date?: string;
		note?: string;
		checks?: Array<{ id: string; name: string; ok: boolean; detail: string }>;
	};
	payload?: {
		generated_at?: string;
		trade_date?: string;
		note?: string;
		window_hours?: number;
		market?: {
			session_status?: string;
			limit_up_count?: number;
			board_count?: number;
			first_board_count?: number;
			max_streak?: number;
			reopened_count?: number;
			broken_rate?: number;
		};
		pit?: { news_pit_ok?: boolean; money_pit_ok?: boolean; available_news?: number };
		calibration?: Record<string, unknown>;
		quadrants?: ResonanceRow[];
		calibrated?: ResonanceCalibrated[];
		market_themes?: Array<Record<string, unknown>>;
		disclaimer?: string;
	};
	error?: string;
	hint?: string;
};

export function fetchResonanceBoard(config: BackendConfig, init?: RequestInit): Promise<ResonanceBoard> {
	return requestJSON<ResonanceBoard>(config, '/api/v1/strategy/resonance/board', init);
}

// ============ 执行网关（easy-stock 信号 → QMT/Ptrade） ============
// 与后端 internal/execution 及 /api/v1/execution/* 端点对齐。
// P0+P1：sim 模式，不接真实下单；live 在后端被护栏守卫拒绝。

export type ExecutionPlanTarget = {
	code: string;
	target_weight: number;
	assumed_price: number;
	signal_score?: number;
	notional_cny?: number;
	shares: number;
	est_notional: number;
	action?: string;
	prefilter?: string;
	warning?: string;
};

export type ExecutionTargetPlan = {
	schema_version?: string;
	strategy?: string;
	pit_audit_passed?: boolean;
	signal_id?: string;
	signal_date?: unknown;
	execute_date?: unknown;
	period?: unknown;
	role?: unknown;
	status?: unknown;
	gross_weight?: number;
	n_holdings?: number;
	mode?: string;
	paper_dir?: string;
	generated_at?: string;
	targets?: ExecutionPlanTarget[];
	warnings?: string[];
};

export type ExecutionTargetResponse = {
	ok?: boolean;
	plan?: ExecutionTargetPlan;
	error?: string;
};

export type ExecutionSignalMeta = {
	period?: unknown;
	role?: unknown;
	signal_date?: unknown;
	execute_date?: unknown;
	status?: unknown;
	n_holdings?: unknown;
	gross_weight?: unknown;
};

export type ExecutionStatus = {
	mode?: string;
	production_enabled?: boolean;
	kill_switch_engaged?: boolean;
	pit_whitelist?: string[];
	strategy_allowed?: string;
	paper_dir?: string;
	adapter?: string;
	adapter_ready?: boolean;
	latest_signal?: ExecutionSignalMeta | null;
	audit_path?: string;
	audit_count?: number;
	warnings?: string[];
};

export type ExecutionStatusResponse = {
	ok?: boolean;
	status?: ExecutionStatus;
	error?: string;
};

export type ExecutionFill = {
	code: string;
	side: string;
	price: number;
	shares: number;
	notional: number;
	order_id?: string;
	fill_id?: string;
	filled_at?: string;
	source?: string;
	note?: string;
};

export type ExecutionIntentAck = {
	accepted?: boolean;
	mode?: string;
	intent_id?: string;
	strategy?: string;
	signal_id?: string;
	execute_on?: string;
	reviewed_at?: string;
	warnings?: string[];
	fills?: ExecutionFill[];
	message?: string;
};

export type ExecutionIntentResponse = {
	ok?: boolean;
	ack?: ExecutionIntentAck;
	error?: string;
};

export type ExecutionFillResponse = {
	ok?: boolean;
	ack?: { recorded?: boolean; fill_id?: string; recorded_at?: string };
	error?: string;
};

export type ExecutionKillResponse = {
	ok?: boolean;
	kill_switch_engaged?: boolean;
	error?: string;
};

// fetchExecutionTarget 生成 core2 可执行目标权重表（只读预览）。
export function fetchExecutionTarget(config: BackendConfig, strategy = 'core2', init?: RequestInit): Promise<ExecutionTargetResponse> {
	const sep = strategy ? `?strategy=${encodeURIComponent(strategy)}` : '';
	return requestJSON<ExecutionTargetResponse>(config, `/api/v1/execution/target${sep}`, init);
}

// fetchExecutionStatus 返回网关当前状态（只读）。
export function fetchExecutionStatus(config: BackendConfig, init?: RequestInit): Promise<ExecutionStatusResponse> {
	return requestJSON<ExecutionStatusResponse>(config, '/api/v1/execution/status', init);
}

// postExecutionIntent 受理执行意图（sim 模拟成交 / live 守卫拒绝）。
export function postExecutionIntent(config: BackendConfig, intent: Record<string, unknown>, init?: RequestInit): Promise<ExecutionIntentResponse> {
	return requestJSON<ExecutionIntentResponse>(config, '/api/v1/execution/intent', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(intent),
		...init,
	});
}

// postExecutionFill 登记一笔成交。
export function postExecutionFill(config: BackendConfig, fill: Record<string, unknown>, init?: RequestInit): Promise<ExecutionFillResponse> {
	return requestJSON<ExecutionFillResponse>(config, '/api/v1/execution/fill', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(fill),
		...init,
	});
}

// postExecutionKillSwitch 拉闸 / 解除拉闸（engaged=true 为拉闸）。
export function postExecutionKillSwitch(config: BackendConfig, engaged: boolean, init?: RequestInit): Promise<ExecutionKillResponse> {
	const path = engaged ? '/api/v1/execution/killswitch/engage' : '/api/v1/execution/killswitch/disengage';
	return requestJSON<ExecutionKillResponse>(config, path, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: '{}',
		...init,
	});
}
