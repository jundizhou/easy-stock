import { requestJSON, type BackendConfig } from './backend';

/** MER 四维分位（T 趋势 / B 广度 / L 参与 / R 风险，滚动分位 0-100，逆向·估值语义）。 */
export type MerDim = {
	key: string;
	name: string;
	score: number;
	weights: string;
	read?: string;
};

export type MerVerdict = {
	ok: boolean;
	asof?: string;
	generated_at?: string;
	tone?: string;
	state?: { confirmed?: string; raw?: string };
	index?: {
		name?: string;
		hs300?: number;
		ma60?: number;
		ma60_dev_pct?: number;
		r5_pct?: number;
		r60_pct?: number;
		dd60_pct?: number;
		n_valid?: number;
	};
	scores?: Record<string, number>;
	dims?: MerDim[];
	verdict?: {
		headline?: string;
		env_score?: number;
		worst?: string;
		best?: string;
		coverage?: string;
		risk?: string;
		falsify?: Record<string, string>;
		basis_note?: string;
	};
	similar?: {
		periods?: Array<Record<string, unknown>>;
		forward?: Record<string, unknown>;
		risk_budget?: { note?: string; h60_mdd_mean?: number; h60_mdd_worst_p10?: number };
	};
	disclaimer?: string;
	error?: string;
	hint?: string;
};

export type ClipTrackPeriod = {
	period: number;
	signal_date: string;
	role: string;
	evidence_class: string;
	status: string;
	n_holdings: number;
	recorded_at?: string;
	file: string;
};

export type ClipTrackConfig = {
	track?: string;
	version?: string;
	anchor_date?: string;
	t0_date?: string;
	rebalance_days?: number;
	target_periods?: number;
	selection?: { top_n?: number; tail_clip_q?: number };
	declared_differences_from_core2_v1?: Array<Record<string, unknown>>;
	[key: string]: unknown;
};

export type ClipTrackPayload = {
	ok: boolean;
	track_dir: string;
	config: ClipTrackConfig | null;
	config_error?: string;
	summary: Record<string, unknown> | null;
	summary_error?: string;
	periods: ClipTrackPeriod[];
	settled: Array<Record<string, unknown>>;
	nav: Array<Record<string, unknown>>;
	oos: {
		contaminated_start: string;
		contaminated_end: string;
		anchor_date: string;
		target_periods: number;
		basis: string;
	};
};

/** OOS 门禁派生结果（纯函数输出，见 deriveOosGate）。 */
export type OosGate = {
	/** LOCKED：前向已结算期数未达标；OPEN：≥24 期达标。 */
	state: 'LOCKED' | 'OPEN';
	/** 前向期（signal_date ≥ anchor）已落信号数。 */
	forwardSignals: number;
	/** 前向已结算期数（write-once settled 文件）。 */
	forwardSettled: number;
	target: number;
	/** 进度 0-1（forwardSettled/target，未开始为 0）。 */
	progress: number;
	/** 受污染区间（永久失去样本外资格）。 */
	contaminatedStart: string;
	contaminatedEnd: string;
};

export function fetchQuantVerdict(config: BackendConfig, init?: RequestInit): Promise<MerVerdict> {
	return requestJSON<MerVerdict>(config, '/api/v1/quant/proxy?path=/api/verdict', init);
}

export function fetchQuantTrack(config: BackendConfig, init?: RequestInit): Promise<ClipTrackPayload> {
	return requestJSON<ClipTrackPayload>(config, '/api/v1/quant/track', init);
}

/**
 * OOS 门禁派生（纯函数）：
 * - 前向期 = signal_date ≥ anchor_date（2026-10-08）；T0=2026-09-30 属 PRE_ANCHOR 描述性记录，不计入。
 * - 门禁 LOCKED 直到 forwardSettled ≥ target（24 期）——2024-01-01~2026-09-18 受污染区间
 *   永久失去样本外资格（governance/ADR-20261001），任何前向不足 24 期的结论都不可采信。
 */
export function deriveOosGate(track: ClipTrackPayload | null): OosGate | null {
	if (!track?.oos || !track.periods) return null;
	const target = Number(track.oos.target_periods) > 0 ? Number(track.oos.target_periods) : 24;
	const anchor = (track.oos.anchor_date || '2026-10-08').slice(0, 10);
	const forwardSignals = track.periods.filter(p => (p.signal_date || '').slice(0, 10) >= anchor).length;
	const settledPeriods = new Set(
		(track.settled || []).map(item => Number(item.period)).filter(n => Number.isFinite(n)),
	);
	const forwardSettled = track.periods.filter(
		p => (p.signal_date || '').slice(0, 10) >= anchor && settledPeriods.has(p.period),
	).length;
	return {
		state: forwardSettled >= target ? 'OPEN' : 'LOCKED',
		forwardSignals,
		forwardSettled,
		target,
		progress: target > 0 ? Math.min(forwardSettled / target, 1) : 0,
		contaminatedStart: track.oos.contaminated_start,
		contaminatedEnd: track.oos.contaminated_end,
	};
}
