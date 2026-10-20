import { Activity, AlertTriangle, ArrowLeftRight, BarChart3, Bot, ChevronDown, ChevronUp, Clock3, Compass, Gauge, LoaderCircle, Newspaper, Play, Sparkles, TrendingUp } from 'lucide-react';
import { type ReactNode, useEffect, useMemo, useState } from 'react';
import type { BackendConfig } from '../lib/backend';
import { type CockpitTarget, cockpitTargetLabels, formatPercent } from '../lib/cockpit';
import {
	type PatrolBrief,
	type PatrolEmotion,
	type PatrolFacts,
	type PatrolFlowBoard,
	type PatrolFlowItem,
	type PatrolIndexQuote,
	type PatrolIntraday,
	type PatrolLimitUp,
	clockText,
	foldTimeline,
	latestBriefFor,
	narrativeNotice,
	narrativeVerifyRule,
	intradaySummary,
	stripLeadingTitle,
	patrolPhaseLabel,
	patrolPhases,
	sessionIntradayBriefs,
	TIMELINE_FOLD_LIMIT,
} from '../lib/cockpit-patrol';
import { useCockpitLiveFacts } from '../lib/use-cockpit-live-facts';
import { useCockpitPatrol } from '../lib/use-cockpit-patrol';
import { PanelErrorBoundary } from './PanelErrorBoundary';
import './CockpitPatrolPanel.css';

// 欧洲三项指数已不在采集口径内；不同上游的代码不一致（FTSE/UKX），同时按名称兜底。
const OMITTED_OVERSEAS_CODES = new Set(['FTSE', 'UKX', 'GDAXI', 'DAX', 'FCHI', 'CAC']);
const OMITTED_OVERSEAS_NAMES = new Set(['英国富时100', '富时100', '德国DAX30', '德国DAX', '法国CAC40', '法国CAC']);

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
};

type PatrolPhaseKey = (typeof patrolPhases)[number]['phase'];

const triggerText: Record<string, string> = { scheduled: '定时巡查', manual: '手动巡查' };

// 金额按上游原始单位（元）换算显示；只改变显示精度，不改变数值。
export function formatMoney(value: number) {
	const abs = Math.abs(value);
	if (abs >= 1e8) return `${(abs / 1e8).toFixed(2)} 亿`;
	if (abs >= 1e4) return `${(abs / 1e4).toFixed(0)} 万`;
	return abs.toFixed(0);
}

// 事实卡：既是图表容器，也是进入对应工作台的入口。整卡可点，悬停时才露出目标页名称。
function Fact({ title, icon, source, badge, children, wide = false, onOpen, openLabel, centerBadge = false }: { title: string; icon?: ReactNode; source?: string; badge?: ReactNode; children: ReactNode; wide?: boolean; onOpen?: () => void; openLabel?: string; centerBadge?: boolean }) {
	return (
		<section
			className={`patrol-fact${wide ? ' is-wide' : ''}${onOpen ? ' is-clickable' : ''}`}
			role={onOpen ? 'link' : undefined}
			tabIndex={onOpen ? 0 : undefined}
			aria-label={onOpen && openLabel ? `${title}，打开${openLabel}` : undefined}
			onClick={onOpen}
			onKeyDown={onOpen ? (event) => {
				if (event.key === 'Enter' || event.key === ' ') {
					event.preventDefault();
					onOpen();
				}
			} : undefined}
		>
			<header className={centerBadge ? 'is-center-badge' : undefined}>
				{centerBadge ? (
					// 三栏网格：标题 | 居中徽标 | 右侧动作，让徽标与标题同一行且水平居中。
					<>
						<div className="patrol-fact-title">
							{icon}
							<h4>{title}</h4>
						</div>
						{badge ? <div className="patrol-fact-badge">{badge}</div> : null}
						<div className="patrol-fact-meta">
							{source ? <small>{source}</small> : null}
						</div>
					</>
				) : (
					<>
						<div className="patrol-fact-title">
							{icon}
							<h4>{title}</h4>
							{badge}
						</div>
						<div className="patrol-fact-meta">
							{source ? <small>{source}</small> : null}
						</div>
					</>
				)}
			</header>
			{children}
		</section>
	);
}

// 指数：能量条。条形长度 = |涨跌幅| / 同一最大绝对值，海外与 A 股共用一把尺子，
// 红涨绿跌只看颜色，幅度直接比长度。
function IndexGroup({ caption, items, maxAbs }: { caption: string; items?: PatrolIndexQuote[] | null; maxAbs: number }) {
	const rows = items ?? [];
	if (!rows.length) return null;
	const ups = rows.filter((item) => item.change_percent > 0).length;
	const downs = rows.filter((item) => item.change_percent < 0).length;
	return (
		<div className="index-group">
			<header>
				<h5>{caption}</h5>
				<span className="index-breadth">
					<em className="is-up">{ups} 涨</em>
					<em className="is-down">{downs} 跌</em>
				</span>
			</header>
			<ul className="index-list" aria-label={`${caption}：${rows.map((item) => `${item.name} ${formatPercent(item.change_percent)}`).join('，')}`}>
				{rows.map((item) => {
					const change = Number.isFinite(item.change_percent) ? item.change_percent : 0;
					const width = maxAbs ? Math.min(100, (Math.abs(change) / maxAbs) * 100) : 0;
					const up = change >= 0;
					return (
						<li key={item.code || item.name} title={`${item.name} ${typeof item.price === 'number' ? item.price.toFixed(2) : '--'} ${formatPercent(change)}`}>
							<span className="index-name">{item.name}</span>
							<span className="index-price">{typeof item.price === 'number' ? item.price.toFixed(2) : '--'}</span>
							<span className="index-track" aria-hidden="true">
								<i className={`index-fill ${up ? 'is-up' : 'is-down'}`} style={change === 0 ? undefined : { width: `max(3px, ${width}%)` }} />
							</span>
							<em className={`index-change ${up ? 'is-up' : 'is-down'}`}>{formatPercent(change)}</em>
						</li>
					);
				})}
			</ul>
		</div>
	);
}

