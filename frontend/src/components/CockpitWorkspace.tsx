import { AlertTriangle, BarChart3, BookMarked, BookOpen, BrainCircuit, Bot, Flame, Gauge, LayoutDashboard, RefreshCw, Sparkles, WalletCards } from 'lucide-react';
import type { ReactNode } from 'react';
import { useId, useMemo, useState } from 'react';
import type { BackendConfig, MarketEmotionHistory, MarketEmotionPoint, MarketIndexSnapshot, PortfolioInspectionJob, SourceHealth } from '../lib/backend';
import { requestJSON } from '../lib/backend';
import { pollShortTermProgress } from '../lib/short-term-progress';
import {
	type AttentionItem,
	type CockpitTarget,
	buildAttentionItems,
	buildCockpitBriefPrompt,
	cockpitTargetLabels,
	emotionPhaseTone,
	formatPercent,
	latestInspection,
} from '../lib/cockpit';
import { type CockpitResource, useCockpitResource } from '../lib/use-cockpit-resource';
import { CockpitLiveModules, CockpitPatrolPanel } from './CockpitPatrolPanel';
import './CockpitWorkspace.css';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
	onNavigate: (target: CockpitTarget, subView?: string) => void;
	onAskAI: (prompt: string) => void;
	onRefresh: () => void;
};

const quickModules: Array<{ target: CockpitTarget; icon: ReactNode; detail: string }> = [
	{ target: 'market', icon: <BarChart3 size={16} aria-hidden="true" />, detail: '指数、资金、龙虎榜与研报' },
	{ target: 'limit-up', icon: <Flame size={16} aria-hidden="true" />, detail: '连板梯队与情绪周期' },
	{ target: 'themes', icon: <LayoutDashboard size={16} aria-hidden="true" />, detail: '主线题材与个股梯队' },
	{ target: 'portfolio-inspection', icon: <WalletCards size={16} aria-hidden="true" />, detail: '持仓评分与优化建议' },
	{ target: 'stock-ai', icon: <BrainCircuit size={16} aria-hidden="true" />, detail: '个股分级研究' },
	{ target: 'reviews', icon: <BookOpen size={16} aria-hidden="true" />, detail: '大V观点与每日共识' },
	{ target: 'mastery', icon: <BookMarked size={16} aria-hidden="true" />, detail: '游资心法资料库' },
	{ target: 'ai', icon: <Bot size={16} aria-hidden="true" />, detail: '基于本地数据的 AI 对话' },
];

const loadIndexes = async (config: BackendConfig, signal: AbortSignal, publish: (data: MarketIndexSnapshot[]) => void) => {
	const payload = await requestJSON<{ data: MarketIndexSnapshot[] }>(config, '/api/v1/market/indexes?scope=core', { signal });
	publish(payload.data || []);
};

const loadEmotion = async (config: BackendConfig, signal: AbortSignal, publish: (data: MarketEmotionHistory) => void) => {
	await pollShortTermProgress<MarketEmotionHistory>(config, '/api/v1/short-term/emotion-history', (value) => {
		if (value.data) publish(value.data);
	}, { signal, refresh: false, budgetMS: 160_000 });
};

const loadInspections = async (config: BackendConfig, signal: AbortSignal, publish: (data: PortfolioInspectionJob[]) => void) => {
	const payload = await requestJSON<{ data: PortfolioInspectionJob[] }>(config, '/api/v1/portfolio-inspections?limit=5', { signal });
	publish(payload.data || []);
};

const loadSources = async (config: BackendConfig, signal: AbortSignal, publish: (data: SourceHealth[]) => void) => {
	const payload = await requestJSON<{ sources: SourceHealth[] }>(config, '/api/v1/sources', { signal });
	publish(payload.sources || []);
};

function clock(iso: string) {
	if (!iso) return '未加载';
	return new Date(iso).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false });
}

// 维度分统一按区间着色：75 以上健康、60 以上中性、其余偏弱；只表达分数区间，不改写业务结论。
function scoreTone(score: number) {
	if (score >= 75) return 'is-good';
	if (score >= 60) return 'is-mid';
	return 'is-poor';
}

function stateLabel<T>(resource: CockpitResource<T>) {
	if (resource.state === 'loading' && !resource.data) return '加载中';
	if (resource.state === 'error' && !resource.data) return resource.error || '暂不可用';
	return resource.receivedAt ? `${clock(resource.receivedAt)} 更新` : '';
}

