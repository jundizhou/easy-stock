import { useEffect, useState } from 'react';
import './stock-research.css';
import { ArrowUpRight, CheckCircle2, CircleAlert, FileText, History, LoaderCircle, SearchCheck, Square, Trash2 } from 'lucide-react';
import type { StockAIAnalysis } from '../lib/backend';
import { conditionStatusLabel, evidenceLevelLabel, isResearchRunning, researchConditionValue, researchLevelLabel, safeResearchURL, type ResearchClaim, type ResearchJob, type ResearchJobSummary, type ResearchReport, type ResearchRequest, type ResearchVerification } from '../lib/stock-research';

export function StockResearchOptions({ purpose, horizon, cost, onPurpose, onHorizon, onCost }: { purpose: ResearchRequest['purpose']; horizon: ResearchRequest['horizon']; cost: string; onPurpose: (value: ResearchRequest['purpose']) => void; onHorizon: (value: ResearchRequest['horizon']) => void; onCost: (value: string) => void }) {
	return <div className="stock-research-options">
		<div role="group" aria-label="研究目的">{([['observe', '观察'], ['new_position', '准备新开仓'], ['holding', '已有持仓']] as const).map(([value, label]) => <button type="button" key={value} aria-pressed={purpose === value} className={purpose === value ? 'active' : ''} onClick={() => onPurpose(value)}>{label}</button>)}</div>
		<label>研究周期<select aria-label="研究周期" value={horizon} onChange={(event) => onHorizon(event.target.value as ResearchRequest['horizon'])}><option value="short">超短观察</option><option value="swing">波段研究</option><option value="medium">中期研究</option></select></label>
		{purpose === 'holding' && <label>持仓成本<input aria-label="持仓成本" type="number" min="0.01" step="0.01" value={cost} onChange={(event) => onCost(event.target.value)} placeholder="选填" /></label>}
	</div>;
}

export function StockResearchProgress({ job, onCancel }: { job: ResearchJob | null; onCancel: () => void }) {
	const [now, setNow] = useState(() => Date.now());
	const running = Boolean(job && isResearchRunning(job));
	useEffect(() => {
		if (!running) return;
		const timer = window.setInterval(() => setNow(Date.now()), 1000);
		return () => window.clearInterval(timer);
	}, [running, job?.id]);
	if (!job) return null;
	const elapsed = formatElapsedTime(job.started_at, running ? now : Date.parse(job.completed_at || job.updated_at));
	return <div className={`stock-research-progress ${job.status}`} role="status">
		{running ? <LoaderCircle className="spin" size={17} /> : job.status === 'succeeded' ? <CheckCircle2 size={17} /> : <CircleAlert size={17} />}
		<span className="stock-research-progress-message">{job.message}{job.error ? `：${job.error}` : ''}</span>
		<strong className="stock-research-progress-elapsed">目前总耗时 {elapsed}</strong>
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
	const openSource = (id: string) => { setTab('evidence'); setSourceID(id); };
	return <div className="stock-research-report">
		<header className="stock-research-report-header"><div><strong>{view === 'risk' ? '条件与执行边界' : view === 'expectation' ? '情景与后续核验' : 'AI研究判断'}</strong><span>{researchLevelLabel(report.analysis_level || report.request.analysis_level)} · 证据{evidenceLevelLabel(report.evidence_level)} · {report.model || '当前模型'}</span></div><div role="tablist" aria-label="研究内容"><button role="tab" aria-selected={tab === 'research'} onClick={() => setTab('research')}>研判</button><button role="tab" aria-selected={tab === 'evidence'} onClick={() => setTab('evidence')}>证据 {report.sources.length}</button></div></header>
		{tab === 'evidence' ? <ResearchEvidence report={report} selectedID={sourceID} /> : <>
			{view === 'research' && <><div className="stock-research-thesis"><h3>{report.headline}</h3><Claim claim={report.thesis} onSource={openSource} /><p><strong>核心分歧</strong>{report.main_conflict || '暂未形成明确的分歧判断'}</p></div>
			<div className="stock-research-arguments"><section><h4>支持依据</h4>{report.support.length ? report.support.map((claim, i) => <Claim key={i} claim={claim} onSource={openSource} />) : <p>尚未取得足够支持依据</p>}</section><section><h4>反对依据</h4>{report.counter.length ? report.counter.map((claim, i) => <Claim key={i} claim={claim} onSource={openSource} />) : <p>未取得直接反证，不代表不存在风险</p>}</section></div>
			{report.alternatives.length > 0 && <section className="stock-research-band"><h4>替代解释</h4>{report.alternatives.map((claim, i) => <Claim key={i} claim={claim} onSource={openSource} />)}</section>}
			<section className="stock-research-baseline"><strong>{report.baseline_relation === 'disagree' ? '与量化基线存在分歧' : report.baseline_relation === 'agree' ? '与量化基线方向一致' : '尚不足以与量化基线比较'}</strong><p>{report.baseline_reason}</p><span>量化基线 {analysis.scorecard.overall} · AI未改写基线分数</span></section></>}
			<ResearchDecision analysis={analysis} onSource={openSource} />
			<section className="stock-research-band"><header><h4>确认与失效条件</h4>{onVerify && <button type="button" onClick={onVerify} disabled={verifying}>{verifying ? <LoaderCircle size={14} className="spin" /> : <SearchCheck size={14} />}核对后续行情</button>}</header>
				{report.conditions.length ? report.conditions.map((condition) => { const check = verification?.checks.find((item) => item.condition_id === condition.id); return <div className="stock-research-condition" key={condition.id}><span className={report.invalidation_ids.includes(condition.id) ? 'invalidation' : ''}>{report.invalidation_ids.includes(condition.id) ? '失效条件' : '观察条件'}</span><div><strong>{condition.text}</strong><p>{researchConditionValue(condition)}</p><small>{condition.window === 'next_close' ? '下一交易日收盘' : condition.window === 'next_disclosure' ? '后续披露' : '后续5个交易日'}{check ? ` · ${check.detail}${check.as_of ? ` · ${check.as_of}` : ''}` : ''}</small></div><em>{conditionStatusLabel(check?.status || 'pending')}</em></div>; }) : <p>尚未形成可核验条件</p>}
				{verification && <p className="stock-research-muted">{verification.summary} · {formatDate(verification.checked_at)}</p>}
			</section>
			{view !== 'risk' && report.scenarios.length > 0 && <section className="stock-research-band"><h4>条件情景</h4><div className="stock-research-scenarios">{report.scenarios.map((scenario) => <article key={scenario.key}><strong>{scenario.name}</strong><p>{scenario.description}</p><small>{scenario.condition_ids.map((id) => report.conditions.find((item) => item.id === id)?.text).filter(Boolean).join('；')}</small><p>{scenario.response}</p></article>)}</div></section>}
			<section className="stock-research-band"><h4>信息缺口与限制</h4><ul>{report.limitations.map((item, i) => <li key={i}>{item}</li>)}</ul></section>
		</>}
		<footer className="stock-research-footer"><span>分析时点 {formatDate(report.cutoff_at)} · 快照 {report.snapshot_id.slice(0, 8)} · v{report.snapshot_version}</span><span>{report.attempts.length} 次模型调用 · {Math.round(report.attempts.reduce((sum, item) => sum + item.duration_ms, 0) / 1000)} 秒模型耗时 · {report.prompt_version}{report.compression ? ` · 证据${report.compression.original_content_bytes > report.compression.selected_content_bytes ? '已压缩' : '未压缩'}` : ''}</span></footer>
	</div>;
}

