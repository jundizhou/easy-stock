import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
	Plus, RefreshCw, Radar, Trash2, Save, FlaskConical, LoaderCircle, X, ArrowRightLeft,
	ShieldAlert, Activity, TrendingUp, Gauge, Waves, Wind, SlidersHorizontal, Star, Search,
	Download, Crosshair, PieChart, Wallet, BellRing, Flame,
	ChevronDown, ChevronUp, ChevronLeft, ChevronRight, ListPlus, Pencil, Play, Pause, Check,
} from 'lucide-react';
import { MonitorSelect } from './MonitorSelect';
import type { BackendConfig } from '../lib/backend';
import {
	fetchMonitorOverview, fetchMonitorRules, saveMonitorRule, toggleMonitorRule, deleteMonitorRule, testMonitorRule,
	fetchMonitorEvents, deleteMonitorEvent, clearMonitorEvents,
	fetchMonitorSettings, saveMonitorSettings, fetchMonitorRadar, radarKindLabels,
	fetchMonitorSectors, monitorSectorFieldHints, monitorSectorDimensionLabels,
	fetchMonitorTargets, fetchMonitorStockSuggest, fetchMonitorWatchlist, fetchMonitorWatchFunds, addMonitorWatch, removeMonitorWatch, clearMonitorWatch, updateMonitorWatchNote,
	fetchMonitorAbnormal, fetchMonitorAbnormalCalc, monitorAbnormalLevelLabels, monitorAbnormalBoardLabels,
	monitorRuleTypeLabels, monitorFieldLabels, monitorScopeLabels, monitorSignalHints,
	type MonitorRule, type MonitorRuleType, type MonitorEvent,
	type MonitorOverview, type MonitorScope, type MonitorCondition,
	type MonitorSectorOption, type MonitorScopeTargets, type RadarReport, type RadarFinding,
	type MonitorSymbolHit, type MonitorWatchItem, type MonitorWatchCoverage, type MonitorWatchFundFlowView,
	type MonitorAbnormalView, type MonitorAbnormalItem, type MonitorAbnormalCalc, type MonitorAbnormalWindow,
} from '../lib/monitor';
import { confirmDialog } from './ConfirmDialog';
import { monitorLiveBus, useLiveQuoteVersion, useLiveStreamStatus, liveStatusLabel, selectMonitorQuoteValue } from '../lib/monitorLiveBus';
import { LivePriceCell, LivePriceText } from './LivePriceCell';
import './monitor-table.css';
import './monitor.css';

type MonitorTab = 'abnormal' | 'radar' | 'events' | 'watch';

const availableFields: Record<MonitorRuleType, Array<{ key: string; op?: MonitorCondition['op'] }>> = {
	price: [{ key: 'last' }, { key: 'change_pct' }, { key: 'cost_distance_pct' }],
	volume: [{ key: 'volume_ratio' }, { key: 'turnover_rate' }, { key: 'amount' }],
	abnormal: [{ key: 'limit_up' }, { key: 'broken_limit' }, { key: 'limit_down' }, { key: 'high_60d' }, { key: 'low_60d' }],
	deviation: [{ key: 'dev_3d' }, { key: 'dev_10d' }, { key: 'dev_30d' }],
	market: [{ key: 'index_change_pct' }, { key: 'up_count' }, { key: 'down_count' }, { key: 'limit_up_count' }],
	date: [{ key: 'days_to_remind' }],
	signal: [
		{ key: 'ma5_up_ma20' }, { key: 'ma5_down_ma20' }, { key: 'ma20_up_ma60' }, { key: 'ma20_down_ma60' },
		{ key: 'macd_golden' }, { key: 'macd_dead' }, { key: 'boll_upper_break' }, { key: 'boll_lower_break' },
		{ key: 'vol_double' }, { key: 'vol_shrink' },
	],
	sector: [
		{ key: 'sector_change' }, { key: 'sector_speed' }, { key: 'sector_flow' }, { key: 'sector_flow_delta' },
		{ key: 'sector_breadth' }, { key: 'sector_limit_up' }, { key: 'sector_limit_down' },
	],
};

const emptyRule = (): Partial<MonitorRule> => ({
	name: '', type: 'price', enabled: true, severity: 'warning', scope: 'symbols',
	conditions: [{ field: 'last', op: 'gte', value: 0 }], logic: 'and',
	cooldown_seconds: 300, filters: { exclude_st: true },
});

// 一键预设：覆盖「持仓该不该卖 / 该不该盯」的四条最高频规则。
// 之所以给预设而不是留一张白纸：新装系统里 0 条规则时，用户并不知道
// 该配什么阈值；先给四条能立刻生效的，再让他在右侧改数字，成本最低。
const quickStartPresets: Array<{
	key: string;
	title: string;
	desc: string;
	icon: typeof Radar;
	rule: Partial<MonitorRule>;
}> = [
	{
		key: 'cost-protect',
		title: '持仓成本保护',
		desc: '现价距成本跌幅超过 5% 时提醒，避免小亏拖成大亏',
		icon: ShieldAlert,
		rule: {
			name: '持仓跌破成本 5%', type: 'price', scope: 'positions', severity: 'warning',
			conditions: [{ field: 'cost_distance_pct', op: 'lte', value: -5 }],
			logic: 'and', cooldown_seconds: 900, filters: { exclude_st: true },
		},
	},
	{
		key: 'volume-surge',
		title: '持仓放量异动',
		desc: '量比 ≥ 2 时提醒，放量往往先于价格选择方向',
		icon: Activity,
		rule: {
			name: '持仓放量（量比≥2）', type: 'volume', scope: 'positions', severity: 'info',
			conditions: [{ field: 'volume_ratio', op: 'gte', value: 2 }],
			logic: 'and', cooldown_seconds: 900, filters: { exclude_st: true },
		},
	},
	{
		key: 'high-60d',
		title: '持仓创 60 日新高',
		desc: '突破前高时提醒，是加仓还是止盈由你决定',
		icon: TrendingUp,
		rule: {
			name: '持仓创 60 日新高', type: 'abnormal', scope: 'positions', severity: 'info',
			conditions: [{ field: 'high_60d', op: 'gte', value: 1 }],
			logic: 'and', cooldown_seconds: 1800, filters: { exclude_st: true },
		},
	},
	{
		key: 'market-drop',
		title: '大盘急跌',
		desc: '指数跌幅 ≤ -2% 时提醒，先看是不是系统性风险',
		icon: Gauge,
		rule: {
			name: '大盘急跌（指数≤-2%）', type: 'market', scope: 'all', severity: 'critical',
			conditions: [{ field: 'index_change_pct', op: 'lte', value: -2 }],
			logic: 'and', cooldown_seconds: 900, filters: { exclude_st: true },
		},
	},
	{
		key: 'macd-golden',
		title: '关注仓 MACD 金叉',
		desc: '技术信号：动能由弱转强的当日提醒一次，可组合其他信号',
		icon: Radar,
		rule: {
			name: '关注仓 MACD 金叉', type: 'signal', scope: 'watchlist', severity: 'info',
			conditions: [{ field: 'macd_golden', op: 'gte', value: 1 }],
			logic: 'and', cooldown_seconds: 3600, filters: { exclude_st: true },
		},
	},
];

function formatMoney(value: number) {
	if (Math.abs(value) >= 1e8) return `${(value / 1e8).toFixed(2)} 亿`;
	if (Math.abs(value) >= 1e4) return `${(value / 1e4).toFixed(0)} 万`;
	return value.toLocaleString('zh-CN', { maximumFractionDigits: 2 });
}

