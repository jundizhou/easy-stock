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
	pit_audit?: { passed?: boolean; note?: string };
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
