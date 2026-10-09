import { useEffect, useState, type ReactNode } from 'react';
import './stock-research.css';
import { ArrowUpRight, CheckCircle2, ChevronDown, CircleAlert, FileText, GitBranch, History, ListChecks, LoaderCircle, Play, Scale, SearchCheck, ShieldCheck, Square, TrendingDown, TrendingUp, Trash2, type LucideIcon } from 'lucide-react';
import type { StockAIAnalysis } from '../lib/backend';
import { conditionStatusLabel, evidenceLevelLabel, isResearchRunning, researchEvidenceReasons, researchConditionValue, researchLevelLabel, safeResearchURL, type ResearchClaim, type ResearchJob, type ResearchJobSummary, type ResearchReport, type ResearchRequest, type ResearchVerification } from '../lib/stock-research';

export function StockResearchOptions({ purpose, horizon, cost, costError, onPurpose, onHorizon, onCost }: { purpose: ResearchRequest['purpose']; horizon: ResearchRequest['horizon']; cost: string; costError?: string; onPurpose: (value: ResearchRequest['purpose']) => void; onHorizon: (value: ResearchRequest['horizon']) => void; onCost: (value: string) => void }) {
	return <div className="stock-research-options">
		<div className="stock-research-option-field"><strong>用途</strong><div role="group" aria-label="研究目的">{([['observe', '观察'], ['new_position', '准备新开仓'], ['holding', '已有持仓']] as const).map(([value, label]) => <button type="button" key={value} aria-pressed={purpose === value} className={purpose === value ? 'active' : ''} onClick={() => onPurpose(value)}>{label}</button>)}</div></div>
		<div className="stock-research-option-field"><strong>周期</strong><div role="group" aria-label="研究周期">{([['short', '超短'], ['swing', '波段'], ['medium', '中期']] as const).map(([value, label]) => <button type="button" key={value} aria-pressed={horizon === value} className={horizon === value ? 'active' : ''} onClick={() => onHorizon(value)}>{label}</button>)}</div></div>
		{purpose === 'holding' && <div className="stock-research-option-field"><label htmlFor="stock-research-cost">持仓成本<span>选填</span></label><div className="stock-research-cost-input"><input id="stock-research-cost" aria-label="持仓成本" aria-invalid={Boolean(costError)} aria-describedby={costError ? 'stock-research-cost-error' : undefined} type="number" min="0.01" step="any" value={cost} onChange={(event) => onCost(event.target.value)} placeholder="填写每股成本" /><span>元 / 股</span></div>{costError && <p id="stock-research-cost-error" role="alert">{costError}</p>}</div>}
	</div>;
}

export function StockResearchProgress({ job, onCancel, onResume, resuming = false }: { job: ResearchJob | null; onCancel: () => void; onResume?: () => void; resuming?: boolean }) {
	const [now, setNow] = useState(() => Date.now());
	const running = Boolean(job && isResearchRunning(job));
	useEffect(() => {
		if (!running) return;
		const timer = window.setInterval(() => setNow(Date.now()), 1000);
		return () => window.clearInterval(timer);
	}, [running, job?.id]);
	if (!job) return null;
	const elapsed = formatElapsedTime(job.started_at, running ? now : Date.parse(job.completed_at || job.updated_at));
	const stageSeconds = job.budget?.stage_timeout_seconds || 0;
	const stageBudget = running && stageSeconds > 0 && !['queued', 'collecting', 'baseline'].includes(job.stage) ? `；当前模型阶段最多 ${Math.ceil(stageSeconds / 60)} 分钟` : '';
	return <div className={`stock-research-progress ${job.status}`} role="status">
		{running ? <LoaderCircle className="spin" size={17} /> : job.status === 'succeeded' ? <CheckCircle2 size={17} /> : <CircleAlert size={17} />}
		<span className="stock-research-progress-message">{job.message}{stageBudget}{job.error ? `：${job.error}` : ''}</span>
		<strong className="stock-research-progress-elapsed">目前总耗时 {elapsed}</strong>
		{!running && job.resume_available && onResume && <button type="button" className="stock-research-resume" disabled={resuming} onClick={onResume}><Play size={15} />{resuming ? '提交中…' : '继续研究'}</button>}
		{running && <button type="button" onClick={onCancel} title="停止本次研究" aria-label="停止本次研究"><Square size={15} /></button>}
	</div>;
}

