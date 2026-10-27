import { requestJSON, type BackendConfig, type MarketFundFlow, type SourceMeta } from './backend';
import { markWatched, setWatchSymbols, unmarkWatched } from './watchlist-registry';

// 监控中心的类型与 API 访问，与后端 internal/monitor 的 JSON 结构一一对应。
// 时间字段后端一律给 UTC RFC3339，展示层负责换时区。

export type MonitorRuleType = 'price' | 'volume' | 'abnormal' | 'deviation' | 'market' | 'date' | 'signal' | 'sector';
export type MonitorSeverity = 'info' | 'warning' | 'critical';
export type MonitorScope = 'symbols' | 'all' | 'watchlist' | 'positions' | 'inspection';

export type MonitorCondition = {
	field: string;
	op: 'gte' | 'lte' | 'between' | 'cross_up' | 'cross_down' | 'eq';
	value: number;
	value2?: number | null;
};

export type MonitorRule = {
	id: string;
	name: string;
	type: MonitorRuleType;
	enabled: boolean;
	severity: MonitorSeverity;
	scope: MonitorScope;
	symbols?: string[];
	filters?: {
		boards?: string[];
		exclude_st?: boolean;
		min_turnover?: number;
		min_amount?: number;
		max_symbols?: number;
	};
	conditions: MonitorCondition[];
	logic: 'and' | 'or';
	cooldown_seconds: number;
	notify_channels?: string[];
	source: 'manual' | 'lot' | 'position';
	lot_id?: string;
	remind_date?: string;
	lead_days?: number;
	trigger_count: number;
	last_triggered_at?: string | null;
	created_at: string;
	updated_at: string;
};

export type MonitorEvent = {
	id: number;
	rule_id: string;
	rule_name: string;
	rule_type: string;
	symbol: string;
	name: string;
	severity: MonitorSeverity;
	trigger_kind: string;
	message: string;
	detail?: Record<string, unknown>;
	price?: number;
	change_percent?: number;
	trade_date: string;
	notify_status: string;
	notify_error?: string;
	notified_at?: string | null;
	created_at: string;
};

export type MonitorLot = {
	id: string;
	symbol: string;
	name: string;
	quantity: number;
	cost_price: number;
	buy_date: string;
	target_pct: number;
	stop_pct: number;
	remind_date: string;
	lead_days: number;
	source: 'manual' | 'position';
	position_account?: string;
	detached?: boolean;
	enabled: boolean;
	note?: string;
	created_at: string;
	updated_at: string;
};

export type MonitorLotView = MonitorLot & {
	price: number;
	market_value: number;
	pnl: number;
	pnl_pct: number;
	target_price: number;
	stop_price: number;
	to_target_pct: number;
	to_stop_pct: number;
	days_to_remind?: number | null;
	status: 'holding' | 'profit_hit' | 'stop_hit' | 'due' | 'closed';
};

export type MonitorEngineStatus = {
	running: boolean;
	enabled: boolean;
	snapshot_age_seconds: number;
	snapshot_count: number;
	snapshot_error?: string;
	snapshot_source?: string;
	snapshot_stale?: boolean;
	snapshot_warning?: string;
	session_open?: boolean;
	last_snapshot_at?: string;
	last_quote_at?: string;
	last_run_at?: string;
	last_error?: string;
	rule_count: number;
	lot_count: number;
};

export type MonitorOverview = {
	engine: MonitorEngineStatus;
	events_today: number;
	critical_today: number;
	rule_count: number;
	enabled_rule_count: number;
	lot_count: number;
	enabled_lot_count: number;
	position_lot_count: number;
	latest_events: MonitorEvent[];
};

export type MonitorSettings = {
	enabled: boolean;
	quote_interval_sec: number;
	snapshot_interval_sec: number;
	quiet_hours: string;
	digest_seconds: number;
};

export const monitorRuleTypeLabels: Record<MonitorRuleType, string> = {
	price: '价格',
	volume: '量能',
	abnormal: '异动',
	deviation: '偏离值',
	market: '大盘',
	date: '到期',
	signal: '技术信号',
	sector: '板块',
};

