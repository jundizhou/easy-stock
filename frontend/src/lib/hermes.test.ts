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
	it('streams reasoning separately and reconciles the final snapshot without duplication', async () => {
		vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket);
		const onReasoning = vi.fn();
		const onDelta = vi.fn();
		const promise = streamHermesPrompt({ config: { backendUrl: 'http://localhost', token: '' }, prompt: '测试', onReasoning, onDelta });
		const socket = FakeWebSocket.instances[0]; socket.open();
		socket.receive({ method: 'event', params: { type: 'reasoning.delta', payload: { text: '\n先检查' } } });
		socket.receive({ method: 'event', params: { type: 'reasoning.delta', payload: { text: ' 数据。\n' } } });
		expect(onReasoning.mock.calls).toEqual([['\n先检查'], ['\n先检查 数据。\n']]);
		expect(onDelta).not.toHaveBeenCalled();
		socket.receive({ method: 'event', params: { type: 'message.delta', payload: { text: '已检查。' } } });
		socket.receive({ method: 'event', params: { type: 'message.complete', payload: { text: '已检查。', reasoning: '先检查 数据。', status: 'complete' } } });
		await expect(promise).resolves.toMatchObject({ content: '已检查。', reasoning: '\n先检查 数据。\n' });
		expect(onReasoning).toHaveBeenCalledTimes(2);
	});

	it('supports reasoning returned only with the final response', async () => {
		vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket);
		const onReasoning = vi.fn();
		const promise = streamHermesPrompt({ config: { backendUrl: 'http://localhost', token: '' }, prompt: '测试', onReasoning });
		const socket = FakeWebSocket.instances[0]; socket.open();
		socket.receive({ method: 'event', params: { type: 'message.complete', payload: { text: '最终回复', reasoning: '已核对相关数据。' } } });
		await expect(promise).resolves.toMatchObject({ reasoning: '已核对相关数据。' });
		expect(onReasoning).toHaveBeenCalledWith('已核对相关数据。');
	});

	it('keeps reasoning across tool rounds when the final snapshot covers only the last round', async () => {
		vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket);
		const onReasoning = vi.fn();
		const promise = streamHermesPrompt({ config: { backendUrl: 'http://localhost', token: '' }, prompt: '测试', onReasoning });
		const socket = FakeWebSocket.instances[0]; socket.open();
		socket.receive({ method: 'event', params: { type: 'reasoning.delta', payload: { text: '先查询数据。' } } });
		socket.receive({ method: 'event', params: { type: 'tool.start', payload: { name: '查询行情' } } });
		socket.receive({ method: 'event', params: { type: 'tool.complete', payload: { name: '查询行情' } } });
		socket.receive({ method: 'event', params: { type: 'reasoning.delta', payload: { text: '再核对' } } });
		socket.receive({ method: 'event', params: { type: 'message.complete', payload: { text: '最终回复', reasoning: '再核对结果。' } } });
		await expect(promise).resolves.toMatchObject({ reasoning: '先查询数据。\n\n再核对结果。' });
		expect(onReasoning).toHaveBeenLastCalledWith('先查询数据。\n\n再核对结果。');
	});

	it('treats spinner notices and assistant previews as progress without fabricating reasoning', async () => {
		vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket);
		const onReasoning = vi.fn();
		const onStatus = vi.fn();
		const promise = streamHermesPrompt({ config: { backendUrl: 'http://localhost', token: '' }, prompt: '测试', onReasoning, onStatus });
		const socket = FakeWebSocket.instances[0]; socket.open();
		socket.receive({ method: 'event', params: { type: 'thinking.delta', payload: { text: 'Waiting for provider…' } } });
		expect(onStatus).toHaveBeenLastCalledWith({ kind: 'process', text: 'Waiting for provider…' });
		socket.receive({ method: 'event', params: { type: 'reasoning.available', payload: { text: '你好！' } } });
		socket.receive({ method: 'event', params: { type: 'message.complete', payload: { text: '你好！' } } });
		await expect(promise).resolves.toEqual({ content: '你好！', hermesSessionID: '' });
		expect(onReasoning).not.toHaveBeenCalled();
	});

	it('ends the wait on a Hermes error event and ignores late reasoning', async () => {
		vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket);
		const onReasoning = vi.fn();
		const promise = streamHermesPrompt({ config: { backendUrl: 'http://localhost', token: '' }, prompt: '测试', onReasoning });
		const socket = FakeWebSocket.instances[0]; socket.open();
		socket.receive({ method: 'event', params: { type: 'error', payload: { message: 'agent init failed' } } });
		await expect(promise).rejects.toThrow('agent init failed');
		socket.receive({ method: 'event', params: { type: 'reasoning.delta', payload: { text: 'late' } } });
		expect(onReasoning).not.toHaveBeenCalled();
	});

	it('builds an authenticated backend websocket URL', () => {
		expect(buildHermesWebSocketURL({ backendUrl: 'https://127.0.0.1:20001', token: 'desktop token' }))
			.toBe('wss://127.0.0.1:20001/api/v1/ai/ws?token=desktop+token');
	});

	it('creates a Hermes session, streams text, and keeps the stored session id', async () => {
		vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket);
		const deltas: string[] = [];
		const usageReports: Array<{ prompt_tokens: number; completion_tokens: number; total_tokens: number; model: string }> = [];
		const promise = streamHermesPrompt({
			config: { backendUrl: 'http://127.0.0.1:20001', token: 'token' },
			prompt: '分析市场拐点',
			analysisID: 'saved-analysis-1',
			onDelta: (content) => deltas.push(content),
			onUsage: (usage) => usageReports.push(usage),
		});
		const socket = FakeWebSocket.instances[0];
		socket.open();
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'gateway.ready', payload: {} } });
		const setup = JSON.parse(socket.sent[0]);
		expect(setup.method).toBe('session.create');
		expect(setup.params).not.toHaveProperty('client');
		socket.receive({ jsonrpc: '2.0', id: setup.id, result: { session_id: 'live-1', stored_session_id: 'stored-1' } });
		const submit = JSON.parse(socket.sent[1]);
		expect(submit).toMatchObject({ method: 'prompt.submit', params: { session_id: 'live-1', text: '分析市场拐点', analysis_id: 'saved-analysis-1' } });
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'message.delta', payload: { text: '第一段' } } });
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'message.complete', payload: { content: '完整回复', usage: { model: 'kimi-k3', input: 22, output: 8, total: 30 } } } });
		await expect(promise).resolves.toEqual({ content: '完整回复', hermesSessionID: 'stored-1' });
		expect(deltas).toEqual(['第一段', '完整回复']);
		expect(usageReports).toEqual([{ prompt_tokens: 22, completion_tokens: 8, total_tokens: 30, model: 'kimi-k3' }]);
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

	it('answers Hermes 0.21 server approval requests with the same JSON-RPC id', async () => {
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
		socket.receive({ jsonrpc: '2.0', id: setup.id, result: { session_id: 'live-new' } });
		socket.receive({ jsonrpc: '2.0', id: 'srq-approval', method: 'approval', params: { session_id: 'live-new', request_id: 'approval-1', description: '执行命令', choices: ['once', 'deny'] } });
		approve?.('once');
		expect(JSON.parse(socket.sent[2])).toEqual({ jsonrpc: '2.0', id: 'srq-approval', result: { choice: 'once' } });
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'message.complete', payload: { content: '已完成' } } });
		await expect(promise).resolves.toMatchObject({ content: '已完成' });
	});

	it('surfaces clarify requests and sends the typed answer', async () => {
		vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket);
		let clarify: ((answer: string) => void) | undefined;
		let received: { question: string; choices: string[]; requestID: string } | undefined;
		const promise = streamHermesPrompt({
			config: { backendUrl: 'http://127.0.0.1:20001', token: '' },
			prompt: '分析题材',
			onClarify: (request, respond) => { received = request; clarify = respond; },
		});
		const socket = FakeWebSocket.instances[0]; socket.open();
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'gateway.ready', payload: {} } });
		const setup = JSON.parse(socket.sent[0]);
		socket.receive({ jsonrpc: '2.0', id: setup.id, result: { session_id: 'live-5' } });
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'clarify.request', session_id: 'live-5', payload: { question: '要结合实时数据吗？', choices: ['实时数据', '仅历史知识'], request_id: 'abc123' } } });
		expect(received).toEqual({ question: '要结合实时数据吗？', choices: ['实时数据', '仅历史知识'], requestID: 'abc123' });
		clarify?.('实时数据');
		expect(JSON.parse(socket.sent[2])).toMatchObject({ method: 'clarify.respond', params: { request_id: 'abc123', answer: '实时数据' } });
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'message.complete', payload: { content: '已完成' } } });
		await expect(promise).resolves.toMatchObject({ content: '已完成' });
	});

	it('answers batched Hermes 0.21 clarify requests in order', async () => {
		vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket);
		const answers: Array<(answer: string) => void> = [];
		const promise = streamHermesPrompt({
			config: { backendUrl: 'http://127.0.0.1:20001', token: '' },
			prompt: '分析题材',
			onClarify: (_request, respond) => { answers.push(respond); },
		});
		const socket = FakeWebSocket.instances[0]; socket.open();
		socket.receive({ jsonrpc: '2.0', method: 'event', params: { type: 'gateway.ready', payload: {} } });
		const setup = JSON.parse(socket.sent[0]);
		socket.receive({ jsonrpc: '2.0', id: setup.id, result: { session_id: 'live-batch' } });
		socket.receive({ jsonrpc: '2.0', id: 'srq-clarify', method: 'clarify', params: { session_id: 'live-batch', questions: [
			{ qid: 'q1', question: '选择市场', choices: ['A股'] },
			{ qid: 'q2', question: '选择周期', choices: ['短线'] },
		] } });
		expect(answers).toHaveLength(1);
		answers[0]('A股');
		expect(answers).toHaveLength(2);
		answers[1]('短线');
		expect(JSON.parse(socket.sent[2])).toEqual({ jsonrpc: '2.0', id: 'srq-clarify', result: { answers: { q1: 'A股', q2: '短线' } } });
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