// 涨停梯队：四项同单位计数用柱形对比高度，最高连板单独作为读数。
function LadderColumns({ limitUp, previousMax }: { limitUp: PatrolLimitUp; previousMax?: number }) {
	const rows = [
		{ label: '涨停', value: limitUp.limit_up },
		{ label: '首板', value: limitUp.first_board },
		{ label: '连板', value: limitUp.board_count },
		{ label: '回封', value: limitUp.reopened_count },
	];
	const max = Math.max(1, ...rows.map((row) => row.value));
	return (
		<div className="ladder">
			<div className="ladder-hero">
				<strong>
					{limitUp.max_streak}
					<small>板</small>
				</strong>
				<span>最高连板{typeof previousMax === 'number' && previousMax > 0 ? ` · 上日 ${previousMax} 板` : ''}</span>
			</div>
			<div className="ladder-cols" aria-label={rows.map((row) => `${row.label} ${row.value}`).join('，')}>
				{rows.map((row) => (
					<div className="ladder-col" key={row.label}>
						<em>{row.value}</em>
						<span className="ladder-plot">
							<i style={{ height: `${(row.value / max) * 100}%` }} />
						</span>
						<span className="ladder-label">{row.label}</span>
					</div>
				))}
			</div>
		</div>
	);
}

// 情绪周期：涨停 / 跌停 / 炸板 的构成用环形图，中心放情绪分，旁边按数量与占比列出。
function EmotionDonut({ emotion }: { emotion: PatrolEmotion }) {
	const parts = [
		{ label: '涨停', value: emotion.limit_up, tone: 'up' },
		{ label: '跌停', value: emotion.limit_down, tone: 'down' },
		{ label: '炸板', value: emotion.broken, tone: 'warn' },
	];
	const total = parts.reduce((sum, part) => sum + Math.max(0, part.value), 0);
	const radius = 50;
	const circumference = 2 * Math.PI * radius;
	let cursor = 0;
	const segments = parts.map((part) => {
		const length = total ? (Math.max(0, part.value) / total) * circumference : 0;
		const segment = { ...part, length, offset: cursor };
		cursor += length;
		return segment;
	});
	return (
		<>
			<div className="emotion">
				<div className="emotion-donut">
					<svg viewBox="0 0 120 120" role="img" aria-label={parts.map((part) => `${part.label} ${part.value}`).join('，')}>
						<circle className="emotion-track" cx="60" cy="60" r={radius} />
						{segments.map((segment) => (
							<circle
								key={segment.label}
								className={`tone-${segment.tone}`}
								cx="60"
								cy="60"
								r={radius}
								strokeDasharray={`${Math.max(1, segment.length - 2.5)} ${circumference}`}
								strokeDashoffset={-segment.offset}
								transform="rotate(-90 60 60)"
							/>
						))}
					</svg>
					<div className="emotion-center">
						<strong>{Number.isFinite(emotion.score) ? emotion.score.toFixed(1) : '--'}</strong>
						<span>情绪分</span>
					</div>
				</div>
				<ul className="emotion-legend">
					{segments.map((segment) => (
						<li key={segment.label}>
							<i className={`tone-${segment.tone}`} aria-hidden="true" />
							<span>{segment.label}</span>
							<strong>{segment.value}</strong>
							<em>{total ? `${((segment.value / total) * 100).toFixed(1)}%` : '--'}</em>
						</li>
					))}
				</ul>
			</div>
			<div className="emotion-notes">
				<span>
					首板 <strong>{emotion.first_board}</strong>
				</span>
				<span>
					炸板率 <strong>{(emotion.final_break_rate * 100).toFixed(1)}%</strong>
				</span>
			</div>
		</>
	);
}

