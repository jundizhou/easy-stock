import { describe, expect, it } from 'vitest';
import type { Quote } from './backend';
import { valuePortfolio, valuePortfolioHolding } from './portfolio-valuation';

const holding = { symbol: '600001.SH', name: '示例', weight: 30, costPrice: '20' };
const quote = (price: number) => ({ symbol: holding.symbol, price } as Quote);

describe('portfolio valuation', () => {
 it('uses cost and allocated capital for stable whole-share quantity as prices change', () => {
  const atCost = valuePortfolioHolding(holding, 500000, quote(20));
  const higher = valuePortfolioHolding(holding, 500000, quote(22));
  expect(atCost.quantity).toBe(7500);
  expect(higher.quantity).toBe(atCost.quantity);
  expect(higher).toMatchObject({ costValue: 150000, marketValue: 165000, profit: 15000 });
  expect(higher.profitPercent).toBeCloseTo(10);
  expect(valuePortfolioHolding(holding, 1000000, quote(22)).quantity).toBe(15000);
 });
 it('aggregates mixed gains and losses by invested cost and keeps rounding remainder in cash', () => {
  const second = { ...holding, symbol: '600002.SH', weight: 20, costPrice: '30' };
  const value = valuePortfolio({ profile: 'balanced', holdings: [holding, second] }, { [holding.symbol]: quote(22), [second.symbol]: quote(27) });
  expect(value.holdings[1].quantity).toBe(3333);
  expect(value.holdings[1].profit).toBe(-9999);
  expect(value.costValue).toBe(249990);
  expect(value.marketValue).toBe(254991);
  expect(value.cash).toBe(250010);
  expect(value.profit).toBe(5001);
  expect(value.profitPercent).toBeCloseTo(5001 / 249990 * 100);
 });
 it('retains the entry snapshot as default cost and respects an explicit manual cost', () => {
  expect(valuePortfolioHolding({ ...holding, costPrice: '', entryPrice: 10 }, 500000, quote(22))).toMatchObject({ costPrice: 10, quantity: 15000, profit: 180000 });
  expect(valuePortfolioHolding({ ...holding, entryPrice: 10 }, 500000, quote(22))).toMatchObject({ costPrice: 20, quantity: 7500, profit: 15000 });
 });
 it('never reports missing costs, missing quotes, or nonpositive prices as zero profit', () => {
  for (const price of [0, -1, NaN, Infinity]) expect(valuePortfolioHolding(holding, 500000, quote(price)).profit).toBeNull();
  expect(valuePortfolioHolding({ ...holding, costPrice: '' }, 500000, quote(20)).quantity).toBeNull();
  expect(valuePortfolioHolding({ ...holding, costPrice: 'bad', entryPrice: 10 }, 500000, quote(20)).profit).toBeNull();
  expect(valuePortfolio({ profile: 'balanced', holdings: [holding] }, {})).toMatchObject({ marketValue: null, profit: null, profitPercent: null, cash: 350000 });
  expect(valuePortfolioHolding(holding, 0, quote(20)).quantity).toBeNull();
 });
 it('shows an empty new plan as cash with zero holdings and zero profit', () => {
  expect(valuePortfolio({ profile: 'balanced', holdings: [] }, {})).toMatchObject({ cash: 500000, marketValue: 0, profit: 0, profitPercent: 0 });
 });
});
