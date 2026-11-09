// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { requestJSON } from '../lib/backend';
import { confirmDialog } from './ConfirmDialog';
import { PersonalReviewJournal } from './PersonalReviewJournal';

vi.mock('../lib/backend', () => ({ requestJSON: vi.fn() }));
vi.mock('./ConfirmDialog', () => ({ confirmDialog: vi.fn() }));
const config = { backendUrl: 'http://fixture.invalid', token: '' };
let entries: Array<{ id: string; title: string; entry_date: string; content: string; revision: number; created_at: string; updated_at: string }>;
let failSave: boolean;
beforeEach(() => {
	localStorage.clear(); vi.clearAllMocks(); failSave = false;
	entries = [{ id: 'saved-1', title: '昨日复盘', entry_date: '2026-11-06', content: '## 昨日观察\n保留原始观点', revision: 1, created_at: '2026-11-06T10:00:00Z', updated_at: '2026-11-06T10:00:00Z' }];
	vi.mocked(confirmDialog).mockResolvedValue(true);
	vi.mocked(requestJSON).mockImplementation(async (_config, path, init) => {
		if (init?.method === 'POST' || init?.method === 'PUT') {
			if (failSave) throw new Error('测试：写入失败');
			const input = JSON.parse(String(init.body));
			const old = entries.find((entry) => path.endsWith(`/${entry.id}`));
			const entry = { ...input, id: old?.id || 'new-1', revision: old ? old.revision + 1 : 1, created_at: old?.created_at || '2026-11-07T10:00:00Z', updated_at: '2026-11-07T10:00:00Z' };
			entries = [entry, ...entries.filter((entry) => entry.id !== old?.id)];
			return { data: entry } as never;
		}
		if (path.includes('?')) {
			const query = new URL(path, config.backendUrl).searchParams.get('q') || '';
			const result = entries.filter((entry) => `${entry.title}${entry.content}${entry.entry_date}`.includes(query));
			return { data: result, total: result.length } as never;
		}
		return { data: entries.find((entry) => path.endsWith(`/${entry.id}`)) } as never;
	});
});
afterEach(cleanup);
const show = () => render(<PersonalReviewJournal config={config} refreshKey={0} />);
const title = () => screen.getByLabelText('日记标题') as HTMLInputElement;
const content = () => screen.getByRole('textbox', { name: '日记正文' }) as HTMLTextAreaElement;
const save = () => screen.getByRole('button', { name: '保存日记' }) as HTMLButtonElement;

it('creates a diary, reopens it and updates the same entry', async () => {
	show(); await screen.findByRole('button', { name: /昨日复盘/ });
	fireEvent.change(title(), { target: { value: '我的交易复盘' } });
	fireEvent.change(content(), { target: { value: '## 今日总结\n**保持纪律**\n- [ ] 明日观察' } });
	fireEvent.change(screen.getByLabelText('复盘日期'), { target: { value: '2026-11-07' } });
	fireEvent.click(save());
	await screen.findByText('日记已保存到本机');
	expect(entries).toHaveLength(2);
	expect(save().disabled).toBe(true);
	fireEvent.click(screen.getByRole('button', { name: /昨日复盘/ }));
	await waitFor(() => expect(title().value).toBe('昨日复盘'));
	fireEvent.click(screen.getByRole('button', { name: /我的交易复盘/ }));
	await waitFor(() => expect(content().value).toContain('保持纪律'));
	fireEvent.change(content(), { target: { value: '修订后的计划\n原文仍可编辑' } });
	fireEvent.click(save());
	await screen.findByText('日记已保存到本机');
	expect(entries).toHaveLength(2);
	expect(entries[0].revision).toBe(2);
	expect(entries[0].content).toBe('修订后的计划\n原文仍可编辑');
});

it('retains unsaved drafts across navigation and does not discard them when switching is cancelled', async () => {
	const view = show(); await screen.findByRole('button', { name: /昨日复盘/ });
	fireEvent.change(title(), { target: { value: '未完成的复盘' } });
	fireEvent.change(content(), { target: { value: '尚未保存的交易反思' } });
	view.unmount(); show();
	expect(title().value).toBe('未完成的复盘');
	expect(content().value).toBe('尚未保存的交易反思');
	vi.mocked(confirmDialog).mockResolvedValue(false);
	fireEvent.click(await screen.findByRole('button', { name: /昨日复盘/ }));
	await waitFor(() => expect(confirmDialog).toHaveBeenCalledOnce());
	expect(content().value).toBe('尚未保存的交易反思');
	expect(requestJSON).not.toHaveBeenCalledWith(config, '/api/v1/reviews/journals/saved-1');
});

it('preserves the current editor after a failed save and allows retrying', async () => {
	show();
	fireEvent.change(title(), { target: { value: '失败后重试' } });
	fireEvent.change(content(), { target: { value: '这段正文不能丢失' } });
	failSave = true; fireEvent.click(save());
	await screen.findByRole('alert');
	expect(content().value).toBe('这段正文不能丢失');
	expect(save().disabled).toBe(false);
	expect(entries).toHaveLength(1);
	failSave = false; fireEvent.click(save());
	await screen.findByText('日记已保存到本机');
	expect(localStorage.getItem('easy-stock:personal-review-draft:v1')).toBeNull();
});

it('formats selected text and previews Markdown without rendering raw HTML', async () => {
	vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { callback(0); return 0; });
	try {
		show(); fireEvent.change(content(), { target: { value: '交易纪律' } });
		content().setSelectionRange(0, 4);
		fireEvent.click(screen.getByRole('button', { name: '插入粗体' }));
		expect(content().value).toBe('**交易纪律**');
		fireEvent.change(content(), { target: { value: '**交易纪律**\n<script>window.untrusted = true</script>' } });
		fireEvent.click(screen.getByRole('button', { name: '预览' }));
		expect(screen.getByText('交易纪律').tagName).toBe('STRONG');
		expect(document.querySelector('.journal-preview script')).toBeNull();
		fireEvent.click(screen.getByRole('button', { name: '编辑' }));
		expect(content().value).toContain('**交易纪律**');
		await act(async () => {});
	} finally { vi.unstubAllGlobals(); }
});

it('searches saved titles, content and dates without changing the current draft', async () => {
	show(); await screen.findByRole('button', { name: /昨日复盘/ });
	fireEvent.change(title(), { target: { value: '当前草稿' } });
	fireEvent.change(screen.getByLabelText('搜索我的日记'), { target: { value: '不存在' } });
	fireEvent.click(screen.getByRole('button', { name: '搜索' }));
	await screen.findByText('没有找到相关日记');
	expect(title().value).toBe('当前草稿');
	fireEvent.change(screen.getByLabelText('搜索我的日记'), { target: { value: '2026-11-06' } });
	fireEvent.click(screen.getByRole('button', { name: '搜索' }));
	await screen.findByRole('button', { name: /昨日复盘/ });
	expect(title().value).toBe('当前草稿');
});