// 盘中判断：两个 0–100% 比率，数值在前、刻度在后，不与涨跌配色混用。
function IntradayMeters({ intraday }: { intraday: PatrolIntraday }) {
	const breadth = intradaySummary(intraday.summary, intraday.status);
	const risk = Number.isFinite(intraday.risk_score) ? Math.round(intraday.risk_score) : null;
	const ratios = [
		{ label: '高位股下跌率', value: intraday.high_down_rate, tone: 'is-risk' },
		{ label: '高位股走弱率', value: intraday.high_weak_rate, tone: 'is-warn' },
	];
	return (
		<div className="intraday">
			<div className="intraday-top">
				<span className="patrol-badge">{intraday.status}</span>
				{breadth ? <span className="patrol-badge is-soft">{breadth}</span> : null}
				{risk !== null ? <span className="intraday-risk">风险分 <b>{risk}</b></span> : null}
			</div>
			<div className="meters">
				{ratios.map((row) => {
					const ratio = Math.max(0, Math.min(1, Number.isFinite(row.value) ? row.value : 0));
					return (
						<div className={`meter ${row.tone}`} key={row.label}>
							<div className="meter-head">
								<span>{row.label}</span>
								<strong>{`${(ratio * 100).toFixed(1)}%`}</strong>
							</div>
							<i>
								<b style={{ width: `${ratio * 100}%` }} />
								<em style={{ left: '25%' }} />
								<em style={{ left: '50%' }} />
								<em style={{ left: '75%' }} />
							</i>
						</div>
					);
				})}
			</div>
			{(intraday.limit_up != null || intraday.max_streak != null || intraday.previous_max_streak != null) && (
				<p className="intraday-foot">
					{intraday.limit_up != null && <span>涨停 <b>{intraday.limit_up}</b></span>}
					{intraday.max_streak != null && <span>最高 <b>{intraday.max_streak}</b> 板</span>}
					{intraday.previous_max_streak != null && <span>上日 <b>{intraday.previous_max_streak}</b> 板</span>}
				</p>
			)}
		</div>
	);
}

// 资金：净流出在左、净流入在右，两侧共用一条中轴与同一最大绝对值，长度可以直接对比。
function FlowTornado({ board }: { board?: PatrolFlowBoard | null }) {
	if (!board || (!board.inflow?.length && !board.outflow?.length)) return null;
	const inflow = board.inflow ?? [];
	const outflow = board.outflow ?? [];
	const maxAbs = Math.max(0, ...[...inflow, ...outflow].map((item) => Math.abs(item.net_inflow)));
	const width = (value: number) => `${maxAbs ? (Math.abs(value) / maxAbs) * 100 : 0}%`;
	const label = (items: PatrolFlowItem[]) => items.map((item) => `${item.name} ${formatMoney(item.net_inflow)}`).join('，');
	return (
		<div className="tornado">
			{outflow.length > 0 && (
				<div className="tornado-side is-out">
					<header>{`净流出 · ${outflow.length}`}</header>
					<ul aria-label={label(outflow)}>
						{outflow.map((item) => (
							<li key={item.name}>
								<span className="t-name">{item.name}</span>
								<em className="t-value">{formatMoney(item.net_inflow)}</em>
								<span className="t-bar">
									<i style={{ width: width(item.net_inflow) }} />
								</span>
							</li>
						))}
					</ul>
				</div>
			)}
			{inflow.length > 0 && (
				<div className="tornado-side is-in">
					<header>{`净流入 · ${inflow.length}`}</header>
					<ul aria-label={label(inflow)}>
						{inflow.map((item) => (
							<li key={item.name}>
								<span className="t-bar">
									<i style={{ width: width(item.net_inflow) }} />
								</span>
								<em className="t-value">{formatMoney(item.net_inflow)}</em>
								<span className="t-name">{item.name}</span>
							</li>
						))}
					</ul>
				</div>
			)}
		</div>
	);
}

// 题材涨跌：同一批概念样本按即时涨跌幅排序，领涨/领跌各取前五，条形按同一最大绝对值缩放。
function ThemeMoves({ board }: { board: PatrolFlowBoard }) {
	const seen = new Set<string>();
	const items: PatrolFlowItem[] = [];
	for (const item of [...(board.inflow ?? []), ...(board.outflow ?? [])]) {
		if (seen.has(item.name)) continue;
		seen.add(item.name);
		items.push(item);
	}
	const ups = items.filter((item) => item.change_percent > 0).sort((a, b) => b.change_percent - a.change_percent);
	const downs = items.filter((item) => item.change_percent < 0).sort((a, b) => a.change_percent - b.change_percent);
	const max = Math.max(0.01, ...items.map((item) => Math.abs(item.change_percent)));
	if (!ups.length && !downs.length) return <p className="patrol-empty">暂无题材涨跌数据</p>;
	// 单边缺失时不做半栏留白：把有数据的一侧铺满并显示更多行。
	const single = !ups.length || !downs.length;
	const take = single ? 6 : 5;
	return (
		<div className={`move-board${single ? ' is-single' : ''}`}>
			{ups.length > 0 && (
				<div className="move-col is-up">
					<span className="move-cap">领涨 · {Math.min(take, ups.length)}</span>
					<ul>{ups.slice(0, take).map((item) => <MoveRow key={item.name} item={item} max={max} />)}</ul>
				</div>
			)}
			{downs.length > 0 && (
				<div className="move-col is-down">
					<span className="move-cap">领跌 · {Math.min(take, downs.length)}</span>
					<ul>{downs.slice(0, take).map((item) => <MoveRow key={item.name} item={item} max={max} />)}</ul>
				</div>
			)}
		</div>
	);
}

