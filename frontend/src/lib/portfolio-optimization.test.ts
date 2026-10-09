import { describe, expect, it } from 'vitest';
import type { PortfolioInspectionReport } from './backend';
import { qualifiedOptimizationScore, portfolioScoreImprovement, portfolioScoringVersion } from './portfolio-optimization';

function score(value: number): PortfolioInspectionReport['conclusion'] {
 return {score_available:true,total_score:value,dimensions:['holding_logic','portfolio_structure','risk_capacity','strategy_fit'].map((key) => ({key,score:value,weight:100}))} as PortfolioInspectionReport['conclusion'];
}
describe('portfolio optimization score admission', () => {
 it('compares health scores only within the same scoring version and verified rubric', () => {
  const report = (n: number) => ({algorithm_version:portfolioScoringVersion,conclusion:score(n)}) as PortfolioInspectionReport;
  expect(portfolioScoreImprovement(report(60),report(75))).toBe(15);
  expect(portfolioScoreImprovement(report(75),report(60))).toBe(-15);
  expect(portfolioScoreImprovement({...report(60),algorithm_version:'portfolio-ai-score-v3'},report(75))).toBeUndefined();
  const inflated=report(60);inflated.conclusion.total_score=100;
  expect(portfolioScoreImprovement(report(60),inflated)).toBeUndefined();
 });
 it('requires an independently available score of at least70', () => {
  expect(qualifiedOptimizationScore(score(69))).toBe(false);
  expect(qualifiedOptimizationScore(score(70))).toBe(true);
  expect(qualifiedOptimizationScore({...score(90),score_available:false})).toBe(false);
 });
 it('allows65–69 only with at least five points improvement and no weak dimension', () => {
  expect(qualifiedOptimizationScore(score(65),score(60))).toBe(true);
  expect(qualifiedOptimizationScore(score(69),score(64))).toBe(true);
  expect(qualifiedOptimizationScore(score(64),score(50))).toBe(false);
  expect(qualifiedOptimizationScore(score(69),score(65))).toBe(false);
  expect(qualifiedOptimizationScore(score(69),{...score(60),total_score:59})).toBe(false);
  const weak=score(67);weak.dimensions!.forEach((d,i) => {d.score=[49,79,79,68][i];});
  // Rounded weighted total is67, but one dimension remains under50.
  expect(qualifiedOptimizationScore(weak,score(50))).toBe(false);
 });
 it('recomputes the rubric instead of trusting stored totals or weights', () => {
  expect(qualifiedOptimizationScore({...score(69),total_score:100})).toBe(false);
  expect(qualifiedOptimizationScore(score(70))).toBe(true);
 });
 it('rejects missing, duplicated, invalid and unknown dimensions', () => {
  for (const update of [(c:ReturnType<typeof score>) => c.dimensions!.pop(),(c:ReturnType<typeof score>) => {c.dimensions![0].key='strategy_fit';},(c:ReturnType<typeof score>) => {c.dimensions![0].score=101;},(c:ReturnType<typeof score>) => {c.dimensions![0].key='unknown';}]) {
   const c=score(75);update(c);expect(qualifiedOptimizationScore(c)).toBe(false);
  }
 });
});