// 面板外壳沿用系统工作台的写法：标题栏 + 内容区，内容区自己滚动。
// 有 onOpen 时整卡可点跳转，不再显示右下角的跳转字样按钮。
function Card({ title, icon, resource, onOpen, openLabel, children, className = '', action }: { title: string; icon: ReactNode; resource?: CockpitResource<unknown>; onOpen?: () => void; openLabel?: string; children: ReactNode; className?: string; action?: ReactNode }) {
	return (
		<section
			className={`cockpit-card ${className}${onOpen ? ' is-clickable' : ''}`.trim()}
			role={onOpen ? 'link' : undefined}
			tabIndex={onOpen ? 0 : undefined}
			aria-label={onOpen ? `${title}，打开${openLabel || '对应模块'}` : undefined}
			onClick={onOpen}
			onKeyDown={onOpen ? (event) => {
				if (event.key === 'Enter' || event.key === ' ') {
					event.preventDefault();
					onOpen();
				}
			} : undefined}
		>
			<header>
				<div className="cockpit-card-title">{icon}<h3>{title}</h3></div>
				{action}
				{resource && <small className={resource.state === 'error' && !resource.data ? 'is-error' : ''}>{stateLabel(resource)}</small>}
			</header>
			<div className="cockpit-card-body">{children}</div>
		</section>
	);
}

// 情绪分走势：渐变面积 + 平滑曲线 + 末端呼吸点；指针扫过时给出当日日期、分数与阶段。
function EmotionSpark({ points }: { points: MarketEmotionPoint[] }) {
	const [hover, setHover] = useState<number | null>(null);
	const gradientID = `cockpit-spark-${useId().replace(/[^a-zA-Z0-9]/g, '')}`;
	if (points.length < 2) return <p className="cockpit-muted">情绪历史样本不足</p>;

	const width = 300;
	const height = 108;
	const padX = 8;
	const padY = 14;
	const scores = points.map((point) => point.emotion_score);
	const min = Math.min(...scores);
	const max = Math.max(...scores);
	const span = max - min || 1;
	const xs = points.map((_, index) => padX + (index / (points.length - 1)) * (width - padX * 2));
	const ys = points.map((point) => height - padY - ((point.emotion_score - min) / span) * (height - padY * 2));
	const line = smoothPath(xs, ys);
	const area = `${line} L ${xs[xs.length - 1].toFixed(2)} ${height} L ${xs[0].toFixed(2)} ${height} Z`;
	const last = points.length - 1;
	const active = hover !== null && points[hover] ? hover : null;
	const readout = active !== null ? points[active] : points[last];

	const locate = (event: React.PointerEvent<SVGSVGElement>) => {
		const rect = event.currentTarget.getBoundingClientRect();
		if (!rect.width) return;
		const x = ((event.clientX - rect.left) / rect.width) * width;
		let nearest = 0;
		for (let index = 1; index < xs.length; index += 1) {
			if (Math.abs(xs[index] - x) < Math.abs(xs[nearest] - x)) nearest = index;
		}
		setHover(nearest);
	};

	return (
		<div className="cockpit-spark">
			<svg
				viewBox={`0 0 ${width} ${height}`}
				role="img"
				aria-label={`近 ${points.length} 个交易日情绪分趋势，最新 ${scores[last].toFixed(1)}`}
				onPointerMove={locate}
				onPointerLeave={() => setHover(null)}
			>
				<defs>
					<linearGradient id={gradientID} x1="0" y1="0" x2="0" y2="1">
						<stop offset="0%" stopColor="currentColor" stopOpacity="0.24" />
						<stop offset="100%" stopColor="currentColor" stopOpacity="0" />
					</linearGradient>
				</defs>
				<path d={area} fill={`url(#${gradientID})`} />
				<path d={line} className="cockpit-spark-line" fill="none" />
				{active !== null && <line className="cockpit-spark-guide" x1={xs[active]} y1={padY - 6} x2={xs[active]} y2={height - padY + 6} />}
				<circle className="cockpit-spark-halo" cx={xs[last]} cy={ys[last]} r="6" />
				<circle className="cockpit-spark-dot" cx={xs[last]} cy={ys[last]} r="3" />
				{active !== null && active !== last && <circle className="cockpit-spark-dot" cx={xs[active]} cy={ys[active]} r="3" />}
			</svg>
			<p className="cockpit-spark-read">
				<time>{readout.trade_date.slice(5)}</time>
				<strong>{readout.emotion_score.toFixed(1)}</strong>
				<span>{readout.phase}</span>
			</p>
		</div>
	);
}

