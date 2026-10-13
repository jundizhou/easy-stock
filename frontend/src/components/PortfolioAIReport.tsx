import { Activity, BrainCircuit, CircleAlert, ExternalLink, GitBranch, HeartPulse, History, ListChecks, Plus, Scale, ShieldCheck, WalletCards } from 'lucide-react';
import type { ReactNode } from 'react';
import type { PortfolioETFScoring, PortfolioEvidenceRef, PortfolioInspectionReport, InstrumentAnalysis } from '../lib/backend';
import { portfolioScoringVersion } from '../lib/portfolio-optimization';
import { safeResearchURL } from '../lib/stock-research';
import { ResearchPanel } from './StockResearchReport';

type Props = { report: PortfolioInspectionReport; onNew: () => void; onOpenStockAnalysis: (analysis: InstrumentAnalysis) => void; onRefresh: (symbol: string) => void; busy?: boolean; onOptimize?: () => void; afterSummary?: ReactNode };
export function PortfolioAIReportView({ report, onNew, onOpenStockAnalysis, onRefresh, busy, onOptimize, afterSummary }: Props) {
 const { conclusion: ai, metrics } = report;
 const coverage = metrics.ai_research_coverage_percent ?? 0;
 const successful = report.holdings.filter((r) => r.status === 'succeeded' && r.analysis?.ai.status === 'ready').length;
 const reused = report.holdings.filter((r) => r.research_origin === 'reused').length;
 const fresh = report.holdings.filter((r) => r.research_origin === 'new').length;
 const names = new Map(report.holdings.map((r) => [r.holding.symbol, r.holding.name || r.analysis?.name || r.holding.symbol]));
 const score = ai.score_available && typeof ai.total_score === 'number' && Number.isFinite(ai.total_score) ? ai.total_score : undefined;
 const containsETF = report.holdings.some(r => r.analysis?.instrument_type === 'etf');
 const stopCoverage = metrics.stop_loss_coverage_percent ?? 0;
 return <div className="portfolio-report stock-research-report portfolio-ai-report">
  <header className={`stock-ai-verdict portfolio-report-verdict ${ai.risk_level.includes('高') || ai.risk_level === '中' ? 'risk' : 'strong'}`}>
   <div className="stock-ai-identity"><span>持仓 AI 分析</span><h2>{report.profile.label}型组合</h2><div className={`portfolio-total-score${score === undefined ? ' pending' : ''}`} role="group" aria-label="本次持仓分析综合评分"><span>巡检综合评分</span><strong>{score ?? '待完成'}{score !== undefined && <small>/ 100</small>}</strong></div><small>{portfolioDate(report.generated_at)} · {horizonLabel(report.request?.horizon)}</small></div>
   <div className="stock-ai-conclusion"><div className="stock-ai-tags"><span>{ai.risk_level === '待评估' ? '风险待评估' : `${ai.risk_level}风险`}</span><span>AI 研究 {successful}/{report.holdings.length} 只 · {coverage}% 持仓</span><span>复用 {reused} 份 · 新研究 {fresh} 份</span></div><h3>组合结论</h3><p>{ai.executive_summary}</p><small>风险判断：{ai.risk_reason || '组合评估尚未完成'}</small></div>
   <div className="stock-ai-verdict-actions">{onOptimize && <button type="button" className="primary" disabled={busy} onClick={onOptimize}><BrainCircuit size={15} />AI 优化持仓</button>}<button type="button" className="primary" disabled={busy} onClick={onNew}><Plus size={15} />新建分析</button></div>
  </header>
  {ai.score_available && <p className="portfolio-scoring-scope">{report.algorithm_version === portfolioScoringVersion ? containsETF ? '评分评价股票与 ETF 组合：总仓位和现金比例不加扣分；ETF 按底层资产、基金产品与执行条件判断，持仓重叠按披露范围解释。' : '评分仅评价股票组合：总仓位、满仓和现金比例不加扣分；集中度按股票内部配比评价。60分基本可用，70分整体合理，80分较好。' : '此报告使用历史评分口径；重新评估后采用当前评分口径。'}</p>}
  {ai.score_available && <section className="stock-ai-kpis portfolio-score-dimensions" aria-label="组合 AI 四维评分">
   {(ai.dimensions || []).map((d, i) => <article key={d.key} className={`stock-ai-kpi ${['blue', 'purple', 'amber', 'green'][i]}`}><div>{i === 0 ? <HeartPulse size={17} /> : i === 1 ? <Scale size={17} /> : i === 2 ? <ShieldCheck size={17} /> : <Activity size={17} />}{d.label}</div><strong>{d.score}</strong><small>权重 {d.weight}%</small><p>{d.reason}</p><details><summary>评分依据与来源</summary>{(d.adjustments || []).map((a, i) => <p key={i}><b>{a.points > 0 ? '+' : ''}{a.points} · </b>{a.reason}</p>)}{(d.limitations || []).map((l, i) => <div className="portfolio-evidence-limitation" key={i}><p>{l}</p><ExplanationEvidence report={report} path={`dimensions.${d.key}.limitations[${i}]`} /></div>)}<PortfolioEvidence refs={d.evidence_refs || []} report={report} /></details></article>)}
  </section>}
  {ai.score_available && ai.etf_scoring && <PortfolioETFScoreDetails scoring={ai.etf_scoring} report={report} />}
  {afterSummary}
  <div className="portfolio-report-grid">
   <ResearchPanel label="优先处理" title="处理顺序" icon={ListChecks}><ReportList report={report} field="adjustment_order" items={ai.adjustment_order || []} empty="组合评估完成后生成处理顺序" ordered /></ResearchPanel>
   <ResearchPanel label="风险识别" title="主要风险" icon={CircleAlert} className="stock-research-counter"><ReportList report={report} field="primary_risks" items={ai.primary_risks || []} empty={ai.score_available ? '没有识别到突出风险' : '组合评估尚未完成'} /></ResearchPanel>
  </div>
  <section className="portfolio-holding-report" aria-label="逐项持仓判断"><header><div><span>当前持仓</span><h3>逐项持仓判断</h3></div><small>当前仓位与成本 · 原研究可追溯</small></header><div className="portfolio-holding-cards">
   {report.holdings.map((r) => {
    const item = ai.holdings?.find((h) => h.symbol === r.holding.symbol);
    const research = r.analysis?.research_report;
    const pnl = report.facts?.[`${r.holding.symbol}.pnl_percent`];
    const price = report.facts?.[`${r.holding.symbol}.price`];
    const fund = r.analysis?.instrument_type === 'etf' ? r.analysis : undefined;
    const etfAdjustment = ai.etf_scoring?.adjustments.find((a) => a.symbol === r.holding.symbol);
    return <ResearchPanel key={r.holding.symbol} label={r.holding.symbol} title={names.get(r.holding.symbol) || r.holding.symbol} icon={WalletCards} className="portfolio-holding-card" action={research && r.analysis && <button type="button" onClick={() => onOpenStockAnalysis(r.analysis!)}><ExternalLink size={14} />查看原研究报告</button>}>
     <div className="portfolio-holding-meta"><span>仓位 {r.holding.weight_percent}%</span><span>{item?.portfolio_role || '角色待评估'}</span>{item && <span className={`portfolio-priority ${item.action_priority === '优先处理' ? 'danger' : item.action_priority === '保持' ? 'positive' : 'neutral'}`}>{item.action_priority}</span>}<span>{r.holding.cost_price ? `成本 ${r.holding.cost_price}` : '成本未填写'}</span><span>行情 {price?.available ? String(price.value) : '未知'}</span><span>盈亏 {pnl?.available ? `${Number(pnl.value) > 0 ? '+' : ''}${pnl.value}%` : '未估算'}</span></div>
     <p>{item?.conclusion || research?.headline || r.error || '标的研究尚未完成'}</p>
     {fund && <div className="portfolio-etf-summary"><strong>ETF · {fund.etf.profile.tracking_target || '跟踪对象待核实'}</strong>
      {!fund.etf.direction && <p className="portfolio-evidence-limitation">原报告未采集方向事实，相关判断依据有限。</p>}
      {research?.etf_judgments && <>
       <p><b>底层方向：</b>{research.etf_judgments.underlying.text}</p><p><b>产品适配：</b>{research.etf_judgments.product.text}</p><p><b>介入条件：</b>{research.etf_judgments.entry.text}</p>
       {(research.etf_judgments.technical || research.etf_judgments.news) && <details><summary>原研究技术面、消息面与引用</summary>{[research.etf_judgments.technical, research.etf_judgments.news].map((j, i) => j && <div key={i}><p><b>{i === 0 ? '技术面' : '消息面'}：</b>{j.text}</p><PortfolioEvidence report={report} refs={j.source_ids.map((source_id) => ({ report_id: r.analysis_id, source_id }))} /></div>)}</details>}
      </>}
      {etfAdjustment && <p className={etfAdjustment.premium_eligible && (etfAdjustment.premium_percent ?? 0) >= 3 ? 'portfolio-etf-premium danger' : 'portfolio-etf-premium'}>{etfAdjustment.premium_eligible ? `参考溢价 ${etfAdjustment.premium_percent?.toFixed(2)}% · ${etfAdjustment.premium_basis === 'dated_observation' ? '盘后观察' : '评估时点参考'} ${portfolioDate(etfAdjustment.observed_at)}` : `溢价风险未核验：${etfAdjustment.premium_reason}`}</p>}
      {!!fund.etf.execution_blockers?.length && <details><summary>ETF 执行限制</summary><ul>{fund.etf.execution_blockers.map((limitation, i) => <li key={i}>{limitation}</li>)}</ul></details>}
     </div>}
     {item && <div className="portfolio-holding-conditions"><div><strong>动作</strong><p>{item.action}</p></div><div><strong>确认条件</strong><p>{item.confirmation}</p></div><div className="invalidation"><strong>失效条件</strong><p>{item.invalidation}</p></div></div>}
     <small className="portfolio-research-source">{originLabel(r.research_origin)} · {levelLabel(research?.analysis_level || research?.request.analysis_level)} · 原研究周期 {horizonLabel(research?.request.horizon)}<br />报告完成 {portfolioDate(r.report_completed_at)} · 证据截至 {portfolioDate(r.research_cutoff_at)}<br />{r.quote_message || '保留原研究快照'} · 行情时间 {portfolioDate(price?.as_of)}</small>
     <button type="button" className="portfolio-refresh-stock" disabled={busy} onClick={() => onRefresh(r.holding.symbol)}><History size={14} />重新研究此标的并评估组合</button>
    </ResearchPanel>;
   })}
  </div></section>
  <ResearchPanel label="共同驱动" title="组合结构与联动" icon={Activity}><ReportList report={report} field="concentration_findings" items={ai.concentration_findings || []} empty={ai.score_available ? '未发现突出共同驱动风险' : '组合评估尚未完成'} /><div className="portfolio-risk-groups">{(ai.risk_groups || []).map((g, i) => <article key={i}><strong>{g.name} · {g.weight_percent}% 仓位</strong><small>{g.symbols.map((s) => names.get(s) || s).join('、')}</small><p>{g.reason}</p><PortfolioEvidence refs={g.evidence_refs || []} report={report} /></article>)}</div>{(ai.risk_groups?.length ?? 0) > 1 && <small>风险分组可能重叠，占比不可直接相加。</small>}</ResearchPanel>
  {(ai.scenarios?.length ?? 0) > 0 && <ResearchPanel label="条件推演" title="组合情景" icon={GitBranch}><div className="stock-research-scenarios">{ai.scenarios.map((s) => <article key={s.name}><strong>{s.name}</strong><p>{s.condition}</p><p>{s.portfolio_action}</p></article>)}</div></ResearchPanel>}
  <ResearchPanel label="后续核验" title="下次检查" icon={ShieldCheck}><ReportList report={report} field="next_checklist" items={ai.next_checklist || []} empty="暂无检查事项" /></ResearchPanel>
  <details className="portfolio-research-baseline stock-ai-panel"><summary>研究来源、量化基线与数据限制</summary>
   <p>AI 研究覆盖 {successful}/{report.holdings.length} 只 · {coverage}% 持仓；持仓 {metrics.total_position_percent}% · 现金 {metrics.cash_percent}%。</p>
   <p>静态止损预算：{stopCoverage === 0 ? '未设置有效静态止损方案，无法估算此项' : `${metrics.stop_loss_risk_percent.toFixed(2)}% 总资产损失估算 · 覆盖 ${stopCoverage}% 持仓`}{stopCoverage > 0 && stopCoverage < 100 && '（仅已知部分，其他仓位风险未知）'}。不包含跳空、滑点等实际偏差。</p>
   <p>固定止损预算与综合评分分别评估，缺少固定止损价不代表零风险。</p>
   <div className="portfolio-facts-table"><table><thead><tr><th>指标</th><th>数值</th><th>口径与限制</th></tr></thead><tbody>{Object.entries(report.facts || {}).map(([key, f]) => <tr key={key}><td>{factLabel(key, names, containsETF)}</td><td>{f.available ? displayFact(f.value) : '未知'}</td><td>{f.method}{f.as_of && <small>证据时点 {portfolioDate(f.as_of)}</small>}{f.limitation && <small>{f.limitation}</small>}</td></tr>)}</tbody></table></div>
   <ReportList report={report} field="data_limitations" items={ai.data_limitations || []} empty="没有额外列出的研究限制" />
   <small>生成 {portfolioDate(report.generated_at)} · 评分版本 {report.algorithm_version} {report.model && `· 模型 ${report.model}`}</small>
  </details>
  <footer className="stock-research-footer">评分是当前证据下的研究评价，不表示胜率或预期收益。仅用于研究与复盘。</footer>
 </div>;
}
export function PortfolioETFScoreDetails({ scoring: e, report }: { scoring: PortfolioETFScoring; report: PortfolioInspectionReport }) {
 const names = new Map(report.holdings.map((r) => [r.holding.symbol, r.holding.name || r.analysis?.name || r.holding.symbol]));
 return <ResearchPanel label="ETF 风险评分" title="分散增益与溢价风险" icon={ShieldCheck} className="portfolio-etf-scoring">
  <p>风险维度：基础 {e.base_risk_score} → 最终 {e.final_risk_score} · 分散增益 +{e.diversification_bonus.toFixed(2)} · 溢价扣分 −{e.premium_penalty.toFixed(2)}</p>
  <small>按全部证券持仓内部配比计算，专项调整只应用一次；评估时点 {portfolioDate(e.evaluated_at)}。规则阈值用于研究评分。</small>
  {e.minimum_risk_level && <p>高溢价仓位触发风险等级下限：{e.minimum_risk_level}风险。</p>}
  <div className="portfolio-etf-adjustments">{e.adjustments.map((a) => <article key={a.symbol} className={a.premium_eligible && (a.premium_percent ?? 0) >= 3 ? 'premium-warning' : ''}>
   <strong>{names.get(a.symbol) || a.symbol} · 占证券持仓 {a.weight_percent.toFixed(2)}%</strong>
   <p>分散增益 +{a.bonus_contribution.toFixed(2)} · 溢价扣分 −{a.penalty_contribution.toFixed(2)}（风险维度分）</p><small>{a.diversification_reason}</small>
   <p>{a.premium_eligible ? `参考溢价 ${a.premium_percent?.toFixed(2)}% · ${a.premium_basis === 'dated_observation' ? '盘后观察' : '评估时点参考'}` : '溢价风险未核验'}</p><small>{a.premium_reason}{a.premium_eligible && ` · 市价 ${portfolioDate(a.observed_at)} · IOPV ${portfolioDate(a.iopv_at)}`}</small>
   <PortfolioEvidence refs={a.evidence_refs} report={report} />
  </article>)}</div>
  {e.reversion_loss_percent !== undefined && <p>参考价值不变、已核验正溢价完全回归时，已知部分约影响证券持仓 {e.reversion_loss_percent.toFixed(2)}%；参考偏离核验覆盖 {e.reversion_coverage_percent.toFixed(2)}%。这是情景估算，不是预期损失，未核验部分未知。</p>}
 </ResearchPanel>;
}
export function PortfolioEvidence({ refs, report }: { refs: PortfolioEvidenceRef[]; report: PortfolioInspectionReport }) {
 return <div className="portfolio-evidence">{refs.map((ref, i) => {
  if (ref.fact) { const f = report.facts?.[ref.fact]; return <details key={i}><summary>{f?.method || ref.fact}</summary><p>{f?.available ? displayFact(f.value) : '未知'} · {portfolioDate(f?.as_of)}</p>{f?.limitation && <small>{f.limitation}</small>}</details>; }
  const r = report.holdings.find((r) => r.analysis_id === ref.report_id);
  const source = r?.analysis?.research_report?.sources.find((s) => s.id === ref.source_id);
  const url = safeResearchURL(source?.url);
  return <details key={i}><summary>{r?.holding.name || r?.holding.symbol} · {source?.title || ref.source_id}</summary><p>{source?.content || '原报告保留了该引用'}</p><small>{portfolioDate(source?.published_at || source?.captured_at)}</small>{url && <a href={url} target="_blank" rel="noreferrer">查看来源</a>}</details>;
 })}</div>;
}
function ExplanationEvidence({ report, path }: { report: PortfolioInspectionReport; path: string }) {
 const detail = report.conclusion.explanation_details?.[path];
 if (!detail) return null;
 const names = detail.symbols?.map((symbol) => report.holdings.find((r) => r.holding.symbol === symbol)?.holding.name || symbol);
 return <>{!!names?.length && <small>涉及持仓：{names.join('、')}</small>}{!!detail.evidence_refs?.length && <PortfolioEvidence refs={detail.evidence_refs} report={report} />}</>;
}
function ReportList({ items, empty, ordered, report, field }: { items: string[]; empty: string; ordered?: boolean; report: PortfolioInspectionReport; field: string }) { const Tag = ordered ? 'ol' : 'ul'; return items.length ? <Tag>{items.map((s, i) => <li key={i}>{s}<ExplanationEvidence report={report} path={`${field}[${i}]`} /></li>)}</Tag> : <p className="portfolio-list-empty">{empty}</p>; }
function displayFact(value: unknown) { return typeof value === 'object' ? JSON.stringify(value) : String(value); }
export function portfolioDate(value?: string) { if (!value || value.startsWith('0001-')) return '时间未知'; const d = new Date(value); return Number.isNaN(d.getTime()) ? '时间未知' : d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }); }
export function originLabel(value?: string) { return ({ reused: '复用成功报告', new: '新研究', shared_running: '共享已有研究' }[value || ''] || '报告来源待确认'); }
export function horizonLabel(value?: string) { return ({ short: '超短', swing: '波段', medium: '中期' }[value || ''] || '未指定'); }
function levelLabel(value?: string) { return ({ quick: '快速', standard: '标准', deep: '深度' }[value || ''] || 'AI 研究'); }
function factLabel(key: string, names: Map<string, string>, containsETF = false) {
 const labels: Record<string, string> = { total_position_percent: '总仓位 %', cash_percent: '现金 %', max_single_percent: '最大单票仓位 %', top_three_percent: '前三大仓位 %', equity_max_single_percent: '最大单票占股票持仓 %', equity_top_three_percent: '前三大占股票持仓 %', equity_weight_percent: '占股票持仓 %', equity_known_stop_loss_risk_percent: '已知止损损失占股票持仓 %', concentration_hhi: '持仓集中度 HHI', ai_research_coverage_percent: 'AI 研究覆盖 %', stop_loss_coverage_percent: '静态止损覆盖 %', known_stop_loss_risk_percent: '已知止损损失占总资产 %', weight_percent: '仓位 %', cost_price: '成本', price: '行情价格', pnl_percent: '成本盈亏 %', atr_14_percent: '14 日 ATR %', historical_drawdown_percent: '历史收盘回撤 %' };
 Object.assign(labels, { 'instrument_type': '标的类型', 'etf.asset_class': 'ETF 底层资产类别', 'etf.cross_border': '跨境属性', 'etf.tracking_target': '跟踪对象', 'etf.holdings': '披露持仓', 'etf.execution_blockers': 'ETF 执行限制', 'etf.direction.classification': '方向分类与依据', 'etf.direction.technical': '方向技术事实', 'etf.direction.news_coverage': '方向消息覆盖', 'etf.diversification': 'ETF 分散增益依据', 'etf.premium_assessment': 'ETF 参考溢价与扣分依据', 'etf.premium_scoring_eligible': '溢价扣分资格', 'etf.spread_percent': '原盘口买卖价差 %', 'etf.premium_announcements': '基金溢价公告' });
 if (containsETF) for (const key of Object.keys(labels)) labels[key] = labels[key].replace('股票持仓', '证券持仓');
 if (key.startsWith('correlation.')) { const symbols = [...names.keys()].filter((s) => key.includes(s)); return `历史相关性 · ${symbols.map((s) => names.get(s)).join(' / ')}`; }
 for (const [symbol, name] of names) { if (key.startsWith(`${symbol}.`)) return `${name} · ${labels[key.slice(symbol.length + 1)] || key}`; }
 return labels[key] || key;
}
