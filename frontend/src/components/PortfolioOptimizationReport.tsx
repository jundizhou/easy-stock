import { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowRight, ArrowDown, BrainCircuit, Check, CircleAlert, ExternalLink, History, LoaderCircle, Pause, Minus, ShieldCheck } from 'lucide-react';
import type { BackendConfig, PortfolioHolding, PortfolioInspectionReport, PortfolioOptimizationJob, PortfolioOptimizationPlan, PortfolioResearchRequest, StockAIAnalysis } from '../lib/backend';
import { requestJSON } from '../lib/backend';
import { ResearchPanel } from './StockResearchReport';
import { PortfolioEvidence } from './PortfolioAIReport';
import './portfolio-optimization.css';
import { optimizationCandidateSource, optimizationLiquiditySource, portfolioScoreImprovement, portfolioOptimizationVersion, portfolioOptimizationPromptVersion, qualifiedOptimizationScore } from '../lib/portfolio-optimization';

type Props = { config: BackendConfig | null; sourceId: string; canStart: boolean; startSignal: number; onApply: (request: PortfolioResearchRequest) => void; onOpenStockAnalysis: (analysis: StockAIAnalysis) => void; applyBusy: boolean; onRefreshSource: () => void };
const outcomeLabel: Record<string,string> = { conditional: '需等待条件', accepted: '有可采纳方案', unchanged: '本次未调整', no_feasible_plan: '未生成可行调整方案', below_target_score: '评分与改善要求未通过', review_invalid: '复评无效，未生成优化结果', incomplete: '研究 / 评估未完成' };
const optimizationSteps = [{ key:'preparing', label:'准备资料' }, { key:'screening', label:'筛选候选' }, { key:'researching', label:'个股研究' }, { key:'proposing', label:'搜索配仓' }, { key:'assessing', label:'独立复评' }, { key:'completed', label:'完成' }];
const stageLabel = Object.fromEntries(optimizationSteps.map((step) => [step.key,step.label]));
const dateLabel = (value?: string) => value && !value.startsWith('0001-') && Number.isFinite(new Date(value).getTime()) ? new Date(value).toLocaleString('zh-CN', { hour12:false }) : '待确认';

