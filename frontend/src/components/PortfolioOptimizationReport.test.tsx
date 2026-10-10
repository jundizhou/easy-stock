import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import type { PortfolioInspectionReport, PortfolioOptimizationJob, PortfolioOptimizationPlan } from '../lib/backend';
import { portfolioScoringVersion } from '../lib/portfolio-optimization';
import { PortfolioAIReportView } from './PortfolioAIReport';
import { OptimizationPlan } from './PortfolioOptimizationReport';

function report(score: number): PortfolioInspectionReport {
 return {
  id: 'score-fixture', algorithm_version: portfolioScoringVersion, generated_at: '2026-10-09T10:04:48Z',
  profile: { label: '均衡' }, request: { horizon: 'swing', holdings: [{ symbol: '600001.SH', name: '测试持仓', weight_percent: 80 }] },
  holdings: [], metrics: { total_position_percent: 80, cash_percent: 20 },
  conclusion: {
   score_available: true, total_score: score, risk_level: '中', executive_summary: '巡检结论',
   dimensions: ['holding_logic','portfolio_structure','risk_capacity','strategy_fit'].map((key,i) => ({key,label:key,score,weight:[35,25,25,15][i],reason:'评分依据',evidence_refs:[]})),
  },
 } as unknown as PortfolioInspectionReport;
}

function fixture(inspection = 66, original = 59, target = 75) {
 const plan: PortfolioOptimizationPlan = {
  name: '评分对比', status: 'conditional', target: report(target).request!.holdings,
  checks: {valid:true,total_position_percent:80,cash_percent:20,sold_percent:0,bought_percent:0,retained_percent:80,replacement_ratio_percent:0,maximum_replacement_ratio_percent:70,change_budget_mode:'replacement_share',errors:[]},
  allocations: [], improvements: [], original_comparison: report(original), target_comparison: report(target), assessment_order: 'original_first',
  assessment: {accepted:true,reason:'优化结论',original_issue:'集中风险',tradeoffs:[],residual_risks:[],evidence_refs:[]},
 };
 const job = {source_report:report(inspection),results:[],plans:[plan]} as unknown as PortfolioOptimizationJob;
 return {job,plan};
}

function render({job,plan}: ReturnType<typeof fixture>) {
 return renderToStaticMarkup(<PortfolioAIReportView report={job.source_report} onNew={() => {}} onRefresh={() => {}} onOpenStockAnalysis={() => {}}
  afterSummary={<OptimizationPlan job={job} plan={plan} selected={false} applyBusy={false} onApply={() => {}} onOpenStockAnalysis={() => {}}/>}/>);
}

describe('portfolio optimization score provenance', () => {
 it('distinguishes the inspection score from the paired review and its improvement', () => {
  const data=fixture();
  const before=JSON.stringify(data);
  const html=render(data);
  expect(html).toContain('<span>巡检综合评分</span><strong>66<small>/ 100</small>');
  expect(html).toContain('巡检综合评分：66 分；原持仓优化复评分：59 分');
  expect(html).toContain('59<small>优化复评分</small>');
  expect(html).toContain('75<small>优化复评分</small>');
  expect(html).toContain('本次优化复评 59 → 75 分 · 本次优化改善 +16 分');
  expect(html).not.toContain('本次优化改善 +9 分');
  expect(html.indexOf('评分来源说明')).toBeLessThan(html.indexOf('原持仓与建议持仓对比'));
  expect(html.indexOf('原持仓与建议持仓对比')).toBeLessThan(html.indexOf('查看评分对比与调仓依据'));
  expect(JSON.stringify(data)).toBe(before);
 });
 it('does not turn a missing inspection score into a new improvement baseline', () => {
  const data=fixture();data.job.source_report.conclusion.score_available=false;
  const html=render(data);
  expect(html).toContain('巡检综合评分：暂无有效评分');
  expect(html).toContain('本次优化改善 +16 分');
 });
 it('keeps the review provenance even when both evaluations agree', () => {
  const html=render(fixture(66,66,75));
  expect(html).toContain('巡检综合评分：66 分；原持仓优化复评分：66 分');
  expect(html).toContain('本次优化改善 +9 分');
 });
 it('preserves a valid zero in the review comparison', () => {
  const html=render(fixture(66,0,75));
  expect(html).toContain('原持仓优化复评分：0 分');
  expect(html).toContain('0<small>优化复评分</small>');
  expect(html).toContain('本次优化改善 +75 分');
 });
 it('does not promote failed, pending or unverifiable reviews into score cards', () => {
  for (const update of [
   (p:PortfolioOptimizationPlan) => {p.status='invalid_review';},
   (p:PortfolioOptimizationPlan) => {p.status='pending_review';},
   (p:PortfolioOptimizationPlan) => {p.assessment=undefined;},
   (p:PortfolioOptimizationPlan) => {p.original_comparison.conclusion.score_available=false;},
   (p:PortfolioOptimizationPlan) => {p.target_comparison.conclusion.total_score=99;},
   (p:PortfolioOptimizationPlan) => {p.original_comparison.algorithm_version='portfolio-ai-score-v3';},
  ]) {
   const data=fixture();update(data.plan);const html=render(data);
   expect(html).not.toContain('portfolio-comparison-score');
   expect(html).not.toContain('本次优化改善');
  }
 });
});
