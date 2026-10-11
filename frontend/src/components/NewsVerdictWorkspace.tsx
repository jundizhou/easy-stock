import { useEffect, useState } from 'react';
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
interface VerdictResponse {
	items: VerdictItem[];
	engine: { configured: boolean; script: string; days: number; limit: number; llm_used: boolean };
}

const DIRECTION_CLASS: Record<string, string> = {
	利好: 'v-bull',
	利空: 'v-bear',
	中性: 'v-neutral',
	混合: 'v-mixed',
	无法判断: 'v-unknown',
};

function Dimension({ label, value }: { label: string; value: string }) {
	return (
		<div className="v-dim">
			<span className="v-dim-label">{label}</span>
			<span className="v-dim-value">{value}</span>
		</div>
	);
}

export function NewsVerdictWorkspace({
	config,
	refreshKey,
}: {
	config: BackendConfig | null;
	refreshKey: number;
}) {
	const [items, setItems] = useState<VerdictItem[]>([]);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState('');
	const [engine, setEngine] = useState<VerdictResponse['engine'] | null>(null);

	useEffect(() => {
		if (!config) return;
		const controller = new AbortController();
		setLoading(true);
		setError('');
		fetch(new URL('/api/v1/news/verdict?days=2&limit=100', config.backendUrl).toString(), {
			signal: controller.signal,
			headers: config.token ? { Authorization: `Bearer ${config.token}` } : {},
		})
			.then((r) => {
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
			})
			.then((data: VerdictResponse) => {
				setItems(data.items ?? []);
				setEngine(data.engine ?? null);
			})
			.catch((e) => {
				if (e?.name !== 'AbortError') setError(e?.message || String(e));
			})
			.finally(() => setLoading(false));
		return () => controller.abort();
	}, [config, refreshKey]);

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

			{loading && <div className="nv-loading">研判引擎计算中…</div>}
			{error && <div className="nv-error">加载失败：{error}</div>}
			{!loading && !error && items.length === 0 && (
				<div className="nv-empty">
					暂无资讯或引擎未配置（设置环境变量 NEWS_FERMENT_DIR 指向 news_ferment 目录）。
				</div>
			)}

			<div className="nv-list">
				{items.map((item) => {
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
				})}
			</div>
		</section>
	);
}