export function PortfolioOptimizationWorkspace({ config, sourceId, canStart, startSignal, onApply, onOpenStockAnalysis, applyBusy, onRefreshSource }: Props) {
 const [jobs,setJobs] = useState<Array<Pick<PortfolioOptimizationJob,'id'|'status'|'started_at'|'outcome'>>>([]);
 const [job,setJob] = useState<PortfolioOptimizationJob | null>(null);
 const [busy,setBusy] = useState(false);
 const [launching,setLaunching] = useState(false);
 const [error,setError] = useState('');
 const [candidates,setCandidates] = useState('');
 const lastSignal = useRef(startSignal);
 const sectionRef=useRef<HTMLElement>(null);
 const remember = useCallback((next: PortfolioOptimizationJob) => { setJob(next);setJobs((items) => [next,...items.filter((item) => item.id !== next.id)]); },[]);
 useEffect(() => {
  if (!config) return; let active=true;
  requestJSON<{data:Array<Pick<PortfolioOptimizationJob,'id'|'status'|'started_at'|'outcome'>>}>(config,`/api/v1/portfolio-inspections/${encodeURIComponent(sourceId)}/optimizations`).then(async (p) => { if (active) {setJobs(p.data || []);if(p.data?.[0]) {const full=await requestJSON<{data:PortfolioOptimizationJob}>(config,`/api/v1/portfolio-optimizations/${encodeURIComponent(p.data[0].id)}`);if(active)setJob((current) => current || full.data);} } }).catch((cause) => { if (active) setError(cause instanceof Error ? cause.message : '优化历史加载失败'); });
  return () => {active=false;};
 },[config,sourceId]);
 const start = useCallback(async () => {
  if (!config || !canStart || busy || job?.status==='running') return;sectionRef.current?.scrollIntoView({behavior:'smooth',block:'nearest'});setBusy(true);setLaunching(true);setError('');
  try { const symbols=candidates.split(/[\s,，;；]+/).filter(Boolean);const p=await requestJSON<{data:PortfolioOptimizationJob}>(config,`/api/v1/portfolio-inspections/${encodeURIComponent(sourceId)}/optimizations`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({candidate_symbols:symbols,...(job && !job.resume_available && (job.status!=='succeeded' || job.outcome==='review_invalid') ? {restart_from:job.id} : {})})});remember(p.data); }
  catch (cause) {setError(cause instanceof Error ? cause.message : '优化启动失败');} finally {setBusy(false);setLaunching(false);}
 },[config,canStart,busy,candidates,sourceId,remember,job]);
 useEffect(() => { if (startSignal !== lastSignal.current) {lastSignal.current=startSignal;void start();} },[startSignal,start]);
 useEffect(() => {
  if (!config || !job || job.status !== 'running') return; let active=true;let pending=false;
  const poll=async () => {if(pending)return;pending=true;try {const p=await requestJSON<{data:Partial<PortfolioOptimizationJob> & Pick<PortfolioOptimizationJob,'id'|'status'|'stage'>}>(config,`/api/v1/portfolio-optimizations/${encodeURIComponent(job.id)}?view=progress`);if(active) {if(p.data.status!=='running'){const full=await requestJSON<{data:PortfolioOptimizationJob}>(config,`/api/v1/portfolio-optimizations/${encodeURIComponent(job.id)}`);if(active)remember(full.data);}else {setJob((current) => current?.id===p.data.id ? {...current,...p.data} : current);}}}catch(cause){if(active)setError(cause instanceof Error?cause.message:'进度读取失败，重新进入可恢复');}finally{pending=false;}};
  const timer=window.setInterval(() => void poll(),3000);void poll();return () => {active=false;window.clearInterval(timer);};
 },[config,job?.id,job?.status,remember]);
 const control=async(action:'resume'|'cancel') => {if(!config||!job)return;setBusy(true);setError('');try {const p=await requestJSON<{data?:PortfolioOptimizationJob}>(config,`/api/v1/portfolio-optimizations/${encodeURIComponent(job.id)}/${action}`,{method:'POST'});if(action==='resume' && p.data)remember(p.data);}catch(cause){setError(cause instanceof Error?cause.message:'操作失败');}finally{setBusy(false);}};
 const completed = job?.status==='succeeded' && !launching;
 const preferredPlan = completed && job.selected_plan!==undefined ? job.plans[job.selected_plan] : undefined;
 const approved = Boolean(preferredPlan?.checks?.valid && preferredPlan.assessment?.accepted && !preferredPlan.rejection_reasons?.length && !preferredPlan.risk_checks?.some((r) => r.hard && !r.passed) && qualifiedOptimizationScore(preferredPlan.target_comparison.conclusion,preferredPlan.original_comparison.conclusion) && ['conditional','accepted'].includes(preferredPlan.status) && job?.version===portfolioOptimizationVersion && job.model_prompt_version===portfolioOptimizationPromptVersion);
 const otherPlans = completed ? job.plans.filter((p) => !approved || p!==preferredPlan) : [];
 return <section ref={sectionRef} className="portfolio-optimization" aria-label="持仓优化">
  <div className="portfolio-optimization-process stock-ai-panel">
   <header className="portfolio-optimization-heading">
    <div className="portfolio-optimization-title"><BrainCircuit size={18}/><h3>AI 优化持仓</h3></div>
    <div className="portfolio-optimization-controls">
     {jobs.length>0 && <label className="portfolio-optimization-history"><History size={14}/><select aria-label="优化记录" value={job?.id || ''} disabled={busy} onChange={(e) => {if(!config)return;setError('');void requestJSON<{data:PortfolioOptimizationJob}>(config,`/api/v1/portfolio-optimizations/${encodeURIComponent(e.target.value)}`).then((p) => remember(p.data)).catch((cause) => setError(cause instanceof Error ? cause.message:'记录加载失败'));}}>{jobs.map((j) => <option key={j.id} value={j.id}>{dateLabel(j.started_at)} · {j.status==='running'?'进行中':outcomeLabel[j.outcome || ''] || j.status}</option>)}</select></label>}
     {job?.status==='running' ? <button type="button" className="portfolio-task-action" disabled={busy} onClick={() => void control('cancel')}>停止优化</button> : <button type="button" className="portfolio-task-action" disabled={!canStart || busy || applyBusy} onClick={() => void start()}>{launching ? <LoaderCircle size={14} className="spin"/> : <BrainCircuit size={14}/>}{launching?'正在启动':'开始优化'}</button>}
    </div>
   </header>
   <OptimizationTimeline job={job} launching={launching}/>
   {!!job?.portfolio_needs?.length && <div className="portfolio-optimization-needs"><strong>本次需要改善</strong><ul>{job.portfolio_needs.map((need,i) => <li key={i}>{need}</li>)}</ul></div>}
   <div className="portfolio-optimization-status" aria-live="polite">
    {launching ? <p role="status">正在创建优化任务…</p> : job?.status==='running' ? <>
     <p role="status"><span className="portfolio-optimization-live-dot"/>{job.message || `正在${stageLabel[job.stage] || '处理'}`}</p>
     <small>第{(job.revision_count || 0)+1}轮 · 候选 {job.candidates.length} 只 · 选中 {job.candidates.filter((c) => c.selected).length} 只 · 已完成研究 {job.completed_research ?? job.results.filter((r) => r.status==='succeeded').length} 份{job.model_progress && (job.stage==='proposing' || job.stage==='assessing') && ` · 本次模型 ${Math.round(job.model_progress.elapsed_ms/1000)} 秒 · ${job.model_progress.text_bytes>0?'正在生成结果':job.model_progress.reasoning_bytes>0?'正在思考':'等待响应'}`}</small>
     {job.checkpoint_progress && <small>已保存股票判断 {job.checkpoint_progress.saved_stocks}/{job.checkpoint_progress.total_stocks} · 待补 {job.checkpoint_progress.pending_parts} 项 · 已保存复评部分 {job.checkpoint_progress.saved_review_blocks} 项 · 已复评 {job.checkpoint_progress.reviewed_configurations} 个配置</small>}
     <small>离开页面可继续，完成后自动显示持仓对比。</small>
    </> : completed ? <p><ShieldCheck size={15}/>{outcomeLabel[job.outcome || ''] || '评估完成'} · {dateLabel(job.completed_at || job.updated_at)}</p> : job?.resume_available ? <p>{job.status==='cancelled'?'优化已停止':'优化尚未完成'}，已保存报告与方案，可恢复当前阶段。</p> : <p>开始优化后，可在这里查看当前进度。</p>}
   </div>
   {job?.resume_available && !launching && <div className="portfolio-report-warning portfolio-recovery"><CircleAlert size={16}/><span>{job.error || job.message}</span><button type="button" disabled={busy} onClick={() => void control('resume')}>恢复缺失阶段</button></div>}
   {job && !job.resume_available && !launching && job.status!=='running' && (job.status!=='succeeded' || job.outcome==='review_invalid') && <div className="portfolio-report-warning portfolio-recovery"><CircleAlert size={16}/><span>{job.error || job.outcome_reason || job.message}。可重新开始，24小时内有效研究继续复用。</span><button type="button" disabled={busy || !canStart} onClick={() => void start()}>重新开始优化</button></div>}
   {error && <div className="portfolio-error" role="alert"><CircleAlert size={16}/><span>{error}</span>{(error.includes('过期') || error.includes('先刷新巡检')) && <button type="button" disabled={applyBusy} onClick={onRefreshSource}>刷新原巡检</button>}</div>}
   {!canStart && <div className="portfolio-report-warning"><CircleAlert size={16}/><span>请先补齐或刷新巡检，完成当前口径的四维评分后再优化。24小时内有效个股研究会复用。</span><button type="button" disabled={applyBusy} onClick={onRefreshSource}>刷新原巡检</button></div>}
  </div>
  {completed && <div className="portfolio-optimization-primary">
   {job.version && job.version!==portfolioOptimizationVersion && <div className="portfolio-report-warning"><CircleAlert size={16}/><span>这份记录使用历史评分或筛选规则。请重新优化以应用当前筛选、复评与采纳规则，24小时内有效个股研究继续复用。</span></div>}
   {job.version===portfolioOptimizationVersion && job.model_prompt_version!==portfolioOptimizationPromptVersion && <div className="portfolio-report-warning"><CircleAlert size={16}/><span>这份记录使用旧版复评，请重新优化后再使用目标方案；24小时内有效个股研究继续复用。</span></div>}
   {approved && preferredPlan ? <OptimizationPlan job={job} plan={preferredPlan} selected={approved} applyBusy={applyBusy} onApply={onApply} onOpenStockAnalysis={onOpenStockAnalysis}/> : <UnchangedResult job={job}/>}
  </div>}
  {!!job?.revision_history?.length && <details className="stock-ai-panel portfolio-optimization-fold"><summary>查看方案改进记录（{job.revision_history.length}）</summary><div>{job.revision_history.map((r,i) => <article key={i}><p>第{r.round}轮 · {r.name} · 独立复评 {r.conclusion.total_score ?? '未知'}分 / 优化目标70分</p><p>{r.error}</p>{r.conclusion.dimensions?.map((d) => <p key={d.key}>{d.label} {d.score}：{d.reason}</p>)}<p>{r.assessment?.residual_risks?.join('；')}</p></article>)}</div></details>}
  {!!job?.proposal?.range_adjustments?.length && <details className="stock-ai-panel portfolio-optimization-fold"><summary>查看配仓范围调整</summary><div>{job.proposal.range_adjustments.map((r) => <p key={r.symbol}>{job.results.find((s) => s.holding.symbol===r.symbol)?.holding.name || r.symbol} · AI范围建议 {r.min_weight}%–{r.max_weight}% · {r.reason}</p>)}{job.proposal.range_bound_corrections?.map((text,i) => <p key={i}>{text}</p>)}</div></details>}
  {otherPlans.length>0 && <details className="stock-ai-panel portfolio-optimization-fold"><summary>{otherPlans.every((p) => p.status==='invalid_review')?'查看复评异常方案':'查看其他方案'}（{otherPlans.length}）</summary><div>{otherPlans.map((p,i) => <OptimizationPlan key={i} job={job!} plan={p} selected={false} applyBusy={applyBusy} onApply={onApply} onOpenStockAnalysis={onOpenStockAnalysis}/>)}</div></details>}
  <details className="stock-ai-panel portfolio-optimization-fold portfolio-optimization-settings"><summary>候选与优化规则</summary><div>
   <p>先从最多16个初启行业分批检查，最多检查80只股票、耗时3分钟；淘汰后继续补选，保留最多8只合格候选，排除ST、上一交易日涨跌停及近20个交易日涨幅超过50%的股票，再按行业分散选出最多6只完成个股AI研究，同时核对来源板块和股票目录，同一行业最多1只。预算内合格行业不足6个时保留实际数量。</p><p>财务筛选采用双通道：营收高增/稳增，或持续盈利、经营稳健且估值合理，不要求营收利润高增。估值通道需两期营收同比≥-5%、归母及扣非利润同比≥-20%、扣非/归母≥50%，最新ROE和经营现金流为正；PE(TTM)≤25、PB≤3，银行上限12和1.5。仅用最新有效交易日数据，缺失不视为便宜。这是初筛区间，最终还需比较行业、周期、业务及组合用途。</p><p>以70分以上为优化目标：程序在AI确认的投资范围内搜索盈利质量、波动和结构，先用统一标准从可行配置中选出一个，再独立复评。只有有效复评指出问题且配置发生实质改善时，最多再复评一个配置，复用原研究。完整搜索后，65–69分且比原组合提高至少5分、四维均不低于50分的方案也可作为备选，明确显示未达70分目标。独立偏好、实际改善和风险、资金约束均通过才可采纳。按盈利质量、增长来源、估值、量价位置、组合角色和实际风险比较旧股增持与新股配置。报告资料等级不决定能否增持；值得保留与值得增加资金分别判断。</p><p>评分仅评价股票组合本身：总仓位、满仓和现金比例不加扣分，股票内部集中度仍评价；集中问题只归结构维度。沿用原风格与周期，保持股票总仓位和现金。被替换资金最多占初始股票仓位的70%，卖出与买入只计一次。</p>
   <label className="portfolio-optimization-candidates">指定可选候选股票（最多8只）<input value={candidates} onChange={(e) => setCandidates(e.target.value)} disabled={busy || job?.status==='running'} placeholder="例如 600519 000858"/></label><small>用户候选也须通过相同的筛选规则，空白时自动筛选。</small>
  </div></details>
  {!!job?.model_attempts?.length && <details className="stock-ai-panel portfolio-optimization-fold portfolio-model-diagnostics"><summary>模型阶段记录</summary><div><p>输入统计为业务提示词 UTF-8 字节数，不含运行时系统指令。首轮方案输入最多22 KiB（研究股票超过10只时最多28 KiB）、改进方案和复评24 KiB，含格式修复最多32 KiB；每轮方案和复评阶段各最多8分钟，全部轮次及恢复共用24分钟累计运行上限，暂停时间不计入。每个操作最多纠错4次，同一错误连续3次则停止；已通过的条目独立保存。</p><div className="portfolio-optimization-scores"><table><thead><tr><th>阶段</th><th>业务输入</th><th>耗时 / 调用预算</th><th>返回正文</th><th>结果</th></tr></thead><tbody>{job.model_attempts.map((a,i) => <tr key={i}><td>{a.round ? `第${a.round}轮 · ` : ''}{stageLabel[a.stage] || a.stage}</td><td>{(a.prompt_bytes/1024).toFixed(1)} KiB<br/>{a.prompt_bytes.toLocaleString()} 字节</td><td>{Math.round(a.duration_ms/1000)} 秒 / {a.budget_ms ? `${Math.round(a.budget_ms/1000)} 秒` : '旧版共享预算'}</td><td>{a.response_bytes.toLocaleString()} 字节</td><td>{a.error || '校验通过'}</td></tr>)}</tbody></table></div></div></details>}
  {job?.screening_audit && <details className="stock-ai-panel portfolio-optimization-fold"><summary>代码筛查：检查 {job.screening_audit.checked} 只 · 合格 {job.screening_audit.qualified} 只 · 保留 {job.screening_audit.retained} 只</summary><div><p>耗时 {Math.ceil(job.screening_audit.duration_ms/1000)} 秒 · 上限 {job.screening_audit.check_limit} 只 / {Math.round(job.screening_audit.budget_ms/60000)} 分钟 · 行业分散可选 {job.screening_audit.diverse} 只 · 实际进入 AI {job.candidates.filter((c) => c.selected).length} 只</p><p>{({candidates_ready:'合格候选与行业数量已充足',industry_limit:'本轮可检查范围的行业分散已达上限，保留实际数量',check_limit:'已达到检查数量上限',time_limit:'已达到代码筛查时间上限，保留已通过的结果',pool_exhausted:'已检查完可用的初启行业候选'})[job.screening_audit.stop_reason] || job.screening_audit.stop_reason}</p>{job.screening_audit.records?.map((r) => <p key={r.symbol}>{r.name || r.symbol} · {r.industry} · {r.qualified?'合格':'淘汰'}：{r.reason}</p>)}</div></details>}
  {job && <details className="portfolio-research-baseline stock-ai-panel portfolio-optimization-fold"><summary>候选范围、交易资格与研究来源</summary><div><p>原巡检历史评分 {job.source_report.conclusion.total_score ?? '未知'}；不用于直接计算本次评分改善。最新有效交易日 {job.latest_trade_date || '待确认'} · 统一行情冻结 {dateLabel(job.snapshot_at)}</p><p>复用 {job.reused_stocks} 份 · 新研究 {job.new_stocks} 份 · 优化链初始报告 {job.root_source_id}</p>{job.candidates.map((c) => <p key={c.symbol}>{c.name || c.symbol}{c.industry_group && ` · 行业：${c.industry_group}`} · {optimizationCandidateSource(c.source)} · {c.reason}{c.portfolio_fit_reason && ` · 组合匹配：${c.portfolio_fit_reason}`}{c.screening && <small> · 代码筛选{c.screening.qualified?'通过':'未通过'}{c.screening.recent_return_percent!==undefined && ` · 近${c.screening.return_sessions}交易日 ${c.screening.recent_return_percent.toFixed(1)}%`}{c.screening.revenue_yoy!==undefined && ` · 营收同比 ${c.screening.revenue_yoy.toFixed(1)}%`}{c.screening.deducted_profit_share!==undefined && ` · 同期扣非/归母 ${(c.screening.deducted_profit_share*100).toFixed(1)}%`}{c.screening.valuation_reason && ` · ${c.screening.valuation_reason}`}{c.screening.valuation && ` · 估值日期 ${dateLabel(c.screening.valuation.trade_time)} · ${c.screening.valuation.meta.source}`}{c.screening.financial_method && ` · ${c.screening.financial_method}`}</small>}</p>)}{(job.eligibility || []).map((e) => <p key={e.symbol}>{job.results.find((r) => r.holding.symbol===e.symbol)?.holding.name || job.candidates.find((c) => c.symbol===e.symbol)?.name || e.symbol} · {e.locked?'锁定调整':e.can_increase?'允许条件核验':'不可新增/增持'} · {e.reason}{e.liquidity_trade_date && <small> · 成交日期 {e.liquidity_trade_date} · {optimizationLiquiditySource(e.liquidity_source)}</small>}</p>)}{job.limitations.map((l,i) => <p key={i}>{l}</p>)}{job.results.map((r) => <p key={r.holding.symbol}>{r.holding.name || r.holding.symbol} · 报告 {r.analysis_id} · 完成 {dateLabel(r.report_completed_at)} · 证据 {dateLabel(r.research_cutoff_at)}{r.analysis && <button type="button" onClick={() => onOpenStockAnalysis(r.analysis!)}><ExternalLink size={13}/>查看研究</button>}</p>)}</div></details>}
 </section>;
}

