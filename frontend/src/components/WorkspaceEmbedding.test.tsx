// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { MonitorWorkspace } from './MonitorWorkspace';
import { TradingMastery } from './TradingMastery';

const { portfolio, diary } = vi.hoisted(() => ({ portfolio: vi.fn(), diary: vi.fn() }));
vi.mock('./PortfolioInspectionWorkspace', () => ({ PortfolioInspectionWorkspace: (props: unknown) => { portfolio(props); return <div data-testid="inspection" />; } }));
vi.mock('./ReviewDiary', () => ({ ReviewDiary: (props: unknown) => { diary(props); return <div data-testid="diary" />; } }));
afterEach(() => { cleanup(); vi.clearAllMocks(); });

it('opens inspection after the watchlist tab in place and forwards refresh and actions', () => {
 const callbacks = { onOpenSettings: vi.fn(), onOpenNotificationSettings: vi.fn(), onOpenStockAnalysis: vi.fn(), onOpenMarketIndexes: vi.fn() };
 const hash = window.location.hash;
 const { container, rerender } = render(<MonitorWorkspace config={null} refreshKey={0} portfolioRefreshKey={7} {...callbacks} />);
 const tabs = container.querySelector('.monitor-tabs')!;
 expect(Array.from(tabs.querySelectorAll('button')).map(button => button.textContent?.trim()).slice(-2)).toEqual(['关注仓', '持仓 AI 巡检']);
 expect(screen.queryByTestId('inspection')).toBeNull();
 fireEvent.click(screen.getByRole('button', { name: '持仓 AI 巡检' }));
 expect(container.querySelector('.monitor-portfolio-inspection [data-testid="inspection"]')).not.toBeNull();
 expect(window.location.hash).toBe(hash);
 expect(portfolio).toHaveBeenLastCalledWith(expect.objectContaining({ config: null, refreshKey: 7, onOpenSettings: callbacks.onOpenSettings, onOpenNotificationSettings: callbacks.onOpenNotificationSettings, onOpenStockAnalysis: callbacks.onOpenStockAnalysis }));
 rerender(<MonitorWorkspace config={null} refreshKey={1} portfolioRefreshKey={8} {...callbacks} />);
 expect(portfolio).toHaveBeenLastCalledWith(expect.objectContaining({ refreshKey: 8 }));
 fireEvent.click(screen.getByRole('button', { name: '关注仓' }));
 expect(screen.queryByTestId('inspection')).toBeNull();
});

it('switches between mastery and the embedded diary without navigation and forwards refresh', () => {
 const notify = vi.fn();
 const { rerender } = render(<TradingMastery config={null} refreshKey={2} onAskAI={() => {}} onOpenNotificationSettings={notify} />);
 const hash = window.location.hash;
 expect(screen.queryByTestId('diary')).toBeNull();
 fireEvent.click(screen.getByRole('button', { name: '大V复盘日记' }));
 expect(screen.getByTestId('diary')).toBeTruthy();
 expect(window.location.hash).toBe(hash);
 expect(diary).toHaveBeenLastCalledWith({ config: null, refreshKey: 2, onOpenNotificationSettings: notify });
 rerender(<TradingMastery config={null} refreshKey={3} onAskAI={() => {}} onOpenNotificationSettings={notify} />);
 expect(diary).toHaveBeenLastCalledWith(expect.objectContaining({ refreshKey: 3 }));
 fireEvent.click(screen.getByRole('button', { name: '游资心法' }));
 expect(screen.queryByTestId('diary')).toBeNull();
});