function MoveRow({ item, max }: { item: PatrolFlowItem; max: number }) {
	const inflow = item.net_inflow / 1e8;
	return (
		<li title={`${item.name} ${formatPercent(item.change_percent)} · 主力净流入 ${inflow >= 0 ? '+' : ''}${inflow.toFixed(2)} 亿`}>
			<span>{item.name}</span>
			<i><b style={{ width: `${Math.max(3, (Math.abs(item.change_percent) / max) * 100)}%` }} /></i>
			<em>{formatPercent(item.change_percent)}</em>
		</li>
	);
}

function FactsView({ facts, onNavigate }: { facts: PatrolFacts; onNavigate?: (target: CockpitTarget, subView?: string) => void }) {
	const { emotion, intraday, limit_up: limitUp } = facts;
	const [flowTab, setFlowTab] = useState<'industry' | 'concept'>('industry');
	const overseas = (facts.overseas ?? []).filter(
		(item) => !OMITTED_OVERSEAS_CODES.has(item.code.toUpperCase()) && !OMITTED_OVERSEAS_NAMES.has(item.name),
	);
	const cn = facts.cn_indexes ?? [];
	const hasIndexes = Boolean(overseas.length || cn.length);
	const indexMax = Math.max(0, ...[...overseas, ...cn].map((item) => Math.abs(item.change_percent || 0)));
	const news = facts.news ?? [];
	// 题材涨跌用按涨跌幅排序的取样（concept_moves），资金榜样本只按净流入取两端，
	// 领跌侧常常凑不满；老版本后端没有该字段时回落到资金榜。
	const theories = facts.concept_moves ?? facts.concept_flows;
	const theoryCount = new Set([...(theories?.inflow ?? []), ...(theories?.outflow ?? [])].map((item) => item.name)).size;
	// 同一行只剩一张卡时让它占满整行，避免半行留白。
	const ladderWide = !intraday;
	const intradayWide = !limitUp;
	// subView 落到行情总览的某个模块：市场核心指数 / 行业资金 / 资金流向等。
	const open = (target: CockpitTarget, subView?: string, label?: string) =>
		onNavigate ? { onOpen: () => onNavigate(target, subView), openLabel: label ?? cockpitTargetLabels[target] } : {};
	// 行业资金与概念资金共用一张卡，用分段开关切换维度；当前维度无数据时回落到有数据的一侧。
	const flowTabs = ([
		{ key: 'industry' as const, label: '行业', board: facts.industry_flows },
		{ key: 'concept' as const, label: '概念', board: facts.concept_flows },
	]).filter((tab) => tab.board?.inflow?.length || tab.board?.outflow?.length);
	const activeFlow = flowTabs.find((tab) => tab.key === flowTab) ?? flowTabs[0];
	return (
		<div className="patrol-facts">
			{hasIndexes && (
				<Fact title="指数" icon={<Activity size={15} aria-hidden="true" />} wide {...open('market', 'core-indexes', '市场核心指数')}>
					<div className={`index-groups${overseas.length && cn.length ? '' : ' is-single'}`}>
						<IndexGroup caption="海外" items={overseas} maxAbs={indexMax} />
						<IndexGroup caption="A 股" items={cn} maxAbs={indexMax} />
					</div>
				</Fact>
			)}

			{limitUp && (
				<Fact title="涨停梯队" icon={<BarChart3 size={15} aria-hidden="true" />} source={`${limitUp.trade_date} · ${limitUp.session_status}`} wide={ladderWide} {...open('limit-up')}>
					<LadderColumns limitUp={limitUp} previousMax={intraday?.previous_max_streak} />
				</Fact>
			)}

			{emotion && (
				<Fact title="情绪周期" icon={<Gauge size={15} aria-hidden="true" />} source={`${emotion.trade_date} 收盘`} badge={<span className="patrol-badge is-soft">{emotion.phase}</span>} {...open('limit-up')}>
					<EmotionDonut emotion={emotion} />
				</Fact>
			)}

			{intraday && (
				<Fact title="盘中判断" icon={<Compass size={15} aria-hidden="true" />} wide={intradayWide} {...open('limit-up')}>
					<IntradayMeters intraday={intraday} />
				</Fact>
			)}

			{theoryCount > 0 && (
				<Fact title="题材涨跌" icon={<TrendingUp size={15} aria-hidden="true" />} source={`概念资金样本 · ${theoryCount} 个题材`} wide={intradayWide} {...open('themes')}>
					<ThemeMoves board={theories!} />
				</Fact>
			)}

			{activeFlow && (
				<Fact
					title="资金流向"
					icon={<ArrowLeftRight size={15} aria-hidden="true" />}
					wide
					centerBadge
					// 行业维度跳行情总览的「行业资金」，概念维度跳「资金流向」。
					{...open('market', activeFlow.key === 'concept' ? 'sector-flow' : 'industry-flow', activeFlow.key === 'concept' ? '行情总览 · 资金流向' : '行情总览 · 行业资金')}
					badge={
						flowTabs.length > 1 ? (
							// 整卡可点跳转行情总览，开关区域必须拦住点击与回车，避免切换维度时误跳页。
							// 滑块按档位等分平移，与行情总览的分段控件同一套视觉。
							<div
								className="flow-switch"
								role="tablist"
								aria-label="资金流向维度"
								onClick={(event) => event.stopPropagation()}
								onKeyDown={(event) => event.stopPropagation()}
							>
								<span
									className="flow-switch-thumb"
									aria-hidden="true"
									style={{ transform: `translateX(${Math.max(0, flowTabs.findIndex((tab) => tab.key === activeFlow.key)) * 100}%)` }}
								/>
								{flowTabs.map((tab) => (
									<button
										key={tab.key}
										type="button"
										role="tab"
										aria-selected={tab.key === activeFlow.key}
										className={tab.key === activeFlow.key ? 'active' : ''}
										onClick={() => setFlowTab(tab.key)}
									>
										{tab.label}
									</button>
								))}
							</div>
						) : null
					}
				>
					<FlowTornado board={activeFlow.board} />
				</Fact>
			)}

			{news.length > 0 && (
				<Fact title="快讯" icon={<Newspaper size={15} aria-hidden="true" />} source={`${news.length} 条`} wide {...open('market')}>
					<ol className="news-timeline">
						{news.map((item, index) => (
							<li key={`${item.title}-${index}`}>
								<time>{item.published_at || '--'}</time>
								<span>{item.title}</span>
							</li>
						))}
					</ol>
				</Fact>
			)}
		</div>
	);
}