function OptimizationTimeline({job,launching}: {job:PortfolioOptimizationJob|null;launching:boolean}) {
 const finished = job?.status==='succeeded' && !launching;
 const current = launching ? 0 : finished ? optimizationSteps.length-1 : job ? optimizationSteps.findIndex((s) => s.key===job.stage) : -1;
 const active = launching || job?.status==='running';
 const interrupted = !active && Boolean(job?.resume_available || ['failed','incomplete','cancelled'].includes(job?.status || ''));
 return <div className="portfolio-optimization-timeline" aria-label="AI 优化持仓进度">
  <div className="portfolio-optimization-rail" aria-hidden="true"><i style={{width:`${Math.max(0,current)/(optimizationSteps.length-1)*100}%`}}/></div>
  <ol>{optimizationSteps.map((s,i) => {
   const skipped = finished && s.key==='assessing' && !job?.plans.some((p) => p.assessment) && !job?.model_attempts?.some((a) => a.stage==='assessing');
   const done = !skipped && (finished || (current>=0 && i<current));
   const here = i===current && (active || interrupted);
   const state = skipped ? 'skipped' : done ? 'done' : here ? interrupted ? 'paused' : 'active' : 'pending';
   return <li key={s.key} className={state} data-stage={s.key} aria-current={here?'step':undefined} aria-label={`${s.label}：${skipped?'未执行':done?'已完成':here?interrupted?'已停止 / 未完成':'进行中':'未开始'}`}><span className="portfolio-optimization-node" aria-hidden="true">{skipped ? <Minus size={14}/> : done ? <Check size={14}/> : here ? interrupted ? job?.status==='cancelled' ? <Pause size={12}/> : <CircleAlert size={14}/> : <LoaderCircle size={14} className="spin"/> : <i/>}</span><span>{s.label}{skipped && <small>未执行</small>}</span></li>;
  })}</ol>
 </div>;
}

