import { useEffect, useMemo, useState } from 'react';
import type { BackendConfig } from '../lib/backend';

interface VerdictObject {
	name: string;
	effect: string; // 利好 / 利空 / 中性
	role: string; // beneficiary / adverse / indirect
}
interface VerdictData {
	direction: string;
	intensity: string;
	evidence: string;
	fact_stage: string;
	expectation_gap: string;
	horizon: string;
	object_effects: VerdictObject[];
	matched_rules: string[];
	facts: string[];
	inferences: string[];
	assumptions: string[];
	reversal_conditions: string[];
	verify_indicators: string[];
}
interface VerdictItem {
	uid: string;
	title: string;
	source: string;
	published_at: string | null;
	verdict: VerdictData;
	extraction: {
		objects: VerdictObject[];
		transmission_hypotheses: string[];
		confidence: string;
		llm_used: boolean;
	};
}
interface SampleRef {
	uid: string;
	title: string;
	source: string;
	published_at: string | null;
}
interface ThemeStat {
	rank: number;
	name: string;
	heat: number;
	count: number;
	velocity: number;
	diversity: number;
	stage: string;
	sentiment: string;
	bullish: number;
	bearish: number;
	trend?: string; // 加速 / 平稳 / 退潮
	samples: SampleRef[];
}
interface VerdictResponse {
	items: VerdictItem[];
	engine: { configured: boolean; script: string; days: number; limit: number; llm_used: boolean };
}
interface FermentResponse {
	themes: ThemeStat[];
	window_hours: number;
	sources?: Record<string, number>;
	views?: Record<string, ThemeStat[]>;
	windows?: Record<string, ThemeStat[]>;
	engine: { configured: boolean; script: string; window: number; days: number; top: number };
}
interface ThemeStock {
	symbol: string;
	name: string;
	price?: number;
	change_percent: number;
	limit_up_streak?: number;
	rank_role?: string;
	rank_score?: number;
	main_net_inflow?: number;
}
interface ThemeStockGroup {
	board: string;
	count: number;
	stocks: ThemeStock[];
}
interface ThemeStocksResponse {
	theme: string;
	matched: boolean;
	matched_name?: string;
	match_source?: string;
	match_score?: number;
	change_percent?: number;
	limit_up_count?: number;
	leaders?: string[];
	stocks: ThemeStock[];
	groups?: ThemeStockGroup[];
	hints?: string[];
	reason?: string;
	note?: string;
}

// 板块名过长（多细分领域）时截断为「前3 / 等 N 组」，hover 看全量。
function formatBoardNames(name: string): string {
	const parts = name.split(/\s*\/\s*/).filter(Boolean);
	if (parts.length <= 3) return name;
	return `${parts.slice(0, 3).join(' / ')} 等 ${parts.length} 组`;
}

// 单个个股 chip（分组/平铺共用）：名称 + 代码 + 角色 + 涨跌幅/行情缺 + 连板。
function renderStockChip(s: ThemeStock) {
	const hasQuote = (s.price ?? 0) > 0 || (s.change_percent ?? 0) !== 0;
	return (
		<span className="nv-stock" key={s.symbol} title={s.symbol}>
			<b>{s.name}</b>
			<span className="nv-stock-code">{s.symbol}</span>
			{s.rank_role ? <span className="nv-stock-role">{s.rank_role}</span> : null}
			{hasQuote ? (
				<span className={s.change_percent >= 0 ? 'nv-stock-up' : 'nv-stock-down'}>
					{s.change_percent >= 0 ? '+' : ''}
					{Number(s.change_percent ?? 0).toFixed(2)}%
				</span>
			) : (
				<span className="nv-stock-nodata">行情缺</span>
			)}
			{s.limit_up_streak ? (
				<span className="nv-stock-streak">{s.limit_up_streak}板</span>
			) : null}
		</span>
	);
}

const DIRECTION_CLASS: Record<string, string> = {
	利好: 'v-bull',
	利空: 'v-bear',
	中性: 'v-neutral',
	混合: 'v-mixed',
	无法判断: 'v-unknown',
};

// 强度排序权重（逐条研判按强度排序用；verdict 输出的强度取值为 大/中/小）
const INTENSITY_RANK: Record<string, number> = { 大: 3, 中: 2, 小: 1 };