export function CockpitPatrolPanel(props: Props) {
	return (
		<PanelErrorBoundary title="AI 巡查">
			<CockpitPatrolContent {...props} />
		</PanelErrorBoundary>
	);
}

function CockpitPatrolContent({ config, refreshKey }: Props) {
	const patrol = useCockpitPatrol(config, refreshKey);
	const [pickedPhase, setPickedPhase] = useState<PatrolPhaseKey | null>(null);
	const [selectedID, setSelectedID] = useState<string>('');
	const [narrativeOpen, setNarrativeOpen] = useState(false);
	const [timelineOpen, setTimelineOpen] = useState(false);

	const currentPhase = patrol.status?.phase;
	const followPhase: PatrolPhaseKey = currentPhase === 'intraday' || currentPhase === 'post_close' ? currentPhase : currentPhase === 'pre_open' ? 'pre_open' : 'post_close';
	const activePhase: PatrolPhaseKey = pickedPhase ?? followPhase;

	const sessionDate = patrol.status?.session_date ?? '';
	const timeline = useMemo(() => sessionIntradayBriefs(patrol.briefs, sessionDate), [patrol.briefs, sessionDate]);
	const phaseBriefs = useMemo(() => patrol.briefs.filter((brief) => brief.phase === activePhase), [patrol.briefs, activePhase]);

	const selected: PatrolBrief | null = useMemo(() => {
		const byID = phaseBriefs.find((brief) => brief.id === selectedID);
		return byID ?? latestBriefFor(patrol.briefs, activePhase);
	}, [patrol.briefs, phaseBriefs, selectedID, activePhase]);

	// 默认只显示当天最近若干次盘中巡查，展开后回到全天；当天条数不多时不做折叠。
	const foldedTimeline = useMemo(
		() => foldTimeline(timeline, TIMELINE_FOLD_LIMIT, timelineOpen, selected?.id ?? ''),
		[timeline, timelineOpen, selected?.id],
	);

	useEffect(() => {
		setSelectedID('');
	}, [activePhase]);

	// 换到新交易日就收回到默认的最近若干次，不让上一天展开的列表延续过来。
	useEffect(() => {
		setTimelineOpen(false);
	}, [sessionDate]);

	// 切换记录或刷新后回到收起状态，避免上一次的展开延续到新记录。
	useEffect(() => {
		setNarrativeOpen(false);
	}, [selected?.id]);

	const notice = selected ? narrativeNotice(selected) : null;
	// 已核对通过时不占一条横幅，只在叙述标题旁保留一个状态标签；只有未生成或失败时才需要整行说明。
	const noticeWorthBanner = Boolean(notice) && !(selected?.narrative_status === 'ready' && !selected?.unverified_numbers?.length);
	const verified = selected?.narrative_status === 'ready' && !selected?.unverified_numbers?.length;
	const narrative = selected?.narrative ? stripLeadingTitle(selected.narrative) : '';
	const collapsible = narrative.length > 240;
	const evidence = useMemo(() => narrativeEvidence(selected?.facts), [selected?.facts]);
	// 起止时间同分钟时只显示一次，避免「10:10 – 10:10」这种重复读数。
	const startText = clockText(selected?.started_at);
	const endText = selected?.finished_at ? clockText(selected.finished_at) : '';
	const windowText = endText && endText !== startText ? `${startText} – ${endText}` : '';
	// 后端的下次巡查标签已经带时间（如「盘中巡查 10:40」），不要再拼一次时间。
	const nextRunTime = patrol.status?.next_run_at ? clockText(patrol.status.next_run_at) : '';
	const nextRunLabel = patrol.status?.next_run_label || '';
	const nextRun = nextRunLabel.includes(nextRunTime) ? nextRunLabel : [nextRunTime, nextRunLabel].filter(Boolean).join(' · ') || '--';
	const runPhase: PatrolPhaseKey = followPhase;

	if (!config) return null;

	return (
		<section className="patrol" aria-label="AI 巡查">
			{/* 工具栏在主区滚动时保持可见：阶段页签与「立即巡查」是高频操作。 */}
			<div className="patrol-toolbar">
				<header className="patrol-status">
					<div className="patrol-status-main">
						<span className={`patrol-phase phase-${currentPhase || 'rest'}`}>{patrolPhaseLabel(currentPhase || 'rest')}</span>
						<div>
							<strong><Bot size={16} aria-hidden="true" />AI 自动巡查</strong>
							<small>
								会话 {sessionDate || '--'}{patrol.status && !patrol.status.trading_day ? ' · 休市' : ''} · 下次 {nextRun}
							</small>
						</div>
					</div>
					<div className="patrol-status-side">
						{patrol.status && (
							<span className={patrol.status.ai_available ? 'patrol-ai is-on' : 'patrol-ai'}>
								{patrol.status.ai_available ? 'AI 已连接' : 'AI 未配置'}
							</span>
						)}
						<button type="button" className="patrol-run" disabled={patrol.starting || patrol.running || !currentPhase} onClick={() => void patrol.start(runPhase)}>
							{patrol.running ? <LoaderCircle className="spin" size={14} aria-hidden="true" /> : <Play size={14} aria-hidden="true" />}
							{patrol.running ? '巡查进行中' : `立即${patrolPhaseLabel(runPhase)}巡查`}
						</button>
					</div>
				</header>

				<div className="patrol-tabs" role="tablist" aria-label="巡查阶段">
					{/* 选中态用白色滑块平移表示（档位等分），与行情总览的分段控件同一套视觉。 */}
					<span
						className="patrol-tabs-thumb"
						aria-hidden="true"
						style={{ transform: `translateX(${Math.max(0, patrolPhases.findIndex((item) => item.phase === activePhase)) * 100}%)` }}
					/>
					{patrolPhases.map((item) => {
						const latest = latestBriefFor(patrol.briefs, item.phase);
						return (
							<button
								type="button"
								role="tab"
								aria-selected={activePhase === item.phase}
								key={item.phase}
								className={activePhase === item.phase ? 'active' : ''}
								onClick={() => setPickedPhase(item.phase)}
							>
								{item.label}
								<small>{latest ? `最近 ${clockText(latest.started_at)}` : '暂无记录'}</small>
							</button>
						);
					})}
				</div>

				{activePhase === 'intraday' && timeline.length > 0 && (
					<ol className="patrol-timeline" aria-label="今日盘中巡查时间线">
						{foldedTimeline.hidden > 0 && (
							<li>
								<button type="button" className="patrol-timeline-more" onClick={() => setTimelineOpen(true)}>
									更早 {foldedTimeline.hidden} 次
								</button>
							</li>
						)}
						{foldedTimeline.visible.map((brief) => (
							<li key={brief.id}>
								<button type="button" className={selected?.id === brief.id ? 'active' : ''} onClick={() => setSelectedID(brief.id)}>
									<Clock3 size={12} aria-hidden="true" />
									{clockText(brief.started_at)}
									<em className={brief.status === 'failed' ? 'is-fail' : ''}>{brief.status === 'running' ? '进行中' : brief.status === 'failed' ? '失败' : triggerText[brief.trigger] || brief.trigger}</em>
								</button>
							</li>
						))}
						{timelineOpen && timeline.length > TIMELINE_FOLD_LIMIT && (
							<li>
								<button type="button" className="patrol-timeline-more" onClick={() => setTimelineOpen(false)}>
									收起
								</button>
							</li>
						)}
					</ol>
				)}
			</div>

			<div className="patrol-body">
				{patrol.error && <p className="patrol-error"><AlertTriangle size={14} aria-hidden="true" />{patrol.error}</p>}

				{!selected && (
					<p className="patrol-empty">
						{patrol.loading ? '正在读取巡查记录…' : `暂无${patrolPhaseLabel(activePhase)}巡查记录，计划时间到达后自动生成。`}
					</p>
				)}

				{selected && (
					<article className="patrol-brief">
						<header>
							<div className="patrol-brief-head">
								<strong><Clock3 size={15} aria-hidden="true" />{selected.label}</strong>
								<span>{triggerText[selected.trigger] || selected.trigger}</span>
								{windowText ? <span>{windowText}</span> : null}
							</div>
							{selected.status === 'running' && <LoaderCircle className="spin" size={16} aria-hidden="true" />}
						</header>

						{selected.caveat && <p className="patrol-notice tone-warn">{selected.caveat}</p>}

						{noticeWorthBanner && notice && <p className={`patrol-notice tone-${notice.tone}`}>{notice.text}</p>}

						{selected.unverified_numbers?.length ? (
							<div className="patrol-unverified">
								<strong>以下数值未能在采集数据中找到，请人工核对：{selected.unverified_numbers.length} 个</strong>
								<span>{selected.unverified_numbers.join('、')}</span>
							</div>
						) : null}

						{/* 巡查面板只留结论：图表与快讯交给下方固定模块，按分钟级刷新实时数据。 */}
						{narrative && (
							<section className="patrol-narrative-block">
								<header>
									<div className="patrol-narrative-title">
										<Sparkles size={14} aria-hidden="true" />
										<h4>AI 叙述</h4>
										{verified && <span className="patrol-verify" title={narrativeVerifyRule}>数值已核对</span>}
									</div>
									{collapsible && (
										<button
											type="button"
											className="patrol-narrative-toggle"
											aria-expanded={narrativeOpen}
											title={`全文 ${narrative.length} 字`}
											onClick={() => setNarrativeOpen((open) => !open)}
										>
											{narrativeOpen ? '收起' : '展开全文'}
											{narrativeOpen ? <ChevronUp size={13} aria-hidden="true" /> : <ChevronDown size={13} aria-hidden="true" />}
										</button>
									)}
								</header>

								{evidence.length > 0 && (
									<p className="patrol-narrative-sources">
										<span>依据</span>
										{evidence.map((name) => <em key={name}>{name}</em>)}
									</p>
								)}

								{narrativeOpen || !collapsible ? (
									<div className="patrol-narrative">
										<Narrative markdown={narrative} />
									</div>
								) : (
									<div className="patrol-narrative is-preview">
										<NarrativePreview markdown={narrative} />
									</div>
								)}
							</section>
						)}

						{!narrative && selected.narrative_status && selected.narrative_status !== 'ready' && (
							<p className="patrol-empty">{narrativeFallback(selected.narrative_status)}</p>
						)}
					</article>
				)}
			</div>
		</section>
	);
}