function UnchangedResult({job}: {job:PortfolioOptimizationJob}) {
 const holdings = job.source_report.request?.holdings || job.source_report.holdings.map((r) => r.holding);
 return <ResearchPanel label="评估完成" title={job.outcome==='below_target_score'?'评分与改善要求未通过，保留原持仓':job.outcome==='no_feasible_plan'?'未生成可行调整方案':job.outcome==='review_invalid'?'独立复评无效，原持仓未调整':'本次未生成调整方案'} icon={job.outcome==='below_target_score'?CircleAlert:ShieldCheck} className="portfolio-optimization-result"><p className="portfolio-optimization-decision">{job.outcome_reason || '当前投资比较与约束下未形成可采纳的新组合，原持仓暂未调整，不代表已通过优化推荐。'}</p><HoldingComparison original={holdings} target={holdings} targetLabel="原持仓（暂未调整）"/></ResearchPanel>;
}

function OptimizationPlan({job,plan,selected,onApply,onOpenStockAnalysis,applyBusy}: {job:PortfolioOptimizationJob;plan:PortfolioOptimizationPlan;selected:boolean;onApply:Props['onApply'];onOpenStockAnalysis:Props['onOpenStockAnalysis'];applyBusy:boolean}) {
 if (!plan.checks?.valid) return <ResearchPanel label="方案校验" title={plan.name} icon={CircleAlert}><p>{plan.error || plan.checks?.errors.join('；') || '未形成可行配置'}</p></ResearchPanel>;
 const current = job.source_report.request!.holdings;
 const rows = [...current.map((h) => h.symbol),...plan.target.filter((h) => !current.some((o) => o.symbol===h.symbol)).map((h) => h.symbol),...plan.allocations.filter((a) => !current.some((h) => h.symbol===a.symbol) && !plan.target.some((h) => h.symbol===a.symbol)).map((a) => a.symbol)];
 const originalMap=new Map(current.map((h) => [h.symbol,h]));const targetMap=new Map(plan.target.map((h) => [h.symbol,h]));
 const scoreImprovement=portfolioScoreImprovement(plan.original_comparison, plan.target_comparison);
 const total=current.reduce((sum,h) => sum+h.weight_percent,0);
 const waitsForEntry=(a:PortfolioOptimizationPlan['allocations'][number]) => a.investment?.action==='wait' || (a.allocation_conditions || []).some((c) => c.kind==='entry' && c.status==='pending');
 const evidenceReport: PortfolioInspectionReport={...plan.original_comparison,holdings:job.results,facts:{...plan.original_comparison.facts,...job.union_facts}};
 const apply=() => { const request:PortfolioResearchRequest={...job.source_report.request!,holdings:plan.target.map((h) => ({...h,cost_price:h.weight_percent>(originalMap.get(h.symbol)?.weight_percent || 0)?undefined:h.cost_price})),source_optimization_id:job.id,force_symbols:[]};onApply(request); };
 return <ResearchPanel label={selected?(plan.target_comparison.conclusion.total_score!<70?'合格备选 · 未达70分目标':'通过复评 · 条件性目标'):plan.status==='invalid_review'?'复评异常 · 暂无有效评分':plan.status==='pending_review'?'方案尚未完成复评':'未采纳方案'} title="持仓优化结果" icon={ShieldCheck} className="portfolio-optimization-result">
  {plan.status==='invalid_review' && <p role="alert" className="portfolio-report-warning">复评发生数据引用错误，尚无有效评分，不能判断这份方案是否值得采纳。{plan.error}</p>}
  {plan.allocations.some((a) => waitsForEntry(a) && (targetMap.get(a.symbol)?.weight_percent || 0)>(originalMap.get(a.symbol)?.weight_percent || 0)) && <p className="portfolio-report-warning">等待买点：目标组合在相应价格或条件满足后再调整，当前实际持仓仍为左侧原组合。</p>}
  <div className="stock-ai-tags">{plan.allocations.filter((a) => (targetMap.get(a.symbol)?.weight_percent || 0)>(originalMap.get(a.symbol)?.weight_percent || 0)).map((a) => <span key={a.symbol}>{targetMap.get(a.symbol)?.name || a.symbol} · {a.investment?.role || '建议配置'} · {waitsForEntry(a)?'等待买点':selected?'优先配置':'方案拟配置'}</span>)}</div>
  <HoldingComparison original={current} target={plan.target} targetLabel={selected?'建议持仓':plan.status==='invalid_review'?'候选目标（复评异常）':'候选目标（未采纳）'} originalScore={plan.original_comparison.conclusion.total_score} targetScore={plan.target_comparison.conclusion.total_score}/>
  {scoreImprovement !== undefined && <p className="portfolio-scoring-scope">股票组合健康度 {plan.original_comparison.conclusion.total_score} → {plan.target_comparison.conclusion.total_score} 分 · 本次优化改善 {scoreImprovement > 0 ? '+' : ''}{scoreImprovement} 分（同次独立复评）。总仓位和现金比例不参与评分。</p>}
  <div className="stock-ai-tags"><span>总仓位 {total}% → {plan.checks.total_position_percent}%</span><span>现金 {100-total}% → {plan.checks.cash_percent}%</span><span>相对初始替换 {plan.checks.replacement_ratio_percent.toFixed(1)}% / 上限70%</span><span>保留重叠 {plan.checks.retained_percent}% 总资产</span></div>
  {plan.status!=='invalid_review' && <p className="portfolio-optimization-decision">{selected && plan.target_comparison.conclusion.total_score!<70 ? job.outcome_reason : (!selected && (plan.rejection_reasons?.join('；') || plan.error)) || plan.assessment?.reason || '方案仅为建议配置，尚未成交。'}</p>}
  {selected && <div className="portfolio-optimization-apply"><button type="button" className="primary" disabled={applyBusy} onClick={apply}>使用该方案发起新巡检</button><small>新增/增持成本为空，待实际成交后确认；原报告和原成本保持可追溯。确认条件未满足时先保留原组合。</small></div>}
  <details className="portfolio-optimization-result-details"><summary>查看评分对比与调仓依据</summary><div>
  {plan.allocation_search && <p>程序配仓搜索：{plan.allocation_search.method} · 检查 {plan.allocation_search.evaluated_allocations.toLocaleString()} 个整数配仓状态 · 归母和扣非均盈利仓位 {plan.allocation_search.profitable_weight_percent}% · 扣非亏损仓位 {plan.allocation_search.deducted_loss_weight_percent}% · ATR与历史相关性的波动代理 {plan.allocation_search.volatility_proxy_percent.toFixed(2)}%（用于排序，不是复评分数或未来损失预测）。</p>}
  <p>本次资金来源 {plan.trade_sold_percent ?? plan.checks.sold_percent} 个总资产百分点 → 买入用途 {plan.trade_bought_percent ?? plan.checks.bought_percent} 个百分点；累计替换始终相对优化链初始组合。</p>{(plan.funding || []).map((f,i) => <p key={i}>目标资金配对：{originalMap.get(f.from_symbol)?.name || f.from_symbol} → {targetMap.get(f.to_symbol)?.name || f.to_symbol} {f.weight_percent} 个百分点（尚未成交）</p>)}
  {plan.improvements.map((m,i) => m.kind==='investment' ? <div key={i}><p>投资比较：{originalMap.get(m.from_symbol || '')?.name || m.from_symbol} → {targetMap.get(m.to_symbol || '')?.name || m.to_symbol} · {m.weight_percent} 个百分点</p><p>{m.reason}</p><p>代价：{m.tradeoff}</p><PortfolioEvidence refs={m.evidence_refs || []} report={evidenceReport}/></div> : <p key={i}>{m.issue}：{m.before} → {m.after}</p>)}
  {(plan.risk_checks || []).map((r,i) => <p key={i} className={r.hard && !r.passed?'portfolio-report-warning':undefined}>{r.name}：{r.before_percent}% → {r.after_percent}% · {r.hard?`硬性检查${r.passed?'通过':'未通过'}，上限${r.limit_percent}%`:'由独立复评判断，不作硬性否决'}<small> · {r.basis}</small></p>)}
  {plan.error && plan.status!=='invalid_review' && <p className="portfolio-report-warning">未采纳原因：{plan.error}</p>}
  {plan.assessment && <>{plan.assessment.preferred_configuration && <p>复评对应：A 为{plan.assessment_order==='target_first'?'建议持仓':'原持仓'}，B 为{plan.assessment_order==='target_first'?'原持仓':'建议持仓'}。</p>}<p>{plan.assessment.reason}</p><PortfolioEvidence refs={plan.assessment.evidence_refs} report={evidenceReport}/><ul>{[...plan.assessment.tradeoffs,...plan.assessment.residual_risks].map((s,i) => <li key={i}>{s}</li>)}</ul><div className="portfolio-optimization-scores"><table><caption>同一证据与行情下的独立复评（非未来收益）</caption><thead><tr><th>维度</th><th>原组合</th><th>建议组合</th></tr></thead><tbody>{plan.original_comparison.conclusion.dimensions?.map((d) => { const next=plan.target_comparison.conclusion.dimensions?.find((n) => n.key===d.key);return <tr key={d.key}><th>{d.label}</th><td>{d.score}<details><summary>依据</summary><p>{d.reason}</p><PortfolioEvidence refs={d.evidence_refs} report={plan.original_comparison}/></details></td><td>{next?.score ?? '未知'}<details><summary>依据</summary><p>{next?.reason}</p><PortfolioEvidence refs={next?.evidence_refs || []} report={plan.target_comparison}/></details></td></tr>;})}<tr><th>综合评分 / 风险</th><td>{plan.original_comparison.conclusion.total_score} / {plan.original_comparison.conclusion.risk_level}</td><td>{plan.target_comparison.conclusion.total_score} / {plan.target_comparison.conclusion.risk_level}</td></tr><tr><th>最大单票 / 前三大仓位（占总资产，执行展示）</th><td>{plan.original_comparison.metrics.max_single_percent ?? '未知'}% / {plan.original_comparison.metrics.top_three_percent ?? '未知'}%</td><td>{plan.target_comparison.metrics.max_single_percent ?? '未知'}% / {plan.target_comparison.metrics.top_three_percent ?? '未知'}%</td></tr><tr><th>成功 AI 研究覆盖</th><td>{plan.original_comparison.metrics.ai_research_coverage_percent ?? '未知'}%</td><td>{plan.target_comparison.metrics.ai_research_coverage_percent ?? '未知'}%</td></tr><tr><th>历史相关性</th><td>{correlationSummary(plan.original_comparison)}</td><td>{correlationSummary(plan.target_comparison)}</td></tr></tbody></table></div></>}
  {job.plans.length>0 && plan.original_comparison.conclusion.risk_groups?.map((g,i) => <p key={i}>共同驱动 {g.name}：{g.weight_percent}% → {plan.target_comparison.conclusion.risk_groups?.[i]?.weight_percent ?? '未知'}%（分组固定，可能重叠）</p>)}
  <div className="portfolio-optimization-actions">{rows.map((symbol) => {const a=plan.allocations.find((a) => a.symbol===symbol);const research=job.results.find((r) => r.holding.symbol===symbol);const conditions=research?.analysis?.research_report?.conditions || [];return a && <details key={symbol}><summary>{originalMap.get(symbol)?.name || targetMap.get(symbol)?.name || research?.holding.name || symbol} · {!originalMap.has(symbol) && !targetMap.has(symbol)?'本次未纳入':actionLabel(originalMap.get(symbol)?.weight_percent || 0,targetMap.get(symbol)?.weight_percent || 0)}</summary><p>目标比例：{originalMap.get(symbol)?.weight_percent || 0}% → {targetMap.get(symbol)?.weight_percent || 0}%（尚未成交）</p><p>模型配置理由：{a.reason}</p><p>方案参考比例：{a.preferred_weight}% · 资金用途：{a.funding_reason}</p><p>增量资金比较：{a.suitability_reason || '本次仅保留或减持'}</p>{a.investment && <InvestmentDetails investment={a.investment}/>} {(a.allocation_conditions || []).map((c,i) => <div key={i}><p>{c.kind==='entry'?'入场':'退出'}：{c.text} · 待验证</p><small>观察方法：{c.verification}</small><PortfolioEvidence refs={c.evidence_refs} report={evidenceReport}/></div>)}{!!a.confirmation_ids?.length && <p>原确认条件：{a.confirmation_ids.map((id) => conditions.find((c) => c.id===id)?.text || id).join('；')}</p>}{!!a.invalidation_ids?.length && <p>原失效条件：{a.invalidation_ids.map((id) => conditions.find((c) => c.id===id)?.text || id).join('；')}</p>}<PortfolioEvidence refs={a.evidence_refs} report={evidenceReport}/>{research?.analysis && <button onClick={() => onOpenStockAnalysis(research.analysis!)}><ExternalLink size={13}/>查看个股研究</button>}</details>;})}</div>
  </div></details>
 </ResearchPanel>;
}