const SOURCE_VIEWS: Array<{ key: string; label: string }> = [
	{ key: 'merged', label: '合并源' },
	{ key: 'easy-stock', label: '实时(easy-stock)' },
	{ key: 'quant', label: '历史(quant)' },
];
const WINDOW_KEYS = ['6', '24', '48'];

function Dimension({ label, value }: { label: string; value: string }) {
	return (
		<div className="v-dim">
			<span className="v-dim-label">{label}</span>
			<span className="v-dim-value">{value}</span>
		</div>
	);
}

function VerdictCard({ item }: { item: VerdictItem }) {
	const v = item.verdict;
	return (
		<article className={`nv-card nv-card-${v.direction}`} key={item.uid}>
			<header className="nv-card-head">
				<span className={`nv-direction ${DIRECTION_CLASS[v.direction] ?? 'v-unknown'}`}>
					{v.direction}
				</span>
				<span className="nv-title" title={item.title}>
					{item.title}
				</span>
				<span className="nv-source">
					{item.source}
					{item.published_at ? ` · ${item.published_at.slice(0, 10)}` : ''}
				</span>
			</header>

			<div className="nv-dimensions">
				<Dimension label="强度" value={v.intensity} />
				<Dimension label="证据可信度" value={`${v.evidence}（${v.fact_stage}）`} />
				<Dimension label="预期差" value={v.expectation_gap} />
				<Dimension label="影响周期" value={v.horizon} />
			</div>

			{v.object_effects.length > 0 && (
				<div className="nv-section">
					<span className="nv-section-label">影响对象</span>
					<div className="nv-objects">
						{v.object_effects.map((o, i) => (
							<span className={`nv-object nv-obj-${o.effect}`} key={i} title={o.role}>
								{o.name}：{o.effect}
								{o.role === 'indirect' ? '（待核实）' : ''}
							</span>
						))}
					</div>
				</div>
			)}

			{v.inferences.length > 0 && (
				<div className="nv-section">
					<span className="nv-section-label">传导链 / 推断</span>
					<ul className="nv-chain">
						{v.inferences.map((inf, i) => (
							<li key={i}>{inf}</li>
						))}
					</ul>
				</div>
			)}

			{v.reversal_conditions.length > 0 && (
				<div className="nv-section">
					<span className="nv-section-label">反证条件（失效）</span>
					<ul className="nv-reversal">
						{v.reversal_conditions.map((rc, i) => (
							<li key={i}>{rc}</li>
						))}
					</ul>
				</div>
			)}

			{v.verify_indicators.length > 0 && (
				<div className="nv-section nv-muted">
					<span className="nv-section-label">验证指标</span>
					<span className="nv-verify">{v.verify_indicators.join('；')}</span>
				</div>
			)}
		</article>
	);
}