export const monitorFieldLabels: Record<string, string> = {
	last: '现价',
	change_pct: '涨跌幅%',
	cost_distance_pct: '距成本%',
	volume_ratio: '量比',
	turnover_rate: '换手率%',
	amount: '成交额',
	limit_up: '涨停',
	broken_limit: '炸板',
	limit_down: '跌停',
	high_60d: '60日新高',
	low_60d: '60日新低',
	dev_3d: '3日偏离%',
	dev_10d: '10日偏离%',
	dev_30d: '30日偏离%',
	index_change_pct: '指数涨跌幅%',
	up_count: '上涨家数',
	down_count: '下跌家数',
	limit_up_count: '涨停家数',
	days_to_remind: '距提醒日',
	// 技术信号库（与后端 internal/monitor/signals.go 一一对应）
	ma5_up_ma20: 'MA5 上穿 MA20',
	ma5_down_ma20: 'MA5 下穿 MA20',
	ma20_up_ma60: 'MA20 上穿 MA60',
	ma20_down_ma60: 'MA20 下穿 MA60',
	macd_golden: 'MACD 金叉',
	macd_dead: 'MACD 死叉',
	boll_upper_break: '突破布林上轨',
	boll_lower_break: '跌破布林下轨',
	vol_double: '放量（≥5日均量2倍）',
	vol_shrink: '缩量（≤5日均量一半）',
	// 板块监控字段（与后端 internal/monitor/sector.go 对应）
	sector_change: '板块涨跌幅%',
	sector_flow: '主力净额(亿)',
	sector_flow_delta: '30分钟净额变化(亿)',
	sector_speed: '5分钟涨跌变化%',
	sector_breadth: '成分上涨占比%',
	sector_limit_up: '板块涨停家数',
	sector_limit_down: '板块跌停家数',
};

// 技术信号下拉的补充说明：告诉用户这个信号大概意味着什么，
// 而不是让一串 ma5_up_ma20 自己解释自己。
export const monitorSignalHints: Record<string, string> = {
	ma5_up_ma20: '短线转强，常见于回调结束',
	ma5_down_ma20: '短线转弱，警惕回撤',
	ma20_up_ma60: '中线趋势转多',
	ma20_down_ma60: '中线趋势转空',
	macd_golden: '动能由弱转强',
	macd_dead: '动能由强转弱',
	boll_upper_break: '波动放大，情绪偏亢奋',
	boll_lower_break: '超卖边缘，注意止跌信号',
	vol_double: '资金异动，注意方向',
	vol_shrink: '交投转淡，等待方向',
};

export const monitorScopeLabels: Record<MonitorScope, string> = {
	symbols: '指定标的',
	all: '全市场',
	watchlist: '关注仓',
	positions: '持仓',
	// 巡检持仓已并入「持仓」（两份清单取并集）；inspection 仅历史规则兼容。
	inspection: '持仓',
};

// ScopeTarget 是一个作用域当前命中的标的：规则编辑器用它把
// 「这条规则在盯哪几支票」透明地摆在用户面前。
export type MonitorScopeTarget = { symbol: string; name?: string };

export type MonitorScopeTargets = Partial<Record<MonitorScope, MonitorScopeTarget[]>>;

export const fetchMonitorTargets = (config: BackendConfig) =>
	requestJSON<{ data: MonitorScopeTargets }>(config, '/api/v1/monitor/targets');

// 巡检组合存在浏览器本地，后台引擎读不到，只能由巡检页把当前草稿推过去。
// 不同步的话「巡检持仓」作用域会按历史报告的老清单跑，和页面对不上。
export const syncMonitorInspectionHoldings = (config: BackendConfig, holdings: MonitorScopeTarget[]) =>
	requestJSON<{ data: { count: number; updated_at: string } }>(config, '/api/v1/monitor/inspection-holdings', {
		method: 'PUT',
		body: JSON.stringify({ holdings }),
	});