function narrativeFallback(status: string): string {
	switch (status) {
		case 'unavailable':
			return '本次巡查未生成 AI 叙述（AI 未配置或调用失败），下方固定模块仍是本次采集到的真实数据。';
		case 'failed':
			return '本次巡查的 AI 叙述生成失败，事实数据仍已保存，可在下方固定模块查看。';
		case 'running':
			return 'AI 正在生成叙述…';
		default:
			return '本次巡查没有 AI 叙述。';
	}
}

type NarrativeBlock =
	| { kind: 'section'; ordinal: string; title: string }
	| { kind: 'paragraph'; text: string }
	| { kind: 'list'; ordered: boolean; items: string[] };

// 叙述来自固定提示词模板（小标题 + 自然段 + 少量列表），这里按块解析成可排版的结构，
// 不引第三方渲染器，才能给标题、数字与来源标注分别上样式。
function parseNarrative(markdown: string): NarrativeBlock[] {
	const blocks: NarrativeBlock[] = [];
	const lines = markdown.split('\n');
	let paragraph: string[] = [];
	let list: { ordered: boolean; items: string[] } | null = null;

	const flushParagraph = () => {
		if (paragraph.length) {
			blocks.push({ kind: 'paragraph', text: paragraph.join(' ') });
			paragraph = [];
		}
	};
	const flushList = () => {
		if (list) {
			blocks.push({ kind: 'list', ordered: list.ordered, items: list.items });
			list = null;
		}
	};

	for (const raw of lines) {
		const line = raw.trim();
		if (!line) {
			flushParagraph();
			flushList();
			continue;
		}
		const heading = /^#{2,3}\s+(.+)$/.exec(line);
		if (heading) {
			flushParagraph();
			flushList();
			const ordinal = /^([一二三四五六七八九十]+)[、.:：]?\s*(.+)$/.exec(heading[1]);
			blocks.push(ordinal
				? { kind: 'section', ordinal: ordinal[1], title: ordinal[2] }
				: { kind: 'section', ordinal: '', title: heading[1] });
			continue;
		}
		const ordered = /^\d+[.)]\s+/.test(line);
		const bullet = /^[-*]\s+/.test(line);
		if (ordered || bullet) {
			flushParagraph();
			if (!list || list.ordered !== ordered) {
				flushList();
				list = { ordered, items: [] };
			}
			list.items.push(line.replace(/^(\d+[.)]|[-*])\s+/, ''));
			continue;
		}
		flushList();
		paragraph.push(line);
	}
	flushParagraph();
	flushList();
	return blocks;
}

