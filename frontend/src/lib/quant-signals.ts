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

/**
 * 受污染区间结束年份 +1 —— OOS 时间轴「唯一合法前向区间」起点文案用。
 *
 * 必须先 Number() 再相加：`'2026'.slice(0,4) + 1` 会拼成字符串 "20261"。
 */
export function nextYearOf(isoDate: string): string {
	const year = Number((isoDate || '').slice(0, 4));
	if (!Number.isFinite(year) || year <= 0) return '—';
	return String(year + 1);
}

/** 数据新鲜度派生结果（见 describeFreshness）。 */
export type Freshness = {
	/** 距今日历天数；asof 缺失或不可解析时为 null。 */
	days: number | null;
	/** fresh ≤3 天 · aging 4-7 天 · stale ≥8 天 · unknown 无法判定。 */
	level: 'fresh' | 'aging' | 'stale' | 'unknown';
	/** 徽标文案，可直接渲染。 */
	label: string;
	stale: boolean;
};

const DAY_MS = 86_400_000;

/**
 * MER 数据陈旧时统一给出的排查说明。
 *
 * MER 不是前端算出来的：数据走「日线库 → quant_terminal 评判 → verdict_latest.json」。
 * 中间任一段停更，前端刷新无论点多少次都拿不到新值，因此必须把排查路径写给用户。
 */
export const MER_STALE_HINT =
	'这不是刷新失效，而是上游数据停更：先补齐日线数据（本机增量补数脚本），再由 Quant Terminal（8766）重算并写出 '
	+ 'C:\\quant\\mer\\verdict_latest.json，页面即可看到新值。';

/**
 * 数据新鲜度（纯函数）。
 *
 * 用 asof（数据截止日）而非「上次点击刷新的时间」来判断，避免把陈旧数据包装成最新结论。
 */
export function describeFreshness(asof: string | undefined, now: Date = new Date()): Freshness {
	const iso = (asof || '').slice(0, 10);
	const unknown: Freshness = { days: null, level: 'unknown', label: '未标注数据日期', stale: false };
	if (!/^\d{4}-\d{2}-\d{2}$/.test(iso)) return unknown;
	const anchor = Date.parse(`${iso}T00:00:00`);
	if (Number.isNaN(anchor)) return unknown;
	// 锚在本地零点，向下取整即为日历天数差（用 round 会把半天算成一天）。
	const days = Math.max(0, Math.floor((now.getTime() - anchor) / DAY_MS));
	const label = days === 0 ? '今日' : days === 1 ? '昨天' : `${days} 天前`;
	const level: Freshness['level'] = days <= 3 ? 'fresh' : days <= 7 ? 'aging' : 'stale';
	return { days, level, label, stale: level === 'stale' };
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
