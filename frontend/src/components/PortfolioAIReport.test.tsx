import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { PortfolioAIReportView, portfolioDate } from './PortfolioAIReport';
import type { PortfolioInspectionReport } from '../lib/backend';
import { portfolioScoringVersion } from '../lib/portfolio-optimization';

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
  expect(current).toContain('股票组合健康度');
  expect(current).toContain('总仓位、满仓和现金比例不加扣分');
  expect(current).toContain('70分整体合理');
  expect(markup()).toContain('此报告使用历史评分口径');
  expect(markup()).not.toContain('总仓位、满仓和现金比例不加扣分');
 });
 it('shows complete AI scores without requiring static stops and separates scope', () => {
  const text = markup(); expect(text).toContain('组合综合评分'); expect(text).toContain('71'); expect(text).toContain('未设置有效静态止损方案'); expect(text).toContain('复用成功报告'); expect(text).toContain('原研究周期'); expect(text).toContain('成本未填写'); expect(text).not.toContain('覆盖不足，暂不评分');
 });
 it('does not present incomplete output as a zero score', () => {
  const value = { ...report, conclusion: { ...report.conclusion, total_score: undefined, score_available: false } }; const text = markup(value); expect(text).toContain('待完成'); expect(text).not.toContain('组合 AI 四维评分');
 });
 it('does not present unset timestamps as real report dates', () => { expect(portfolioDate('0001-01-01T00:00:00Z')).toBe('时间未知'); });
 it('shows retained evidence for normalized explanation objects and dimension limitations', () => {
  const value = { ...report, conclusion: { ...report.conclusion, primary_risks: ['共同驱动需核验'], explanation_details: { 'primary_risks[0]': { symbols: ['600519.SH'], evidence_refs: [{ fact: '600519.SH.pnl_percent' }] }, 'dimensions.holding_logic.limitations[0]': { evidence_refs: [{ fact: '600519.SH.pnl_percent' }] } }, dimensions: [{ ...report.conclusion.dimensions![0], limitations: ['成本证据缺口'] }] } };
  const text = markup(value);
  expect(text).toContain('共同驱动需核验'); expect(text).toContain('涉及持仓：界面测试股票'); expect(text).toContain('成本证据缺口'); expect(text.match(/未填写成本/g)?.length).toBeGreaterThanOrEqual(2);
 });
});
