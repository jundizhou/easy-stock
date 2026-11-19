// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { StrictMode } from 'react';
import { requestJSON } from '../../lib/backend';
import { ETFSubscriptionView } from './ETFSubscriptionView';

vi.mock('../../lib/backend', async (original) => ({ ...await original<typeof import('../../lib/backend')>(), requestJSON: vi.fn() }));
const config = { backendUrl: 'http://test.invalid', token: '' };
const data = {
 start_date: '2026-10-17', source_date: '2026-11-16', source_generated_at: '2026-11-17T17:22:40+08:00', report_date: '2025-12-31', latest_date: '2026-11-16', latest_complete_date: '2026-11-16', expected: 22,
 days: [{ date: '2026-11-13', previous_date: '2026-11-12', estimated_net: -1e8, inflow: 1e8, outflow: -2e8, coverage: 22, subscribed: 5, redeemed: 17 }, { date: '2026-11-16', previous_date: '2026-11-13', estimated_net: 3e8, inflow: 4e8, outflow: -1e8, coverage: 22, subscribed: 18, redeemed: 4 }],
 series: [{ code: '510300', name: '华泰柏瑞沪深300ETF', index: '沪深300', exchange: 'SSE', latest_price: 9.999, change_percent: .3, quote_time: '2026-11-17T15:00:00+08:00', points: [{ date: '2026-11-13', previous_date: '2026-11-12', shares: 1e8, previous_shares: 1.1e8, share_change: -1e7, close: 4, price_date: '2026-11-13', estimated_net: -4e7, verification: '交易所已核验' }, { date: '2026-11-16', previous_date: '2026-11-13', shares: 1.1e8, previous_shares: 1e8, share_change: 1e7, close: 4.054, price_date: '2026-11-16', estimated_net: 4.054e7, verification: '交易所已核验' }] }],
 checks: [{ code: '510300', date: '2026-11-16', snapshot_shares: 1.1e8, official_shares: 1.1e8, status: '交易所已核验' }], warnings: [],
};
const meta = { source: 'test:subscription', fetched_at: '2026-11-17T18:00:00+08:00', stale: false, latency_ms: 1 };
beforeEach(() => { vi.useFakeTimers(); window.localStorage.clear(); vi.mocked(requestJSON).mockReset().mockResolvedValue({ data, meta }); });
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); });
async function mount() { let view: ReturnType<typeof render>; const onAskAI = vi.fn(); await act(async () => { view = render(<ETFSubscriptionView config={config} refreshKey={0} onAskAI={onAskAI} />); }); return { view: view!, onAskAI }; }
describe('share-based ETF subscription view', () => {
 it('renders subscriptions directly without the removed intraday tab or explanatory banners', async () => {
  await mount();
  expect(screen.queryByRole('tab', { name: /盘中资金流/ })).toBeNull();
  expect(screen.queryByText(/以 ETF 份额变化观察全市场/)).toBeNull();
  expect(screen.queryByText(/与参考看板相同的 22 只宽基/)).toBeNull();
  expect(screen.queryByText(/来源 test:subscription/)).toBeNull();
  expect(screen.queryByText('计算口径、数据核验与限制')).toBeNull();
  expect(requestJSON).toHaveBeenCalledTimes(1); expect(vi.mocked(requestJSON).mock.calls[0][1]).toBe('/api/v1/market/national-team-etfs/subscriptions');
  expect(screen.queryByText('核心 ETF 同步大单净流出线索')).toBeNull();
 });
 it('shows subscription estimates, matching historical close, and separate live quote', async () => {
  const { onAskAI } = await mount();
  expect(screen.getByText('+3.00 亿')).toBeTruthy(); expect(screen.getByText('4.054')).toBeTruthy();
  expect(screen.getByText(/9.999/)).toBeTruthy(); expect(screen.getByText(/份额最新齐备日 2026-11-16/)).toBeTruthy();
  expect(screen.getByRole('img', { name: 'ETF净申购估算折线图 2026-11-13 至 2026-11-16' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '交给 AI 解读' })); expect(onAskAI.mock.calls[0][0]).toContain('不得用盘中大单净额');
 });
 it('switches disclosed dates and chart measures without losing timestamp context', async () => {
  await mount(); fireEvent.change(screen.getByLabelText('查看份额披露日期'), { target: { value: '2026-11-13' } });
  expect(screen.getByText('−1.00 亿')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '每日净申购' }));
  expect(screen.getByRole('button', { name: '每日净申购' }).getAttribute('aria-pressed')).toBe('true'); expect(requestJSON).toHaveBeenCalledTimes(1);
 });
 it('polls published data and stops when paused or unmounted', async () => {
  const { view } = await mount(); await act(async () => { vi.advanceTimersByTime(60_000); }); expect(requestJSON).toHaveBeenCalledTimes(2);
  fireEvent.click(screen.getByRole('button', { name: '自动同步' })); await act(async () => { vi.advanceTimersByTime(120_000); }); expect(requestJSON).toHaveBeenCalledTimes(2);
  view.unmount(); await act(async () => { vi.advanceTimersByTime(120_000); }); expect(requestJSON).toHaveBeenCalledTimes(2);
 });
 it('marks retained subscription estimates stale after failure', async () => {
  await mount(); vi.mocked(requestJSON).mockRejectedValueOnce(new Error('feed failed'));
  await act(async () => { vi.advanceTimersByTime(60_000); }); expect(screen.getByText(/刷新失败，显示缓存净申购快照/)).toBeTruthy(); expect(screen.getByText('+3.00 亿')).toBeTruthy();
 });
 it('shows the saved snapshot immediately on return while fresh data is still loading', async () => {
  vi.setSystemTime(new Date('2026-11-18T14:00:00+08:00'));
  const first = await mount(); first.view.unmount();
  let finish!: (value: unknown) => void;
  vi.mocked(requestJSON).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
  let next!: ReturnType<typeof render>;
  act(() => { next = render(<ETFSubscriptionView config={config} refreshKey={0} onAskAI={vi.fn()} />); });
  expect(screen.getByText('+3.00 亿')).toBeTruthy();
  expect(screen.queryByLabelText('行情数据加载中')).toBeNull();
  expect(screen.getByText('同步中…')).toBeTruthy();
  await act(async () => { finish({ data: { ...data, days: data.days.map((day) => ({ ...day, estimated_net: 4e8 })) }, meta }); });
  expect(screen.getByText('+4.00 亿')).toBeTruthy();
  next.unmount();
 });
 it('coalesces StrictMode replay into one upstream request', async () => {
  await act(async () => { render(<StrictMode><ETFSubscriptionView config={config} refreshKey={0} onAskAI={vi.fn()} /></StrictMode>); });
  expect(requestJSON).toHaveBeenCalledTimes(1);
  expect(screen.getByText('华泰柏瑞沪深300ETF').getAttribute('title')).toBe('华泰柏瑞沪深300ETF');
 });
 it('recalculates the chart, summary, details and AI evidence for a custom inclusive range', async () => {
  const { onAskAI } = await mount();
  fireEvent.change(screen.getByLabelText('净申购时间范围'), { target: { value: 'custom' } });
  fireEvent.change(screen.getByLabelText('净申购开始日期'), { target: { value: '2026-11-16' } });
  fireEvent.click(screen.getByRole('button', { name: '应用' }));
  expect(screen.getByText('所选区间累计（估算）').closest('article')?.textContent).toContain('+3.00 亿');
  expect(screen.getByText('1 个披露交易日 · 单位：亿元')).toBeTruthy();
  expect(screen.getByLabelText('查看份额披露日期').querySelectorAll('option')).toHaveLength(1);
  expect(screen.getByRole('img', { name: 'ETF净申购估算折线图 2026-11-16 至 2026-11-16' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '交给 AI 解读' }));
  expect(onAskAI.mock.calls[0][0]).toContain('所示窗口 2026-11-16 至 2026-11-16，区间估算净申购 +3.00 亿');
  expect(onAskAI.mock.calls[0][0]).toContain('比较基准 2026-11-13');
  expect(requestJSON).toHaveBeenCalledTimes(1);
 });
 it('includes older disclosures in year totals and restores a month when switching back', async () => {
  const older = { ...data.days[0], date: '2026-01-05', previous_date: '2025-12-31', estimated_net: 5e8 };
  vi.mocked(requestJSON).mockResolvedValueOnce({ data: { ...data, start_date: '2025-11-17', days: [older, ...data.days] }, meta });
  await mount();
  const total = () => screen.getByText('所选区间累计（估算）').closest('article')?.textContent;
  expect(total()).toContain('+2.00 亿');
  fireEvent.change(screen.getByLabelText('净申购时间范围'), { target: { value: '1y' } });
  expect(total()).toContain('+7.00 亿');
  expect(screen.getByText('3 个披露交易日 · 单位：亿元')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('净申购时间范围'), { target: { value: '1m' } });
  expect(total()).toContain('+2.00 亿');
  expect(requestJSON).toHaveBeenCalledTimes(1);
 });
 it('blocks invalid ranges and shows no estimate for weekends without disclosure', async () => {
  await mount();
  fireEvent.change(screen.getByLabelText('净申购时间范围'), { target: { value: 'custom' } });
  fireEvent.change(screen.getByLabelText('净申购开始日期'), { target: { value: '2026-11-16' } });
  fireEvent.change(screen.getByLabelText('净申购结束日期'), { target: { value: '2026-11-13' } });
  expect(screen.getByRole('alert').textContent).toContain('开始日期不能晚于结束日期');
  expect(screen.getByRole('button', { name: '应用' }).hasAttribute('disabled')).toBe(true);
  fireEvent.change(screen.getByLabelText('净申购开始日期'), { target: { value: '2026-11-14' } });
  fireEvent.change(screen.getByLabelText('净申购结束日期'), { target: { value: '2026-11-15' } });
  fireEvent.click(screen.getByRole('button', { name: '应用' }));
  expect(screen.getByText('暂无可绘制的净申购历史')).toBeTruthy();
  expect(screen.getByText('所选区间累计（估算）').closest('article')?.textContent).toContain('--');
 });
});


