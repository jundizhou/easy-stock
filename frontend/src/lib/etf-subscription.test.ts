import { describe, expect, it } from 'vitest';
import { subscriptionChartPoints, subscriptionWindowNet, subscriptionPrompt, subscriptionTrialStatus, type ETFSubscriptionDashboard, type ETFSubscriptionTrial } from './etf-subscription';
const subscriptionSample: ETFSubscriptionDashboard = {
 start_date: '2026-10-17', source_date: '2026-11-16', source_generated_at: '2026-11-17T17:22:40+08:00', report_date: '2025-12-31', latest_date: '2026-11-16', latest_complete_date: '2026-11-16', expected: 22,
 days: [{ date: '2026-11-13', previous_date: '2026-11-12', estimated_net: -1e8, inflow: 1e8, outflow: -2e8, coverage: 22, subscribed: 5, redeemed: 17 }, { date: '2026-11-16', previous_date: '2026-11-13', estimated_net: 3e8, inflow: 4e8, outflow: -1e8, coverage: 22, subscribed: 18, redeemed: 4 }],
 series: [{ code: '510300', name: '华泰柏瑞沪深300ETF', index: '沪深300', exchange: 'SSE', latest_price: 9.999, change_percent: .3, quote_time: '2026-11-17T15:00:00+08:00', points: [{ date: '2026-11-13', previous_date: '2026-11-12', shares: 1e8, previous_shares: 1.1e8, share_change: -1e7, close: 4, price_date: '2026-11-13', estimated_net: -4e7, verification: '交易所已核验' }, { date: '2026-11-16', previous_date: '2026-11-13', shares: 1.1e8, previous_shares: 1e8, share_change: 1e7, close: 4.054, price_date: '2026-11-16', estimated_net: 4.054e7, verification: '交易所已核验' }] }],
 checks: [{ code: '510300', date: '2026-11-16', snapshot_shares: 1.1e8, official_shares: 1.1e8, status: '交易所已核验' }], warnings: [],
};
describe('ETF share subscription evidence', () => {
 it('accumulates daily subscription amounts once and preserves signs', () => {
  expect(subscriptionChartPoints(subscriptionSample, 'pool', true).map((p) => p.net)).toEqual([-1e8, 2e8]);
  expect(subscriptionWindowNet(subscriptionSample)).toBe(2e8);
  expect(subscriptionChartPoints(subscriptionSample, '510300', false).map((p) => p.net)).toEqual([-4e7, 4.054e7]);
 });
 it('never turns a partial day into zero or silently skips it in cumulative totals', () => {
  const partial = { ...subscriptionSample, days: [{ ...subscriptionSample.days[0], estimated_net: null }, subscriptionSample.days[1]] };
  expect(subscriptionWindowNet(partial)).toBeNull();
  expect(subscriptionChartPoints(partial, 'pool', true).map((p) => p.net)).toEqual([null, null]);
 });
 it('retains the completed-month total while a newer disclosure day is incomplete', () => {
  const pending = { ...subscriptionSample, latest_date: '2026-11-17', days: [...subscriptionSample.days, { ...subscriptionSample.days[1], date: '2026-11-17', estimated_net: null, coverage: 14 }] };
  expect(subscriptionWindowNet(pending)).toBe(2e8);
  expect(subscriptionChartPoints(pending, 'pool', true).at(-1)?.time).toBe('2026-11-16');
 });
 it('keeps source date, historical price date and account attribution restrictions in AI evidence', () => {
  const prompt = subscriptionPrompt(subscriptionSample, subscriptionSample.days[1], { source: 'test:subscription', fetched_at: '2026-11-17T18:00:00+08:00', stale: false, latency_ms: 1 });
  expect(prompt).toContain('不能把全池净申购额称为国家队实际买入额');
  expect(prompt).toContain('价格日期 2026-11-16'); expect(prompt).toContain('测算收盘价 4.054'); expect(prompt).not.toContain('测算收盘价 9.999');
 });
});


describe('today subscription trial freshness', () => {
 const trial = { date: '2026-11-17', status: '当日试算已齐备', items: [{ estimated_net: 1e8, quote_time: '2026-11-17T10:30:00+08:00' }] } as ETFSubscriptionTrial;
 const meta = { stale: false } as never;
 it('expires paused intraday estimates and rejects rollover as today data', () => {
  expect(subscriptionTrialStatus(trial, meta, Date.parse('2026-11-17T10:30:00+08:00'))).toBe('当日试算已齐备');
  expect(subscriptionTrialStatus(trial, meta, Date.parse('2026-11-17T10:34:00+08:00'))).toContain('已过期');
  expect(subscriptionTrialStatus(trial, meta, Date.parse('2026-11-17T12:00:00+08:00'))).toContain('已过期');
  expect(subscriptionTrialStatus(trial, meta, Date.parse('2026-11-17T18:00:00+08:00'))).toContain('已过期');
  expect(subscriptionTrialStatus(trial, meta, Date.parse('2026-11-18T10:30:00+08:00'))).toBe('历史试算，非今日');
  expect(subscriptionTrialStatus(trial, { stale: true } as never, Date.parse('2026-11-17T10:30:00+08:00'))).toContain('缓存试算');
 });
});
