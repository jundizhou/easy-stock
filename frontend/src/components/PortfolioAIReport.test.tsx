import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { PortfolioAIReportView, portfolioDate } from './PortfolioAIReport';
import type { PortfolioInspectionReport } from '../lib/backend';
import { etfFixture } from '../test-utils/etf-fixture';
import { portfolioScoringVersion } from '../lib/portfolio-optimization';
import { etfDirectionFixture } from '../test-utils/etf-fixture';

const report = {
 id: 'fixture', algorithm_version: 'portfolio-ai-score-v3', profile: { label: '均衡' }, request: { horizon: 'swing' }, generated_at: '2026-10-04T06:00:00Z',
 metrics: { ai_research_coverage_percent: 100, total_position_percent: 60, cash_percent: 40, stop_loss_coverage_percent: 0 },
 holdings: [{ holding: { symbol: '600519.SH', name: '界面测试股票', weight_percent: 60 }, status: 'succeeded', research_origin: 'reused', analysis_id: 'stock-fixture', analysis: { ai: { status: 'ready' }, research_report: { headline: '测试逻辑', request: { horizon: 'short' }, sources: [] } } }],
 facts: { '600519.SH.pnl_percent': { available: false, value: null, method: '未填写成本' } },
 conclusion: { total_score: 71, score_available: true, risk_level: '高', risk_reason: '集中风险', executive_summary: '界面测试报告', confidence_level: '中', confidence_reason: '周期差异', dimensions: [{ key: 'holding_logic', label: '持仓逻辑质量', score: 70, weight: 35, reason: '逻辑待核验', adjustments: [], evidence_refs: [], limitations: [] }], holdings: [], primary_risks: [], adjustment_order: [], next_checklist: [], concentration_findings: [], scenarios: [], data_limitations: [] },
} as unknown as PortfolioInspectionReport;
const markup = (value = report) => renderToStaticMarkup(<PortfolioAIReportView report={value} onNew={() => {}} onRefresh={() => {}} onOpenStockAnalysis={() => {}} />);
describe('portfolio AI report', () => {
 it('labels the stock-only score and preserves historical scoring scope', () => {
  const current = markup({ ...report, algorithm_version: portfolioScoringVersion });
  expect(current).toContain('本次持仓分析综合评分');
  expect(current).toContain('<span>巡检综合评分</span><strong>71<small>/ 100</small>');
  expect(current).toContain('总仓位、满仓和现金比例不加扣分');
  expect(current).toContain('70分整体合理');
  expect(markup()).toContain('此报告使用历史评分口径');
  expect(markup()).not.toContain('总仓位、满仓和现金比例不加扣分');
 });
 it('shows complete AI scores without requiring static stops and separates scope', () => {
  const text = markup(); expect(text).toContain('综合评分'); expect(text).toContain('71'); expect(text).toContain('未设置有效静态止损方案'); expect(text).toContain('复用成功报告'); expect(text).toContain('原研究周期'); expect(text).toContain('成本未填写'); expect(text).not.toContain('覆盖不足，暂不评分');
 });
 it('does not present incomplete output as a zero score', () => {
  const value = { ...report, conclusion: { ...report.conclusion, total_score: undefined, score_available: false } }; const text = markup(value); expect(text).toContain('待完成'); expect(text).not.toContain('组合 AI 四维评分');
 });
 it('shows the completed inspection score and dimensions before optimization', () => {
  const text = renderToStaticMarkup(<PortfolioAIReportView report={{ ...report, algorithm_version: portfolioScoringVersion, conclusion: { ...report.conclusion, total_score: 66 } }} afterSummary={<section>优化模块</section>} onNew={() => {}} onRefresh={() => {}} onOpenStockAnalysis={() => {}} />);
  expect(text).toContain('<span>巡检综合评分</span><strong>66<small>/ 100</small>');
  expect(text.indexOf('本次持仓分析综合评分')).toBeLessThan(text.indexOf('优化模块'));
  expect(text.indexOf('组合 AI 四维评分')).toBeLessThan(text.indexOf('优化模块'));
 });
 it('preserves a valid zero and never substitutes a stale or invalid total', () => {
  const complete = markup({ ...report, conclusion: { ...report.conclusion, total_score: 0 } });
  expect(complete).toContain('<strong>0<small>/ 100</small>');
  for (const conclusion of [{ ...report.conclusion, score_available: false }, { ...report.conclusion, total_score: Number.NaN }]) {
   const text = markup({ ...report, conclusion });
   expect(text).toContain('<strong>待完成</strong>');
   expect(text).not.toContain('<small>/ 100</small>');
  }
 });
 it('opens ETF research in a mixed portfolio without stock field assumptions',()=>{
  const a=etfFixture();
  const value={...report,algorithm_version:portfolioScoringVersion,holdings:[...report.holdings,{holding:{symbol:a.symbol,name:a.name,weight_percent:20},status:'succeeded',analysis:a}]};
  const text=markup(value);expect(text).toContain('股票与 ETF 组合');expect(text).toContain('查看原研究报告');expect(text).not.toContain('NaN');
 });
 it('does not present unset timestamps as real report dates', () => { expect(portfolioDate('0001-01-01T00:00:00Z')).toBe('时间未知'); });
 it('shows ETF risk adjustments and premium timing without changing historical reports', () => {
  const a = etfFixture();
  const etf = { holding: {symbol:a.symbol,name:a.name,weight_percent:50}, status:'succeeded', analysis:a, analysis_id:'etf-report' };
  const value = { ...report, algorithm_version:portfolioScoringVersion, holdings:[etf], conclusion:{...report.conclusion,etf_scoring:{rule_version:'portfolio-etf-risk-v1',evaluated_at:'2026-10-13T08:00:00Z',base_risk_score:72,final_risk_score:60,diversification_bonus:8,premium_penalty:20,minimum_risk_level:'中',reversion_loss_percent:4.76,reversion_coverage_percent:100,adjustments:[{symbol:a.symbol,weight_percent:100,diversification_bonus:8,diversification_reason:'宽基分散个别公司风险',premium_percent:5,premium_penalty:20,premium_eligible:true,premium_basis:'dated_observation',premium_reason:'仅供盘后研究',observed_at:'2026-10-13T07:01:00Z',iopv_at:'2026-10-13T07:01:00Z',bonus_contribution:8,penalty_contribution:20,evidence_refs:[]}]}} } as PortfolioInspectionReport;
  const text = markup(value);
  expect(text).toContain('分散增益与溢价风险'); expect(text).toContain('基础 72 → 最终 60');
  expect(text).toContain('参考溢价 5.00%'); expect(text).toContain('盘后观察'); expect(text).toContain('中风险');
  expect(text).toContain('不是预期损失'); expect(text).toContain('原报告未采集方向事实');
  expect(markup()).not.toContain('分散增益与溢价风险');
 });
 it('displays ETF technical and news judgments with their original references', () => {
  const a = etfDirectionFixture();
  const text = markup({...report,holdings:[{holding:{symbol:a.symbol,name:a.name,weight_percent:60},status:'succeeded',analysis:a,analysis_id:'fixture'}]});
  expect(text).toContain('原研究技术面、消息面与引用'); expect(text).toContain('底层方向'); expect(text).toContain('产品适配');
  expect(text).not.toContain('原报告未采集方向事实');
 });
 it('shows retained evidence for normalized explanation objects and dimension limitations', () => {
  const value = { ...report, conclusion: { ...report.conclusion, primary_risks: ['共同驱动需核验'], explanation_details: { 'primary_risks[0]': { symbols: ['600519.SH'], evidence_refs: [{ fact: '600519.SH.pnl_percent' }] }, 'dimensions.holding_logic.limitations[0]': { evidence_refs: [{ fact: '600519.SH.pnl_percent' }] } }, dimensions: [{ ...report.conclusion.dimensions![0], limitations: ['成本证据缺口'] }] } };
  const text = markup(value);
  expect(text).toContain('共同驱动需核验'); expect(text).toContain('涉及持仓：界面测试股票'); expect(text).toContain('成本证据缺口'); expect(text.match(/未填写成本/g)?.length).toBeGreaterThanOrEqual(2);
 });
});
