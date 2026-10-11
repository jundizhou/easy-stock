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
	samples: SampleRef[];
}
interface VerdictResponse {
	items: VerdictItem[];
	engine: { configured: boolean; script: string; days: number; limit: number; llm_used: boolean };
}
interface FermentResponse {
	themes: ThemeStat[];
	window_hours: number;
	engine: { configured: boolean; script: string; window: number; days: number; top: number };
}

const DIRECTION_CLASS: Record<string, string> = {
	利好: 'v-bull',
	利空: 'v-bear',
	中性: 'v-neutral',
	混合: 'v-mixed',
	无法判断: 'v-unknown',
};

// 强度排序权重（逐条研判按强度排序用）
const INTENSITY_RANK: Record<string, number> = { 强: 3, 中: 2, 弱: 1 };

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
		<article className="nv-card" key={item.uid}>
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
	const [themes, setThemes] = useState<ThemeStat[]>([]);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState('');
	const [engine, setEngine] = useState<VerdictResponse['engine'] | null>(null);

	const [tab, setTab] = useState<'rank' | 'detail'>('rank');
	const [drillTheme, setDrillTheme] = useState<ThemeStat | null>(null);
	const [hideUnknown, setHideUnknown] = useState(true);
	const [sortBy, setSortBy] = useState<'heat' | 'intensity'>('intensity');

	useEffect(() => {
		if (!config) return;
		const controller = new AbortController();
		setLoading(true);
		setError('');
		const headers: Record<string, string> = {};
		if (config.token) headers.Authorization = `Bearer ${config.token}`;
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
				setThemes(fm.themes ?? []);
				setEngine(vd.engine ?? null);
			})
			.catch((e) => {
				if (e?.name !== 'AbortError') setError(e?.message || String(e));
			})
			.finally(() => setLoading(false));
		return () => controller.abort();
	}, [config, refreshKey]);

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
				<h2>资讯研判 · 利好/利空五维度</h2>
				<p className="nv-subtitle">
					事实 → 对象 → 传导链 → 预期差 → 分周期结论 → 验证。仅研究观察，
					<strong>不生成交易指令</strong>。
					{engine && (
						<span className="nv-engine">
							{' '}
							{engine.llm_used ? 'AI 抽取：真实 LLM' : 'AI 抽取：规则弱实现'} ·{' '}
							{engine.configured ? '引擎已配置' : '引擎未配置'}
						</span>
					)}
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

			{/* 下钻：题材样本明细 */}
			{drillTheme && (
				<div className="nv-drill">
					<div className="nv-drill-head">
						<button type="button" className="nv-back" onClick={() => setDrillTheme(null)}>
							← 返回排行
						</button>
						<span className="nv-drill-title">
							{drillTheme.name} · {drillTheme.stage} · 热度 {drillTheme.heat}
						</span>
						<span className={`nv-drill-sentiment nv-sent-${drillTheme.sentiment}`}>
							{drillTheme.sentiment}
						</span>
					</div>
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
			{!drillTheme && !loading && !error && tab === 'rank' && themes.length > 0 && (
				<div className="nv-rank">
					{themes.map((t) => (
						<button
							type="button"
							className="nv-rank-row"
							key={t.rank}
							onClick={() => setDrillTheme(t)}
							title="点击下钻到该题材样本资讯"
						>
							<span className="nv-rank-no">{t.rank}</span>
							<span className="nv-rank-name">{t.name}</span>
							<span className={`nv-rank-stage nv-stage-${t.stage}`}>{t.stage}</span>
							<span className={`nv-rank-sentiment nv-sent-${t.sentiment}`}>
								{t.sentiment}
							</span>
							<span className="nv-rank-heat">
								热度 <b>{t.heat}</b>
							</span>
							<span className="nv-rank-meta">
								{t.count}条 · 增速{t.velocity} · {t.diversity}源
							</span>
						</button>
					))}
				</div>
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
