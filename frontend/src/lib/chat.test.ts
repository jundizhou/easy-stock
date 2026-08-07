import { describe, expect, it } from 'vitest';
import { clearHermesSessionIDs, deriveChatTitle, parseStoredConversations, storeableConversations, type ChatConversation } from './chat';

describe('AI chat history helpers', () => {
	it('derives a compact title from the first message', () => {
		expect(deriveChatTitle('  分析\n这套交易体系的核心拐点  ')).toBe('分析 这套交易体系的核心拐点');
		expect(Array.from(deriveChatTitle('这是一个超过二十四个字符并且应该被截断的会话标题测试内容')).length).toBeLessThanOrEqual(25);
	});

	it('ignores invalid stored data and sorts valid conversations', () => {
		const raw = JSON.stringify([
			conversation('old', '2026-08-01T00:00:00.000Z'),
			{ broken: true },
			conversation('new', '2026-08-02T00:00:00.000Z'),
		]);
		expect(parseStoredConversations(raw).map((item) => item.id)).toEqual(['new', 'old']);
		expect(parseStoredConversations('{')).toEqual([]);
	});

	it('limits local history to the newest thirty conversations', () => {
		const values = Array.from({ length: 35 }, (_, index) => conversation(String(index), `2026-08-${String(index + 1).padStart(2, '0')}T00:00:00.000Z`));
		expect(storeableConversations(values)).toHaveLength(30);
	});

	it('clears Hermes sessions after changing the global chat model without removing messages', () => {
		const current = conversation('current', '2026-08-07T00:00:00.000Z');
		current.hermes_session_id = 'hermes-old-model';
		current.messages = [{ id: 'message-1', role: 'user', content: '保留这条消息', created_at: current.created_at }];

		const [next] = clearHermesSessionIDs([current]);

		expect(next.hermes_session_id).toBeUndefined();
		expect(next.messages).toEqual(current.messages);
	});
});

function conversation(id: string, updatedAt: string): ChatConversation {
	return { id, title: id, messages: [], created_at: updatedAt, updated_at: updatedAt };
}