function InvestmentDetails({investment}: {investment:NonNullable<PortfolioOptimizationPlan['allocations'][number]['investment']>}) {
 const rows: Array<[string,string]>=[['投资角色',investment.role],['盈利质量',investment.business],['增长来源',investment.growth],['估值与空间',investment.valuation],['买入位置',investment.timing],['组合作用',investment.portfolio_fit],['主要风险',investment.risk],['退出',investment.exit],['其他资金用途比较',investment.opportunity_cost],['原报告意见',investment.prior_opinion],['周期适用',investment.period_suitability]];
 return <div className="portfolio-investment-details">{rows.filter(([,value]) => value).map(([label,value]) => <p key={label}><strong>{label}：</strong>{value}</p>)}</div>;
}


function HoldingComparison({original,target,targetLabel,originalScore,targetScore}: {original:PortfolioHolding[];target:PortfolioHolding[];targetLabel:string;originalScore?:number;targetScore?:number}) {
 const originalMap = new Map(original.map((h) => [h.symbol,h]));
 const targetMap = new Map(target.map((h) => [h.symbol,h]));
 const rows = [...original.map((h) => h.symbol),...target.filter((h) => !originalMap.has(h.symbol)).map((h) => h.symbol)];
 const total = original.reduce((sum,h) => sum+h.weight_percent,0);
 const targetTotal = target.reduce((sum,h) => sum+h.weight_percent,0);
 return <div className="portfolio-comparison" aria-label="原持仓与建议持仓对比">
  <div className="portfolio-comparison-side"><header><div><h3>原持仓</h3><small>优化前配置</small></div>{typeof originalScore==='number' && <span className="portfolio-comparison-score">{originalScore}<small>综合评分</small></span>}</header>{rows.map((symbol) => <HoldingRow key={symbol} symbol={symbol} holding={originalMap.get(symbol)} name={targetMap.get(symbol)?.name}/>)}<strong className="portfolio-comparison-total">股票 {total}% · 现金 {100-total}%</strong></div>
  <div className="portfolio-comparison-arrow" aria-hidden="true"><ArrowRight className="desktop-arrow" size={28}/><ArrowDown className="mobile-arrow" size={24}/></div>
  <div className="portfolio-comparison-side target"><header><div><h3>{targetLabel}</h3><small>{targetLabel==='原持仓（暂未调整）'?'本次未生成新组合':'建议配置 · 尚未成交'}</small></div>{typeof targetScore==='number' && <span className="portfolio-comparison-score">{targetScore}<small>综合评分</small></span>}</header>{rows.map((symbol) => <HoldingRow key={symbol} symbol={symbol} holding={targetMap.get(symbol)} name={originalMap.get(symbol)?.name} originalWeight={originalMap.get(symbol)?.weight_percent || 0}/>)}<strong className="portfolio-comparison-total">股票 {targetTotal}% · 现金 {100-targetTotal}%</strong></div>
 </div>;
}

