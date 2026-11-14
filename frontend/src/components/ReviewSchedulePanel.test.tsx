// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { requestJSON } from '../lib/backend';
import { ReviewDiary } from './ReviewDiary';
import { ReviewSchedulePanel } from './ReviewSchedulePanel';

vi.mock('../lib/backend', async (importOriginal) => ({
 ...await importOriginal<typeof import('../lib/backend')>(), requestJSON: vi.fn(),
}));
const config = { backendUrl: 'http://fixture.invalid', token: '' };
const schedulePath = '/api/v1/reviews/daily-summary/schedule';
beforeEach(() => {
 vi.clearAllMocks();
 vi.mocked(requestJSON).mockImplementation(async (_config, path, init) => {
  if (path === schedulePath) return { data: init?.method === 'PUT' ? JSON.parse(String(init.body)) : { enabled: false, time: '22:00', channels: [] } } as never;
  if (path === '/api/v1/settings/notifications') return { data: { dingtalk: { enabled: true, webhook: { configured: true } }, feishu: { enabled: false, webhook: { configured: false } } } } as never;
  throw new Error(`Unexpected test request: ${path}`);
 });
});
afterEach(cleanup);

describe('merged review automation', () => {
 it('renders only one automation disclosure and one complete subscription area in the diary', () => {
  const doc = new DOMParser().parseFromString(renderToStaticMarkup(<ReviewDiary config={null} refreshKey={0} onOpenNotificationSettings={() => {}} />), 'text/html');
  const panel = doc.querySelector('.review-automation-settings')!;
  expect(doc.querySelectorAll('.review-automation-settings')).toHaveLength(1);
  expect(panel.hasAttribute('open')).toBe(false);
  expect(panel.textContent).not.toContain('支持雪球、淘股吧主页订阅');
  expect(panel.querySelectorAll('details')).toHaveLength(0);
  expect(doc.querySelectorAll('.review-subscription-form')).toHaveLength(1);
  expect(doc.querySelectorAll('.review-subscription-list')).toHaveLength(1);
  expect(panel.querySelector('.review-subscription-form')).not.toBeNull();
  expect(panel.querySelector('.review-subscription-list')).not.toBeNull();
  expect(panel.querySelector('.review-subscription-form')?.closest('form')).not.toBe(panel.querySelector('.review-automation-schedule-form'));
  expect(doc.querySelectorAll('form form')).toHaveLength(0);
  expect(doc.querySelectorAll('.review-automation, .review-automation-combined')).toHaveLength(0);
  expect(doc.querySelector('.review-summary-history')).toBeNull();
  expect(doc.body.textContent).not.toContain('历史复盘');
 });

 it('saves the same enabled, time and channel fields after the layout merge', async () => {
  const { container } = render(<ReviewSchedulePanel config={config} onOpenSettings={() => {}} onView={() => {}} />);
  fireEvent.click(container.querySelector('summary')!);
  await waitFor(() => expect((screen.getByRole('button', { name: '保存设置' }) as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(screen.getByRole('checkbox', { name: '每日定时复盘' }));
  fireEvent.change(screen.getByLabelText('北京时间'), { target: { value: '21:30' } });
  fireEvent.click(screen.getByRole('checkbox', { name: '钉钉' }));
  fireEvent.click(screen.getByRole('button', { name: '保存设置' }));
  await screen.findByText('定时复盘已保存');
  const saves = vi.mocked(requestJSON).mock.calls.filter(call => call[2]?.method === 'PUT');
  expect(saves).toHaveLength(1);
  expect(saves[0][1]).toBe(schedulePath);
  expect(JSON.parse(String(saves[0][2]?.body))).toEqual({ enabled: true, time: '21:30', channels: ['dingtalk'] });
 });

 it('keeps unavailable notification channels disabled and settings navigation accessible', async () => {
  const open = vi.fn();
  const { container } = render(<ReviewSchedulePanel config={config} onOpenSettings={open} onView={() => {}} />);
  fireEvent.click(container.querySelector('summary')!);
  await waitFor(() => expect((screen.getByRole('button', { name: '保存设置' }) as HTMLButtonElement).disabled).toBe(false));
  expect((screen.getByRole('checkbox', { name: '飞书 未配置' }) as HTMLInputElement).disabled).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: '配置通知机器人' }));
  expect(open).toHaveBeenCalledOnce();
 });
});