// 智能雷达：后端主动扫描持仓与关注列表，给出带理由的分级结论。
// 结构与后端 internal/monitor/radar.go 一一对应。
export type RadarRuleSeed = {
	name: string;
	type: MonitorRuleType;
	severity: MonitorSeverity;
	scope: MonitorScope;
	symbols: string[];
	conditions: MonitorCondition[];
	logic: 'and' | 'or';
	cooldown_seconds: number;
};

export type RadarFinding = {
	symbol: string;
	name: string;
	kind: 'opportunity' | 'risk' | 'watch';
	score: number;
	tags: string[];
	reasons: string[];
	price: number;
	change_percent: number;
	ma_state: string;
	vol_ratio_5: number;
	position_60d: number;
	have_position_60d: boolean;
	momentum_5d: number;
	is_position: boolean;
	// source 是这只票进入雷达池的来源（持仓 / 关注仓 / 规则标的）。
	source?: string;
	signals: string[];
	// 近 30 日收盘序列：行内迷你走势图；缺数据的标的留空。
	closes?: number[];
	suggested_rule?: RadarRuleSeed;
};

// RadarMover 是扫描池里当日涨跌靠前的标的：整体风平浪静时填空态。
export type RadarMover = {
	symbol: string;
	name: string;
	change_percent: number;
	volume_ratio: number;
};

// RadarIndex 是一条宽基指数当日涨跌（id 与行情服务指数目录一致）。
export type RadarIndex = {
	id: string;
	name: string;
	change_pct: number;
};

// RadarMarket 是扫描时点的大盘环境（宽度 + 指数 + 成交额）。
export type RadarMarket = {
	have_indexes: boolean;
	index_change_pct: number;
	indexes?: RadarIndex[];
	have_breadth: boolean;
	up_count: number;
	down_count: number;
	limit_up_count: number;
	total_amount: number; // 亿元
};

export type RadarReport = {
	scanned: number;
	skipped: number;
	trade_date: string;
	generated_at: string;
	summary: string;
	counts: Record<string, number>;
	findings: RadarFinding[];
	market?: RadarMarket | null;
	movers?: RadarMover[];
	error?: string;
	total_counts?: Record<string, number>;
	total_findings?: number;
	source_counts?: Record<string, number>;
	targets?: RadarTargetState[];
};

export type RadarTargetState = {
	symbol: string;
	name?: string;
	source: string;
	status: 'pending' | 'evaluated' | 'insufficient_data';
	kind?: RadarFinding['kind'];
	reason?: string;
	daily_as_of?: string;
};

export const radarKindLabels: Record<RadarFinding['kind'], string> = {
	opportunity: '机会',
	risk: '风险',
	watch: '观察',
};

export const fetchMonitorRadar = (config: BackendConfig, force = false) =>
	requestJSON<{ data: RadarReport }>(config, `/api/v1/monitor/radar${force ? '?force=1' : ''}`);

// 异动预警：交易所异常波动口径（3/10/30 日偏离值 vs 基准指数）。
// 结构与后端 internal/monitor/abnormal.go、service.go 的 AbnormalView 对齐。
export type MonitorAbnormalWindowAvailability = Partial<Record<'3d' | '10d' | '30d', boolean>>;

export type MonitorAbnormalWindow = {
	key: string;
	label: string;
	dev: number;
	threshold: number;
	threshold_up?: number;
	threshold_down?: number;
	direction: 'up' | 'down';
	progress: number;
	remaining: number;
	trigger_price: number;
};

export type MonitorAbnormalItem = {
	symbol: string;
	name: string;
	board: string;
	price: number;
	change_percent: number;
	turnover_rate: number;
	volume_ratio: number;
	amount: number;
	signals: string[];
	level: string;
	// 达到「严重异常波动」标准（10 日 ±100%/−50%、30 日 ±200%/−70%）：
	// 3 日窗口越线只是普通异常波动，不能进「严重异动」榜。
	severe?: boolean;
	dev_3d: number;
	dev_10d: number;
	dev_30d: number;
	worst_key: string;
	threshold: number;
	direction: 'up' | 'down';
	remaining: number;
	trigger_price: number;
	events_10d: number;
	// 最近一次越过异常波动阈值的交易日（YYYY-MM-DD），空 = 30 日内未触发。
	last_trigger_date?: string;
	windows: MonitorAbnormalWindow[];
	window_availability?: MonitorAbnormalWindowAvailability;
	is_st?: boolean;
	// 用户从异动页侧栏加入监控的个股：没到阈值也持续展示当前距离。
	monitored?: boolean;
	daily_as_of?: string;
	calculated_at?: string;
	historical?: boolean;
};

