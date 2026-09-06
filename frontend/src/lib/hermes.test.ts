import { afterEach, describe, expect, it, vi } from 'vitest';
import { buildHermesWebSocketURL, streamHermesPrompt } from './hermes';

class FakeWebSocket {
	static CONNECTING = 0;
	static OPEN = 1;
	static CLOSING = 2;
	static CLOSED = 3;
	static instances: FakeWebSocket[] = [];
	readyState = FakeWebSocket.CONNECTING;
	sent: string[] = [];
	onmessage: ((event: { data: string }) => void) | null = null;
	onerror: (() => void) | null = null;
	onclose: (() => void) | null = null;
	constructor(public url: string) { FakeWebSocket.instances.push(this); }
	send(value: string) { this.sent.push(value); }
	close() { this.readyState = FakeWebSocket.CLOSED; this.onclose?.(); }
	open() { this.readyState = FakeWebSocket.OPEN; }
	receive(frame: unknown) { this.onmessage?.({ data: JSON.stringify(frame) }); }
}

afterEach(() => {
	vi.unstubAllGlobals();
	FakeWebSocket.instances = [];
});

describe('Hermes TUI gateway client', () => {
	it('builds an authenticated backend websocket URL', () => {
		expect(buildHermesWebSocketURL({ backendUrl: 'https://127.0.0.1:20001', token: 'desktop token' }))
			.toBe('wss://127.0.0.1:20001/api/v1/ai/ws?token=desktop+token');
	});

	it('creates a Hermes session, streams text, and keeps the stored session id', async () => {
		vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket);
		const deltas: string[] = [];
		const promise = streamHermesPrompt({
			config: { backendUrl: 'http://127.0.0.1:20001', token: 'token' },
			prompt: '分析市场拐点',
			onDelta: (content) => deltas.push(content),
		});
		const socket = FakeWebSocket.instances[0];
		socket.open();
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'gateway.ready', payload: {} } });
		const setup = JSON.parse(socket.sent[0]);
		expect(setup.method).toBe('session.create');
		socket.receive({ jsonrpc: '2.0', id: setup.id, result: { session_id: 'live-1', stored_session_id: 'stored-1' } });
		const submit = JSON.parse(socket.sent[1]);
		expect(submit).toMatchObject({ method: 'prompt.submit', params: { session_id: 'live-1', text: '分析市场拐点' } });
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'message.delta', payload: { text: '第一段' } } });
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'message.complete', payload: { content: '完整回复' } } });
		await expect(promise).resolves.toEqual({ content: '完整回复', hermesSessionID: 'stored-1' });
		expect(deltas).toEqual(['第一段', '完整回复']);
	});

	it('falls back to a new session when stored Hermes history no longer exists', async () => {
		vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket);
		const promise = streamHermesPrompt({
			config: { backendUrl: 'http://127.0.0.1:20001', token: '' },
			prompt: '继续',
			hermesSessionID: 'missing',
		});
		const socket = FakeWebSocket.instances[0];
		socket.open();
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'gateway.ready', payload: {} } });
		const resume = JSON.parse(socket.sent[0]);
		expect(resume.method).toBe('session.resume');
		socket.receive({ jsonrpc: '2.0', id: resume.id, error: { code: 4007, message: 'session not found' } });
		const create = JSON.parse(socket.sent[1]);
		expect(create.method).toBe('session.create');
		socket.receive({ jsonrpc: '2.0', id: create.id, result: { session_id: 'live-2', stored_session_id: 'stored-2' } });
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'message.complete', payload: { content: '继续完成' } } });
		await expect(promise).resolves.toEqual({ content: '继续完成', hermesSessionID: 'stored-2' });
	});

	it('surfaces approval requests and sends the selected choice', async () => {
		vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket);
		let approve: ((choice: 'once' | 'session' | 'deny') => void) | undefined;
		const promise = streamHermesPrompt({
			config: { backendUrl: 'http://127.0.0.1:20001', token: '' },
			prompt: '安装技能',
			onApproval: (_request, respond) => { approve = respond; },
		});
		const socket = FakeWebSocket.instances[0]; socket.open();
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'gateway.ready', payload: {} } });
		const setup = JSON.parse(socket.sent[0]);
		socket.receive({ jsonrpc: '2.0', id: setup.id, result: { session_id: 'live-3' } });
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'approval.request', session_id: 'live-3', payload: { pattern_key: 'execute_code', description: '执行命令' } } });
		expect(approve).toBeTypeOf('function'); approve?.('once');
		expect(JSON.parse(socket.sent[2])).toMatchObject({ method: 'approval.respond', params: { session_id: 'live-3', choice: 'once' } });
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'message.complete', payload: { content: '已完成' } } });
		await expect(promise).resolves.toMatchObject({ content: '已完成' });
	});

	it('turns an error completion into a rejected request', async () => {
		vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket);
		const promise = streamHermesPrompt({ config: { backendUrl: 'http://127.0.0.1:20001', token: '' }, prompt: '测试' });
		const socket = FakeWebSocket.instances[0]; socket.open();
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'gateway.ready', payload: {} } });
		const setup = JSON.parse(socket.sent[0]);
		socket.receive({ jsonrpc: '2.0', id: setup.id, result: { session_id: 'live-4' } });
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'message.complete', payload: { status: 'error', text: '模型暂时不可用' } } });
		await expect(promise).rejects.toThrow('模型暂时不可用');
	});
});