export function NewsVerdictWorkspace({
	config,
	refreshKey,
}: {
	config: BackendConfig | null;
	refreshKey: number;
}) {
	const [verdictItems, setVerdictItems] = useState<VerdictItem[]>([]);
	const [verdictMap, setVerdictMap] = useState<Record<string, VerdictItem>>({});
	const [ferment, setFerment] = useState<FermentResponse | null>(null);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState('');
	const [engine, setEngine] = useState<VerdictResponse['engine'] | null>(null);

	const [tab, setTab] = useState<'rank' | 'detail'>('rank');
	const [sourceView, setSourceView] = useState<string>('merged');
	const [windowKey, setWindowKey] = useState<string>('48');
	const [drillTheme, setDrillTheme] = useState<ThemeStat | null>(null);
	const [probeTheme, setProbeTheme] = useState('');
	const [themeStocks, setThemeStocks] = useState<ThemeStocksResponse | null>(null);
	const [stocksLoading, setStocksLoading] = useState(false);
	const [hideUnknown, setHideUnknown] = useState(true);
	const [sortBy, setSortBy] = useState<'heat' | 'intensity'>('intensity');

	const headers = useMemo<Record<string, string>>(() => {
		const h: Record<string, string> = {};
		if (config?.token) h.Authorization = `Bearer ${config.token}`;
		return h;
	}, [config]);

	useEffect(() => {
		if (!config) return;
		const controller = new AbortController();
		setLoading(true);
		setError('');
		const fet = (path: string) =>
			fetch(new URL(path, config.backendUrl).toString(), {
				signal: controller.signal,
				headers,
			}).then((r) => {
				if (!r.ok) {
					return r
						.json()
						.then((j) => {
							throw new Error(j?.error || `HTTP ${r.status}`);
						})
						.catch(() => {
							throw new Error(`HTTP ${r.status}`);
						});
				}
				return r.json();
			});

		Promise.all([
			fet('/api/v1/news/verdict?days=2&limit=100'),
			fet('/api/v1/news/ferment?window=48&days=2&top=20'),
		])
			.then(([vd, fm]: [VerdictResponse, FermentResponse]) => {
				const items = vd.items ?? [];
				const map: Record<string, VerdictItem> = {};
				for (const it of items) map[it.uid] = it;
				setVerdictItems(items);
				setVerdictMap(map);
				setFerment(fm);
				setEngine(vd.engine ?? null);
			})
			.catch((e) => {
				if (e?.name !== 'AbortError') setError(e?.message || String(e));
			})
			.finally(() => setLoading(false));
		return () => controller.abort();
	}, [config, refreshKey, headers]);

	// 下钻题材时拉取「受益个股池」（复用行情侧题材成分）
	// probeTheme：未命中时用户点「行情侧题材参考」改查的题材名。
	useEffect(() => {
		const target = drillTheme ? probeTheme || drillTheme.name : '';
		if (!config || !target) {
			setThemeStocks(null);
			return;
		}
		const controller = new AbortController();
		setStocksLoading(true);
		setThemeStocks(null);
		const url = new URL('/api/v1/news/theme/stocks', config.backendUrl);
		url.searchParams.set('theme', target);
		url.searchParams.set('limit', '20');
		fetch(url.toString(), { signal: controller.signal, headers })
			.then((r) => (r.ok ? r.json() : Promise.reject(new Error(`HTTP ${r.status}`))))
			.then((j: ThemeStocksResponse) => setThemeStocks(j))
			.catch((e) => {
				if (e?.name !== 'AbortError') {
					setThemeStocks({
						theme: target,
						matched: false,
						stocks: [],
						reason: e?.message || String(e),
					});
				}
			})
			.finally(() => setStocksLoading(false));
		return () => controller.abort();
	}, [config, drillTheme, probeTheme, headers]);

	const openDrill = (t: ThemeStat) => {
		setProbeTheme('');
		setDrillTheme(t);
	};
	const closeDrill = () => {
		setProbeTheme('');
		setDrillTheme(null);
	};

	// 当前视图的排行（源切换 + 时间窗切换）
	const rankThemes = useMemo<ThemeStat[]>(() => {
		if (!ferment) return [];
		if (sourceView === 'merged') {
			return ferment.windows?.[windowKey] ?? ferment.themes ?? [];
		}
		return ferment.views?.[sourceView] ?? [];
	}, [ferment, sourceView, windowKey]);

	// 热度条比例基准（取当前视图最大热度，至少 1 防除零）
	const maxHeat = useMemo(
		() => Math.max(1, ...rankThemes.map((t) => t.heat)),
		[rankThemes],
	);

	// 逐条研判：筛选 + 排序（提升信噪比）
	const detailItems = useMemo(() => {
		let list = verdictItems;
		if (hideUnknown) list = list.filter((it) => it.verdict.direction !== '无法判断');
		if (sortBy === 'intensity') {
			list = [...list].sort(
				(a, b) =>
					(INTENSITY_RANK[b.verdict.intensity] ?? 0) -
					(INTENSITY_RANK[a.verdict.intensity] ?? 0),
			);
		}
		return list;
	}, [verdictItems, hideUnknown, sortBy]);

	// 下钻：题材样本 → 对应研判详情
	const drillItems = useMemo(() => {
		if (!drillTheme) return [];
		return drillTheme.samples
			.map((s) => verdictMap[s.uid])
			.filter((it): it is VerdictItem => Boolean(it));
	}, [drillTheme, verdictMap]);

	return (
		<section className="news-verdict-workspace">
			<div className="nv-header">
				<div className="nv-header-top">
					<h2>资讯研判 · 利好/利空五维度</h2>
					{engine && (
						<span className="nv-engine">
							{engine.llm_used ? 'AI 抽取：真实 LLM' : 'AI 抽取：规则弱实现'} ·{' '}
							{engine.configured ? '引擎已配置' : '引擎未配置'}
						</span>
					)}
				</div>
				<p className="nv-subtitle">
					事实 → 对象 → 传导链 → 预期差 → 分周期结论 → 验证。仅研究观察，
					<strong>不生成交易指令</strong>。
				</p>
			</div>

			{!drillTheme && (
				<div className="nv-tabs">
					<button
						type="button"
						className={tab === 'rank' ? 'nv-tab active' : 'nv-tab'}
						onClick={() => setTab('rank')}
					>
						催化排行
					</button>
					<button
						type="button"
						className={tab === 'detail' ? 'nv-tab active' : 'nv-tab'}
						onClick={() => setTab('detail')}
					>
						逐条研判（{verdictItems.length}）
					</button>
				</div>
			)}

			{loading && <div className="nv-loading">研判引擎计算中…</div>}
			{error && <div className="nv-error">加载失败：{error}</div>}
			{!loading && !error && verdictItems.length === 0 && (
				<div className="nv-empty">
					暂无资讯或引擎未配置（设置环境变量 NEWS_FERMENT_DIR 指向 news_ferment 目录）。
				</div>
			)}

			{/* 下钻：题材样本 + 受益个股池 */}
			{drillTheme && (
				<div className="nv-drill">
				<div className="nv-drill-head">
					<button type="button" className="nv-back" onClick={closeDrill}>
						← 返回排行
					</button>
					<span className="nv-drill-title">{drillTheme.name}</span>
					<span className="nv-drill-heat">
						热度 <b>{drillTheme.heat}</b>
					</span>
					<span className={`nv-rank-stage nv-stage-${drillTheme.stage}`}>
						{drillTheme.stage}
					</span>
					{drillTheme.trend && (
						<span className={`nv-trend nv-trend-${drillTheme.trend}`}>
							{drillTheme.trend}
						</span>
					)}
					<span className={`nv-rank-sentiment nv-sent-${drillTheme.sentiment}`}>
						{drillTheme.sentiment}
					</span>
					{probeTheme && probeTheme !== drillTheme.name && (
						<span className="nv-drill-probe">个股池改查行情侧「{probeTheme}」</span>
					)}
				</div>

					{/* 受益个股池（行情侧热门题材龙头池 / 东财板块成分，仅研究观察） */}
					<div className="nv-stocks-block">
						<span className="nv-section-label">
							受益个股池
							{themeStocks?.matched_name ? (
								<span
									className={`nv-match-src nv-match-src-${
										themeStocks.match_source === 'eastmoney-catalog' ? 'catalog' : 'radar'
									}`}
									title={themeStocks.matched_name}
								>
									{themeStocks.match_source === 'eastmoney-catalog'
										? '东财行业/概念'
										: '行情侧题材'}
									「{formatBoardNames(themeStocks.matched_name)}」
								</span>
							) : null}
						</span>
						{stocksLoading && <div className="nv-muted">个股池加载中…</div>}
						{!stocksLoading && themeStocks && themeStocks.matched && themeStocks.stocks.length > 0 && (
							themeStocks.groups && themeStocks.groups.length > 0 ? (
								// 宽题材按细分板块组展示（AI算力 → 算力/CPO/光通信/数据中心/AI芯片…）
								<div className="nv-stock-groups">
									{themeStocks.groups.map((g) => (
										<div className="nv-stock-group" key={g.board}>
											<span className="nv-stock-group-title">
												{g.board}
												<i>{g.count} 只</i>
											</span>
											<div className="nv-stocks">
												{g.stocks.map((s) => renderStockChip(s))}
											</div>
										</div>
									))}
								</div>
							) : (
								<div className="nv-stocks">
									{themeStocks.stocks.map((s) => renderStockChip(s))}
								</div>
							)
						)}
						{!stocksLoading && themeStocks && themeStocks.matched && themeStocks.stocks.length === 0 && (
							<div className="nv-muted">
								{themeStocks.reason ||
									`「${themeStocks.matched_name ?? themeStocks.theme}」当前无成分股数据。`}
							</div>
						)}
						{!stocksLoading && themeStocks && !themeStocks.matched && (
							<div className="nv-muted">
								未匹配到行情侧题材
								{themeStocks.reason ? `（${themeStocks.reason}）` : ''}
								——该资讯题材属宏观/暂无对应板块时属正常。
							</div>
						)}
						{!stocksLoading &&
							themeStocks &&
							!themeStocks.matched &&
							(themeStocks.hints?.length ?? 0) > 0 && (
								<div className="nv-hints">
									<span className="nv-hints-label">行情侧题材参考</span>
									{themeStocks.hints?.map((h) => (
										<button
											type="button"
											className="nv-hint"
											key={h}
											onClick={() => setProbeTheme(h)}
											title="按该行情侧题材重新查询个股池"
										>
											{h}
										</button>
									))}
								</div>
							)}
						{themeStocks?.note && <div className="nv-muted nv-note">{themeStocks.note}</div>}
					</div>

					{/* 该题材样本的逐条研判 */}
					{drillItems.length === 0 ? (
						<div className="nv-empty">该题材样本暂无逐条研判详情。</div>
					) : (
						<div className="nv-list">
							{drillItems.map((it) => (
								<VerdictCard item={it} key={it.uid} />
							))}
						</div>
					)}
				</div>
			)}

			{/* 主视图：催化排行 */}
			{!drillTheme && !loading && !error && tab === 'rank' && (
				<>
					<div className="nv-controls">
						<label className="nv-check">
							数据源
							<select value={sourceView} onChange={(e) => setSourceView(e.target.value)}>
								{SOURCE_VIEWS.map((s) => (
									<option key={s.key} value={s.key}>
										{s.label}
									</option>
								))}
							</select>
						</label>
						<label className="nv-check">
							时间窗
							<select
								value={windowKey}
								disabled={sourceView !== 'merged'}
								onChange={(e) => setWindowKey(e.target.value)}
							>
								{WINDOW_KEYS.map((w) => (
									<option key={w} value={w}>
										{w}h
									</option>
								))}
							</select>
						</label>
						{ferment?.sources && (
							<span className="nv-count">
								源条数：实时 {ferment.sources['easy-stock'] ?? 0} · 历史{' '}
								{ferment.sources.quant ?? 0} · 合并 {ferment.sources.merged ?? 0}
							</span>
						)}
					</div>
					{rankThemes.length === 0 ? (
						<div className="nv-empty">该视图/时间窗内题材样本不足。</div>
					) : (
					<div className="nv-rank">
						{rankThemes.map((t) => (
							<button
								type="button"
								className={`nv-rank-row${t.rank <= 3 ? ' nv-rank-top' : ''}`}
								key={`${t.rank}-${t.name}`}
								onClick={() => openDrill(t)}
								title="点击下钻：受益个股池 + 该题材样本资讯"
							>
								<span className={`nv-rank-no${t.rank <= 3 ? ' nv-rank-no-top' : ''}`}>
									{t.rank}
								</span>
								<span className="nv-rank-name">{t.name}</span>
								{t.trend && (
									<span className={`nv-trend nv-trend-${t.trend}`}>{t.trend}</span>
								)}
								<span className={`nv-rank-stage nv-stage-${t.stage}`}>{t.stage}</span>
								<span className={`nv-rank-sentiment nv-sent-${t.sentiment}`}>
									{t.sentiment}
								</span>
								<span className="nv-rank-heat">
									热度 <b>{t.heat}</b>
									<i
										className="nv-heat-bar"
										style={{ width: `${Math.round((t.heat / maxHeat) * 100)}%` }}
									/>
								</span>
								<span className="nv-rank-meta">
									{t.count}条 · 增速{t.velocity} · {t.diversity}源
								</span>
							</button>
						))}
					</div>
					)}
				</>
			)}

			{/* 逐条研判 */}
			{!drillTheme && !loading && !error && tab === 'detail' && (
				<div className="nv-controls">
					<label className="nv-check">
						<input
							type="checkbox"
							checked={hideUnknown}
							onChange={(e) => setHideUnknown(e.target.checked)}
						/>
						隐藏「无法判断」
					</label>
					<label className="nv-check">
						排序
						<select
							value={sortBy}
							onChange={(e) => setSortBy(e.target.value as 'heat' | 'intensity')}
						>
							<option value="intensity">按强度</option>
							<option value="heat">按热度</option>
						</select>
					</label>
					<span className="nv-count">显示 {detailItems.length} / {verdictItems.length}</span>
				</div>
			)}

			{!drillTheme && !loading && !error && tab === 'detail' && detailItems.length > 0 && (
				<div className="nv-list">
					{detailItems.map((it) => (
						<VerdictCard item={it} key={it.uid} />
					))}
				</div>
			)}
		</section>
	);
}
