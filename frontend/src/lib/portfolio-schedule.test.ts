import { describe, expect, it } from 'vitest';
import { defaultSchedule, scheduleRequest, scheduleSnapshotMatches, scheduleTimeLabel } from './portfolio-schedule';
import type { PortfolioPlan } from './portfolio-draft';
const plan: PortfolioPlan = { id: 'p1', name: '均衡组合', draft: { profile: 'balanced', holdings: [{ symbol: '600519.SH', name: '示例', weight: 40, costPrice: '123' }] } };
describe('portfolio schedules', () => {
 it('defaults to tomorrow in Beijing independently of the host timezone', () => {
  const value = defaultSchedule(plan, new Date('2026-10-10T17:00:00Z'));
  expect(value).toMatchObject({ enabled: false, start_date: '2026-10-12', time: '15:30', interval: 3, unit: 'days', channels: [] });
  expect(scheduleTimeLabel('2026-10-10T07:30:00Z')).toContain('15:30');
  expect(scheduleTimeLabel('0001-01-01T00:00:00Z')).toBe('暂无');
 });
 it('detects changed holdings and research settings without treating object key order as a change', () => {
  const request = scheduleRequest(plan);
  expect(request.holdings[0].cost_price).toBe(123);
  expect(scheduleSnapshotMatches(request, Object.fromEntries(Object.entries(request).reverse()) as typeof request)).toBe(true);
  expect(scheduleSnapshotMatches(request, { ...request, holdings: [{ ...request.holdings[0], weight_percent: 60 }] })).toBe(false);
  expect(scheduleSnapshotMatches(request, { ...request, research_level: 'deep' })).toBe(false);
  expect(scheduleSnapshotMatches(request, { ...request, portfolio_plan_id: 'p2' })).toBe(false);
 });
});