function formatElapsedTime(startedAt: string, endAt: number) {
	const started = Date.parse(startedAt);
	if (!Number.isFinite(started) || !Number.isFinite(endAt)) return '计算中';
	const seconds = Math.max(0, Math.floor((endAt - started) / 1000));
	const minutes = Math.floor(seconds / 60);
	return `${minutes}分${String(seconds % 60).padStart(2, '0')}秒`;
}

export function StockResearchHistory({ items, activeID, onOpen, onRemove }: { items: ResearchJobSummary[]; activeID: string; onOpen: (id: string) => void; onRemove: (id: string) => void }) {
	const [expanded, setExpanded] = useState(false);
	if (!items.length) return null;
	return <section className="stock-research-history" aria-label="研究历史">
		<header><History size={15} /><strong>研究记录</strong><span>{items.length}</span><button type="button" onClick={() => setExpanded(!expanded)}>{expanded ? '收起' : '全部记录'}</button></header>
		<div className={expanded ? 'expanded' : ''}>{items.slice(0, expanded ? 50 : 4).map((item) => <article key={item.id} className={item.id === activeID ? 'active' : ''}>
			<button type="button" onClick={() => onOpen(item.id)}><strong>{item.name}</strong><time>{formatDate(item.started_at)}</time><span>{isResearchRunning(item) ? item.message : item.headline || item.message}</span></button>
			<button type="button" disabled={isResearchRunning(item)} onClick={() => onRemove(item.id)} title={`删除${item.name}本次记录`} aria-label={`删除${item.name}本次记录`}><Trash2 size={13} /></button>
		</article>)}</div>
	</section>;
}