function actionLabel(old:number,next:number) {if(old===next)return '保持';if(old===0)return `新增 ${next}%`;if(next===0)return '清仓';return `${next>old?'增持':'减持'} ${Math.abs(next-old)} 个百分点`;}
function HoldingRow({symbol,holding,name,originalWeight}: {symbol:string;holding?:PortfolioHolding;name?:string;originalWeight?:number}) {
 const weight=holding?.weight_percent || 0;const increased=originalWeight!==undefined && weight>originalWeight;
 return <article className="portfolio-comparison-row"><div><strong>{holding?.name || name || symbol}</strong><small>{symbol}</small></div><b>{weight>0?`${weight}%`:originalWeight===undefined?'未持有':'0%'}</b><span>{originalWeight===undefined ? holding?.cost_price ? `原成本 ${holding.cost_price}`:'成本未填写' : actionLabel(originalWeight,weight)}</span><small>{originalWeight!==undefined && (increased?'成本待实际确认':weight===0?'清仓计划，尚未成交':holding?.cost_price?`原成本 ${holding.cost_price}`:'成本未填写')}</small></article>;
}

function correlationSummary(report:PortfolioInspectionReport) {
 if ((report.request?.holdings.length || 0)<2) return '不适用（单只持仓）';
 const pairs=Object.entries(report.facts || {}).filter(([key]) => key.startsWith('correlation.'));
 const known=pairs.filter(([,fact]) => fact.available).length;
 if(known===0) return '未知，配对样本不足';
 return `已核验 ${known}/${pairs.length} 对 · 高相关 ${report.metrics.high_correlations?.length || 0} 对${known<pairs.length?' · 其余未知':''}`;
}