const NARRATIVE_INLINE = /\*\*([^*]+)\*\*|（([^）]{1,32})）|([+-]?\d[\d,]*(?:\.\d+)?%)/g;
// 括号里是采集来源或推断标记时做成小标签，普通括号说明保持原文。
const NARRATIVE_TAGS = ['A股指数', '海外指数', '指数', '涨停梯队', '盘中情绪', '情绪周期', '行业资金', '概念资金', '快讯', '数据源', '外围', '对比上次巡查', '盘中快照', '收盘口径'];

function renderInline(text: string, prefix: string): ReactNode[] {
	const nodes: ReactNode[] = [];
	let last = 0;
	for (const match of text.matchAll(NARRATIVE_INLINE)) {
		const index = match.index ?? 0;
		if (index > last) nodes.push(text.slice(last, index));
		const [token, bold, parenthetical, percent] = match;
		if (bold) {
			nodes.push(<strong key={`${prefix}-${index}`}>{bold}</strong>);
		} else if (parenthetical !== undefined) {
			if (parenthetical.startsWith('推断')) {
				nodes.push(<span key={`${prefix}-${index}`} className="nv-tag is-infer">{parenthetical}</span>);
			} else {
				// 「（A股指数、涨停梯队）」这类多来源标注拆成独立小标签，避免一颗长胶囊压住正文。
				const parts = parenthetical.split(/[、，,]/).map((part) => part.trim()).filter(Boolean);
				const sources = parts.every((part) => NARRATIVE_TAGS.some((key) => part.startsWith(key)));
				if (sources) {
					parts.forEach((part, partIndex) => {
						nodes.push(<span key={`${prefix}-${index}-${partIndex}`} className="nv-tag">{part}</span>);
					});
				} else {
					nodes.push(token);
				}
			}
		} else if (percent) {
			// A 股口径：上行红、下行绿；不带符号的百分比只做等宽加重。
			const tone = percent.startsWith('+') ? ' is-up' : percent.startsWith('-') ? ' is-down' : '';
			nodes.push(<b key={`${prefix}-${index}`} className={`nv-num${tone}`}>{percent}</b>);
		}
		last = index + token.length;
	}
	if (last < text.length) nodes.push(text.slice(last));
	return nodes;
}