function formatTime(value?: string | null) {
	if (!value) return '—';
	const date = new Date(value);
	return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function formatDataTime(value?: string) {
	return value && Number.isFinite(Date.parse(value)) && Date.parse(value) > 0 ? formatTime(value) : '未提供';
}

function quoteConnectionLabel(status: ReturnType<typeof useLiveStreamStatus>) {
	if (status.connection === 'connecting') return '连接中';
	if (status.connection === 'reconnecting') return '连接中断';
	if (status.connection === 'closed') return '未连接';
	if (status.warning) return '行情降级';
	if (status.stale) return '行情过期';
	return status.sessionOpen ? '实时连接' : '非交易时段';
}

// 雷达展开面板里的「建议规则」条件文案：字段名走 monitorFieldLabels，符号直接给数学记号。
const conditionOpText: Record<string, string> = {
	gte: '≥', lte: '≤', gt: '>', lt: '<', eq: '=', between: '介于', cross_up: '上穿', cross_down: '下穿',
};

function describeRadarCondition(condition: MonitorCondition) {
	const label = monitorFieldLabels[condition.field] ?? condition.field;
	const op = conditionOpText[condition.op] ?? condition.op;
	if (condition.op === 'between') return `${label} ${op} ${condition.value}~${condition.value2 ?? ''}`;
	return `${label} ${op} ${condition.value}`;
}

export function MonitorWorkspace({ config, refreshKey, onOpenMarketIndexes }: { config: BackendConfig | null; refreshKey: number; onOpenMarketIndexes: () => void }) {
	// 默认停在「触发与规则」：进监控中心先看今天触发了什么、规则还开着哪些，
	// 异动预警是发现标的之后才要细看的一页。
	const [tab, setTab] = useState<MonitorTab>('events');
	// 关注仓搜索框在页签行右侧，加入成功后通过 tick 让列表立刻刷新。
	const [watchTick, setWatchTick] = useState(0);
	useEffect(() => {
		if (!config) return;
		return monitorLiveBus.connect(config);
	}, [config?.backendUrl, config?.token]);
	return (
		<section className="monitor-workspace" aria-label="监控中心">
			<div className="monitor-head">
				<div className="monitor-tabs" role="tablist">
					<button type="button" className={tab === 'abnormal' ? 'active' : ''} onClick={() => setTab('abnormal')}><BellRing size={15} /> 异动预警</button>
					<button type="button" className={tab === 'radar' ? 'active' : ''} onClick={() => setTab('radar')}><Radar size={15} /> 智能雷达</button>
					<button type="button" className={tab === 'events' ? 'active' : ''} onClick={() => setTab('events')}><Gauge size={15} /> 触发与规则</button>
					<button type="button" className={tab === 'watch' ? 'active' : ''} onClick={() => setTab('watch')}><Star size={15} /> 关注仓</button>
				</div>
				{tab === 'watch' && <WatchSearchBox config={config} onAdded={() => setWatchTick((n) => n + 1)} />}
			</div>
			{tab === 'abnormal' && <AbnormalTab config={config} />}
			{tab === 'radar' && <RadarTab config={config} onOpenMarketIndexes={onOpenMarketIndexes} />}
			{tab === 'events' && <EventsTab config={config} refreshKey={refreshKey} />}
			{tab === 'watch' && <WatchTab config={config} refreshKey={refreshKey} reloadTick={watchTick} onOpenRadar={() => setTab('radar')} onOpenRules={() => setTab('events')} />}
		</section>
	);
}

// WatchSearchBox：关注仓的搜索添加框，挂在页签行右上角。
// 支持名称、代码、拼音（后端走东财建议接口，上游原生支持拼音，前端不用带拼音表）。
function WatchSearchBox({ config, onAdded }: { config: BackendConfig | null; onAdded: () => void }) {
	const [query, setQuery] = useState('');
	const [hits, setHits] = useState<MonitorSymbolHit[]>([]);
	const [suggestOpen, setSuggestOpen] = useState(false);
	const [suggesting, setSuggesting] = useState(false);
	const [pending, setPending] = useState<MonitorSymbolHit | null>(null);
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState('');
	const boxRef = useRef<HTMLDivElement | null>(null);

	// 搜索防抖：输入停 250ms 才打接口，清空即收起候选。
	useEffect(() => {
		if (!config || !query.trim()) { setHits([]); setSuggestOpen(false); return; }
		const timer = window.setTimeout(async () => {
			setSuggesting(true);
			try {
				const data = (await fetchMonitorStockSuggest(config, query.trim())).data;
				setHits(data);
				setSuggestOpen(true);
				setError('');
			} catch (cause) {
				setError(String((cause as Error).message));
			} finally {
				setSuggesting(false);
			}
		}, 250);
		return () => window.clearTimeout(timer);
	}, [config, query]);

	// 点击别处收起候选下拉。
	useEffect(() => {
		const onClickAway = (event: MouseEvent) => {
			if (boxRef.current && !boxRef.current.contains(event.target as Node)) setSuggestOpen(false);
		};
		document.addEventListener('mousedown', onClickAway);
		return () => document.removeEventListener('mousedown', onClickAway);
	}, []);

	const add = async (hit: MonitorSymbolHit) => {
		if (!config || busy) return;
		setBusy(true); setError('');
		try {
			await addMonitorWatch(config, { symbol: hit.symbol, name: hit.name, note: '' });
			onAdded();
			setQuery(''); setHits([]); setSuggestOpen(false); setPending(null);
		} catch (cause) {
			setError(String((cause as Error).message));
		} finally {
			setBusy(false);
		}
	};

	const directCode = query.trim().match(/^\d{6}$/);
	return (
		<div className="monitor-watch-search" ref={boxRef}>
			<Search size={14} aria-hidden="true" />
			<input
				value={query}
				onChange={(event) => { setQuery(event.target.value); setPending(null); }}
				onKeyDown={(event) => {
					// 直接输 6 位代码回车也能加，不必等搜索结果。
					if (event.key === 'Enter' && directCode && !busy) {
						void add(pending || { symbol: directCode[0], code: directCode[0], name: '' });
					}
				}}
				placeholder="搜索添加：名称 / 代码 / 拼音，如 gzmt"
				aria-label="搜索股票"
			/>
			{suggesting && <LoaderCircle size={13} className="spin" />}
			{suggestOpen && hits.length > 0 && (
				<div className="monitor-suggest" role="listbox">
					{hits.map((hit) => (
						<button type="button" key={hit.symbol} role="option" aria-selected="false" disabled={busy}
							onClick={() => { setPending(hit); void add(hit); }}>
							<strong>{hit.name}</strong>
							<small>{hit.symbol}</small>
						</button>
					))}
				</div>
			)}
			{suggestOpen && !suggesting && hits.length === 0 && (
				<div className="monitor-suggest empty"><span>没有匹配的 A 股，换个关键词试试</span></div>
			)}
			{error && <div className="monitor-suggest empty"><span>搜索失败：{error}</span></div>}
		</div>
	);
}

// ---------------------------------------------------------------- Tab 0 异动预警

// 交易所异常波动口径（与后端 abnormal.go 同一套规则）：
// 3/10/30 日累计偏离值对照阈值，已触发 / 接近阈值 / 观察三级；
// 单票给逐窗口剩余空间、估算触发价与近 30 个交易日明细。
// 分级阈值：偏离/阈值 ≥1 触发、≥0.7 接近、≥0.5 观察。
const abnormalLevelMeta: Record<string, { label: string; cls: string; hint: string }> = {
	triggered: { label: '估算越线', cls: 'is-triggered', hint: '本地估算偏离已达到阈值，非交易所公告认定' },
	near: { label: '接近阈值', cls: 'is-near', hint: '已走到阈值 70%' },
	watch: { label: '观察', cls: 'is-watch', hint: '已走到阈值 50%' },
};

const abnormalWindowLabels: Record<string, string> = { '3d': '3日偏离', '10d': '10日偏离', '30d': '30日偏离' };
const abnormalWindowKeys = ['3d', '10d', '30d'] as const;

function abnormalSigned(value?: number | null, digits = 2) {
	if (value === null || value === undefined || !Number.isFinite(value)) return '—';
	return `${value > 0 ? '+' : ''}${value.toFixed(digits)}%`;
}

// 距阈值以偏离值的百分点表示，不等于现价还需上涨/下跌的百分比。
function AbnormalDistance({ remaining, direction }: { remaining?: number; direction?: string }) {
	if (remaining === undefined || !Number.isFinite(remaining)) return <span title="样本不足或尚未计算">—</span>;
	const past = remaining <= 0;
	return (
		<span className={`monitor-abn-remaining${past ? ' is-past' : ''}`}
			title={`${direction === 'down' ? '下行' : '上行'}偏离距阈值，单位：百分点；不是现价涨跌幅`}>
			{remaining === 0 ? '恰达阈值' : `${past ? '超出' : '尚差'} ${Math.abs(remaining).toFixed(2)}`}
		</span>
	);
}

function AbnormalRemaining({ item }: { item: MonitorAbnormalItem }) {
	const window = item.windows?.find((entry) => entry.key === item.worst_key);
	return <AbnormalDistance remaining={window?.remaining ?? item.remaining} direction={window?.direction ?? item.direction} />;
}

function abnormalItemDeviation(item: MonitorAbnormalItem, key: typeof abnormalWindowKeys[number]) {
	if (item.window_availability?.[key] === false) return undefined;
	return item.windows?.find((window) => window.key === key)?.dev ?? item[`dev_${key}`];
}

function AbnormalMeter({ progress }: { progress?: number }) {
	if (progress === undefined || !Number.isFinite(progress) || progress < 0) {
		return <span className="monitor-abn-meter-cell" title="样本不足或尚未计算">—</span>;
	}
	const percent = Math.max(0, Math.min(1, progress)) * 100;
	// 未到阈值的比例不显示成 100%，显示舍入不得改变越线含义。
	const label = `${(Math.floor(progress * 1000) / 10).toFixed(1).replace(/\.0$/, '')}%`;
	return (
		<span className={`monitor-abn-meter-cell${progress >= 1 ? ' is-triggered' : ''}`}
			role="meter" aria-label="偏离值接近阈值的程度" aria-valuemin={0} aria-valuemax={100}
			aria-valuenow={percent} aria-valuetext={label} title={`${label} · ${progress >= 1 ? '估算已达到阈值' : '尚未达到阈值'}`}>
			<span className="monitor-abn-meter"><i style={{ width: `${percent}%`, backgroundSize: `${percent > 0 ? 10000 / percent : 100}% 100%` }} /></span>
			<em>{label}</em>
		</span>
	);
}

// 单个窗口的状态卡：偏离值 + 接近度 + 阈值/剩余/触发价，
// 计算器与列表展开行共用同一块，避免两处口径写歪。
function AbnormalWindowTile({ window, windowKey }: { window?: MonitorAbnormalWindow; windowKey: string }) {
	const key = window?.key ?? windowKey;
	if (!window) {
		return (
			<article className="monitor-abn-tile is-unavailable">
				<header><span>{abnormalWindowLabels[key] ?? key}</span><b>—</b></header>
				<p className="monitor-abn-state">样本不足或基准数据缺失</p>
			</article>
		);
	}
	const thresholdLabel = window.threshold_up !== undefined && window.threshold_down !== undefined
		? window.threshold_up === window.threshold_down ? `±${window.threshold_up}%` : `+${window.threshold_up}% / −${window.threshold_down}%`
		: `${window.direction === 'down' ? '−' : '+'}${window.threshold}%`;
	return (
		<article className={`monitor-abn-tile is-${window.direction}${window.remaining <= 0 ? ' is-past' : ''}`}>
			<header>
				<span>{abnormalWindowLabels[window.key] ?? window.label}</span>
				<b>{abnormalSigned(window.dev)}</b>
			</header>
			<AbnormalMeter progress={window.progress} />
			<dl>
				<div><dt>触发阈值</dt><dd>{thresholdLabel}</dd></div>
				<div><dt>距阈值（百分点）</dt><dd><AbnormalDistance remaining={window.remaining} direction={window.direction} /></dd></div>
				<div><dt>估算触发价</dt><dd>{window.remaining > 0 && window.trigger_price > 0 ? window.trigger_price.toFixed(2) : '—'}</dd></div>
			</dl>
		</article>
	);
}

function AbnormalTab({ config }: { config: BackendConfig | null }) {
	const streamStatus = useLiveStreamStatus();
	const [view, setView] = useState<MonitorAbnormalView | null>(null);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState('');
	const [levelFilter, setLevelFilter] = useState('');
	// 单票计算器：既可点列表行带入，也可在左栏直接搜新标的。
	const [calcSymbol, setCalcSymbol] = useState('');
	const [calc, setCalc] = useState<MonitorAbnormalCalc | null>(null);
	const [calcLoading, setCalcLoading] = useState(false);
	const [calcError, setCalcError] = useState('');
	const [query, setQuery] = useState('');
	const [hits, setHits] = useState<MonitorSymbolHit[]>([]);
	// 侧栏监控清单（关注仓）：异动页是查询台，用户把个股加进来，
	// 系统持续算这些标的的 3/10/30 日偏离，折叠列表跟智能雷达同形态。
	const [watch, setWatch] = useState<MonitorWatchItem[]>([]);
	const [watchPending, setWatchPending] = useState('');
	const [watchNotice, setWatchNotice] = useState('');
	// 与智能雷达一致：一行一只，点行向下展开该股的异动明细，再点收起。
	const [expanded, setExpanded] = useState('');
	const [detail, setDetail] = useState<MonitorAbnormalCalc | null>(null);
	const [detailLoading, setDetailLoading] = useState(false);
	const [detailError, setDetailError] = useState('');
	const [detailRetry, setDetailRetry] = useState(0);
	// 移除监控后的乐观隐藏：点移除立即让行消失，避免「最后一个删不掉」的观感
	// （监控中的个股若同时是偏离候选，后端移除后仍在候选池，需本地先隐藏）。
	const [removed, setRemoved] = useState<Set<string>>(new Set());

	const load = useCallback(async (configValue: BackendConfig | null) => {
		if (!configValue) return;
		setLoading(true);
		setError('');
		try {
			const payload = await fetchMonitorAbnormal(configValue, 'deviation', 200);
			setView(payload.data);
		} catch (cause) {
			setError(String((cause as Error).message));
		} finally {
			setLoading(false);
		}
	}, []);

	// 监控清单跟预警结果同节奏刷新：加入/移除后侧栏与主列表同时跟上。
	const loadWatch = useCallback(async (configValue: BackendConfig | null) => {
		if (!configValue) return;
		try {
			const payload = await fetchMonitorWatchlist(configValue);
			setWatch(payload.data ?? []);
		} catch {
			setWatch([]);
		}
	}, []);

	useEffect(() => { void load(config); }, [config, load]);
	useEffect(() => { void loadWatch(config); }, [config, loadWatch, view?.generated_at]);

	// 展开行内容：点行才拉，且跟着 60 秒自刷一起更新，避免展开区停在旧数。
	useEffect(() => {
		if (!config || !expanded) {
			setDetail(null);
			setDetailLoading(false);
			setDetailError('');
			return;
		}
		const symbol = expanded;
		let alive = true;
		setDetailLoading(true);
		setDetailError('');
		fetchMonitorAbnormalCalc(config, symbol)
			.then((payload) => { if (alive) setDetail(payload.data); })
			.catch((cause) => {
				if (alive) { setDetail(null); setDetailError(String((cause as Error).message)); }
			})
			.finally(() => { if (alive) setDetailLoading(false); });
		return () => { alive = false; };
	}, [config, expanded, view?.generated_at, detailRetry]);

	// 预警页 60 秒自刷：偏离值是慢变量，但盘中触发状态值得自动跟进。
	useEffect(() => {
		const timer = setInterval(() => { void load(config); }, 60000);
		return () => clearInterval(timer);
	}, [config, load]);

	const loadCalc = useCallback(async (configValue: BackendConfig | null, symbol: string) => {
		if (!configValue || !symbol) return;
		setCalcLoading(true);
		setCalcError('');
		try {
			const payload = await fetchMonitorAbnormalCalc(configValue, symbol);
			setCalc(payload.data);
		} catch (cause) {
			setCalcError(String((cause as Error).message));
		} finally {
			setCalcLoading(false);
		}
	}, []);

	useEffect(() => { void loadCalc(config, calcSymbol); }, [config, calcSymbol, loadCalc, view?.generated_at]);

	// 搜索防抖：250ms 内只发一次建议请求。
	useEffect(() => {
		if (!config || query.trim().length < 1) {
			setHits([]);
			return;
		}
		let alive = true;
		const timer = setTimeout(() => {
			fetchMonitorStockSuggest(config, query.trim())
				.then((payload) => { if (alive) setHits((payload.data ?? []).slice(0, 6)); })
				.catch(() => { if (alive) setHits([]); });
		}, 250);
		return () => { alive = false; clearTimeout(timer); };
	}, [config, query]);

	const addWatch = async (target: { symbol: string; name?: string }) => {
		if (!config || watchPending) return;
		setWatchPending(target.symbol);
		setWatchNotice('');
		try {
			await addMonitorWatch(config, { symbol: target.symbol, name: target.name });
			await loadWatch(config);
			setWatchNotice(`已将 ${target.name || target.symbol} 加入监控，右侧列表持续跟踪`);
			setQuery('');
			setHits([]);
			// 若此前乐观移除过，重新加入时恢复显示。
			setRemoved((prev) => {
				if (!prev.has(target.symbol)) return prev;
				const next = new Set(prev);
				next.delete(target.symbol);
				return next;
			});
			void load(config);
		} catch (cause) {
			setWatchNotice(`加入失败：${String((cause as Error).message)}`);
		} finally {
			setWatchPending('');
		}
	};

	const removeWatch = async (symbol: string) => {
		if (!config || watchPending) return;
		setWatchPending(symbol);
		setWatchNotice('');
		try {
			await removeMonitorWatch(config, symbol);
			await loadWatch(config);
			// 乐观隐藏：立即让该行从列表消失，解决「最后一个删不掉」的观感问题。
			setRemoved((prev) => new Set(prev).add(symbol));
			if (expanded === symbol) setExpanded('');
			if (calcSymbol === symbol) {
				setCalcSymbol('');
				setCalc(null);
			}
			void load(config);
		} catch (cause) {
			setWatchNotice(`移除失败：${String((cause as Error).message)}`);
		} finally {
			setWatchPending('');
		}
	};

	const toggleRow = (symbol: string) => setExpanded((current) => (current === symbol ? '' : symbol));

	const items = view?.items ?? [];
	const watchSymbols = useMemo(() => new Set(watch.map((entry) => entry.symbol)), [watch]);
	// 展示列表 = 后端候选 + 监控个股，剔除已移除（乐观隐藏）的标的。
	const visibleItems = useMemo(
		() => items.filter((item) => !removed.has(item.symbol)),
		[items, removed],
	);
	const windowRisk = useMemo(() => ['3d', '10d', '30d'].map((key) => {
		const windows = visibleItems
			.map((item) => item.windows?.find((window) => window.key === key))
			.filter((window): window is MonitorAbnormalWindow => Boolean(window
				&& Number.isFinite(window.progress) && window.progress >= 0
				&& Number.isFinite(window.threshold) && window.threshold > 0
				&& (window.direction === 'up' || window.direction === 'down')));
		const atRisk = windows.filter((window) => window.progress >= 0.7);
		return {
			key, label: key.replace('d', ' 日'), samples: windows.length,
			near: atRisk.filter((window) => window.progress < 1).length,
			triggered: atRisk.filter((window) => window.progress >= 1).length,
			up: atRisk.filter((window) => window.direction === 'up').length,
			down: atRisk.filter((window) => window.direction === 'down').length,
		};
	}), [visibleItems]);
	// 严重异动榜：只收达到「严重异常波动」标准的个股
	// （10 日 ±100%/−50%、30 日 ±200%/−70%）。3 日窗口越线只是普通异常波动，
	// 不能叫严重——否则一只 3 日涨 13% 的持仓都会被挂到这张榜上。
	const severe = useMemo(() => {
		return visibleItems
			.filter((item) => item.severe)
			.map((item) => {
				const progress = Math.max(0, ...(item.windows ?? []).filter((w) => w.key !== '3d').map((w) => w.progress));
				return { item, progress };
			})
			.sort((a, b) => b.progress - a.progress)
			.slice(0, 12)
			.map((entry) => entry.item);
	}, [visibleItems]);
	const locateRow = (symbol: string) => {
		setLevelFilter('');
		toggleRow(symbol);
		window.requestAnimationFrame(() => document.querySelector(`.monitor-abn-row[data-symbol="${CSS.escape(symbol)}"]`)?.scrollIntoView({ block: 'nearest', inline: 'nearest' }));
	};
	const dailyDates = [...new Set(visibleItems.map((item) => item.daily_as_of).filter((date): date is string => Boolean(date)))].sort();
	const dailyAsOf = dailyDates.length === 0 ? '未提供' : dailyDates.length === 1 ? dailyDates[0] : `${dailyDates[0].slice(5)}～${dailyDates[dailyDates.length - 1].slice(5)}`;
	const historicalCount = visibleItems.filter((item) => item.historical).length;
	const historicalKnown = visibleItems.filter((item) => typeof item.historical === 'boolean').length;
	const filtered = visibleItems.filter((item) => {
		if (!levelFilter) return true;
		if (levelFilter === 'monitored') return Boolean(item.monitored);
		return item.level === levelFilter;
	});
	const counts = {
		triggered: items.filter((item) => item.level === 'triggered').length,
		near: items.filter((item) => item.level === 'near').length,
		monitored: items.filter((item) => item.monitored).length,
	};
	// 顶部三个胶囊就是列表能出现的三类标的：已触发、接近阈值、用户监控中。
	// 「观察」胶囊去掉了——观察级本身不进列表，留着只会永远显示 0。
	const levelPills = [
		{ key: 'triggered', label: '估算越线', cls: 'is-triggered', count: counts.triggered },
		{ key: 'near', label: '接近阈值', cls: 'is-near', count: counts.near },
		{ key: 'monitored', label: '监控中', cls: 'is-monitored', count: counts.monitored },
	];
	const selectedCalc = calc && calc.symbol === calcSymbol ? calc : null;
	const calcLevel = selectedCalc?.level ? abnormalLevelMeta[selectedCalc.level] : null;
	// 快照延迟：超过 3 分钟提示数据可能滞后（快照源间歇抖动时别误读为实时）。
	// -1 = 后端没有快照可比（降级模式），既不当作 0 分钟，也不标红。
	const snapshotAge = view?.snapshot_age_seconds ?? -1;
	const snapshotStale = snapshotAge > 180;

	return (
		<div className="monitor-radar-layout">
			<div className="monitor-radar-strip">
				<div>
					<strong>{loading ? '正在计算…' : '异动预警'}</strong>
					<small title="未接入交易所公告重置及官方同向异动次数；系统提醒次数不等于交易所异动次数">3/10/30 日滚动偏离估算 · 状态非交易所公告认定</small>
				</div>
				<div className="monitor-strip-space" />
				{view?.error && <span className="muted">{view.error}</span>}
				<div className="monitor-abn-searchbox">
					<Search size={13} aria-hidden="true" />
					<input
						className="monitor-abn-search"
						value={query}
						placeholder="搜索个股加入监控"
						onChange={(event) => setQuery(event.target.value)}
					/>
					{hits.length > 0 && (
						<div className="monitor-abn-hits">
							{hits.map((hit) => (
								<div className="monitor-abn-hit" key={hit.symbol}>
									<button
										type="button"
										className="monitor-abn-hit-main"
										onClick={() => void addWatch(hit)}
									>
										<strong>{hit.name}</strong>
										<small>{hit.symbol}</small>
									</button>
									{watchSymbols.has(hit.symbol) ? (
										<span className="monitor-abn-hit-flag" title="已在监控列表中">已监控</span>
									) : (
										<button
											type="button"
											className="monitor-abn-hit-add"
											title="加入监控"
											disabled={watchPending === hit.symbol}
											onClick={() => void addWatch(hit)}
										>
											{watchPending === hit.symbol ? <LoaderCircle className="spin" size={12} /> : <Plus size={12} />}
										</button>
									)}
								</div>
							))}
						</div>
					)}
				</div>
				<button type="button" className="monitor-small-button" disabled={loading} onClick={() => void load(config)}>
					{loading ? <LoaderCircle className="spin" size={13} /> : <RefreshCw size={13} />}
					{loading ? '刷新中' : '重新计算'}
				</button>
			</div>

			<div className="monitor-radar-body is-abnormal">
				<aside className="monitor-rail monitor-abnormal-rail" aria-label="异动概要">
					<div className="monitor-rail-card">
						<header>
							<Flame size={14} aria-hidden="true" />
							<strong>严重异动个股</strong>
							<small title="当前返回结果中的严重异动，最多展示 12 只">{severe.length === 12 ? '前 12 只' : `${severe.length} 只`}</small>
						</header>
						<div className="monitor-abn-rank">
							<div className="monitor-abn-list-head">
								<span>标的</span><span>偏离度</span><span>窗口</span>
							</div>
							{severe.length === 0 && <p className="monitor-rail-note">当前返回结果中，没有个股按所属板块的 10 日或 30 日阈值估算越线；不含官方同向异动次数认定。</p>}
							{severe.map((item, index) => {
								const worst = (item.windows ?? []).filter((w) => w.key !== '3d').sort((a, b) => b.progress - a.progress)[0];
								return (
									<button
										type="button"
										key={item.symbol}
										className="monitor-abn-rank-row"
										title="点击在主列表展开该股明细"
										onClick={() => locateRow(item.symbol)}
									>
										<span className="monitor-abn-rank-target">
											<span className="monitor-abn-rank-no">{index + 1}</span>
											<span className="monitor-abn-rank-name" title={item.name || '名称未提供'}>{item.name || '未提供'}</span>
										</span>
										<span className={`monitor-abn-rank-dev ${worst && worst.dev >= 0 ? 'up' : 'down'}`}>{abnormalSigned(worst?.dev ?? 0)}</span>
										<span className="monitor-abn-badge is-severe">{worst ? `${worst.key.replace('d', '日')}${worst.direction === 'up' ? '上行' : '下行'}` : '未提供'}</span>
									</button>
								);
							})}
						</div>
					</div>

					<div className="monitor-rail-card monitor-window-risk monitor-rail-interaction">
						<header>
							<Activity size={14} aria-hidden="true" />
							<strong>窗口风险概览</strong>
							<small>{view ? `本页 ${visibleItems.length} 只` : '未提供'}</small>
						</header>
						<div className="monitor-window-risk-grid" aria-label="各偏离窗口风险数量">
							<div className="monitor-window-risk-head"><span>观察周期</span><span>临近</span><span>越线</span></div>
							{windowRisk.map((risk) => <div key={risk.key} className="monitor-window-risk-item">
								<div className="monitor-window-risk-values">
									<strong>{risk.label}</strong>
									<b className={risk.near > 0 ? 'is-near' : ''} aria-label={`临近 ${risk.samples > 0 ? risk.near : '未提供'}`}>{risk.samples > 0 ? risk.near : '—'}</b>
									<b className={risk.triggered > 0 ? 'is-triggered' : ''} aria-label={`越线 ${risk.samples > 0 ? risk.triggered : '未提供'}`}>{risk.samples > 0 ? risk.triggered : '—'}</b>
								</div>
								<div className="monitor-window-risk-meta">
									<span>{risk.samples > 0 ? `上行 ${risk.up} · 下行 ${risk.down}` : '方向未提供'}</span>
									<small>{risk.samples > 0 ? `有效样本 ${risk.samples} / ${visibleItems.length}` : '样本未就绪'}</small>
								</div>
							</div>)}
						</div>
						<p className="monitor-rail-note">临近为阈值的 70%～不足 100%，越线为 ≥100%；方向统计以上两类为准，同票可计入多个窗口。</p>
						{view && view.total > visibleItems.length && <p className="monitor-rail-note">仅统计本页返回结果，非全市场数量。</p>}
					</div>

					<div className="monitor-rail-card monitor-data-status monitor-rail-interaction">
						<header>
							<Gauge size={14} aria-hidden="true" />
							<strong>数据状态</strong>
							<small title={liveStatusLabel(streamStatus)}>{quoteConnectionLabel(streamStatus)}</small>
						</header>
						<div className="monitor-rail-rows">
							<div><span>交易日期</span><b>{view?.trade_date || '—'}</b></div>
							<div><span>行情更新时间</span><b>{formatDataTime(view?.snapshot_at)}</b></div>
							<div><span>行情来源</span><b title={view?.snapshot_source}>{view?.snapshot_source || '未提供'}</b></div>
							<div><span>本次计算</span><b>{formatDataTime(view?.generated_at)}</b></div>
							<div><span>日线截至</span><b>{dailyAsOf}</b></div>
							<div><span>历史收盘计算</span><b>{historicalKnown === 0 ? '未提供' : `${historicalCount} / ${historicalKnown} 只`}</b></div>
							<div>
								<span>计算时快照延迟</span>
								<b className={snapshotStale ? 'down' : ''}>
									{view && snapshotAge >= 0 ? snapshotAge < 60 ? `${Math.round(snapshotAge)} 秒` : `${Math.round(snapshotAge / 60)} 分钟` : '未提供'}
								</b>
							</div>
						</div>
						{view?.error && <p className="monitor-rail-note monitor-data-warning">{view.error}</p>}
						{view && view.total > visibleItems.length && <p className="monitor-rail-note">当前返回 {visibleItems.length} / {view.total} 只，左侧统计以返回结果为准。</p>}
					</div>
				</aside>

				<section className="monitor-panel monitor-abnormal-panel" aria-label="异动预警">
					<header>
						<BellRing size={17} />
						<div>
							<strong>{calcSymbol ? '异动计算器' : '异动预警列表'}</strong>
							<small>
								{calcSymbol
									? selectedCalc?.benchmark_label ? `${selectedCalc.name || selectedCalc.symbol} · 基准 ${selectedCalc.benchmark_label}` : '计算中…'
									: `${filtered.length} 只 · 估算越线 ${counts.triggered} / 接近阈值 ${counts.near} · 点行查看逐窗口计算过程`}
							</small>
						</div>
						{view?.generated_at && <small className="monitor-panel-stamp">生成于 {formatTime(view.generated_at)}</small>}
					</header>

					{/* 结论筛选：分段控件，选中项背后是一块会滑过去的高亮。
					    原先这三个是散在顶部条里的胶囊，和「重新计算 / 搜索」挤在一行；
					    移到列表自己的表头下方，读的顺序变成「标题 → 筛选 → 表头 → 数据」。
					    高亮块用 data-active 定位（-1 = 未筛选时停在槽外/隐藏），
					    transform 位移在 CSS 里做，切换时是滑过去的而不是跳过去的。 */}
					<div className="monitor-abn-filterbar">
						<div className="monitor-segmented" role="group" aria-label="按状态筛选"
							data-active={levelPills.findIndex((pill) => pill.key === levelFilter)}>
							<span className="monitor-segmented-thumb" aria-hidden="true" />
							{levelPills.map((pill) => (
								<button
									type="button"
									key={pill.key}
									className={`monitor-segmented-item ${pill.cls}${levelFilter === pill.key ? ' active' : ''}`}
									aria-pressed={levelFilter === pill.key}
									onClick={() => setLevelFilter(levelFilter === pill.key ? '' : pill.key)}
									{...(pill.count === 0 ? { disabled: true, title: '当前没有标的处于这个状态' } : {})}
								>
									{pill.label}
									<small>{pill.count}</small>
								</button>
							))}
						</div>
					</div>

					{calcSymbol && (calcLoading || calcError || selectedCalc) && (
						<div className="monitor-abn-calc">
							{calcLoading && <p className="monitor-abn-state"><LoaderCircle className="spin" size={14} /> 正在计算…</p>}
							{calcError && <p className="monitor-abn-state is-error">{calcError}</p>}
							{selectedCalc && !calcLoading && (
									<>
										{selectedCalc.error && <p className="monitor-abn-state">{selectedCalc.error}</p>}
										<div className="monitor-abn-status">
											{calcLevel && <span className={`monitor-abn-badge ${calcLevel.cls}`}>{calcLevel.label}</span>}
											<strong>{selectedCalc.name || selectedCalc.symbol}</strong>
											{selectedCalc.name && selectedCalc.name !== selectedCalc.symbol && <span>{selectedCalc.symbol}</span>}
											{selectedCalc.is_st && <em className="monitor-abn-st">ST</em>}
											<LivePriceCell as="b" symbol={selectedCalc.symbol} kind="change" fallback={selectedCalc.change_percent} />
											<small>现价 <LivePriceText symbol={selectedCalc.symbol} kind="price" fallback={selectedCalc.price} /> · 基准 {selectedCalc.benchmark_label || '—'}</small>
										</div>
										<div className="monitor-abn-tiles">
											{abnormalWindowKeys.map((key) => (
												<AbnormalWindowTile key={key} windowKey={key} window={selectedCalc.windows?.find((window) => window.key === key)} />
											))}
										</div>
										{(selectedCalc.rows ?? []).length > 0 && (
											<div className="monitor-abn-rows-wrap">
												<table className="trading-table monitor-abn-rows">
													<thead>
														<tr><th>交易日</th><th>个股</th><th>指数</th><th>3日</th><th>10日</th><th>30日</th></tr>
													</thead>
													<tbody>
														{[...(selectedCalc.rows ?? [])].reverse().map((row) => (
															<tr key={row.date}>
																<td>{row.date.slice(5)}</td>
																<td className={row.stock_change >= 0 ? 'up' : 'down'}>{abnormalSigned(row.stock_change)}</td>
																<td className={row.index_change !== null && row.index_change !== undefined ? (row.index_change >= 0 ? 'up' : 'down') : ''}>{abnormalSigned(row.index_change)}</td>
																<td>{abnormalSigned(row.dev_3d)}</td>
																<td>{abnormalSigned(row.dev_10d)}</td>
																<td>{abnormalSigned(row.dev_30d)}</td>
															</tr>
														))}
													</tbody>
												</table>
											</div>
										)}
									</>
							)}
						</div>
					)}

					{error && <div className="monitor-empty-state"><strong>计算失败</strong><span>{error}</span></div>}
					{!error && !loading && filtered.length === 0 && (
						<div className="monitor-empty-state">
							<BellRing size={26} aria-hidden="true" />
							<strong>{items.length > 0 ? '当前筛选下没有标的' : '没有个股走到预警区间'}</strong>
							<span>
								{items.length > 0
									? '换个状态筛选再试：估算越线 / 接近阈值（阈值的 70%）/ 你自己加入的监控。'
									: '列表展示估算越线和接近阈值（阈值的 70%）的个股；加入监控的个股持续保留。估算状态不等于交易所公告认定。'}
							</span>
						</div>
					)}

					{filtered.length > 0 && (
						<div className="trading-table-wrap">
																<table className="trading-table trading-table-fixed monitor-abnormal-table">
										{/* 序号与标的合成一列：序号退到名称左侧做纯索引，不再单占一列。
										    其余十列均分，表头与内容同轴；窄窗口下整表按 min-width 横向滚动。 */}
										<colgroup>
											<col />
											<col />
											<col />
											<col />
											<col />
											<col />
											<col />
											<col />
											<col />
											<col />
											<col />
										</colgroup>
										<thead>
											<tr>
												<th>标的</th><th>现价</th><th>今日</th><th>3日</th><th>10日</th><th>30日</th>
												<th>接近度</th><th title="距偏离阈值的差额，不是现价涨跌幅">距阈值%</th><th>触发价</th><th>状态</th><th>操作</th>
											</tr>
										</thead>
										<tbody>
											{filtered.map((item, index) => {
												const meta = abnormalLevelMeta[item.level];
												const open = expanded === item.symbol;
												const detailData = open && detail && detail.symbol === item.symbol ? detail : null;
												return (
													<Fragment key={item.symbol}>
<tr
														className={`trading-position-row monitor-abn-row${open ? ' expanded' : ''}`}
												data-symbol={item.symbol} data-name={item.name || ""}
												data-tt-row-id={item.symbol}
														title={open ? '点击收起该股的异动明细' : '点击展开该股的异动明细'}
														onClick={() => toggleRow(item.symbol)}
													>
<td className="trading-symbol-cell">
														<span className="trading-symbol-stack">
															<span className="trading-symbol-seq">{index + 1}</span>
															<span className="trading-symbol-body">
																<span className="trading-symbol-line">
																	<span className="trading-symbol-name" title={item.name || item.symbol}>{item.name || item.symbol}</span>
																</span>
															{/* 「监控中」是状态，不是标的的一部分：它待在标的列会把代码挤偏，
															    也和末尾的「状态」列重复。已移到状态列，这里只留代码。 */}
															<span className="trading-symbol-code">{item.symbol}</span>
															</span>
														</span>
													</td>
															<LivePriceCell symbol={item.symbol} kind="price" fallback={item.price} />
															<LivePriceCell symbol={item.symbol} kind="change" fallback={item.change_percent} />
															<td>{abnormalSigned(abnormalItemDeviation(item, '3d'))}</td>
															<td>{abnormalSigned(abnormalItemDeviation(item, '10d'))}</td>
															<td>{abnormalSigned(abnormalItemDeviation(item, '30d'))}</td>
															<td><AbnormalMeter progress={item.windows?.find((w) => w.key === item.worst_key)?.progress} /></td>
															<td><AbnormalRemaining item={item} /></td>
															<td>{item.remaining > 0 && item.trigger_price > 0 ? item.trigger_price.toFixed(2) : '—'}</td>
															<td>
																<div className="monitor-abn-status">
																	{item.monitored && (
																		<span className="monitor-abn-monitored" title="已加入监控：留在列表里持续跟踪">监控中</span>
																	)}
																	{meta && (
																		<span
																			className={`monitor-abn-badge ${meta.cls}`}
																			title={`${meta.hint}${item.events_10d > 0 ? `；近 10 个交易日系统提醒 ${item.events_10d} 次，非交易所异动次数` : ''}`}
																		>
																			{meta.label}
																		</span>
																	)}
																	{item.events_10d > 0 && <small title="近 10 个交易日系统内部提醒次数，非交易所异动次数">提醒{item.events_10d}次</small>}
																</div>
															</td>
															<td>
																{/* 操作按钮不吃整行点击：点按钮是要做事，不是要展开。 */}
																<div className="monitor-abn-ops" onClick={(event) => event.stopPropagation()}>
																	{item.monitored ? (
																		<button type="button" className="monitor-abn-op is-remove" title="移除监控"
																			disabled={watchPending === item.symbol}
																			onClick={() => void removeWatch(item.symbol)}>
																			{watchPending === item.symbol ? <LoaderCircle className="spin" size={13} /> : <Trash2 size={13} />}
																		</button>
																	) : (
																		<button type="button" className="monitor-abn-op" title="加入监控：留在列表里持续跟踪"
																			disabled={watchPending === item.symbol}
																			onClick={() => void addWatch({ symbol: item.symbol, name: item.name })}>
																			{watchPending === item.symbol ? <LoaderCircle className="spin" size={13} /> : <Plus size={13} />}
																		</button>
																	)}
																</div>
															</td>
														</tr>
														{open && (
															<tr className="monitor-abn-detail-row" data-tt-follow={item.symbol}>
																<td colSpan={11}>
																	<div className="monitor-abn-detail">
																		<div className="monitor-abn-detail-top">
																			<div className="monitor-abn-tags">
																				<span>{monitorAbnormalBoardLabels[item.board] ?? (item.board || '—')}</span>
																				{item.is_st && <span>ST</span>}
																				{item.monitored && <span>监控中</span>}
																				<span>
																					{(() => {
																						// 展开明细自带触发日期（服务端从逐日表反查），
																						// 行数据只在监控个股上带这个字段。
																						const date = detailData?.last_trigger_date || item.last_trigger_date;
																						return date ? `最近估算越线 ${date}` : detailData?.rows?.length ? '可用样本未见越线' : '触发日期未提供';
																					})()}
																				</span>
																				<span>基准 {detailData?.benchmark_label || '—'}</span>
																			</div>
																			<small className="monitor-abn-detail-hint">
																				偏离值 = 个股区间涨跌幅 − 基准指数同期涨跌幅；下表是最近 12 个交易日的逐日明细
																			</small>
																		</div>
																		{detailLoading && !detailData && (
																			<p className="monitor-abn-state"><LoaderCircle className="spin" size={14} /> 正在计算该股异动明细…</p>
																		)}
																		{!detailLoading && !detailData && (
																			<p className="monitor-abn-state is-error">
																				{detailError || '暂无可用明细'}
																				<button type="button" className="monitor-link-button" onClick={() => setDetailRetry((value) => value + 1)}>重新加载</button>
																			</p>
																		)}
																		{detailData?.error && <p className="monitor-abn-state">{detailData.error}</p>}
																		{detailData && (
																			<>
																				<div className="monitor-abn-tiles">
																					{abnormalWindowKeys.map((key) => (
																						<AbnormalWindowTile key={key} windowKey={key} window={detailData.windows?.find((window) => window.key === key)} />
																					))}
																				</div>
																				{(detailData.rows ?? []).length > 0 && (
																					<div className="monitor-abn-rows-wrap">
																						<table className="trading-table monitor-abn-rows">
																							<thead>
																								<tr><th>交易日</th><th>个股</th><th>指数</th><th>3日</th><th>10日</th><th>30日</th></tr>
																							</thead>
																							<tbody>
																								{[...(detailData.rows ?? [])].reverse().slice(0, 12).map((row) => (
																									<tr key={row.date}>
																										<td>{row.date.slice(5)}</td>
																										<td className={row.stock_change >= 0 ? 'up' : 'down'}>{abnormalSigned(row.stock_change)}</td>
																										<td className={row.index_change !== null && row.index_change !== undefined ? (row.index_change >= 0 ? 'up' : 'down') : ''}>{abnormalSigned(row.index_change)}</td>
																										<td>{abnormalSigned(row.dev_3d)}</td>
																										<td>{abnormalSigned(row.dev_10d)}</td>
																										<td>{abnormalSigned(row.dev_30d)}</td>
																									</tr>
																								))}
																							</tbody>
																						</table>
																					</div>
																				)}
																			</>
																		)}
																	</div>
																</td>
															</tr>
														)}
													</Fragment>
												);
											})}
										</tbody>
									</table>
						</div>
					)}
				</section>
			</div>
		</div>
	);
}

// ---------------------------------------------------------------- Tab 1 智能雷达

// 雷达页：系统主动扫描持仓（巡检+AI 并集）、关注仓与规则指定标的，给出「机会/风险/观察」分级结论。
// 与规则页的分工：规则是"你写条件它盯"，雷达是"它扫完告诉你谁值得盯"。
function RadarTab({ config, onOpenMarketIndexes }: { config: BackendConfig | null; onOpenMarketIndexes: () => void }) {
	const streamStatus = useLiveStreamStatus();
	const [report, setReport] = useState<RadarReport | null>(null);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState('');
	const [kindFilter, setKindFilter] = useState('');
	const [creating, setCreating] = useState('');
	const [notice, setNotice] = useState('');
	const scanning = useRef(false);

	const scan = useCallback(async (configValue: BackendConfig | null, force = false) => {
		if (!configValue || scanning.current) return;
		scanning.current = true;
		setLoading(true);
		setError('');
		try {
			const payload = await fetchMonitorRadar(configValue, force);
			setReport(payload.data);
			setError(payload.data.error || '');
		} catch (cause) {
			setError(String((cause as Error).message));
		} finally {
			scanning.current = false;
			setLoading(false);
		}
	}, []);

	useEffect(() => {
		void scan(config);
		const timer = window.setInterval(() => { void scan(config); }, 60000);
		return () => window.clearInterval(timer);
	}, [config, scan]);

	// 一键转盯盘规则：直接把后端预填的规则结构走既有创建接口。
	// 后端保证每条结论都带预填（无事件级信号时用 60 日位置/量比/涨跌幅补），
	// 所以操作列不会再出现"有的行有按钮、有的行没有"。
	const convert = async (finding: RadarFinding) => {
		if (!config || creating) return;
		if (!finding.suggested_rule) {
			setNotice(`${finding.name || finding.symbol} 这条结论暂时没有可预填的规则，请在「触发与规则」页手动新建`);
			return;
		}
		setCreating(finding.symbol);
		setNotice('');
		try {
			await saveMonitorRule(config, finding.suggested_rule);
			setNotice(`已为 ${finding.name || finding.symbol} 创建盯盘规则，可在「触发与规则」页调整阈值`);
		} catch (cause) {
			setNotice(`创建失败：${String((cause as Error).message)}`);
		} finally {
			setCreating('');
		}
	};

	const findings = (report?.findings ?? []).filter((item) => !kindFilter || item.kind === kindFilter);
	const counts = report?.total_counts ?? report?.counts ?? {};
	const totalFindings = report?.total_findings ?? (report?.findings ?? []).length;
	const evaluatedCount = report?.targets?.filter((target) => target.status === 'evaluated').length;
	const pendingCount = report?.targets?.filter((target) => target.status === 'pending').length;
	// 与 持仓清单一致：一行一只标的，点行展开判断依据，收起时只留结论与分数。
	const [expanded, setExpanded] = useState<string | null>(null);
	const toggleRow = (symbol: string) => setExpanded((current) => current === symbol ? null : symbol);
	// 风平浪静且池子里有异动：空态改用「今天谁动了」填充，不再白板。
	const quiet = !error && !loading && !kindFilter && report !== null && report.scanned > 0
		&& (report.findings ?? []).length === 0 && (report.movers?.length ?? 0) > 0;
	const market = report?.market;
	// 情绪评级在前端按涨跌家数比推导，后端只给事实。
	const marketMood = (() => {
		if (!market) return null;
		const total = market.up_count + market.down_count;
		if (total === 0 && !market.have_indexes) return null;
		const ratio = total > 0 ? market.up_count / total : 0.5;
		return {
			key: ratio >= 0.6 ? 'warm' : ratio <= 0.4 ? 'cool' : 'neutral',
			label: ratio >= 0.6 ? '偏暖' : ratio <= 0.4 ? '偏弱' : '中性',
		};
	})();
	return (
		<div className="monitor-radar-layout">
			<div className="monitor-radar-strip">
				<div>
					<strong>{loading ? '正在扫描…' : '智能雷达'}</strong>
					<small>{report ? `扫描 ${report.scanned} 只：机会 ${counts.opportunity ?? 0}、风险 ${counts.risk ?? 0}、观察 ${counts.watch ?? 0}${report.total_counts ? '' : '（当前返回结论）'}` : '扫描持仓与关注仓，筛出当下值得看的标的'}</small>
				</div>
				<div className="monitor-strip-space" />
				{notice && <span className="muted">{notice}</span>}
				<MonitorSelect
					value={kindFilter}
					onChange={setKindFilter}
					ariaLabel="按结论筛选"
					block={false}
					options={[
						{ value: '', label: '全部结论' },
						{ value: 'opportunity', label: `机会（${counts.opportunity ?? 0}）` },
						{ value: 'risk', label: `风险（${counts.risk ?? 0}）` },
						{ value: 'watch', label: `观察（${counts.watch ?? 0}）` },
					]}
				/>
				<button type="button" className="monitor-small-button" disabled={loading} onClick={() => void scan(config, true)}>
					{loading ? <LoaderCircle className="spin" size={13} /> : <RefreshCw size={13} />}
					{loading ? '扫描中' : '重新扫描'}
				</button>
			</div>

			<div className="monitor-radar-body">
				{/* 左栏概要：环境 / 完整结论分布 / 本次扫描，右栏保留结论表。
					排版对齐总控台侧栏：图标 + 标题一行，指标走「标签左、数值右」。 */}
				<aside className="monitor-rail" aria-label="雷达概要">
					<a className="monitor-rail-card monitor-rail-interaction monitor-market-card"
						href="#market/core-indexes" aria-label="今日市场：查看行情总览的市场核心指数" title="点击查看行情总览 · 市场核心指数"
						onClick={(event) => {
							if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
							event.preventDefault();
							onOpenMarketIndexes();
						}}>
						<header>
							<Activity size={14} aria-hidden="true" />
							<strong>今日市场</strong>
							{marketMood && <em className={`monitor-radar-mood ${marketMood.key}`}>{marketMood.label}</em>}
						</header>
						<div className="monitor-rail-rows">
							{/* 四大宽基并列：沪 / 深 / 创业板 / 科创板，行间用细分割线隔开。 */}
							{(market?.indexes?.length ?? 0) > 0 ? (
								<div className="monitor-rail-indexes">
									{(market?.indexes ?? []).map((item) => (
										<div key={item.id}>
											<span>{item.name}</span>
											<b className={item.change_pct >= 0 ? 'up' : 'down'}>
												{item.change_pct >= 0 ? '+' : ''}{item.change_pct.toFixed(2)}%
											</b>
										</div>
									))}
								</div>
							) : market?.have_indexes && (
								<div>
									<span>上证指数</span>
									<b className={market.index_change_pct >= 0 ? 'up' : 'down'}>
										{market.index_change_pct >= 0 ? '+' : ''}{market.index_change_pct.toFixed(2)}%
									</b>
								</div>
							)}
							{(market?.up_count ?? 0) + (market?.down_count ?? 0) > 0 ? (
								<>
									<div><span>上涨 / 下跌</span><b><em className="up">{market?.up_count ?? 0}</em> / <em className="down">{market?.down_count ?? 0}</em></b></div>
									<div><span>涨停家数</span><b>{market?.limit_up_count ?? 0}</b></div>
									{(market?.total_amount ?? 0) > 0 && (
										<div>
											<span>两市成交</span>
											<b>{(market?.total_amount ?? 0) >= 10000 ? `${((market?.total_amount ?? 0) / 10000).toFixed(2)} 万亿` : `${(market?.total_amount ?? 0).toFixed(0)} 亿`}</b>
										</div>
									)}
								</>
							) : (
								// 快照没就绪时宁可不给数字，避免"上涨 0 / 下跌 0"被误读成普跌。
								<p className="monitor-rail-note">宽度与成交额未就绪，盘中快照到位后自动补全</p>
							)}
						</div>
					</a>

					<div className="monitor-rail-card">
						<header>
							<PieChart size={14} aria-hidden="true" />
							<strong>结论分布</strong>
							<small>{report ? (report.total_counts ? '全池结论' : '当前返回结论') : '未提供'}</small>
						</header>
						<div className="monitor-rail-kinds">
							{(['opportunity', 'risk', 'watch'] as const).map((kind) => {
								const list = (report?.findings ?? []).filter((item) => item.kind === kind);
								const best = kind === 'risk'
									? list.reduce<RadarFinding | null>((acc, item) => (!acc || item.score < acc.score ? item : acc), null)
									: list.reduce<RadarFinding | null>((acc, item) => (!acc || item.score > acc.score ? item : acc), null);
								const active = kindFilter === kind;
								return (
									<button type="button" key={kind} className={`monitor-rail-kind kind-${kind}${active ? ' active' : ''}`}
										title={active ? '点击取消筛选' : `只看${radarKindLabels[kind]}`}
										onClick={() => setKindFilter(active ? '' : kind)}>
										<span>{radarKindLabels[kind]}</span>
										<strong>{report ? counts[kind] ?? 0 : '—'}</strong>
										<small>{best ? `${kind === 'risk' ? '最弱' : '最强'} ${best.name || best.symbol} ${best.score > 0 ? '+' : ''}${best.score.toFixed(1)}` : '暂无'}</small>
									</button>
								);
							})}
						</div>
						{report && totalFindings > (report.findings ?? []).length && <p className="monitor-rail-note">共 {totalFindings} 条结论，主表展示强度靠前的 {report.findings.length} 条；筛选仅作用于已展示结论。</p>}
					</div>

					<div className="monitor-rail-card monitor-rail-interaction monitor-scan-status">
						<header>
							<Crosshair size={14} aria-hidden="true" />
							<strong>本次扫描</strong>
							<small title={liveStatusLabel(streamStatus)}>{quoteConnectionLabel(streamStatus)}</small>
						</header>
						<div className="monitor-rail-rows">
							<div><span>持仓来源</span><b>{report?.source_counts ? `${report.source_counts['持仓'] ?? 0} 只` : '未提供'}</b></div>
							<div><span>关注仓来源</span><b>{report?.source_counts ? `${report.source_counts['关注仓'] ?? 0} 只` : '未提供'}</b></div>
							<div><span>规则标的来源</span><b>{report?.source_counts ? `${report.source_counts['规则标的'] ?? 0} 只` : '未提供'}</b></div>
							<div><span>去重后入池</span><b>{report ? `${report.scanned} 只` : '未提供'}</b></div>
							<div><span>已完成评估</span><b>{evaluatedCount === undefined ? '未提供' : `${evaluatedCount} 只`}</b></div>
							<div><span>数据不足跳过</span><b>{report ? `${report.skipped} 只` : '未提供'}</b></div>
							{Boolean(pendingCount) && <div><span>尚未完成</span><b>{pendingCount} 只</b></div>}
							<div><span>结论生成时间</span><b>{formatDataTime(report?.generated_at)}</b></div>
						</div>
						<p className="monitor-rail-note">来源按持仓、关注仓、规则标的依次归类，同一标的只计一次。</p>
						{(error || liveStatusLabel(streamStatus)) && <p className="monitor-rail-note monitor-data-warning">{error || liveStatusLabel(streamStatus)}</p>}
					</div>
				</aside>

				<section className="monitor-panel monitor-radar-panel" aria-label="雷达结论">
					{/* 面板头与 持仓清单同构：图标 + 标题 + 一行说明，
						表格本体缩进在面板里，整块有边框和阴影，不再是一张裸表。 */}
					<header>
						<Radar size={17} />
						<div>
							<strong>雷达结论</strong>
							{/* 数量口径写全：先看「扫了多少只」，再看「出了几条结论」。
							    只写「15 条结论」的话，用户会拿它跟关注仓只数对不上而以为漏了数据。 */}
							<small>{report?.scanned
								? `扫描 ${report.scanned} 只 · 共 ${totalFindings} 条结论 · 当前展示 ${findings.length} 条${kindFilter ? '（已筛选）' : ''} · 点行展开判断依据`
								: '扫描持仓与关注仓，筛出当下值得看的标的'}</small>
						</div>
						{report?.generated_at && <small className="monitor-panel-stamp">生成于 {formatTime(report.generated_at)}</small>}
					</header>

		{quiet && (
			<div className="monitor-empty-state">
				<Radar size={26} aria-hidden="true" />
				<strong>眼下风平浪静</strong>
				<span>{`已扫描 ${report?.scanned} 只标的，没有值得建规则的信号；但池子里今天这些标的有异动：`}</span>
				<div className="monitor-radar-movers">
					{(report?.movers ?? []).map((mover) => (
						<span key={mover.symbol} className="monitor-radar-mover">
							<strong>{mover.name || mover.symbol}</strong>
							<LivePriceCell as="em" symbol={mover.symbol} kind="change" fallback={mover.change_percent} />
							{mover.volume_ratio >= 1.5 ? <small>放量</small> : mover.volume_ratio > 0 && mover.volume_ratio <= 0.6 ? <small>缩量</small> : null}
						</span>
					))}
				</div>
			</div>
		)}

		{error && <div className="monitor-empty-state"><strong>扫描失败</strong><span>{error}</span></div>}
		{!error && !loading && !quiet && findings.length === 0 && (
				<div className="monitor-empty-state">
					<Radar size={26} aria-hidden="true" />
					<strong>{kindFilter ? '当前展示列表中没有该类结论' : report?.scanned ? '眼下风平浪静' : '没有可扫描的标的'}</strong>
					<span>{kindFilter
						? `全池${radarKindLabels[kindFilter as RadarFinding['kind']]}结论 ${counts[kindFilter] ?? 0} 条；主表只展示强度靠前的结论。`
						: report?.scanned
						? `已扫描 ${report.scanned} 只标的，没有任何技术信号、形态或位置异动。盘中随时可以再扫。`
						: '雷达扫描的是持仓与关注仓；先在对应页面添加标的，或创建监控规则。'}</span>
				</div>
			)}

			{findings.length > 0 && (
				/* 列表形态与 持仓清单一致：真表格 + 吸顶表头，
				   名称不加框、代码退到第二行；点行展开判断依据（蓝色软底行）。
				   「来自」把以前塞在名称后面的持仓小标签独立成一列——标的从哪个池子
				   进来的，是读这份结论时首先要判断的事。 */
				<div className="trading-table-wrap">
					<table className="trading-table trading-table-fixed monitor-radar-table">
						{/* 序号已并入标的列，其余七列均分；
						    净分里是走势 + 净分条 + 分数，flex 会自己压缩，不会被挤断。 */}
						<colgroup>
							<col />
							<col />
							<col />
							<col />
							<col />
							<col />
							<col />
						</colgroup>
						<thead>
							<tr><th>标的</th><th>来自</th><th>结论</th><th>现价</th><th>今日涨跌</th><th>净分</th><th>操作</th></tr>
						</thead>
						<tbody>
							{findings.map((finding, index) => {
								const open = expanded === finding.symbol;
								const source = finding.source || (finding.is_position ? '持仓' : '关注仓');
								const sourceClass = source === '持仓' ? 'is-position' : source === '关注仓' ? 'is-watch' : 'is-rule';
								return (
									<Fragment key={finding.symbol}>
										<tr
											className={`trading-position-row monitor-radar-row kind-${finding.kind}${open ? ' expanded' : ''}`}
											data-symbol={finding.symbol} data-name={finding.name || ""}
											data-tt-row-id={finding.symbol}
											onClick={() => toggleRow(finding.symbol)}
											title={open ? '点击收起判断依据' : '点击查看判断依据'}
										>
											<td className="trading-symbol-cell">
												<span className="trading-symbol-stack">
													<span className="trading-symbol-seq">{index + 1}</span>
													<span className="trading-symbol-body">
														<span className="trading-symbol-line">
															<span className="trading-symbol-name">{finding.name || finding.symbol}</span>
														</span>
														<span className="trading-symbol-code">{finding.symbol}</span>
													</span>
												</span>
											</td>
											<td><span className={`monitor-radar-source ${sourceClass}`}>{source}</span></td>
											<td><span className={`monitor-radar-badge kind-${finding.kind}`}>{radarKindLabels[finding.kind]}</span></td>
											<LivePriceCell symbol={finding.symbol} kind="price" fallback={finding.price} />
											<LivePriceCell symbol={finding.symbol} kind="change" fallback={finding.change_percent} />
											<td>
												<span className="monitor-radar-scorecell">
													<RadarSpark closes={finding.closes || []} changePercent={finding.change_percent} />
													<RadarScoreBar score={finding.score} />
													<span className={`monitor-radar-score ${finding.score > 0 ? 'up' : finding.score < 0 ? 'down' : ''}`}>
														{finding.score > 0 ? '+' : ''}{finding.score.toFixed(1)}
													</span>
												</span>
											</td>
											<td>
												{/* 操作按钮不吃整行点击：点按钮是要做事，不是要展开。 */}
												<div className="trading-row-actions" onClick={(event) => event.stopPropagation()}>
												<button type="button" className="trading-row-action is-icon" disabled={Boolean(creating)}
													title={finding.suggested_rule ? '用这条结论预填一条盯盘规则' : '这条结论没有可预填的规则'}
													aria-label="转为规则"
													onClick={() => void convert(finding)}>
													{creating === finding.symbol ? <LoaderCircle size={13} className="spin" /> : <ListPlus size={13} />}
												</button>
												<button type="button" className="trading-row-action is-icon" aria-expanded={open}
													title={open ? '收起判断依据' : '查看判断依据'}
													aria-label={open ? '收起判断依据' : '查看判断依据'}
													onClick={() => toggleRow(finding.symbol)}>
													{open ? <ChevronUp size={13} /> : <ChevronDown size={13} />}
												</button>
												</div>
											</td>
										</tr>
										{open && (
											<tr className="monitor-radar-detail-row" data-tt-follow={finding.symbol}>
												{/* 跨满整行：表是七列（标的/来自/结论/现价/今日涨跌/净分/操作），
												    少写几列会让展开面板右边空出一截。 */}
												<td colSpan={7}>
													<div className="monitor-radar-detail">
														{/* 顶部一行：标签 + 一句"这段展开是什么"，避免大段文字直接砸下来。 */}
<div className="monitor-radar-detail-top">
														<div className="monitor-radar-tags">
															{finding.tags.map((tag) => <span key={tag}>{tag}</span>)}
															{finding.tags.length === 0 && <span className="muted">无标签</span>}
														</div>
														<small className="monitor-radar-disclaimer">
															此为系统自动分析结果，不构成投资建议
														</small>
													</div>
														<div className="monitor-radar-detail-grid">
															{/* 判断依据：编号 + 前缀加粗，把"为什么"读成清单而不是一段散文。 */}
															<section className="monitor-radar-card">
																<header>
																	<span>判断依据</span>
																	<em>{finding.reasons.length} 条</em>
																</header>
																<ol className="monitor-radar-reasons">
																	{finding.reasons.map((reason, index) => {
																		const cut = reason.indexOf('：');
																		const head = cut > 0 ? reason.slice(0, cut) : '';
																		const body = cut > 0 ? reason.slice(cut + 1) : reason;
																		return (
																			<li key={reason}>
																				<i aria-hidden="true">{index + 1}</i>
																				<p>{head ? <><b>{head}</b>{body}</> : body}</p>
																			</li>
																		);
																	})}
																	{finding.reasons.length === 0 && <li><p className="muted">无额外理由</p></li>}
																</ol>
															</section>
															{/* 技术面事实：2×2 指标瓦片，60 日位置带一条比例条，数字比句子好读。 */}
															<section className="monitor-radar-card">
																<header>
																	<span>技术面事实</span>
																	<em>日线口径</em>
																</header>
																<div className="monitor-radar-facts">
																	<div className="monitor-radar-fact">
																		<span>均线状态</span>
																		<strong>{finding.ma_state || '—'}</strong>
																	</div>
																	<div className="monitor-radar-fact">
																		<span>量比（5 日）</span>
																		<strong>{finding.vol_ratio_5 > 0 ? finding.vol_ratio_5.toFixed(2) : '—'}</strong>
																	</div>
																	<div className="monitor-radar-fact is-bar">
																		<span>60 日位置</span>
																		<strong>{finding.have_position_60d ? `${finding.position_60d.toFixed(0)}%` : '—'}</strong>
																		<i aria-hidden="true">
																			<b style={{ width: `${finding.have_position_60d ? Math.min(100, Math.max(0, finding.position_60d)) : 0}%` }} />
																		</i>
																	</div>
																	<div className="monitor-radar-fact">
																		<span>5 日动量</span>
																		<strong className={finding.momentum_5d > 0 ? 'up' : finding.momentum_5d < 0 ? 'down' : ''}>
																			{finding.momentum_5d ? `${finding.momentum_5d > 0 ? '+' : ''}${finding.momentum_5d.toFixed(1)}%` : '—'}
																		</strong>
																	</div>
																</div>
															</section>
															{/* 右侧：有预填就展示规则（条件逐条成 chip），没有就列信号明细。 */}
															<section className="monitor-radar-card">
																<header>
																	<span>{finding.suggested_rule ? '建议规则' : '信号明细'}</span>
																	<em>{finding.suggested_rule ? (monitorScopeLabels[finding.suggested_rule.scope] ?? finding.suggested_rule.scope) : `${finding.signals.length} 项`}</em>
																</header>
																{finding.suggested_rule ? (
																	<div className="monitor-radar-rule">
																		<p className="monitor-radar-rule-name">{finding.suggested_rule.name}</p>
																		<div className="monitor-radar-rule-conds">
																			{finding.suggested_rule.conditions.map((condition, index) => (
																				<span className="monitor-radar-cond" key={`${condition.field}-${index}`} title={describeRadarCondition(condition)}>
																					{monitorFieldLabels[condition.field] ?? condition.field}
																					{/* 布尔字段（60日新低这类）写「= 1」不像人话，直接说触发/解除。 */}
																					{condition.op === 'eq' ? (
																						<em>{condition.value ? '触发' : '解除'}</em>
																					) : (
																						<>
																							<em>{conditionOpText[condition.op] ?? condition.op}</em>
																							{condition.op === 'between' ? `${condition.value}~${condition.value2 ?? ''}` : condition.value}
																						</>
																					)}
																				</span>
																			))}
																		</div>
																		<p className="monitor-radar-rule-meta">
																			{finding.suggested_rule.logic === 'or' ? '任一条件成立' : '全部条件成立'} · 冷却 {Math.round(finding.suggested_rule.cooldown_seconds / 60)} 分钟 · {finding.suggested_rule.severity === 'critical' ? '紧急' : finding.suggested_rule.severity === 'warning' ? '提醒' : '记录'}
																		</p>
																	</div>
																) : (
																	<div className="monitor-radar-signal-chips">
																		{finding.signals.length === 0 && <span className="muted">无事件级信号，结论来自形态与位置</span>}
																		{finding.signals.map((signal) => (
																			<span key={signal} className="monitor-radar-cond" title={monitorSignalHints[signal] || undefined}>
																				{monitorFieldLabels[signal] || signal}
																			</span>
																		))}
																	</div>
																)}
															</section>
														</div>
													</div>
												</td>
											</tr>
										)}
									</Fragment>
								);
							})}
						</tbody>
					</table>
				</div>
			)}
				</section>
			</div>
		</div>
	);
}

// RadarSpark 是行内迷你走势：近 30 日收盘折线，终点高于起点画涨色。
// 序列不足两根就不画——宁缺勿假。
function RadarSpark({ closes, changePercent }: { closes: number[]; changePercent: number }) {
	if (!closes || closes.length < 2) return null;
	const min = Math.min(...closes);
	const max = Math.max(...closes);
	const span = max - min || 1;
	const points = closes
		.map((value, index) => {
			const x = (index / (closes.length - 1)) * 62 + 1;
			const y = 19 - ((value - min) / span) * 16;
			return `${x.toFixed(1)},${y.toFixed(1)}`;
		})
		.join(' ');
	const up = closes[closes.length - 1] >= closes[0] || (closes[closes.length - 1] === closes[0] && changePercent >= 0);
	return (
		// 折线保持 64×22 原始比例：不能给 preserveAspectRatio="none"，否则横向
		// 拉伸会把 30 个点摊平，涨跌形态退化成一条直线。
		// 真需要「拉宽」的是它右边的净分强度条，不是这里。
		<svg className="monitor-radar-spark" width={64} height={22} viewBox="0 0 64 22" aria-hidden="true">
			<polyline points={points} fill="none" stroke={up ? 'var(--up)' : 'var(--down)'} strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
		</svg>
	);
}

// RadarScoreBar 是净分双向条：中线为 0，向左看空向右看多，±6 分封顶。
function RadarScoreBar({ score }: { score: number }) {
	const pct = Math.min(Math.abs(score) / 6, 1) * 50;
	return (
		<span className="monitor-radar-scorebar" aria-hidden="true">
			{pct > 0 && (
				<span
					className={`monitor-radar-scorebar-fill ${score > 0 ? 'pos' : 'neg'}`}
					style={score > 0 ? { left: '50%', width: `${pct}%` } : { right: '50%', width: `${pct}%` }}
				/>
			)}
		</span>
	);
}

// ---------------------------------------------------------------- Tab 1 触发记录 + 规则

function EventsTab({ config, refreshKey }: { config: BackendConfig | null; refreshKey: number }) {
	const [overview, setOverview] = useState<MonitorOverview | null>(null);
	const [events, setEvents] = useState<MonitorEvent[]>([]);
	const [severity, setSeverity] = useState('');
	const [rules, setRules] = useState<MonitorRule[]>([]);
	const [editorState, setEditorState] = useState<{ open: boolean; rule: Partial<MonitorRule> | null }>({ open: false, rule: null });
	const [presetBusy, setPresetBusy] = useState('');
	const [notice, setNotice] = useState('');
	const [loadError, setLoadError] = useState('');
	const [overviewError, setOverviewError] = useState(false);
	const loadVersion = useRef(0);
	const pendingLoad = useRef<number | null>(null);

	const createPreset = async (preset: (typeof quickStartPresets)[number]) => {
		if (!config || presetBusy) return;
		setPresetBusy(preset.key);
		setNotice('');
		try {
			await saveMonitorRule(config, preset.rule);
			setNotice(`已创建「${preset.title}」，可在右侧调整阈值`);
			await loadAll(config);
		} catch (cause) {
			setNotice(`创建失败：${String((cause as Error).message)}`);
		} finally {
			setPresetBusy('');
		}
	};

	const loadAll = useCallback(async (configValue: BackendConfig | null) => {
		if (!configValue) return;
		const version = ++loadVersion.current;
		pendingLoad.current = version;
		try {
			const [overviewResult, eventsResult, rulesResult] = await Promise.allSettled([
				fetchMonitorOverview(configValue),
				fetchMonitorEvents(configValue, new URLSearchParams({ limit: '120', ...(severity ? { severity } : {}) }).toString()),
				fetchMonitorRules(configValue),
			]);
			if (version !== loadVersion.current) return;
			setOverviewError(overviewResult.status === 'rejected');
			if (overviewResult.status === 'fulfilled') setOverview(overviewResult.value.data);
			if (eventsResult.status === 'fulfilled') setEvents(eventsResult.value.items);
			if (rulesResult.status === 'fulfilled') setRules(rulesResult.value.items);
			setLoadError([
				overviewResult.status === 'rejected' ? '监控状态获取失败' : '',
				eventsResult.status === 'rejected' ? '触发记录获取失败' : '',
				rulesResult.status === 'rejected' ? '监控规则获取失败' : '',
			].filter(Boolean).join('；'));
		} finally {
			if (pendingLoad.current === version) pendingLoad.current = null;
		}
	}, [severity]);

	useEffect(() => {
		setOverview(null); setOverviewError(false); setLoadError('');
		setEvents([]); setRules([]);
	}, [config?.backendUrl, config?.token]);
	useEffect(() => {
		void loadAll(config);
		return () => { loadVersion.current++; pendingLoad.current = null; };
	}, [config, refreshKey, loadAll]);
	// 页面可见时 5 秒轮询触发记录，后台标签页暂停（visibilityState）。
	useEffect(() => {
		if (!config) return;
		const poll = () => { if (!document.hidden && pendingLoad.current === null) void loadAll(config); };
		const timer = window.setInterval(() => {
			poll();
		}, 5000);
		const onVisibility = () => { poll(); };
		document.addEventListener('visibilitychange', onVisibility);
		return () => { window.clearInterval(timer); document.removeEventListener('visibilitychange', onVisibility); };
	}, [config, loadAll]);

	const engine = overview?.engine;
	const latest = events.length ? events[0].id : 0;
	const running = !overviewError && Boolean(engine?.running);
	const enabled = Boolean(engine?.enabled);
	const snapshotAge = engine?.snapshot_age_seconds ?? 0;
	return (
		<div className="monitor-events-layout">
			<div className="monitor-engine-strip">
				<div className="monitor-engine-main">
					<span className={`monitor-engine-dot ${running && enabled ? 'on' : ''}`} />
					<strong>{overviewError ? '监控状态待确认' : !overview ? '监控状态加载中' : running ? (enabled ? '监控运行中' : '监控已暂停') : '引擎未运行'}</strong>
					<small title={engine?.snapshot_warning}>{overviewError ? '状态获取失败，保留上次成功记录' : enabled ? `快照 ${engine?.snapshot_count ?? 0} 只 · ${snapshotAge}s 前${engine?.snapshot_stale ? ' · 行情降级/过期' : ''}` : '开启后按盘中节奏自动轮询'}</small>
					<small>最近执行 {formatTime(engine?.last_run_at)}</small>
					{loadError || engine?.last_error ? <span className="monitor-engine-error" title={[loadError, engine?.last_error].filter(Boolean).join('；')}>· {[loadError, engine?.last_error].filter(Boolean).join('；')}</span> : null}
				</div>
				<div className="monitor-strip-space" />
				<button type="button" className="monitor-small-button" onClick={() => void loadAll(config)}><RefreshCw size={13} />刷新</button>
				<button
					type="button"
					className={enabled ? 'monitor-small-button' : 'monitor-primary'}
					disabled={!config || !overview || overviewError}
					onClick={async () => { if (config) { try { await saveMonitorSettings(config, { enabled: !enabled }); await loadAll(config); } catch { setLoadError('监控开关保存失败，请刷新后重试'); } } }}
				>{enabled ? '暂停监控' : '开启监控'}</button>
			</div>

			<div className="monitor-stat-grid">
				<div className="monitor-stat">
					<span>今日触发</span>
					<strong>{overview?.events_today ?? 0}</strong>
					<small>最新事件 #{latest || '—'}</small>
				</div>
				<div className={`monitor-stat ${(overview?.critical_today ?? 0) > 0 ? 'critical' : ''}`}>
					<span>紧急事件</span>
					<strong>{overview?.critical_today ?? 0}</strong>
					<small>价格破位 / 批次止损</small>
				</div>
				<div className="monitor-stat">
					<span>启用规则</span>
					<strong>{overview?.enabled_rule_count ?? 0}</strong>
					<small>共 {overview?.rule_count ?? 0} 条 · 触发后按冷却去重</small>
				</div>
				<div className="monitor-stat ready">
					<span>监控标的</span>
					<strong>{engine?.snapshot_count ?? 0}</strong>
					<small>{enabled ? `快照 ${snapshotAge}s 前` : '未开启不取数'}</small>
				</div>
				<div className="monitor-stat">
					<span>持仓批次</span>
					<strong>{overview?.position_lot_count ?? 0}</strong>
					<small>共 {overview?.lot_count ?? 0} 批 · 止盈止损托管</small>
				</div>
			</div>

			{rules.length === 0 && (
				<div className="monitor-quickstart">
					<header>
						<strong>从四条常用规则开始</strong>
						<span>点击即创建，阈值之后在右侧改；也可以自己新建</span>
						{notice && <span className="muted">· {notice}</span>}
					</header>
					<div className="monitor-preset-grid">
						{quickStartPresets.map((preset) => {
							const Icon = preset.icon;
							return (
								<button
									type="button"
									key={preset.key}
									className="monitor-preset"
									disabled={Boolean(presetBusy)}
									onClick={() => void createPreset(preset)}
								>
									<span className="monitor-preset-icon">{presetBusy === preset.key ? <LoaderCircle className="spin" size={14} /> : <Icon size={14} />}</span>
									<div><strong>{preset.title}</strong><small>{preset.desc}</small></div>
								</button>
							);
						})}
					</div>
				</div>
			)}

			<div className="monitor-events-layout-body">
				<section className="monitor-panel monitor-feed" aria-label="触发记录">
					<header>
						<Activity size={17} />
						<div>
							<strong>触发记录</strong>
							<small>今日 {overview?.events_today ?? 0} 条 · 紧急 {overview?.critical_today ?? 0} 条</small>
						</div>
						<div className="monitor-feed-tools">
							<MonitorSelect
								value={severity}
								onChange={setSeverity}
								ariaLabel="按级别筛选"
								options={[
									{ value: '', label: '全部级别' },
									{ value: 'critical', label: '紧急' },
									{ value: 'warning', label: '提醒' },
									{ value: 'info', label: '记录' },
								]}
							/>
							<button type="button" className="monitor-small-button" onClick={async () => {
								if (!config || !await confirmDialog({ title: '清空全部触发记录', message: '将删除所有日期和级别的触发记录，包括当前列表之外的历史记录。此操作不可撤销。', confirmText: '清空全部记录', tone: 'danger' })) return;
								try { await clearMonitorEvents(config); await loadAll(config); }
								catch (cause) { setNotice(`清空失败：${String((cause as Error).message)}`); }
							}}><Trash2 size={13} />清空</button>
						</div>
					</header>
					{events.length === 0 ? (
						<div className="monitor-empty-state">
							<Radar size={26} aria-hidden="true" />
							<strong>今天还没有触发</strong>
							<span>盘中引擎按秒取行情，条件成立才落库；非交易时段不产生盘中事件。规则命中后会出现在这里。</span>
							<button type="button" className="monitor-small-button" onClick={() => setEditorState({ open: true, rule: null })}><Plus size={13} />新建一条规则</button>
						</div>
					) : (
						/* 触发记录也用模拟持仓同款表格：时间/标的/级别/内容/通知/操作，
						   级别在首列留一条色带，扫一眼就知道哪几条是紧急。 */
						<div className="trading-table-wrap">
							<table className="trading-table trading-table-fixed monitor-events-table">
								{/* 操作列只剩一个图标按钮：给 48px 就够，余量还给「触发内容」，
								    否则内容被挤成两行、右侧留一大片空白。 */}
								<colgroup>
									<col style={{ width: '12%' }} />
									<col style={{ width: '20%' }} />
									<col style={{ width: '10%' }} />
									<col style={{ width: '42%' }} />
									<col style={{ width: '10%' }} />
									<col style={{ width: '48px' }} />
								</colgroup>
								<thead>
									<tr><th>时间</th><th>标的</th><th>级别</th><th>触发内容</th><th>通知</th><th>操作</th></tr>
								</thead>
								<tbody>
									{events.map((item) => (
										<tr key={item.id} className={`severity-${item.severity}`}>
											<td>{formatTime(item.created_at)}</td>
											<td>
												<strong className="monitor-cell-title">{item.name || item.symbol || item.rule_name}</strong>
												{(item.symbol || item.price) && (
													<span className="monitor-cell-meta">
														{item.symbol || ''}
														{item.price ? `${item.symbol ? ' · ' : ''}${item.price.toFixed(2)}（${item.change_percent?.toFixed(2)}%）` : ''}
													</span>
												)}
											</td>
											<td><span className={`monitor-severity ${item.severity}`}>{severityLabels[item.severity] ?? item.severity}</span></td>
											<td className="monitor-event-message">{item.message}</td>
											<td><em className={`monitor-notify ${item.notify_status}`}>{notifyLabel(item.notify_status, item.notify_error)}</em></td>
											<td>
												<div className="trading-row-actions">
													<button type="button" className="trading-row-action is-icon rule-action-delete" title="删除这条触发记录" aria-label="删除这条触发记录" onClick={async () => { if (config) { await deleteMonitorEvent(config, item.id); void loadAll(config); } }}><Trash2 size={13} /></button>
												</div>
											</td>
										</tr>
									))}
								</tbody>
							</table>
						</div>
					)}
				</section>
				<section className="monitor-panel monitor-rules" aria-label="监控规则">
					<header>
						<Gauge size={17} />
						<div>
							<strong>监控规则</strong>
							<small>{overview?.enabled_rule_count ?? 0} / {overview?.rule_count ?? 0} 已启用{notice ? ` · ${notice}` : ''}</small>
						</div>
						{/* 面板主操作：主色实心按钮，与行内描边次操作形成主次层次。 */}
						<button type="button" className="monitor-primary" onClick={() => setEditorState({ open: true, rule: null })}><Plus size={13} />新建规则</button>
					</header>
					{rules.length === 0 ? (
						<div className="monitor-empty-state">
							<Gauge size={26} aria-hidden="true" />
							<strong>还没有监控规则</strong>
							<span>规则是「什么条件下提醒你」的一句话。可以先用上方预设，也可以完全自定义字段、阈值与冷却时间。</span>
						</div>
					) : (
						/* 规则清单同样走表格：名称外面一圈描边胶囊，启用=蓝、停用=灰，
						   状态直接长在名字上，不再单占一列；类型/冷却/托管收进下方元信息；
						   行内三个动作按语义配色（启停=蓝、编辑=蓝、删除=红）。 */
						<div className="trading-table-wrap">
							<table className="trading-table trading-table-fixed monitor-rules-table">
								<colgroup>
									<col className="col-rule" />
									<col className="col-scope" />
									<col className="col-trigger" />
									<col className="col-actions" />
								</colgroup>
								<thead>
									<tr><th>规则</th><th>作用域</th><th>触发</th><th>操作</th></tr>
								</thead>
								<tbody>
									{rules.map((rule) => {
										// 作用域胶囊收窄后放不下长串代码： chip 里只放计数，
										// 完整标的列表进 title 悬浮提示。
										const scopeText = rule.type === 'sector'
											? `板块 ${rule.symbols?.length ?? 0} 个`
											: rule.scope === 'symbols'
												? `指定 ${rule.symbols?.length ?? 0} 只`
												: monitorScopeLabels[rule.scope];
										const scopeTitle = rule.scope === 'symbols' && rule.symbols?.length
											? `指定标的 ${joinSymbols(rule.symbols)}`
											: scopeText;
										return (
											<tr key={rule.id}>
												<td>
													<div className="monitor-rule-name">
														<span
															className={`monitor-rule-chip ${rule.enabled ? 'on' : 'off'}`}
															title={`${rule.name || rule.id} · ${rule.enabled ? '已启用' : '已停用'}`}
														>
															{rule.name || rule.id}
														</span>
													</div>
													<span className="monitor-cell-meta">
														冷却 {rule.cooldown_seconds}s
														{rule.source !== 'manual' && (
															<span className="monitor-hosted">{rule.source === 'lot' ? '批次托管' : '模拟盘'}</span>
														)}
													</span>
												</td>
												<td><span className="monitor-scope-tag" title={scopeTitle}>{scopeText}</span></td>
												<td>{rule.trigger_count} 次</td>
											<td>
												<div className="trading-row-actions">
												<button type="button" className="trading-row-action is-icon rule-action-toggle"
													title={rule.enabled ? '停用规则' : '启用规则'}
													aria-label={rule.enabled ? '停用规则' : '启用规则'}
													onClick={async () => {
														if (!config) return;
														try {
															await toggleMonitorRule(config, rule.id, !rule.enabled);
															setNotice(rule.enabled ? '已停用' : '已启用');
														} catch (cause) {
															setNotice(`操作失败：${String((cause as Error).message)}`);
														}
														void loadAll(config);
													}}>{rule.enabled ? <Pause size={13} /> : <Play size={13} />}</button>
												<button
													type="button"
													className="trading-row-action is-icon rule-action-edit"
													disabled={rule.source !== 'manual'}
													title={rule.source === 'manual' ? '编辑规则' : '托管规则由持仓批次生成，改阈值请删掉后自建同类规则'}
													aria-label="编辑规则"
													onClick={() => setEditorState({ open: true, rule })}
												><Pencil size={13} /></button>
												<button
													type="button"
													className="trading-row-action is-icon rule-action-delete"
													title={rule.source === 'lot' ? '删除（同时关闭该持仓批次的止盈止损监控）' : '删除规则'}
													aria-label="删除规则"
													onClick={async () => {
														if (!config) return;
														try {
															await deleteMonitorRule(config, rule.id);
															setNotice('规则已删除');
														} catch (cause) {
															setNotice(`删除失败：${String((cause as Error).message)}`);
														}
														void loadAll(config);
													}}
												><Trash2 size={13} /></button>
												</div>
											</td>
										</tr>
										);
									})}
								</tbody>
							</table>
						</div>
					)}
				</section>
			</div>
			{editorState.open && (
				<RuleEditor
					config={config}
					rule={editorState.rule}
					onClose={() => setEditorState({ open: false, rule: null })}
					onSaved={() => { setEditorState({ open: false, rule: null }); void loadAll(config); }}
				/>
			)}
		</div>
	);
}

// 事件级别的成对文案：列表格里的徽标与筛选下拉共用一套说法。
const severityLabels: Record<string, string> = { critical: '紧急', warning: '提醒', info: '记录' };

function notifyLabel(status: string, error?: string) {
	switch (status) {
		case 'queued': return '已通知';
		case 'pending': return '待合并';
		case 'merged': return '并入摘要';
		case 'failed': return error ? `发送失败：${error}` : '发送失败';
		default: return error || '仅记录';
	}
}

function joinSymbols(symbols?: string[]) {
	if (!symbols?.length) return '';
	return symbols.length <= 4 ? symbols.join('、') : `${symbols.slice(0, 4).join('、')} 等 ${symbols.length} 只`;
}

// ---------------------------------------------------------------- 规则编辑器

// 新建/编辑规则走「模板优先」：常用意图收敛成 5 张卡片 + 1 个高级入口，
// 字段、算子、AND/OR、板块过滤这类专业控件只留在高级编辑里。
// 后端协议不变：模板最终仍生成同一套 type + conditions 结构，已存规则全部兼容。
type RuleTemplateKey = 'quote' | 'abnormal' | 'signal' | 'sector' | 'market' | 'custom';

const simpleOp = (condition: MonitorCondition) => condition.op === 'gte' || condition.op === 'lte';

// 已有规则能否映射回模板：单条件且只用 ≥/≤ 的数值规则 → 行情/板块/大盘；
// 纯布尔多选（同类型）→ 异动/信号。其余（偏离值、between、cross、多条件）进高级。
function ruleTemplateKey(rule: Partial<MonitorRule>): RuleTemplateKey {
	const conditions = rule.conditions || [];
	switch (rule.type) {
		case 'price': case 'volume':
			return conditions.length === 1 && simpleOp(conditions[0]) ? 'quote' : 'custom';
		case 'abnormal':
			return conditions.length > 0 && conditions.every((item) => item.op === 'gte' && Math.abs(item.value) <= 1) ? 'abnormal' : 'custom';
		case 'signal':
			return conditions.length > 0 && conditions.every((item) => item.op === 'gte') ? 'signal' : 'custom';
		case 'sector':
			return conditions.length > 0 && conditions.every((item) => simpleOp(item)
				&& availableFields.sector.some((field) => field.key === item.field)) ? 'sector' : 'custom';
		case 'market':
			return conditions.length === 1 && simpleOp(conditions[0]) ? 'market' : 'custom';
		default:
			return 'custom';
	}
}

const ruleTemplates: Array<{ key: RuleTemplateKey; title: string; desc: string; icon: typeof Radar }> = [
	{ key: 'quote', title: '行情提醒', desc: '涨跌幅、现价到位、距成本、量比', icon: TrendingUp },
	{ key: 'abnormal', title: '盘中异动', desc: '涨停、炸板、跌停、60日新高/新低', icon: Activity },
	{ key: 'signal', title: '技术信号', desc: '金叉死叉、布林突破、放量缩量', icon: Radar },
	{ key: 'sector', title: '板块雷达', desc: '拉升、急拉、资金、普涨、涨停潮——勾场景即生效', icon: Waves },
	{ key: 'market', title: '大盘风向', desc: '指数涨跌幅与涨停家数', icon: Wind },
	{ key: 'custom', title: '高级自定义', desc: '偏离值、多条件组合、板块过滤等', icon: SlidersHorizontal },
];

// 行情模板的候选字段：横跨 price / volume 两个类型，选中字段时自动换类型。
const quoteFieldOptions: Array<{ key: string; label: string; type: MonitorRuleType }> = [
	{ key: 'change_pct', label: '涨跌幅 %', type: 'price' },
	{ key: 'last', label: '现价', type: 'price' },
	{ key: 'cost_distance_pct', label: '距成本 %（适合持仓）', type: 'price' },
	{ key: 'volume_ratio', label: '量比', type: 'volume' },
	{ key: 'turnover_rate', label: '换手率 %', type: 'volume' },
];

// 板块场景：把「字段+算子+阈值」翻译成一句人话，勾选即得一条条件，
// 阈值内联可调；多选时任一场景成立即触发（logic=or）。
// 同一字段允许双向场景（急拉/跳水共用 sector_speed，靠 op 区分）。
const sectorScenarios: Array<{ key: string; label: string; field: string; op: MonitorCondition['op']; value: number; unit: string; dir: '≥' | '≤'; hint: string }> = [
	{ key: 'change_up', label: '板块拉升', field: 'sector_change', op: 'gte', value: 3, unit: '%', dir: '≥', hint: '板块涨跌幅超过阈值（相对昨收）' },
	{ key: 'speed_up', label: '急拉', field: 'sector_speed', op: 'gte', value: 1, unit: '%', dir: '≥', hint: '近 5 分钟板块快速上行' },
	{ key: 'speed_down', label: '跳水', field: 'sector_speed', op: 'lte', value: -1, unit: '%', dir: '≤', hint: '近 5 分钟板块快速回落' },
	{ key: 'flow_in', label: '资金进场', field: 'sector_flow', op: 'gte', value: 5, unit: '亿', dir: '≥', hint: '当日主力净流入超过阈值' },
	{ key: 'flow_out', label: '资金撤离', field: 'sector_flow', op: 'lte', value: -3, unit: '亿', dir: '≤', hint: '当日主力净流出超过阈值' },
	{ key: 'breadth', label: '板块普涨', field: 'sector_breadth', op: 'gte', value: 80, unit: '%', dir: '≥', hint: '成分股上涨占比，80 即普涨' },
	{ key: 'limit_up', label: '涨停潮', field: 'sector_limit_up', op: 'gte', value: 2, unit: '家', dir: '≥', hint: '板块内涨停家数' },
];

// 方向切换：≥ / ≤ 两个按钮，替代裸算子下拉。
function DirectionToggle({ op, onChange }: { op: MonitorCondition['op']; onChange: (op: MonitorCondition['op']) => void }) {
	return (
		<div className="monitor-logic-switch" role="group" aria-label="触发方向">
			<button type="button" className={op !== 'lte' ? 'active' : ''} onClick={() => onChange('gte')}>≥</button>
			<button type="button" className={op === 'lte' ? 'active' : ''} onClick={() => onChange('lte')}>≤</button>
		</div>
	);
}

// 作用域分段按钮：替代下拉，四个系统能力入口一目了然。
// counts 是各作用域当前命中的标的数（来自 /monitor/targets），
// 让「持仓 5、关注仓 0」直接可见——选了空范围一眼就知道。
function ScopeSegmented({ value, onChange, allowAll, counts }: {
	value: MonitorScope; onChange: (scope: MonitorScope) => void; allowAll: boolean;
	counts?: Partial<Record<MonitorScope, number>>;
}) {
	const options: Array<{ key: MonitorScope; label: string }> = [
		{ key: 'positions', label: '持仓' },
		{ key: 'watchlist', label: '关注仓' },
		{ key: 'symbols', label: '指定标的' },
	];
	if (allowAll) options.push({ key: 'all', label: '全市场' });
	return (
		<div className="monitor-seg" role="group" aria-label="作用范围">
			{options.map((item) => (
				<button type="button" key={item.key} className={value === item.key ? 'active' : ''}
					title={monitorScopeLabels[item.key]} onClick={() => onChange(item.key)}>
					{item.label}
					{counts?.[item.key] !== undefined && <small>{counts[item.key]}</small>}
				</button>
			))}
		</div>
	);
}

// ScopeHitLine 把选中范围命中的标的直接亮出来，空范围给出去哪加的指引。
// 名字取前 6 个，其余折叠成「等 N 只」，避免长持仓把弹窗撑爆。
function ScopeHitLine({ scope, targets }: { scope: MonitorScope; targets: MonitorScopeTargets }) {
	if (scope !== 'positions' && scope !== 'watchlist' && scope !== 'inspection') return null;
	const list = targets[scope] || [];
	if (list.length === 0) {
		const hint = scope === 'positions'
			? '还没有持仓——在「持仓AI巡检」维护组合后自动纳入。'
			: scope === 'watchlist'
				? '关注仓还是空的——去本页「关注仓」页签搜索加入标的，或改用「指定标的」。'
				: '还没有巡检报告——去「持仓AI巡检」跑一次，报告里的持仓会自动进入监控。';
		return <small className="muted monitor-scope-empty">{hint}</small>;
	}
	const shown = list.slice(0, 6);
	const rest = list.length - shown.length;
	return (
		<small className="muted">
			将监控 {list.length} 只：{shown.map((item) => item.name || item.symbol).join('、')}{rest > 0 ? ` 等 ${list.length} 只` : ''}
		</small>
	);
}

// 冷却档位：语义化选项替代裸秒数；已有规则的自定义秒数照常显示。
function CooldownPicker({ value, onChange }: { value: number; onChange: (seconds: number) => void }) {
	const steps = [60, 180, 300, 900, 1800, 3600];
	const options = steps.includes(value) ? steps : [...steps, value].sort((a, b) => a - b);
	return (
		<MonitorSelect
			value={String(value)}
			onChange={(next) => onChange(Number(next))}
			ariaLabel="冷却时间"
			options={options.map((seconds) => ({
				value: String(seconds),
				label: seconds % 60 === 0 && seconds >= 60 ? `${seconds / 60} 分钟` : `${seconds} 秒`,
			}))}
		/>
	);
}

// ST 过滤用开关而不是裸复选框：与相邻的数值/下拉字段同一条基线对齐，
// 关掉时明确写出「含 ST」，避免用户误以为过滤生效。
function ExcludeStToggle({ value, onChange }: { value: boolean; onChange: (next: boolean) => void }) {
	return (
		<label className="monitor-toggle">
			<span>ST 过滤</span>
			<span className="monitor-toggle-line">
				<input type="checkbox" role="switch" checked={value} onChange={(event) => onChange(event.target.checked)} aria-label="排除 ST" />
				<em>{value ? '排除 ST' : '含 ST'}</em>
			</span>
		</label>
	);
}

// SectorPicker 是板块规则的多选器：候选来自后端板块榜单（行业+概念）
// 与系统题材库（题材，TH 编号），带当日涨跌幅帮助用户挑。上限 20 个，
// 与后端校验一致。
function SectorPicker({ config, selected, onChange }: {
	config: BackendConfig | null;
	selected: string[];
	onChange: (symbols: string[]) => void;
}) {
	const [sectors, setSectors] = useState<MonitorSectorOption[]>([]);
	const [query, setQuery] = useState('');
	const [dimension, setDimension] = useState<'all' | 'industry' | 'concept' | 'theme'>('all');
	const [error, setError] = useState('');
	useEffect(() => {
		if (!config) return;
		let alive = true;
		fetchMonitorSectors(config)
			.then((payload) => { if (alive) setSectors(payload.data.items || []); })
			.catch((cause) => { if (alive) setError(String((cause as Error).message)); });
		return () => { alive = false; };
	}, [config]);

	const nameByCode = new Map(sectors.map((item) => [item.code, item.name]));
	const keyword = query.trim().toLowerCase();
	const filtered = sectors
		.filter((item) => dimension === 'all' || item.dimension === dimension)
		.filter((item) => !keyword || item.name.toLowerCase().includes(keyword) || item.code.toLowerCase().includes(keyword));
	const hasTheme = selected.some((code) => code.startsWith('TH'));
	const toggle = (code: string) => {
		if (selected.includes(code)) {
			onChange(selected.filter((item) => item !== code));
		} else if (selected.length < 20) {
			onChange([...selected, code]);
		}
	};
	return (
		<div className="monitor-sector-picker">
			<span>监控板块（{selected.length}/20）</span>
			{selected.length > 0 && (
				<div className="monitor-sector-selected">
					{selected.map((code) => (
						<button type="button" key={code} onClick={() => toggle(code)} title="移除">
							{nameByCode.get(code) || code} <X size={11} />
						</button>
					))}
				</div>
			)}
			{hasTheme && (
				<small className="muted">已选题材（TH）来自系统题材库：支持涨跌幅 / 成分上涨占比 / 涨跌停家数（按成员股实时聚合）；主力净额与 5 分钟异动仅东财板块支持。</small>
			)}
			<div className="monitor-sector-tabs">
				{(['all', 'industry', 'concept', 'theme'] as const).map((key) => (
					<button type="button" key={key} className={dimension === key ? 'active' : ''}
						onClick={() => setDimension(key)}>
						{{ all: '全部', industry: '行业', concept: '概念', theme: '题材库' }[key]}
					</button>
				))}
			</div>
			<input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索板块名或代码，例如：半导体 / BK1036 / 机器人" />
			<div className="monitor-sector-list" role="listbox" aria-label="板块候选" aria-multiselectable="true">
				{filtered.slice(0, 120).map((item) => (
					<button type="button" key={item.code} className={selected.includes(item.code) ? 'active' : ''}
						onClick={() => toggle(item.code)}>
						<strong>{item.name}</strong>
						<small>{item.code} · {monitorSectorDimensionLabels[item.dimension] || item.dimension}</small>
						<em className={item.change_percent >= 0 ? 'up' : 'down'}>{item.change_percent >= 0 ? '+' : ''}{item.change_percent.toFixed(2)}%</em>
					</button>
				))}
				{filtered.length === 0 && <small className="muted">{error || '没有匹配的板块'}</small>}
			</div>
		</div>
	);
}

function RuleEditor({ config, rule, onClose, onSaved }: {
	config: BackendConfig | null; rule: Partial<MonitorRule> | null;
	onClose: () => void; onSaved: () => void;
}) {
	const isNew = !rule?.id;
	const [draft, setDraft] = useState<Partial<MonitorRule>>(() => rule ? JSON.parse(JSON.stringify(rule)) : emptyRule());
	const [template, setTemplate] = useState<RuleTemplateKey | null>(() => (isNew ? null : ruleTemplateKey(rule ?? {})));
	const [useAdvanced, setUseAdvanced] = useState(false);
	const [test, setTest] = useState<{ busy: boolean; text: string }>({ busy: false, text: '' });
	const [error, setError] = useState('');
	// 作用域命中的标的：让「持仓 / 关注仓 / 巡检持仓」不再是黑盒，
	// 选完范围立刻看见这条规则实际会盯哪几支票。
	const [targets, setTargets] = useState<MonitorScopeTargets>({});
	useEffect(() => {
		if (!config) return;
		let alive = true;
		fetchMonitorTargets(config)
			.then((payload) => { if (alive) setTargets(payload.data || {}); })
			.catch(() => { /* 预览拿不到不挡编辑；引擎判定走的是同一份数据 */ });
		return () => { alive = false; };
	}, [config]);
	const scopeCounts = useMemo(() => ({
		positions: targets.positions?.length,
		watchlist: targets.watchlist?.length,
		inspection: targets.inspection?.length,
	} as Partial<Record<MonitorScope, number>>), [targets]);
	const ruleType = (draft.type || 'price') as MonitorRuleType;
	const conditions = draft.conditions || [];

	const patch = (part: Partial<MonitorRule>) => setDraft((current) => ({ ...current, ...part }));
	const patchCondition = (index: number, part: Partial<MonitorCondition>) => {
		const next = conditions.map((condition, position) => position === index ? { ...condition, ...part } : condition);
		patch({ conditions: next });
	};
	const addCondition = () => {
		const first = availableFields[ruleType][0];
		patch({ conditions: [...conditions, { field: first.key, op: 'gte', value: 0 }] });
	};

	// 选模板：名称/级别/冷却保留，类型与条件重置到该模板的默认形态；
	// 换模板时清掉不属于该模板的 symbols，避免把股票代码塞进板块规则。
	const pickTemplate = (key: RuleTemplateKey) => {
		setTemplate(key);
		setError('');
		if (key === 'custom') { setUseAdvanced(true); return; }
		setUseAdvanced(false);
		const wasSector = draft.type === 'sector';
		const symbols = key === 'sector'
			? (wasSector ? draft.symbols : [])
			: draft.scope === 'symbols' ? draft.symbols : undefined;
		// 新建规则时不要停在「指定标的但一个代码都没填」的死角：
		// 行情/异动默认看持仓清单，信号默认看关注仓，用户点一下就能改。
		const hasCodes = Boolean(draft.symbols?.length);
		const currentScope = (draft.scope || 'symbols') as MonitorScope;
		const emptySymbols = currentScope === 'symbols' && !hasCodes;
		// 信号也支持全市场：只在「指定标的但一个代码都没填」时退回关注仓，
		// 其余作用域（含全市场）保持用户已经选好的那个。
		const scope: MonitorScope = key === 'signal'
			? (emptySymbols ? 'watchlist' : currentScope)
			: emptySymbols && (key === 'quote' || key === 'abnormal') ? 'positions'
			: currentScope;
		if (key === 'quote') patch({ type: 'price', scope, conditions: [{ field: 'change_pct', op: 'gte', value: 0 }], logic: 'and', symbols });
		else if (key === 'abnormal') patch({ type: 'abnormal', scope, conditions: [{ field: 'limit_up', op: 'gte', value: 1 }], logic: 'or', symbols });
		else if (key === 'signal') patch({ type: 'signal', scope, conditions: [{ field: 'macd_golden', op: 'gte', value: 1 }], logic: 'or', symbols, cooldown_seconds: isNew ? 3600 : draft.cooldown_seconds });
		else if (key === 'sector') patch({ type: 'sector', conditions: [{ field: 'sector_change', op: 'gte', value: 3 }, { field: 'sector_flow', op: 'gte', value: 5 }], logic: 'or', symbols });
		else if (key === 'market') patch({ type: 'market', conditions: [{ field: 'index_change_pct', op: 'lte', value: -2 }], logic: 'and' });
	};

	// 异动/信号模板的 chip 开关：布尔多选，任一成立即触发（logic=or）。
	const toggleChip = (field: string) => {
		if (conditions.some((condition) => condition.field === field)) {
			patch({ conditions: conditions.filter((condition) => condition.field !== field) });
		} else {
			patch({ conditions: [...conditions, { field, op: 'gte', value: 1 }] });
		}
	};

	const save = async () => {
		if (!config) return;
		setError('');
		if (conditions.length === 0) {
			setError(template === 'abnormal' || template === 'signal' ? '至少选择一个异动或信号' : '至少需要一条条件');
			return;
		}
		const normalized: Partial<MonitorRule> = {
			...draft,
			name: draft.name?.trim() || `${monitorRuleTypeLabels[ruleType]}监控`,
			symbols: ruleType === 'sector'
				? (draft.symbols || []).slice(0, 20)
				: draft.scope === 'symbols'
					? (draft.symbols || (draft as { _symbolsText?: string })._symbolsText || '').toString().split(/[,，\s]+/).filter(Boolean)
					: undefined,
		};
		setTest({ busy: true, text: '' });
		try {
			await saveMonitorRule(config, normalized, isNew ? undefined : draft.id);
			onSaved();
		} catch (cause) {
			setError(String((cause as Error).message));
		} finally {
			setTest((current) => ({ ...current, busy: false }));
		}
	};
	const runTest = async () => {
		if (!config) return;
		setError('');
		setTest({ busy: true, text: '' });
		try {
			const payload = await testMonitorRule(config, draft);
			setTest({ busy: false, text: `${payload.data.would_fire} / ${payload.data.total} 个标的会立即触发` });
		} catch (cause) {
			setError(String((cause as Error).message));
			setTest({ busy: false, text: '' });
		}
	};
	// 板块场景的开/关与内联阈值编辑（同字段双向场景靠 field+op 区分）。
	const scenarioActive = (scenario: (typeof sectorScenarios)[number]) =>
		conditions.some((condition) => condition.field === scenario.field && condition.op === scenario.op);
	const toggleScenario = (scenario: (typeof sectorScenarios)[number]) => {
		if (scenarioActive(scenario)) {
			patch({ conditions: conditions.filter((condition) => !(condition.field === scenario.field && condition.op === scenario.op)) });
		} else {
			patch({ conditions: [...conditions, { field: scenario.field, op: scenario.op, value: scenario.value }] });
		}
	};
	const scenarioValue = (scenario: (typeof sectorScenarios)[number]) =>
		conditions.find((condition) => condition.field === scenario.field && condition.op === scenario.op)?.value ?? scenario.value;

	const primary = conditions[0];
	const namePlaceholder = template === 'quote' ? '例如：证券ETF 涨幅超过 3%'
		: template === 'abnormal' ? '例如：持仓炸板提醒'
		: template === 'signal' ? '例如：关注仓 MACD 金叉'
		: template === 'sector' ? '例如：半导体 板块涨跌幅 ≥ 3%'
		: template === 'market' ? '例如：大盘急跌提醒' : '例如：证券ETF 跌破 1.0';
	return (
		<div className="monitor-editor-backdrop" onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
			<div className="monitor-editor" role="dialog" aria-label={isNew ? '新建监控规则' : '编辑监控规则'}>
				<header>
					<strong>{isNew ? '新建监控规则' : '编辑监控规则'}</strong>
					<div className="monitor-editor-head-tools">
						{template !== null && template !== 'custom' && (
							// 高级里改过类型后，先按新形态重新判定模板：判不出来就留在高级。
							<button type="button" className="monitor-link-button" onClick={() => {
								if (useAdvanced) {
									const derived = ruleTemplateKey(draft);
									if (derived === 'custom') return;
									setTemplate(derived);
									setUseAdvanced(false);
								} else {
									setUseAdvanced(true);
								}
							}}>
								{useAdvanced ? '返回简化' : '高级编辑'}
							</button>
						)}
						<button type="button" onClick={onClose} aria-label="关闭"><X size={16} /></button>
					</div>
				</header>
				{template === null ? (
					// 第一步：选模板。五张常用卡 + 高级入口，替代裸的「类型」下拉。
					<div className="monitor-editor-body">
						<div className="monitor-template-grid">
							{ruleTemplates.map((item) => (
								<button type="button" key={item.key} className="monitor-template-card" onClick={() => pickTemplate(item.key)}>
									<item.icon size={17} />
									<strong>{item.title}</strong>
									<small>{item.desc}</small>
								</button>
							))}
						</div>
						<small className="muted">提示：持仓批次的止盈止损规则在「持仓」页签录一笔自动生成；智能雷达里的发现也能一键转规则——大多数时候不需要从这里手动建。</small>
					</div>
				) : useAdvanced || template === 'custom' ? (
					// 高级编辑：完整字段、算子、AND/OR 与过滤，行为与旧版编辑器一致。
					// 已有规则映射不到模板（偏离值、between、cross、多条件）也走这里。
					<div className="monitor-editor-body">
					<label><span>名称</span><input value={draft.name || ''} onChange={(event) => patch({ name: event.target.value })} placeholder={namePlaceholder} /></label>
				<label><span>类型</span>
						<MonitorSelect
							value={ruleType}
							onChange={(next) => {
								const type = next as MonitorRuleType;
								patch({ type, conditions: [{ field: availableFields[type][0].key, op: 'gte', value: 0 }] });
							}}
							ariaLabel="规则类型"
							block
							options={Object.entries(monitorRuleTypeLabels).map(([key, label]) => ({ value: key, label }))}
						/>
					</label>
					<div className="monitor-conditions">
						<span>条件（{draft.logic === 'or' ? '任一成立' : '全部成立'}）</span>
						{ruleType === 'signal' && <small className="muted">信号由引擎按日线自动判定，事件当日触发一次，无需设置阈值。</small>}
						{ruleType === 'sector' && <small className="muted">板块由引擎每 60 秒评估一轮；缺数据的维度（如成分目录对不上）保持沉默，不会误报。</small>}
						{conditions.map((condition, index) => (
							<div className="monitor-condition-row" key={index}>
								<MonitorSelect
									value={condition.field}
									onChange={(next) => patchCondition(index, { field: next })}
									ariaLabel="条件字段"
									options={availableFields[ruleType].map((item) => ({
										value: item.key, label: monitorFieldLabels[item.key] || item.key,
									}))}
								/>
								<MonitorSelect
									value={condition.op}
									onChange={(next) => patchCondition(index, { op: next as MonitorCondition['op'] })}
									disabled={ruleType === 'abnormal' || ruleType === 'signal'}
									ariaLabel="比较方式"
									options={[
										{ value: 'gte', label: '≥' },
										{ value: 'lte', label: '≤' },
										{ value: 'between', label: '介于' },
										{ value: 'cross_up', label: '上穿' },
										{ value: 'cross_down', label: '下穿' },
									]}
								/>
								<input type="number" step="any" value={condition.value} disabled={ruleType === 'abnormal' || ruleType === 'signal'}
									onChange={(event) => patchCondition(index, { value: Number(event.target.value) })} />
								{condition.op === 'between' && <input type="number" step="any" value={condition.value2 ?? 0}
									onChange={(event) => patchCondition(index, { value2: Number(event.target.value) })} />}
								<button type="button" onClick={() => patch({ conditions: conditions.filter((_, position) => position !== index) })} aria-label="删除条件"><Trash2 size={13} /></button>
							</div>
						))}
						{ruleType === 'signal' && conditions.length > 0 && monitorSignalHints[conditions[conditions.length - 1].field] && (
							<small className="muted">{monitorSignalHints[conditions[conditions.length - 1].field]}</small>
						)}
						{ruleType === 'sector' && conditions.length > 0 && monitorSectorFieldHints[conditions[conditions.length - 1].field] && (
							<small className="muted">{monitorSectorFieldHints[conditions[conditions.length - 1].field]}</small>
						)}
						<button type="button" className="monitor-small-button" onClick={addCondition}><Plus size={12} />添加条件</button>
						{conditions.length > 1 && (
							<div className="monitor-logic-switch" role="group" aria-label="条件关系">
								<button type="button" className={draft.logic !== 'or' ? 'active' : ''} onClick={() => patch({ logic: 'and' })}>全部成立</button>
								<button type="button" className={draft.logic === 'or' ? 'active' : ''} onClick={() => patch({ logic: 'or' })}>任一成立</button>
							</div>
						)}
					</div>
					{ruleType !== 'market' && ruleType !== 'date' && ruleType !== 'sector' && (
						<label><span>作用域</span>
							<MonitorSelect
								value={draft.scope ?? 'symbols'}
								onChange={(next) => patch({ scope: next as MonitorScope })}
								ariaLabel="作用域"
								block
								options={Object.entries(monitorScopeLabels).map(([key, label]) => ({ value: key, label }))}
							/>
						</label>
					)}
					{ruleType === 'sector' && <SectorPicker config={config} selected={draft.symbols || []} onChange={(symbols) => patch({ symbols })} />}
					{draft.scope === 'symbols' && ruleType !== 'sector' && (
						<label><span>代码（空格或逗号分隔）</span>
							<input
								value={(draft.symbols || []).join(' ')}
								onChange={(event) => patch({ ...draft, symbols: event.target.value.split(/[,，\s]+/).filter(Boolean) } as Partial<MonitorRule>)}
								placeholder="例如：600519 300750"
							/>
						</label>
					)}
					{ruleType !== 'market' && ruleType !== 'sector' && (
						<div className="monitor-filter-row">
							<ExcludeStToggle value={Boolean(draft.filters?.exclude_st)} onChange={(exclude_st) => patch({ filters: { ...(draft.filters || {}), exclude_st } })} />
							<label><span>最低成交额（亿元）</span><input type="number" value={(draft.filters?.min_amount ?? 0) / 1e8} onChange={(event) => patch({ filters: { ...(draft.filters || {}), min_amount: Number(event.target.value) * 1e8 } })} /></label>
							<label><span>板块</span>
								<MonitorSelect
									value={draft.filters?.boards?.[0] || ''}
									onChange={(next) => patch({ filters: { ...(draft.filters || {}), boards: next ? [next] : [] } })}
									ariaLabel="板块"
									block
									options={[
										{ value: '', label: '全部板块' },
										{ value: 'main', label: '主板' },
										{ value: 'chinext', label: '创业板' },
										{ value: 'star', label: '科创板' },
										{ value: 'bse', label: '北交所' },
									]}
								/>
							</label>
						</div>
					)}
					<div className="monitor-filter-row">
						<label><span>级别</span>
							<MonitorSelect
								value={draft.severity ?? 'warning'}
								onChange={(next) => patch({ severity: next as MonitorRule['severity'] })}
								ariaLabel="级别"
								block
								options={[
									{ value: 'info', label: '记录' },
									{ value: 'warning', label: '提醒' },
									{ value: 'critical', label: '紧急' },
								]}
							/>
						</label>
						<label><span>冷却（秒）</span><input type="number" min={30} value={draft.cooldown_seconds} onChange={(event) => patch({ cooldown_seconds: Number(event.target.value) })} /></label>
					</div>
					</div>
				) : (
					// 简化表单：按模板只留必要项；过滤与组合收进「更多选项」。
					<div className="monitor-editor-body">
						<button type="button" className="monitor-template-back" onClick={() => { setTemplate(null); setError(''); }}><ChevronLeft size={14} />换个类型</button>
						<label><span>名称</span><input value={draft.name || ''} onChange={(event) => patch({ name: event.target.value })} placeholder={namePlaceholder} /></label>
						{(template === 'quote' || template === 'market') && primary && (
							<div className="monitor-simple-row">
								<MonitorSelect
									value={primary.field}
									onChange={(field) => {
										if (template === 'quote') {
											const option = quoteFieldOptions.find((item) => item.key === field);
											patch({ type: option?.type || 'price', conditions: [{ field, op: primary.op, value: primary.value }] });
										} else {
											patchCondition(0, { field });
										}
									}}
									ariaLabel="监控字段"
									options={(template === 'quote'
										? quoteFieldOptions
										: availableFields[template].map((item) => ({ key: item.key, label: monitorFieldLabels[item.key] || item.key, type: template }))
									).map((item) => ({ value: item.key, label: item.label }))}
								/>
								<DirectionToggle op={primary.op} onChange={(op) => patchCondition(0, { op })} />
								<input type="number" step="any" value={primary.value} onChange={(event) => patchCondition(0, { value: Number(event.target.value) })} aria-label="阈值" />
							</div>
						)}
						{template === 'quote' && primary?.field === 'cost_distance_pct' && (
							<small className="muted">距成本按当前持仓成本计算，建议范围选「持仓」。</small>
						)}
						{(template === 'abnormal' || template === 'signal') && (
							<div className="monitor-conditions">
								<span>{template === 'signal' ? '命中任一信号即触发' : '命中任一异动即触发'}</span>
								<div className="monitor-chip-row">
									{availableFields[template].map((item) => (
										<button type="button" key={item.key}
											className={`monitor-chip ${conditions.some((condition) => condition.field === item.key) ? 'active' : ''}`}
											title={template === 'signal' ? (monitorSignalHints[item.key] || monitorFieldLabels[item.key]) : (monitorFieldLabels[item.key] || item.key)}
											onClick={() => toggleChip(item.key)}>
											{monitorFieldLabels[item.key] || item.key}
										</button>
									))}
								</div>
								{template === 'signal' && <small className="muted">信号由引擎按日线自动判定，当日触发一次，无需设阈值。</small>}
							</div>
						)}
						{template === 'sector' && (
							<div className="monitor-conditions">
								<span>勾选要盯的场景，任一成立即提醒（阈值可改）</span>
								<div className="monitor-scenario-list">
									{sectorScenarios.map((scenario) => {
										const active = scenarioActive(scenario);
										return (
											<div className={`monitor-scenario-row ${active ? 'active' : ''}`} key={scenario.key}>
												<button type="button" className={`monitor-chip ${active ? 'active' : ''}`} title={scenario.hint} onClick={() => toggleScenario(scenario)}>
													{scenario.label}
												</button>
												{active && (
													<span className="monitor-scenario-value">
														{scenario.dir}
														<input type="number" step="any" value={scenarioValue(scenario)}
															onChange={(event) => {
																const value = Number(event.target.value);
																patch({ conditions: conditions.map((condition) => condition.field === scenario.field && condition.op === scenario.op ? { ...condition, value } : condition) });
															}} aria-label={`${scenario.label}阈值`} />
														{scenario.unit}
													</span>
												)}
												<small className="muted">{scenario.hint}</small>
											</div>
										);
									})}
								</div>
								<small className="muted">引擎每 60 秒评估一轮；题材库（TH）维度缺资金流与 5 分钟分时，相关场景自动沉默不误报。触发时会附上板块内领涨股。</small>
							</div>
						)}
						{template === 'sector' && <SectorPicker config={config} selected={draft.symbols || []} onChange={(symbols) => patch({ symbols })} />}
						{(template === 'quote' || template === 'abnormal' || template === 'signal') && (
							<div className="monitor-conditions">
								<div className="monitor-simple-row">
									<span className="monitor-simple-label">范围</span>
									<ScopeSegmented value={(draft.scope || 'symbols') as MonitorScope} allowAll counts={scopeCounts} onChange={(scope) => patch({ scope })} />
								</div>
								<ScopeHitLine scope={(draft.scope || 'symbols') as MonitorScope} targets={targets} />
								{template === 'signal' && draft.scope === 'all' && (
									<small className="muted monitor-scope-empty">
										全市场信号逐只按日线计算：默认最多扫描 800 只，可在下方「更多选项（标的过滤）」里用板块 / 最低成交额 / 排除 ST 收窄候选。
									</small>
								)}
							</div>
						)}
						{draft.scope === 'symbols' && template !== 'sector' && (
							<label><span>代码（空格或逗号分隔）</span>
								<input
									value={(draft.symbols || []).join(' ')}
									onChange={(event) => patch({ ...draft, symbols: event.target.value.split(/[,，\s]+/).filter(Boolean) } as Partial<MonitorRule>)}
									placeholder="例如：600519 300750"
								/>
							</label>
						)}
						<details className="monitor-advanced">
							<summary><ChevronRight size={14} />更多选项（标的过滤）</summary>
							{template !== 'market' && template !== 'sector' && (
								<div className="monitor-filter-row">
									<ExcludeStToggle value={Boolean(draft.filters?.exclude_st)} onChange={(exclude_st) => patch({ filters: { ...(draft.filters || {}), exclude_st } })} />
									<label><span>最低成交额（亿元）</span><input type="number" value={(draft.filters?.min_amount ?? 0) / 1e8} onChange={(event) => patch({ filters: { ...(draft.filters || {}), min_amount: Number(event.target.value) * 1e8 } })} /></label>
									<label><span>板块</span>
										<MonitorSelect
											value={draft.filters?.boards?.[0] || ''}
											onChange={(next) => patch({ filters: { ...(draft.filters || {}), boards: next ? [next] : [] } })}
											ariaLabel="板块"
											block
											options={[
												{ value: '', label: '全部板块' },
												{ value: 'main', label: '主板' },
												{ value: 'chinext', label: '创业板' },
												{ value: 'star', label: '科创板' },
												{ value: 'bse', label: '北交所' },
											]}
										/>
									</label>
								</div>
							)}
							{template === 'market' || template === 'sector' ? <small className="muted">该模板没有标的维度，无需过滤。</small> : null}
						</details>
						<div className="monitor-filter-row">
							<label><span>级别</span>
								<MonitorSelect
									value={draft.severity ?? 'warning'}
									onChange={(next) => patch({ severity: next as MonitorRule['severity'] })}
									ariaLabel="级别"
									block
									options={[
										{ value: 'info', label: '记录' },
										{ value: 'warning', label: '提醒' },
										{ value: 'critical', label: '紧急' },
									]}
								/>
							</label>
							<label><span>触发后冷却</span><CooldownPicker value={draft.cooldown_seconds ?? 300} onChange={(cooldown_seconds) => patch({ cooldown_seconds })} /></label>
						</div>
					</div>
				)}
				<footer>
					<span className="muted">{test.text || error}</span>
					<div className="monitor-editor-actions">
						{template !== null && <button type="button" className="monitor-small-button" onClick={runTest} disabled={test.busy}><FlaskConical size={13} />试算</button>}
						{template !== null && <button type="button" className="monitor-primary" onClick={save} disabled={test.busy}><Save size={14} />保存</button>}
					</div>
				</footer>
			</div>
		</div>
	);
}

// ---------------------------------------------------------------- Tab 3 关注仓

// WatchTab：监控中心自己的关注仓清单。搜索添加框在页签行右上角（WatchSearchBox），
// 加入后通过 reloadTick 触发这里刷新；备注在行内直接二次编辑。
// 左栏放这批票的概览：涨跌结构、雷达结论、批量入口——右侧专心放清单。
function WatchQuoteSummary({ items }: { items: MonitorWatchItem[] }) {
	useLiveQuoteVersion();
	const streamStatus = useLiveStreamStatus();
	const heldCount = items.length;
	const liveItems = items.map((item) => ({ ...item, change_percent: selectMonitorQuoteValue(monitorLiveBus.get(item.symbol), streamStatus, 'change', item.change_percent) }));
	const quoted = liveItems.filter((item) => typeof item.change_percent === 'number' && Number.isFinite(item.change_percent));
	const upCount = quoted.filter((item) => (item.change_percent as number) > 0).length;
	const downCount = quoted.filter((item) => (item.change_percent as number) < 0).length;
	const flatCount = quoted.length - upCount - downCount;
	const avgChange = quoted.length > 0
		? quoted.reduce((sum, item) => sum + (item.change_percent as number), 0) / quoted.length
		: null;
	const buckets = [
		{ key: 'ge3', label: '≥+3%', match: (value: number) => value >= 3 },
		{ key: 'up', label: '0~3%', match: (value: number) => value > 0 && value < 3 },
		{ key: 'down', label: '-3~0%', match: (value: number) => value < 0 && value > -3 },
		{ key: 'le3', label: '≤-3%', match: (value: number) => value <= -3 },
	].map((bucket) => ({ key: bucket.key, label: bucket.label, count: quoted.filter((item) => bucket.match(item.change_percent as number)).length }));
	buckets.push({ key: 'flat', label: '平盘', count: flatCount }, { key: 'missing', label: '无行情', count: heldCount - quoted.length });
	const maxBucket = Math.max(1, ...buckets.map((bucket) => bucket.count));
	return <><div className="monitor-rail-card">
						<header>
							<Wallet size={14} aria-hidden="true" />
							<strong>关注仓概览</strong>
							<small title={liveStatusLabel(streamStatus)}>{liveStatusLabel(streamStatus) ? quoteConnectionLabel(streamStatus) : quoted.length === heldCount ? '行情就绪' : `${quoted.length}/${heldCount} 有行情`}</small>
						</header>
						<div className="monitor-watch-total">{heldCount}<small>只</small></div>
						<div className="monitor-rail-rows">
							<div><span>上涨 / 下跌 / 平盘</span><b><em className="up">{upCount}</em> / <em className="down">{downCount}</em> / {flatCount}</b></div>
							<div>
								<span title="有行情标的涨跌幅的算术平均，不代表账户收益">等权平均涨跌</span>
								<b className={avgChange === null ? '' : avgChange >= 0 ? 'up' : 'down'}>
									{avgChange === null ? '—' : `${avgChange >= 0 ? '+' : ''}${avgChange.toFixed(2)}%`}
								</b>
							</div>
						</div>
					</div>

					<div className="monitor-rail-card">
						<header>
							<PieChart size={14} aria-hidden="true" />
							<strong>涨跌分布</strong>
							<small>今日涨跌幅分档</small>
						</header>
						<div className="monitor-watch-bars">
							{buckets.map((bucket) => (
								<div className="monitor-watch-bar" key={bucket.key}>
									<em>{bucket.label}</em>
									<span><i className={bucket.key === 'ge3' || bucket.key === 'up' ? 'up' : bucket.key === 'flat' || bucket.key === 'missing' ? 'neutral' : 'down'} style={{ width: `${(bucket.count / maxBucket) * 100}%` }} /></span>
									<b>{bucket.count}</b>
								</div>
							))}
							{quoted.length === 0 && <p className="monitor-rail-note">暂无有效行情，标的计入“无行情”。</p>}
						</div>
					</div>

					</>;
}

function WatchCoverageSummary({ items, coverage, radar, onOpenRadar, onOpenRules }: {
	items: MonitorWatchItem[]; coverage: MonitorWatchCoverage | null; radar: RadarReport | null; onOpenRadar: () => void; onOpenRules: () => void;
}) {
	const codeOf = (symbol: string) => symbol.replace(/\D/g, '').slice(-6);
	const coverageIndex = new Map((coverage?.items ?? []).map((item) => [codeOf(item.symbol), item]));
	const configured = items.filter((item) => coverageIndex.get(codeOf(item.symbol))?.status === 'covered').length;
	const unconfigured = items.filter((item) => coverageIndex.get(codeOf(item.symbol))?.status === 'unconfigured').length;
	const unknown = items.length - configured - unconfigured;
	const radarIndex = new Map((radar?.targets ?? []).map((item) => [codeOf(item.symbol), item]));
	const states = items.map((item) => radarIndex.get(codeOf(item.symbol)));
	const evaluated = states.filter((item) => item?.status === 'evaluated').length;
	const insufficient = states.filter((item) => item?.status === 'insufficient_data').length;
	const pending = items.length - evaluated - insufficient;
	const counts = Object.fromEntries((['opportunity', 'risk', 'watch'] as const).map((kind) => [kind, states.filter((item) => item?.status === 'evaluated' && item.kind === kind).length]));
	return <div className="monitor-rail-card monitor-watch-coverage">
		<header><ShieldAlert size={14} aria-hidden="true" /><strong>监控覆盖</strong><small>{coverage ? coverage.engine.running ? coverage.engine.enabled ? '引擎运行中' : '引擎已暂停' : '引擎未运行' : '状态未提供'}</small></header>
		<div className="monitor-rail-rows">
			<div><span>已配置适用规则</span><b>{coverage ? `${configured} 只` : '未提供'}</b></div>
			<div><span>未配置适用规则</span><b>{coverage ? `${unconfigured} 只` : '未提供'}</b></div>
			<div><span>覆盖待确认</span><b>{coverage ? `${unknown} 只` : '未提供'}</b></div>
			<div><span>雷达已完成评估</span><b>{radar?.targets ? `${evaluated} / ${items.length} 只` : '未提供'}</b></div>
			<div><span>雷达数据不足</span><b>{radar?.targets ? `${insufficient} 只` : '未提供'}</b></div>
			{pending > 0 && radar?.targets && <div><span>等待扫描 / 未返回</span><b>{pending} 只</b></div>}
		</div>
		<p className="monitor-rail-note">覆盖表示已有适用的启用个股规则；提醒还需引擎运行并满足规则条件。</p>
		{(coverage?.error || (coverage && (!coverage.engine.enabled || !coverage.engine.running))) && <p className="monitor-rail-note monitor-data-warning">{coverage.error || '引擎当前未执行提醒，请到触发与规则检查运行状态。'}</p>}
		<div className="monitor-watch-radar" aria-label="关注仓完整雷达结论">
			{(['opportunity', 'risk', 'watch'] as const).map((kind) => <div className={`kind-${kind}`} key={kind}><small>{radarKindLabels[kind]}</small><b>{radar?.targets ? counts[kind] : '—'}</b></div>)}
		</div>
		<p className="monitor-rail-note">{radar?.targets ? `结论生成于 ${formatDataTime(radar.generated_at)}，仅统计已完成评估的标的。` : '逐票雷达结果未提供，暂不统计结论。'}</p>
		{radar?.error && <p className="monitor-rail-note monitor-data-warning">{radar.error}</p>}
		<div className="monitor-segmented monitor-watch-navigation" role="group" aria-label="关注仓监控操作">
			<span className="monitor-segmented-thumb" aria-hidden="true" />
			<button type="button" className="monitor-segmented-item" onClick={onOpenRules}><SlidersHorizontal size={13} aria-hidden="true" />更改规则</button>
			<button type="button" className="monitor-segmented-item" onClick={onOpenRadar}><Radar size={13} aria-hidden="true" />雷达布控</button>
		</div>
	</div>;
}

function WatchTab({ config, refreshKey, reloadTick, onOpenRadar, onOpenRules }: {
	config: BackendConfig | null; refreshKey: number; reloadTick: number; onOpenRadar: () => void; onOpenRules: () => void;
}) {
	const [items, setItems] = useState<MonitorWatchItem[]>([]);
	const [coverage, setCoverage] = useState<MonitorWatchCoverage | null>(null);
	const [toolsOpen, setToolsOpen] = useState(false);
	const [busy, setBusy] = useState(false);
	const [message, setMessage] = useState('');
	const [error, setError] = useState('');
	const loadVersion = useRef(0);
	const [radar, setRadar] = useState<RadarReport | null>(null);
	const [bulk, setBulk] = useState('');
	const [importing, setImporting] = useState(false);
	const [funds, setFunds] = useState<MonitorWatchFundFlowView>({ data: [], meta: null });
	const [fundLoading, setFundLoading] = useState(false);
	const [fundError, setFundError] = useState('');
	const [fundRefresh, setFundRefresh] = useState(0);
	const fundSymbols = useMemo(() => [...new Set(items.map((item) => item.symbol))].sort().join(','), [items]);
	const fundIndex = useMemo(() => new Map(funds.data.map((item) => [item.symbol || item.code, item])), [funds.data]);
	const fundColumns = [
		{ key: 'net_inflow', label: '总净流入', ratio: false },
		{ key: 'net_inflow_ratio', label: '总净流入率', ratio: true },
		{ key: 'main_net_inflow', label: '主力净流入', ratio: false },
		{ key: 'main_net_inflow_ratio', label: '主力净流入率', ratio: true },
		{ key: 'retail_net_inflow', label: '散户净流入', ratio: false },
		{ key: 'retail_net_inflow_ratio', label: '散户净流入率', ratio: true },
	] as const;
	useEffect(() => {
		setFunds({ data: [], meta: null }); setFundError('');
		if (!config || !fundSymbols) { setFundLoading(false); return; }
		const controller = new AbortController();
		let stopped = false, pending = false;
		const loadFunds = async () => {
			if (pending) return;
			pending = true; setFundLoading(true);
			try {
				const payload = await fetchMonitorWatchFunds(config, fundSymbols.split(','), controller.signal);
				if (!stopped) { setFunds(payload); setFundError(''); }
			} catch (cause) {
				if (!stopped) { setFunds({ data: [], meta: null }); setFundError(`资金数据暂不可用：${String((cause as Error).message)}`); }
			} finally {
				pending = false;
				if (!stopped) setFundLoading(false);
			}
		};
		void loadFunds();
		const timer = window.setInterval(() => { if (!document.hidden) void loadFunds(); }, 60000);
		const onVisible = () => { if (!document.hidden) void loadFunds(); };
		document.addEventListener('visibilitychange', onVisible);
		return () => { stopped = true; controller.abort(); window.clearInterval(timer); document.removeEventListener('visibilitychange', onVisible); };
	}, [config, fundSymbols, refreshKey, reloadTick, fundRefresh]);

	const load = useCallback(async (configValue: BackendConfig | null) => {
		if (!configValue) return;
		const version = ++loadVersion.current;
		try {
			const payload = await fetchMonitorWatchlist(configValue);
			if (version !== loadVersion.current) return;
			setItems(payload.data);
			setCoverage(payload.coverage ?? null);
			setError('');
		} catch (cause) {
			if (version !== loadVersion.current) return;
			setCoverage(null);
			setError(String((cause as Error).message));
		}
	}, []);
	useEffect(() => {
		void load(config);
		return () => { loadVersion.current++; };
	}, [load, config, refreshKey, reloadTick]);

	// 雷达结论按标的交叉匹配：告诉用户"我这批票现在处于什么状态"。
	// 拿不到不影响关注仓本身，静默降级。
	useEffect(() => {
		if (!config) return;
		let alive = true;
		const loadRadar = () => fetchMonitorRadar(config)
			.then((payload) => { if (alive) setRadar(payload.data); })
			.catch(() => { if (alive) setRadar(null); });
		void loadRadar();
		const timer = window.setInterval(() => { if (!document.hidden) { void loadRadar(); void load(config); } }, 60000);
		return () => { alive = false; window.clearInterval(timer); };
	}, [config, refreshKey, reloadTick, load]);

	const removeOne = async (symbol: string) => {
		if (!config) return;
		loadVersion.current++;
		setBusy(true); setError('');
		try {
			await removeMonitorWatch(config, symbol);
			loadVersion.current++;
			setItems((current) => current.filter((item) => item.symbol !== symbol));
			setMessage(`已移出 ${symbol}`);
		} catch (cause) {
			setError(String((cause as Error).message));
		} finally {
			setBusy(false);
		}
	};

	const clearAll = async () => {
		if (!config) return;
		const ok = await confirmDialog({
			title: '清空关注仓',
			message: '关注仓里的标的会全部移除。监控规则本身不受影响，仍会按规则继续盯盘。',
			confirmText: '清空',
			tone: 'danger',
		});
		if (!ok) return;
		loadVersion.current++;
		setBusy(true); setError('');
		try {
			await clearMonitorWatch(config);
			loadVersion.current++;
			setItems([]);
			setMessage('关注仓已清空');
		} catch (cause) {
			setError(String((cause as Error).message));
		} finally {
			setBusy(false);
		}
	};

	// 展开编辑多行备注，沿用原 PATCH；Ctrl/Cmd+Enter 保存，Esc 取消。
	const [editing, setEditing] = useState<string | null>(null);
	const [editNote, setEditNote] = useState('');
	const noteBytes = new TextEncoder().encode(editNote.trim()).length;
	const startEdit = (item: MonitorWatchItem) => { setEditing(item.symbol); setEditNote(item.note || ''); };
	const cancelEdit = () => { setEditing(null); setEditNote(''); };
	const toggleNote = (item: MonitorWatchItem) => { if (!busy) { if (editing === item.symbol) cancelEdit(); else startEdit(item); } };
	const saveNote = async (symbol: string) => {
		if (!config || busy || noteBytes > 200) return;
		loadVersion.current++;
		setBusy(true); setError('');
		try {
			await updateMonitorWatchNote(config, symbol, editNote.trim());
			loadVersion.current++;
			setItems((current) => current.map((item) => item.symbol === symbol ? { ...item, note: editNote.trim() } : item));
			setMessage('备注已更新');
			setEditing(null); setEditNote('');
		} catch (cause) {
			setError(String((cause as Error).message));
		} finally {
			setBusy(false);
		}
	};

	const heldCount = items.length;

	// 概览统计全在本页数据上算：不新增接口，行情缺的标的单独计数不参与均涨。
	const codeOf = (symbol: string) => symbol.replace(/\D/g, '').slice(-6);

	// 从持仓导入：巡检/模拟盘持仓里没在关注仓的补进来，备注标明来源。
	const importHoldings = async () => {
		if (!config || importing) return;
		setImporting(true); setError(''); setMessage('');
		try {
			const payload = await fetchMonitorTargets(config);
			const existing = new Set(items.map((item) => codeOf(item.symbol)));
			const missing = (payload.data?.positions ?? []).filter((target) => target.symbol && !existing.has(codeOf(target.symbol)));
			for (const target of missing) {
				await addMonitorWatch(config, { symbol: target.symbol, name: target.name, note: '来自持仓' });
			}
			await load(config);
			setMessage(missing.length > 0 ? `已从持仓导入 ${missing.length} 只` : '持仓里的标的都已在关注仓');
		} catch (cause) {
			setError(String((cause as Error).message));
		} finally {
			setImporting(false);
		}
	};

	// 批量粘贴：只认代码，名称交给行情补全；逗号/空格/换行都能分隔。
	const addBulk = async () => {
		if (!config || busy) return;
		const codes = bulk.split(/[,，\s]+/).map((item) => item.trim()).filter(Boolean);
		if (codes.length === 0) return;
		const existing = new Set(items.map((item) => codeOf(item.symbol)));
		const fresh = codes.filter((code) => !existing.has(codeOf(code)));
		if (fresh.length === 0) { setMessage('这些代码都已在关注仓'); return; }
		setBusy(true); setError(''); setMessage('');
		try {
			for (const code of fresh) {
				await addMonitorWatch(config, { symbol: code });
			}
			setBulk('');
			await load(config);
			setMessage(`已添加 ${fresh.length} 只，名称稍后由行情补全`);
		} catch (cause) {
			setError(String((cause as Error).message));
		} finally {
			setBusy(false);
		}
	};

	return (
		<div className="monitor-watch">
			<div className="monitor-watch-layout">
				<aside className="monitor-rail" aria-label="关注仓概要">
					<WatchQuoteSummary items={items} />
					<WatchCoverageSummary items={items} coverage={coverage} radar={radar} onOpenRadar={onOpenRadar} onOpenRules={onOpenRules} />

					<div className="monitor-rail-card monitor-watch-tools-card">
						<header>
							<SlidersHorizontal size={14} aria-hidden="true" />
							<strong>清单工具</strong>
							<button type="button" className="monitor-link-button" aria-expanded={toolsOpen} aria-controls="monitor-watch-tools" onClick={() => setToolsOpen((open) => !open)}>{toolsOpen ? '收起' : '展开'}{toolsOpen ? <ChevronUp size={14} /> : <ChevronDown size={14} />}</button>
						</header>
						{toolsOpen && <div id="monitor-watch-tools" className="monitor-watch-actions">
							<textarea className="monitor-watch-bulk" value={bulk} rows={2} disabled={busy}
								onChange={(event) => setBulk(event.target.value)}
								placeholder="批量粘贴代码，逗号/空格/换行分隔，如 600519 300750" />
							<div className="monitor-watch-action-row">
								<button type="button" className="monitor-small-button" disabled={importing || busy} onClick={() => void importHoldings()}>
									{importing ? <LoaderCircle className="spin" size={13} /> : <Download size={13} />}从持仓导入
								</button>
								<button type="button" className="monitor-small-button" disabled={busy || !bulk.trim()} onClick={() => void addBulk()}>
									<Plus size={13} />批量添加
								</button>
							</div>
							<p className="monitor-rail-note">名称请用页签右侧搜索框（支持拼音）；这里只按代码添加。</p>
						</div>}
					</div>
				</aside>

				<section className="monitor-panel monitor-watch-panel" aria-label="关注仓清单">
					<header>
						<Star size={17} />
						<div>
							<strong>关注仓</strong>
							<small>{heldCount > 0
								? `共 ${heldCount} 只 · 引擎按 scope=关注仓 的规则与智能雷达自动扫描这份清单`
								: '搜索加入想盯的票，雷达与规则会自动盯上它们'}{heldCount > 0 && (fundLoading ? ' · 资金加载中' : funds.meta ? ` · 资金${funds.meta.stale ? '缓存' : '更新'} ${formatDataTime(funds.meta.fetched_at)}` : ' · 资金待加载')}</small>
						</div>
						{heldCount > 0 && (
							<div className="monitor-feed-tools">
								<button type="button" className="monitor-small-button" disabled={fundLoading} onClick={() => setFundRefresh((value) => value + 1)} title={funds.meta ? `来源 ${funds.meta.source}；每 60 秒自动刷新，更新时间为数据源抓取时间${funds.meta.fallback_reason ? `；${funds.meta.fallback_reason}` : ''}` : '每 60 秒自动刷新资金数据'}>
									{fundLoading ? <LoaderCircle className="spin" size={13} /> : <RefreshCw size={13} />}刷新资金
								</button>
								<button type="button" className="monitor-small-button" disabled={busy} onClick={() => void clearAll()}>
									<Trash2 size={13} />清空关注仓
								</button>
							</div>
						)}
					</header>
			{heldCount > 0 ? (
				<div className="trading-table-wrap monitor-watch-flow-wrap">
					<table className="trading-table trading-table-fixed monitor-watch-table monitor-watch-flow-table">
						<colgroup>
							{Array.from({ length: 10 }, (_, index) => <col key={index} />)}
						</colgroup>
						<thead>
							<tr><th>标的</th><th>价格</th><th>涨跌幅</th><th title="近5个交易日累计涨跌幅；日线不足时显示 --">5日涨跌</th>{fundColumns.map((column) => <th key={column.key}>{column.label}</th>)}</tr>
						</thead>
						<tbody>
							{items.map((item, index) => {
								const editingRow = editing === item.symbol;
								const flow = fundIndex.get(item.symbol);
								const available = (key: string) => Boolean(flow?.meta.available_fields?.includes(key));
								const detailID = `monitor-watch-note-${item.symbol.replace(/\W/g, '')}`;
								return (
									<Fragment key={item.symbol}>
									<tr className={`monitor-watch-flow-row${editingRow ? ' expanded' : ''}`} data-tt-row-id={item.symbol} data-symbol={item.symbol} data-name={item.name || ''} data-stock-context="watchlist" onClick={() => toggleNote(item)}>
										<td className="trading-symbol-cell">
											<button type="button" className="trading-symbol-stack monitor-watch-expand" aria-expanded={editingRow} aria-controls={detailID} aria-label={`${editingRow ? '收起' : '展开'}${item.name || item.symbol}的备注`} disabled={busy} onClick={(event) => { event.stopPropagation(); toggleNote(item); }}>
												<span className="trading-symbol-seq">{index + 1}</span>
												<span className="trading-symbol-body">
													<span className="trading-symbol-line">
														<span className="trading-symbol-name">{item.name || item.symbol}</span>
													</span>
													<span className="trading-symbol-code">{item.symbol}</span>
												</span>
											</button>
										</td>
										<LivePriceCell symbol={item.symbol} kind="price" fallback={item.have_quote ? item.last : available('price') ? flow?.price : undefined} />
										<LivePriceCell symbol={item.symbol} kind="change" fallback={item.change_percent ?? (available('change_percent') ? flow?.change_percent : undefined)} />
										<td data-tt-value={typeof item.momentum_5d === 'number' && Number.isFinite(item.momentum_5d) ? item.momentum_5d : ''} className={typeof item.momentum_5d !== 'number' || !Number.isFinite(item.momentum_5d) ? 'muted' : item.momentum_5d > 0 ? 'up' : item.momentum_5d < 0 ? 'down' : ''}>{typeof item.momentum_5d === 'number' && Number.isFinite(item.momentum_5d) ? `${item.momentum_5d > 0 ? '+' : ''}${item.momentum_5d.toFixed(2)}%` : '--'}</td>
										{fundColumns.map((column) => {
											const value = flow?.[column.key];
											const valid = available(column.key) && typeof value === 'number' && Number.isFinite(value);
											const amount = valid && !column.ratio ? value === 0 ? '0.00亿' : Math.abs(value) >= 1e8 ? `${(value / 1e8).toFixed(2)}亿` : Math.abs(value) >= 1e4 ? `${(value / 1e4).toFixed(1)}万` : value.toLocaleString('zh-CN') : '';
											return <td key={column.key} data-flow-field={column.key} data-tt-value={valid ? value : ''} className={!valid ? 'muted' : value > 0 ? 'up' : value < 0 ? 'down' : ''}>{!valid ? '--' : column.ratio ? `${value > 0 ? '+' : ''}${value.toFixed(value !== 0 && Math.abs(value) < 0.01 ? 4 : 2)}%` : amount}</td>;
										})}
									</tr>
									{editingRow && <tr className="monitor-watch-note-detail-row" data-tt-follow={item.symbol}><td colSpan={10}>
										<div className="monitor-watch-note-editor" id={detailID}>
											<div className="monitor-watch-note-heading"><strong>{item.name || item.symbol} · 自定义备注</strong><small>{item.created_at ? `加入于 ${new Date(item.created_at).toLocaleDateString('zh-CN')}` : '加入时间未提供'}</small></div>
											<textarea className="monitor-watch-note-input" value={editNote} rows={3} autoFocus disabled={busy} onChange={(event) => setEditNote(event.target.value)} aria-label="编辑备注" placeholder="写下关注原因或观察计划，可换行，也可留空"
												onKeyDown={(event) => { if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) { event.preventDefault(); void saveNote(item.symbol); } if (event.key === 'Escape') { event.preventDefault(); if (!busy) cancelEdit(); } }} />
											<div className="monitor-watch-note-actions">
												<small className={noteBytes > 200 ? 'is-over' : ''} title="沿用现有备注长度限制；中文通常占 3 字节，英文字母占 1 字节">{noteBytes} / 200 字节 · Ctrl/Cmd+Enter 保存</small>
												<button type="button" className="monitor-small-button" disabled={busy || noteBytes > 200} onClick={() => void saveNote(item.symbol)}>{busy ? <LoaderCircle className="spin" size={13} /> : <Check size={13} />}保存备注</button>
												<button type="button" className="monitor-small-button" disabled={busy} onClick={cancelEdit}>取消</button>
												<button type="button" className="trading-row-action is-icon is-danger" disabled={busy} title="移出关注仓" aria-label="移出关注仓" onClick={() => void removeOne(item.symbol)}><Trash2 size={13} /></button>
											</div>
										</div>
									</td></tr>}
									</Fragment>
								);
							})}
						</tbody>
					</table>
				</div>
			) : (
				<div className="monitor-empty-state">
					<Star size={26} aria-hidden="true" />
					<strong>关注仓还是空的</strong>
					<span>在右上角搜索加入想盯的票（支持拼音首字母，如 gzmt）；加入后「触发与规则」里的关注仓作用域、智能雷达都会自动盯上它们。</span>
				</div>
			)}
			{message || error || fundError ? (
				<div className="monitor-watch-foot">
					{(error || message) && <span className="muted">{error || message}</span>}
					{fundError && <span className="muted">{fundError}</span>}
				</div>
			) : null}
				</section>
			</div>
		</div>
	);
}