function Claim({ claim, onSource }: { claim: ResearchClaim; onSource: (id: string) => void }) {
	return <div className="stock-research-claim"><span>{claim.kind === 'fact' ? '原文陈述' : claim.kind === 'opinion' ? '第三方观点' : '研究推断'}</span><p>{claim.text}</p><div>{claim.source_ids.map((id) => <button type="button" key={id} onClick={() => onSource(id)} title={`查看证据 ${id}`}><FileText size={12} />{id}</button>)}</div></div>;
}

function ResearchDecision({ analysis, onSource }: { analysis: StockAIAnalysis; onSource: (id: string) => void }) {
	const report = analysis.research_report!;
	const plan = report.decision.price_plan;
	return <section className="stock-research-band"><h4>{report.decision.status === 'no_plan' ? '暂不形成交易计划' : report.decision.status === 'observe' ? '观察与核实' : '条件化计划'}</h4><p>{report.decision.reason}</p><div className="stock-research-positions"><div><strong>准备新开仓</strong><p>{report.decision.new_position}</p></div><div><strong>已有仓位{report.request.cost_price ? ` · 成本 ${report.request.cost_price.toFixed(2)} 元` : ''}</strong><p>{report.decision.existing_position}</p></div></div>
		{plan && <div className="stock-research-prices">{[['介入参考', plan.entry_anchor], ['失效参考', plan.stop_anchor], ['压力参考', plan.target_anchor]].map(([label, id]) => { const anchor = report.anchors.find((item) => item.id === id); return <div key={label}><span>{label}</span><strong>{anchor ? `${anchor.price.toFixed(2)} 元` : '没有充分依据'}</strong>{anchor && <button type="button" onClick={() => onSource(anchor.source_id)}>{anchor.label}<ArrowUpRight size={12} /></button>}</div>; })}</div>}
	</section>;
}

function ResearchEvidence({ report, selectedID }: { report: ResearchReport; selectedID: string }) {
	const ordered = selectedID ? [...report.sources].sort((left, right) => Number(right.id === selectedID) - Number(left.id === selectedID)) : report.sources;
	return <div className="stock-research-evidence"><section className="stock-research-band"><h4>补证记录</h4>{report.questions.length ? report.questions.map((item, i) => <article key={i}><strong>{item.question}</strong><p>{item.why}</p><small>{item.outcome || '未进一步查询'}</small></article>) : <p>本次未提出额外补证请求</p>}</section>
		{ordered.map((source) => <details key={`${source.id}-${selectedID}`} open={source.id === selectedID || undefined} className={source.id === selectedID ? 'selected' : ''}><summary><FileText size={15} /><strong>{source.title}</strong><span>{source.id}</span></summary><div><p className="stock-research-source-meta">{source.provider || '来源未标注'} · {source.kind} · {source.time_status === 'publication_unknown' ? '发布时间未知' : source.report_date || formatDate(source.published_at || '')}</p><pre>{source.content}</pre>{safeResearchURL(source.url) && <a href={safeResearchURL(source.url)} target="_blank" rel="noreferrer">查看原始来源<ArrowUpRight size={13} /></a>}</div></details>)}
		<section className="stock-research-band"><h4>校验记录</h4><ul>{report.validation_notes.map((note, i) => <li key={i}>{note}</li>)}</ul></section>
	</div>;
}

function formatDate(value: string) { const date = new Date(value); return Number.isNaN(date.getTime()) || date.getFullYear() < 2000 ? '时间未知' : date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }); }