function NarrativeBlocks({ blocks }: { blocks: NarrativeBlock[] }) {
	return (
		<>
			{blocks.map((block, index) => {
				if (block.kind === 'section') {
					return (
						<h5 className="nv-section" key={`s${index}`}>
							{block.ordinal
								? <span className="nv-ordinal">{block.ordinal}</span>
								: <span className="nv-ordinal is-dot" aria-hidden="true">·</span>}
							<span className="nv-title">{block.title}</span>
							<i className="nv-rule" aria-hidden="true" />
						</h5>
					);
				}
				if (block.kind === 'list') {
					return (
						<ol className={`nv-list${block.ordered ? ' is-ordered' : ''}`} key={`l${index}`}>
							{block.items.map((item, itemIndex) => (
								<li key={itemIndex}>{renderInline(item, `${index}-${itemIndex}`)}</li>
							))}
						</ol>
					);
				}
				const note = /^本简报|不构成投资建议/.test(block.text);
				return (
					<p className={`nv-p${note ? ' is-note' : ''}`} key={`p${index}`}>
						{renderInline(block.text, `p${index}`)}
					</p>
				);
			})}
		</>
	);
}

function Narrative({ markdown }: { markdown: string }) {
	const blocks = useMemo(() => parseNarrative(markdown), [markdown]);
	return <div className="nv"><NarrativeBlocks blocks={blocks} /></div>;
}

// 收起态只露首个小标题与第一段，正文由 CSS 收三行；结构与全文一致，展开时不跳版。
function NarrativePreview({ markdown }: { markdown: string }) {
	const blocks = useMemo(() => {
		const all = parseNarrative(markdown);
		const firstParagraph = all.findIndex((block) => block.kind === 'paragraph');
		return firstParagraph < 0 ? all : all.slice(0, firstParagraph + 1);
	}, [markdown]);
	return <div className="nv is-preview"><NarrativeBlocks blocks={blocks} /></div>;
}

// 叙述用到了哪些模块：由本次记录的事实反推，只列真的采到数据的来源。
function narrativeEvidence(facts?: PatrolFacts | null): string[] {
	if (!facts) return [];
	const list: string[] = [];
	if (facts.cn_indexes?.length || facts.overseas?.length) list.push('指数');
	if (facts.limit_up) list.push('涨停梯队');
	if (facts.intraday) list.push('盘中情绪');
	if (facts.emotion) list.push('情绪周期');
	if (facts.industry_flows?.inflow?.length || facts.industry_flows?.outflow?.length) list.push('行业资金');
	if (facts.concept_flows?.inflow?.length || facts.concept_flows?.outflow?.length) list.push('概念资金');
	if (facts.news?.length) list.push('快讯');
	return list;
}

// CockpitLiveModules：驾驶舱的固定模块。数据来自 /cockpit/facts 的实时采集，
// 与巡查记录解耦，按分钟级刷新，不会因为某次巡查失败而空白。
export function CockpitLiveModules({ config, refreshKey, onNavigate }: { config: BackendConfig | null; refreshKey: number; onNavigate: (target: CockpitTarget, subView?: string) => void }) {
	const live = useCockpitLiveFacts(config, refreshKey);
	// 实时采集按指数、情绪、资金、快讯四类给出结论性读数，价格与占比都带时间与来源。
	// 区块标题与采集状态提示已按需求移除：整段常驻不折叠，采集失败时由下方的错误段落提示。
	return (
		<section className="cockpit-live" aria-label="实时盘面">
			{live.error && <p className="patrol-error"><AlertTriangle size={14} aria-hidden="true" />{live.error}</p>}

			{live.data ? (
				<FactsView facts={live.data.facts} onNavigate={onNavigate} />
			) : (
				<p className="patrol-empty">{live.loading ? '正在读取盘面数据…' : '暂无盘面数据'}</p>
			)}
		</section>
	);
}