describe('same-day subscription trial', () => {
 const now = '2026-11-17T10:30:00+08:00';
 const items = Array.from({ length: 22 }, (_, i) => ({ code: String(510300 + i), name: `测试ETF${i}`, share_date: '', shares: null, previous_shares: 100, share_change: null, price: 4, quote_time: now, estimated_net: null, status: '等待今日份额披露' }));
 const pending = { date: '2026-11-17', previous_date: '2026-11-16', expected: 22, share_coverage: 0, baseline_coverage: 22, price_coverage: 22, coverage: 0, estimated_net: null, covered_net: null, status: '等待今日份额披露', items };
 it('shows waiting instead of zero when quotes exist but today shares do not', async () => {
  vi.setSystemTime(new Date(now)); vi.mocked(requestJSON).mockResolvedValueOnce({ data: { ...data, today_trial: pending }, meta }); await mount();
  expect(screen.getByText('当日净申购试算')).toBeTruthy();
  expect(screen.getByText('今日全池试算').closest('article')?.textContent).toContain('--');
  expect(screen.getByText('当日净申购试算').closest('section')?.querySelector('header')?.textContent).toContain('等待今日份额披露');
  expect(screen.queryByText(/今日份额尚未披露，暂不能计算今日净申购/)).toBeNull();
  expect(screen.queryByText(/今日份额差 × 今日最新价/)).toBeNull();
 });
 it('keeps the full-pool amount unavailable and shows partial status without explanatory text', async () => {
  vi.setSystemTime(new Date(now));
  vi.mocked(requestJSON).mockResolvedValueOnce({ data: { ...data, today_trial: { ...pending, share_coverage: 1, coverage: 1, covered_net: 5e6, status: '部分 ETF 可试算，全池未齐备' } }, meta }); await mount();
  expect(screen.getByText('今日全池试算').closest('article')?.textContent).toContain('--');
  expect(screen.getByText('当日净申购试算').closest('section')?.querySelector('header')?.textContent).toContain('部分 ETF 可试算，全池未齐备');
  expect(screen.queryByText(/已覆盖 1 只 ETF 的试算小计/)).toBeNull();
 });
 it('shows a complete live-price trial and hides it after the paused prices expire', async () => {
  vi.setSystemTime(new Date(now));
  const complete = { ...pending, share_coverage: 22, coverage: 22, estimated_net: 1.23e8, covered_net: 1.23e8, status: '当日试算已齐备', items: items.map((item) => ({ ...item, share_date: '2026-11-17', shares: 120, share_change: 20, estimated_net: 1.23e8 / 22, status: '当日份额 × 最新价试算' })) };
  vi.mocked(requestJSON).mockResolvedValueOnce({ data: { ...data, today_trial: complete }, meta }); await mount();
  expect(screen.getByText('+1.23 亿')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '自动同步' })); await act(async () => { vi.advanceTimersByTime(240_000); });
  expect(screen.getByText('当日净申购试算').closest('section')?.querySelector('header')?.textContent).toContain('试算价格已过期，等待刷新'); expect(screen.getByText('今日全池试算').closest('article')?.textContent).toContain('--');
 });
});