export type MonitorAbnormalView = {
	tab: string;
	trade_date: string;
	items: MonitorAbnormalItem[];
	total: number;
	generated_at: string;
	snapshot_age_seconds: number;
	snapshot_at?: string;
	snapshot_source?: string;
	snapshot_stale?: boolean;
	error?: string;
};

export type MonitorAbnormalCalcRow = {
	date: string;
	stock_change: number;
	index_change?: number | null;
	dev_3d?: number | null;
	dev_10d?: number | null;
	dev_30d?: number | null;
};

export type MonitorAbnormalCalc = {
	symbol: string;
	name: string;
	board: string;
	is_st: boolean;
	price: number;
	change_percent: number;
	benchmark_id?: string;
	benchmark_label?: string;
	windows?: MonitorAbnormalWindow[];
	window_availability?: MonitorAbnormalWindowAvailability;
	level?: string;
	rows?: MonitorAbnormalCalcRow[];
	// 最近一次越过阈值的交易日（YYYY-MM-DD），空 = 30 日内未触发。
	last_trigger_date?: string;
	generated_at: string;
	error?: string;
};

export const monitorAbnormalLevelLabels: Record<string, string> = {
	triggered: '已触发',
	near: '接近阈值',
	watch: '观察',
	normal: '正常',
};

export const monitorAbnormalBoardLabels: Record<string, string> = {
	main: '主板',
	chinext: '创业板',
	star: '科创板',
	bse: '北交所',
};

export const fetchMonitorAbnormal = (config: BackendConfig, tab = 'deviation', limit = 200) =>
	requestJSON<{ data: MonitorAbnormalView }>(config, `/api/v1/monitor/abnormal?tab=${tab}&limit=${limit}`);

export const fetchMonitorAbnormalCalc = (config: BackendConfig, symbol: string) =>
	requestJSON<{ data: MonitorAbnormalCalc }>(config, `/api/v1/monitor/abnormal/calc?symbol=${encodeURIComponent(symbol)}`);

// 板块监控：板块选择器候选 + 字段说明。
// dimension theme = 系统题材库题材（TH 编号），观测由成员股快照实时聚合。
export type MonitorSectorOption = {
	code: string;
	name: string;
	dimension: 'industry' | 'concept' | 'theme';
	change_percent: number;
	main_net_inflow: number;
};

export const monitorSectorDimensionLabels: Record<string, string> = {
	industry: '行业',
	concept: '概念',
	theme: '题材库',
};

export const fetchMonitorSectors = (config: BackendConfig) =>
	requestJSON<{ data: { items: MonitorSectorOption[] } }>(config, '/api/v1/monitor/sectors');

export const monitorSectorFieldHints: Record<string, string> = {
	sector_change: '板块指数相对昨收的涨跌幅，来自东财板块榜单',
	sector_flow: '当日累计主力净流入（亿），流入为正',
	sector_flow_delta: '近 30 分钟主力净额变化（亿），加速流入为正、撤离为负',
	sector_speed: '近 5 分钟板块涨跌幅变化（百分点），捕捉快速拉升/回落',
	sector_breadth: '成分股里上涨的占比（%），80 即"普涨"',
	sector_limit_up: '板块内涨停家数（按涨跌停价精确判断，非涨幅近似）',
	sector_limit_down: '板块内跌停家数',
};

const jsonRequest = async <T,>(config: BackendConfig, path: string, method: string, body?: unknown): Promise<T> => {
	return requestJSON<T>(config, path, {
		method,
		headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
		body: body === undefined ? undefined : JSON.stringify(body),
	});
};