export function StockResearchReportView({ analysis, view = 'research', verification, verifying, onVerify }: { analysis: StockAIAnalysis; view?: 'research' | 'expectation' | 'risk'; verification?: ResearchVerification; verifying?: boolean; onVerify?: () => void }) {
	const report = analysis.research_report;
	const [tab, setTab] = useState<'research' | 'evidence'>('research');
	const [sourceID, setSourceID] = useState('');
	if (!report) return null;
	const evidenceReasons = researchEvidenceReasons(report);
	const openSource = (id: string) => { setTab('evidence'); setSourceID(id); };
	return <div className="stock-research-report">
		<header className="stock-ai-panel stock-research-report-header"><div><span>{researchLevelLabel(report.analysis_level || report.request.analysis_level)}</span><strong>{view === 'risk' ? '条件与执行边界' : view === 'expectation' ? '情景与后续核验' : 'AI研究判断'}</strong></div><div className="stock-research-report-meta"><span className={`stock-research-evidence-badge ${report.evidence_level}`}>证据{evidenceLevelLabel(report.evidence_level)}</span><span>{report.model || '当前模型'}</span></div><div role="tablist" aria-label="研究内容"><button type="button" role="tab" aria-selected={tab === 'research'} onClick={() => setTab('research')}>研判</button><button type="button" role="tab" aria-selected={tab === 'evidence'} onClick={() => setTab('evidence')}>证据 {report.sources.length}</button></div></header>
		{tab === 'evidence' ? <ResearchEvidence report={report} selectedID={sourceID} /> : <>
			{evidenceReasons.length > 0 && <ResearchPanel label="证据覆盖" title={`证据${evidenceLevelLabel(report.evidence_level)}的原因`} icon={CircleAlert} ariaLabel="证据评估依据"><ul>{evidenceReasons.map((reason, index) => <li key={index}>{reason}</li>)}</ul></ResearchPanel>}
			{view === 'research' && <><ResearchPanel label="研究结论" title="核心判断" icon={SearchCheck} className="stock-research-thesis"><h3>{report.headline}</h3><Claim claim={report.thesis} onSource={openSource} /><div className="stock-research-conflict"><strong>核心分歧</strong><p>{report.main_conflict || '暂未形成明确的分歧判断'}</p></div></ResearchPanel>
			<div className="stock-research-arguments"><ResearchPanel label="正向证据" title="支持依据" icon={TrendingUp} className="stock-research-support">{report.support.length ? report.support.map((claim, i) => <Claim key={i} claim={claim} onSource={openSource} />) : <p>尚未取得足够支持依据</p>}</ResearchPanel><ResearchPanel label="反向证据" title="反对依据" icon={TrendingDown} className="stock-research-counter">{report.counter.length ? report.counter.map((claim, i) => <Claim key={i} claim={claim} onSource={openSource} />) : <p>未取得直接反证，不代表不存在风险</p>}</ResearchPanel></div>
			{report.alternatives.length > 0 && <ResearchPanel label="其他可能性" title="替代解释" icon={GitBranch}><div className="stock-research-alternatives">{report.alternatives.map((claim, i) => <Claim key={i} claim={claim} onSource={openSource} />)}</div></ResearchPanel>}
			<ResearchPanel label="量化对照" title={report.baseline_relation === 'disagree' ? '与量化基线存在分歧' : report.baseline_relation === 'agree' ? '与量化基线方向一致' : '尚不足以与量化基线比较'} icon={Scale} className="stock-research-baseline"><p>{report.baseline_reason}</p><span>量化基线 {analysis.scorecard.overall} · AI未改写基线分数</span></ResearchPanel></>}
			<ResearchDecision analysis={analysis} onSource={openSource} />
			<ResearchPanel label="后续核验" title="确认与失效条件" icon={ListChecks} action={onVerify && <button type="button" onClick={onVerify} disabled={verifying}>{verifying ? <LoaderCircle size={14} className="spin" /> : <SearchCheck size={14} />}核对后续行情</button>}>
				{report.conditions.length ? report.conditions.map((condition) => { const check = verification?.checks.find((item) => item.condition_id === condition.id); return <div className="stock-research-condition" key={condition.id}><span className={report.invalidation_ids.includes(condition.id) ? 'invalidation' : ''}>{report.invalidation_ids.includes(condition.id) ? '失效条件' : '观察条件'}</span><div><strong>{condition.text}</strong><p>{researchConditionValue(condition)}</p><small>{condition.window === 'next_close' ? '下一交易日收盘' : condition.window === 'next_disclosure' ? '后续披露' : '后续5个交易日'}{check ? ` · ${check.detail}${check.as_of ? ` · ${check.as_of}` : ''}` : ''}</small></div><em>{conditionStatusLabel(check?.status || 'pending')}</em></div>; }) : <p>尚未形成可核验条件</p>}
				{verification && <p className="stock-research-muted">{verification.summary} · {formatDate(verification.checked_at)}</p>}
			</ResearchPanel>
			{view !== 'risk' && report.scenarios.length > 0 && <ResearchPanel label="条件推演" title="条件情景" icon={GitBranch}><div className="stock-research-scenarios">{report.scenarios.map((scenario) => <article key={scenario.key}><strong>{scenario.name}</strong><p>{scenario.description}</p><small>{scenario.condition_ids.map((id) => report.conditions.find((item) => item.id === id)?.text).filter(Boolean).join('；')}</small><p>{scenario.response}</p></article>)}</div></ResearchPanel>}
			<ResearchPanel label="研究边界" title="信息缺口与限制" icon={CircleAlert}><ul>{report.limitations.map((item, i) => <li key={i}>{item}</li>)}</ul></ResearchPanel>
		</>}
		<footer className="stock-research-footer"><span>分析时点 {formatDate(report.cutoff_at)} · 快照 {report.snapshot_id.slice(0, 8)} · v{report.snapshot_version}</span><span>{report.attempts.length} 次模型调用 · {Math.round(report.attempts.reduce((sum, item) => sum + item.duration_ms, 0) / 1000)} 秒模型耗时 · {report.prompt_version}{report.compression ? ` · 证据${report.compression.original_content_bytes > report.compression.selected_content_bytes ? '已压缩' : '未压缩'}` : ''}</span></footer>
	</div>;
}

function ResearchPanel({ label, title, icon: Icon, children, action, className = '', ariaLabel }: { label: string; title: string; icon: LucideIcon; children: ReactNode; action?: ReactNode; className?: string; ariaLabel?: string }) {
	return <section className={`stock-ai-panel stock-research-panel ${className}`.trim()} aria-label={ariaLabel}>
		<header><div><span>{label}</span><h4>{title}</h4></div><div className="stock-research-panel-actions">{action}<Icon size={18} aria-hidden="true" /></div></header>
		<div className="stock-research-panel-body">{children}</div>
	</section>;
}