// 相邻点用 Catmull-Rom 转三次贝塞尔，曲线经过每个真实数据点，不做拟合或补点。
function smoothPath(xs: number[], ys: number[]) {
	let path = `M ${xs[0].toFixed(2)} ${ys[0].toFixed(2)}`;
	for (let index = 0; index < xs.length - 1; index += 1) {
		const p0x = xs[index - 1] ?? xs[index];
		const p0y = ys[index - 1] ?? ys[index];
		const p1x = xs[index];
		const p1y = ys[index];
		const p2x = xs[index + 1];
		const p2y = ys[index + 1];
		const p3x = xs[index + 2] ?? xs[index + 1];
		const p3y = ys[index + 2] ?? ys[index + 1];
		const c1x = p1x + (p2x - p0x) / 6;
		const c1y = p1y + (p2y - p0y) / 6;
		const c2x = p2x - (p3x - p1x) / 6;
		const c2y = p2y - (p3y - p1y) / 6;
		path += ` C ${c1x.toFixed(2)} ${c1y.toFixed(2)}, ${c2x.toFixed(2)} ${c2y.toFixed(2)}, ${p2x.toFixed(2)} ${p2y.toFixed(2)}`;
	}
	return path;
}

export function CockpitWorkspace({ config, refreshKey, onNavigate, onAskAI, onRefresh }: Props) {
	const indexes = useCockpitResource<MarketIndexSnapshot[]>(config, refreshKey, loadIndexes);
	const emotion = useCockpitResource<MarketEmotionHistory>(config, refreshKey, loadEmotion);
	const inspections = useCockpitResource<PortfolioInspectionJob[]>(config, refreshKey, loadInspections);
	const sources = useCockpitResource<SourceHealth[]>(config, refreshKey, loadSources);

	const input = useMemo(() => ({
		indexes: indexes.data,
		emotion: emotion.data,
		inspections: inspections.data,
		sources: sources.data,
	}), [indexes.data, emotion.data, inspections.data, sources.data]);

	const attention: AttentionItem[] = useMemo(() => buildAttentionItems(input), [input]);
	const pendingCount = attention.filter((item) => item.level !== 'info').length;
	const anyLoaded = Boolean(indexes.data || emotion.data || inspections.data || sources.data);

	const headIndex = indexes.data?.find((item) => item.name.includes('上证')) || indexes.data?.[0];
	const latest = emotion.data?.latest;
	const raw = latest?.raw;
	const inspection = latestInspection(inspections.data);
	const conclusion = inspection?.report?.conclusion;
	const dimensions = (conclusion?.dimensions ?? []).filter((dimension) => Number.isFinite(dimension.score));
	const healthyCount = (sources.data || []).filter((source) => source.ok).length;
	const sourceTotal = sources.data?.length || 0;
	const updatedAt = [indexes, emotion, inspections, sources].map((item) => item.receivedAt).filter(Boolean).sort().pop() || '';

	return (
		<div className="cockpit">
			{/* 顶部状态条：只放「此刻的结论性读数 + 操作」，替代原先的横幅，避免与系统顶部栏重复。 */}
			<header className="cockpit-statusbar" aria-label="今日态势">
				<div className="cockpit-status-lead">
					<span className="cockpit-eyebrow">今日态势</span>
					{pendingCount ? (
						<strong className="is-pending">
							<em>{pendingCount}</em>
							<span>项需要你处理</span>
						</strong>
					) : (
						<strong className="is-clear">{anyLoaded ? '当前没有需要处理的事项' : '正在汇总各模块数据'}</strong>
					)}
				</div>

				<dl className="cockpit-pulse">
					<div>
						<dt>{headIndex?.name || '核心指数'}</dt>
						<dd className={headIndex ? (headIndex.change_percent >= 0 ? 'is-up' : 'is-down') : ''}>{headIndex ? formatPercent(headIndex.change_percent) : '--'}</dd>
					</div>
					<div>
						<dt>涨停 / 跌停</dt>
						<dd>{raw ? `${raw.limit_up_count} / ${raw.limit_down_count}` : '--'}</dd>
					</div>
					<div>
						<dt>情绪阶段</dt>
						<dd className={`tone-${emotionPhaseTone(latest?.phase)}`}>{latest?.phase || '--'}</dd>
					</div>
					<div>
						<dt>数据源</dt>
						<dd className={sourceTotal && healthyCount < sourceTotal ? 'is-warn' : ''}>{sourceTotal ? `${healthyCount}/${sourceTotal}` : '--'}</dd>
					</div>
				</dl>

				<div className="cockpit-status-actions">
					<small>{updatedAt ? `${clock(updatedAt)} 更新` : '未更新'}</small>
					<button type="button" className="cockpit-icon-button" onClick={onRefresh} aria-label="刷新总控台数据" title="刷新总控台数据">
						<RefreshCw size={15} aria-hidden="true" />
					</button>
				</div>
			</header>

			<div className="cockpit-body">
				{/* 主区：AI 巡查是驾驶舱的主体内容，自己滚动。 */}
				<div className="cockpit-center">
					{/* 简报动作放主区最上方：放在长面板底部会被直接忽略。 */}
					<Card
						title="AI 总控简报"
						icon={<Sparkles size={17} aria-hidden="true" />}
						className="cockpit-brief"
						action={
							<button
								type="button"
								className="cockpit-primary"
								disabled={!anyLoaded}
								title="只用本页已加载的数据，按优先级给出结论并标注来源模块"
								onClick={() => onAskAI(buildCockpitBriefPrompt(input, attention))}
							>
								<Sparkles size={15} aria-hidden="true" />生成 AI 简报
							</button>
						}
					>
						{null}
					</Card>

					<CockpitPatrolPanel config={config} refreshKey={refreshKey} />

					{/* 图表与快讯是固定模块：自己按分钟级采集，不跟着巡查记录的产出节奏走。 */}
					<CockpitLiveModules config={config} refreshKey={refreshKey} onNavigate={onNavigate} />
				</div>

				{/* 侧栏：盯盘用，密度高于主区，独立滚动。 */}
				<aside className="cockpit-rail" aria-label="盯盘侧栏">
					<Card title="需要你处理" icon={<AlertTriangle size={17} aria-hidden="true" />} className="cockpit-attention">
						{!anyLoaded ? <p className="cockpit-muted">等待模块数据…</p> : (
							<ul className="cockpit-attention-list">
								{attention.map((item) => (
									<li key={item.id} className={`level-${item.level}`}>
										<button type="button" disabled={!item.target} onClick={() => item.target && onNavigate(item.target)}>
											<span className="cockpit-level">{item.level === 'danger' ? '高' : item.level === 'warn' ? '中' : '正常'}</span>
											<span className="cockpit-attention-text"><strong>{item.title}</strong><small>{item.detail}</small></span>
											{item.target && <em>{cockpitTargetLabels[item.target]}</em>}
										</button>
									</li>
								))}
							</ul>
						)}
					</Card>

					<Card title="短线情绪" icon={<Flame size={17} aria-hidden="true" />} resource={emotion} onOpen={() => onNavigate('limit-up')} openLabel="短线连板">
						{latest ? (
							<>
								<div className="cockpit-emotion-head">
									<strong className={`tone-${emotionPhaseTone(latest.phase)}`}>{latest.phase}</strong>
									<span>情绪分 <b>{Math.round(latest.emotion_score)}</b></span>
									<span>最高 <b>{raw?.max_streak ?? '--'}</b> 板</span>
								</div>
								<EmotionSpark points={emotion.data?.points.slice(-12) ?? []} />
							</>
						) : <p className="cockpit-muted">{stateLabel(emotion) || '暂无情绪数据'}</p>}
					</Card>

					<Card className="is-roomy" title="持仓健康" icon={<WalletCards size={17} aria-hidden="true" />} resource={inspections} onOpen={() => onNavigate('portfolio-inspection')} openLabel="持仓巡检">
						{conclusion ? (
							<>
								<div className="cockpit-health">
									<strong>{conclusion.health_score}</strong>
									<div><span>健康分</span><em className={conclusion.risk_level?.includes('高') ? 'is-down' : ''}>风险 {conclusion.risk_level}</em></div>
								</div>
								{dimensions.length > 0 && (
									<ul className="cockpit-dims" aria-label="健康分维度">
										{dimensions.map((dimension) => (
											<li key={dimension.key} title={dimension.reason}>
												<span>{dimension.label}</span>
												<b>{Math.round(dimension.score)}</b>
												<i><b className={scoreTone(dimension.score)} style={{ width: `${Math.max(2, Math.min(100, dimension.score))}%` }} /></i>
												<small>{dimension.weight}%</small>
											</li>
										))}
									</ul>
								)}
								{conclusion.primary_risks?.[0] ? (
									<p className="cockpit-risk" title={conclusion.primary_risks[0]}>
										<span>主要风险</span>
										{conclusion.primary_risks[0]}
									</p>
								) : null}
								<small className="cockpit-score-note">分数由持仓巡检算法评分，非实时行情</small>
							</>
						) : <p className="cockpit-muted">{inspection ? `最近巡检状态：${inspection.status}` : inspections.data ? '暂无巡检记录' : (stateLabel(inspections) || '加载中')}</p>}
					</Card>

					<section className="cockpit-modules" aria-label="模块入口">
						<div className="cockpit-section-title"><Gauge size={15} aria-hidden="true" /><h3>模块入口</h3></div>
						<div className="cockpit-module-list">
							{quickModules.map((module) => (
								<button type="button" key={module.target} title={module.detail} onClick={() => onNavigate(module.target)}>
									{module.icon}
									<span>{cockpitTargetLabels[module.target]}</span>
								</button>
							))}
						</div>
					</section>

				</aside>
			</div>
		</div>
	);
}