export const fetchMonitorOverview = (config: BackendConfig) =>
	requestJSON<{ data: MonitorOverview }>(config, '/api/v1/monitor/overview');
export const fetchMonitorRules = (config: BackendConfig) =>
	requestJSON<{ items: MonitorRule[] }>(config, '/api/v1/monitor/rules');
export const saveMonitorRule = (config: BackendConfig, rule: Partial<MonitorRule>, id?: string) =>
	jsonRequest<{ data: MonitorRule }>(config, id ? `/api/v1/monitor/rules/${id}` : '/api/v1/monitor/rules', id ? 'PUT' : 'POST', rule);
export const toggleMonitorRule = (config: BackendConfig, id: string, enabled: boolean) =>
	jsonRequest<{ ok: boolean }>(config, `/api/v1/monitor/rules/${id}/toggle`, 'POST', { enabled });
export const deleteMonitorRule = (config: BackendConfig, id: string) =>
	jsonRequest<{ ok: boolean }>(config, `/api/v1/monitor/rules/${id}`, 'DELETE');
export const testMonitorRule = (config: BackendConfig, rule: Partial<MonitorRule>) =>
	jsonRequest<{ data: { would_fire: number; total: number; samples: Array<{ symbol: string; name: string; price: number; change_percent: number; signals: string[] }> } }>(
		config, '/api/v1/monitor/rules/test', 'POST', rule);
export const fetchMonitorEvents = (config: BackendConfig, query: string) =>
	requestJSON<{ items: MonitorEvent[]; total: number; matched_total?: number; has_more?: boolean; since_id: number }>(config, `/api/v1/monitor/events${query ? `?${query}` : ''}`);
export const deleteMonitorEvent = (config: BackendConfig, id: number) =>
	jsonRequest<{ ok: boolean }>(config, `/api/v1/monitor/events/${id}`, 'DELETE');
export const clearMonitorEvents = (config: BackendConfig) =>
	jsonRequest<{ ok: boolean }>(config, '/api/v1/monitor/events', 'DELETE');
export const fetchMonitorLots = (config: BackendConfig) =>
	requestJSON<{ items: MonitorLotView[]; total: number }>(config, '/api/v1/monitor/lots');
export const saveMonitorLot = (config: BackendConfig, lot: Partial<MonitorLot>, id?: string) =>
	jsonRequest<{ data: MonitorLot }>(config, id ? `/api/v1/monitor/lots/${id}` : '/api/v1/monitor/lots', id ? 'PUT' : 'POST', lot);
export const deleteMonitorLot = (config: BackendConfig, id: string) =>
	jsonRequest<{ ok: boolean }>(config, `/api/v1/monitor/lots/${id}`, 'DELETE');
export const syncMonitorLots = (config: BackendConfig) =>
	jsonRequest<{ synced: number }>(config, '/api/v1/monitor/lots/sync', 'POST', {});
export const detachMonitorLot = (config: BackendConfig, id: string) =>
	jsonRequest<{ data: MonitorLot }>(config, `/api/v1/monitor/lots/${id}/detach`, 'POST', {});
export const fetchMonitorSettings = (config: BackendConfig) =>
	requestJSON<{ data: MonitorSettings & { notify_enabled: boolean } }>(config, '/api/v1/monitor/settings');
export const saveMonitorSettings = (config: BackendConfig, settings: Partial<MonitorSettings>) =>
	jsonRequest<{ data: MonitorSettings & { notify_enabled: boolean } }>(config, '/api/v1/monitor/settings', 'PUT', settings);

// ---------------------------------------------------------------- 关注仓

// MonitorSymbolHit 是搜索添加的一次命中：后端走东财建议接口，
// 名称 / 6 位代码 / 拼音全拼 / 拼音首字母都能搜到。
export type MonitorSymbolHit = { symbol: string; code: string; name: string };