function Claim({ claim, onSource }: { claim: ResearchClaim; onSource: (id: string) => void }) {
	return <div className="stock-research-claim"><span>{claim.kind === 'fact' ? '原文陈述' : claim.kind === 'opinion' ? '第三方观点' : '研究推断'}</span><p>{claim.text}</p><div>{claim.source_ids.map((id) => <button type="button" key={id} onClick={() => onSource(id)} title={`查看证据 ${id}`}><FileText size={12} />{id}</button>)}</div></div>;
}

function ResearchDecision({ analysis, onSource }: { analysis: StockAIAnalysis; onSource: (id: string) => void }) {
	const report = analysis.research_report!;
	const plan = report.decision.price_plan;
	return <ResearchPanel label="执行边界" title={report.decision.status === 'no_plan' ? '暂不形成交易计划' : report.decision.status === 'observe' ? '观察与核实' : '条件化计划'} icon={ShieldCheck}><p>{report.decision.reason}</p>{!!report.decision.blockers?.length && <ul>{report.decision.blockers.map((reason,i) => <li key={i}>{reason}</li>)}</ul>}<div className="stock-research-positions"><div><strong>准备新开仓</strong><p>{report.decision.new_position}</p></div><div><strong>已有仓位{report.request.cost_price ? ` · 成本 ${report.request.cost_price.toFixed(2)} 元` : ''}</strong><p>{report.decision.existing_position}</p></div></div>
		{plan && <div className="stock-research-prices">{[['介入参考', plan.entry_anchor], ['失效参考', plan.stop_anchor], ['压力参考', plan.target_anchor]].map(([label, id]) => { const anchor = report.anchors.find((item) => item.id === id); return <div key={label}><span>{label}</span><strong>{anchor ? `${anchor.price.toFixed(2)} 元` : '没有充分依据'}</strong>{anchor && <button type="button" onClick={() => onSource(anchor.source_id)}>{anchor.label}<ArrowUpRight size={12} /></button>}</div>; })}</div>}
	</ResearchPanel>;
}

function ResearchEvidence({ report, selectedID }: { report: ResearchReport; selectedID: string }) {
	const ordered = selectedID ? [...report.sources].sort((left, right) => Number(right.id === selectedID) - Number(left.id === selectedID)) : report.sources;
	return <div className="stock-research-evidence"><ResearchPanel label="证据追踪" title="补证记录" icon={SearchCheck}>{report.questions.length ? report.questions.map((item, i) => <article key={i}><strong>{item.question}</strong><p>{item.why}</p><small>{item.outcome || '未进一步查询'}</small></article>) : <p>本次未提出额外补证请求</p>}</ResearchPanel>
		{ordered.map((source) => <details key={`${source.id}-${selectedID}`} open={source.id === selectedID || undefined} className={`stock-ai-panel stock-research-source${source.id === selectedID ? ' selected' : ''}`}><summary><FileText size={16} aria-hidden="true" /><strong>{source.title}</strong><span>{source.id}</span><ChevronDown size={16} className="stock-research-source-chevron" aria-hidden="true" /></summary><div><p className="stock-research-source-meta">{source.provider || '来源未标注'} · {source.kind} · {source.time_status === 'publication_unknown' ? '发布时间未知' : source.report_date || formatDate(source.published_at || '')}</p><pre>{source.content}</pre>{safeResearchURL(source.url) && <a href={safeResearchURL(source.url)} target="_blank" rel="noreferrer">查看原始来源<ArrowUpRight size={13} /></a>}</div></details>)}
		<ResearchPanel label="来源校验" title="校验记录" icon={ShieldCheck}><ul>{report.validation_notes.map((note, i) => <li key={i}>{note}</li>)}</ul></ResearchPanel>
	</div>;
}

function formatDate(value: string) { const date = new Date(value); return Number.isNaN(date.getTime()) || date.getFullYear() < 2000 ? '时间未知' : date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }); }

export { ResearchPanel, Claim as ResearchClaimView };