// MonitorWatchItem 是关注仓列表的一行；行情字段在快照不可用时缺省。
export type MonitorWatchItem = {
	symbol: string;
	name?: string;
	note?: string;
	created_at?: string | null;
	last?: number;
	change_percent?: number | null;
	// momentum_5d 是近 5 个交易日累计涨跌幅（%）；日线不足时缺省。
	momentum_5d?: number | null;
	have_quote?: boolean;
};

export type MonitorWatchCoverage = {
	generated_at: string;
	engine: MonitorEngineStatus;
	items: Array<{ symbol: string; status: 'covered' | 'unconfigured' | 'unknown'; rule_count: number; reason?: string }>;
	error?: string;
};

export const fetchMonitorStockSuggest = (config: BackendConfig, query: string) =>
	requestJSON<{ data: MonitorSymbolHit[] }>(config, `/api/v1/monitor/stock-suggest?q=${encodeURIComponent(query)}`);

// 关注仓的写接口是「全局镜像」的唯一同步出口：右键菜单要在任何页面判断
// 一只票是否已在关注仓，不能让各调用点各自维护一份状态（那正是之前
// 「同一只票在雷达页显示加入关注、在关注仓页显示删除关注」的成因）。
// 这里在请求成功后回写 watchlist-registry，请求失败不动镜像，以服务端为准。
export const fetchMonitorWatchlist = async (config: BackendConfig) => {
	const payload = await requestJSON<{ data: MonitorWatchItem[]; coverage?: MonitorWatchCoverage }>(config, '/api/v1/monitor/watchlist');
	setWatchSymbols(payload.data ?? []);
	return payload;
};

export type MonitorWatchFundFlowView = { data: MarketFundFlow[]; meta: SourceMeta | null };

export async function fetchMonitorWatchFunds(config: BackendConfig, symbols: string[], signal?: AbortSignal): Promise<MonitorWatchFundFlowView> {
	const unique = [...new Set(symbols)].sort();
	const data: MarketFundFlow[] = [];
	let meta: SourceMeta | null = null;
	for (let index = 0; index < unique.length; index += 200) {
		const query = encodeURIComponent(unique.slice(index, index + 200).join(','));
		const payload = await requestJSON<{ data: MarketFundFlow[]; meta: SourceMeta }>(config, `/api/v1/market/flows?dimension=stock&symbols=${query}`, { signal });
		data.push(...payload.data);
		if (meta === null) meta = { ...payload.meta };
		else {
			const stale: boolean = Boolean(meta.stale || payload.meta.stale);
			const oldest: SourceMeta = Date.parse(payload.meta.fetched_at) < Date.parse(meta.fetched_at) ? payload.meta : meta;
			meta = { ...oldest, stale };
		}
	}
	return { data, meta };
}

export const addMonitorWatch = async (config: BackendConfig, item: { symbol: string; name?: string; note?: string }) => {
	const payload = await jsonRequest<{ data: MonitorWatchItem[] | { ok: boolean } }>(config, '/api/v1/monitor/watchlist', 'POST', item);
	// 正常路径返回整份清单；引擎异常时后端退化成 {ok:true}，此时按单只补镜像。
	if (Array.isArray(payload.data)) setWatchSymbols(payload.data);
	else markWatched(item.symbol, item.name);
	return payload;
};

export const removeMonitorWatch = async (config: BackendConfig, symbol: string) => {
	const payload = await jsonRequest<{ data: { ok: boolean } }>(config, `/api/v1/monitor/watchlist/${encodeURIComponent(symbol)}`, 'DELETE');
	unmarkWatched(symbol);
	return payload;
};

// 二次编辑备注：PATCH 语义，note 传空串即清空（POST 的 upsert 空备注不覆盖）。
export const updateMonitorWatchNote = (config: BackendConfig, symbol: string, note: string) =>
	jsonRequest<{ data: { ok: boolean } }>(config, '/api/v1/monitor/watchlist', 'PATCH', { symbol, note });

export const clearMonitorWatch = async (config: BackendConfig) => {
	const payload = await jsonRequest<{ data: { ok: boolean } }>(config, '/api/v1/monitor/watchlist', 'DELETE');
	setWatchSymbols([]);
	return payload;
};
