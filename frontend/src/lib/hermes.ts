import type { BackendConfig } from './backend';
import { logRuntimeEvent, runtimeErrorDetails } from './runtime-log';

export type HermesStreamResult = {
	content: string;
	reasoning?: string;
	hermesSessionID: string;
};

export type HermesUsage = {
	prompt_tokens: number;
	completion_tokens: number;
	total_tokens: number;
	model: string;
};

export type HermesClarifyRequest = {
	question: string;
	choices: string[];
	requestID: string;
};

type HermesBatchClarifyQuestion = {
	qid: string;
	question: string;
	choices?: string[];
	multi_select?: boolean;
};

export type HermesStreamRequest = {
	config: BackendConfig;
	prompt: string;
	analysisID?: string;
	hermesSessionID?: string;
	seedMessages?: Array<{ role: 'user' | 'assistant'; content: string }>;
	onDelta?: (content: string) => void;
	onReasoning?: (content: string) => void;
	onSession?: (sessionID: string) => void;
	onStatus?: (status: { kind: string; text?: string }) => void;
	module?: string;
	onUsage?: (usage: HermesUsage) => void;
	onApproval?: (approval: { patternKey?: string; description?: string; command?: string }, respond: (choice: 'once' | 'session' | 'deny') => void) => void;
	onClarify?: (request: HermesClarifyRequest, respond: (answer: string) => void) => void;
	signal?: AbortSignal;
};

type RPCFrame = {
	id?: string;
	method?: string;
	params?: Record<string, unknown>;
	result?: Record<string, unknown>;
	error?: { code?: number; message?: string };
};

const HANDSHAKE_TIMEOUT_MS = 20_000;

export function buildHermesWebSocketURL(config: BackendConfig) {
	const url = new URL('/api/v1/ai/ws', config.backendUrl);
	url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
	if (config.token) url.searchParams.set('token', config.token);
	return url.toString();
}

export function streamHermesPrompt(request: HermesStreamRequest): Promise<HermesStreamResult> {
	return new Promise((resolve, reject) => {
		let socket: WebSocket;
		let settled = false;
		let ready = false;
		let setupRequestID = '';
		let setupMethod: 'session.create' | 'session.resume' = request.hermesSessionID ? 'session.resume' : 'session.create';
		let submitRequestID = '';
		let liveSessionID = '';
		let storedSessionID = request.hermesSessionID || '';
		let nextID = 1;
		let streamed = '';
		const reasoningBlocks: string[] = [];
		let nextReasoningBlock = true;
		let timeout: ReturnType<typeof setTimeout> | undefined;
		let batchClarify: { frameID: string; questions: HermesBatchClarifyQuestion[]; answers: Record<string, string>; index: number } | undefined;

		const cleanup = () => {
			if (timeout) clearTimeout(timeout);
			request.signal?.removeEventListener('abort', abort);
		};
		const finish = (error?: unknown, content = '') => {
			if (settled) return;
			settled = true;
			cleanup();
			if (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING) socket.close();
			if (error) {
				if (!(error instanceof Error) || error.name !== 'AbortError') {
					logRuntimeEvent('error', 'ai-chat', { event: 'websocket_failure', error: runtimeErrorDetails(error) });
				}
				reject(error);
			}
			else resolve({ content, hermesSessionID: storedSessionID, ...(reasoningBlocks.length ? { reasoning: reasoningBlocks.join('\n\n') } : {}) });
		};
		const armTimeout = (message: string) => {
			if (timeout) clearTimeout(timeout);
			timeout = setTimeout(() => finish(new Error(message)), HANDSHAKE_TIMEOUT_MS);
		};
		const send = (method: string, params: Record<string, unknown>) => {
			const id = `hermes-${nextID++}`;
			socket.send(JSON.stringify({ jsonrpc: '2.0', id, method, params }));
			return id;
		};
		const updateReasoning = (text: string, complete = false) => {
			if (!text) return;
			const last = reasoningBlocks.length - 1;
			const current = reasoningBlocks[last] || '';
			if (complete) {
				// message.complete contains only the LAST model round's reasoning.
				// Reconcile its snapshot without duplicating streamed text or losing earlier rounds.
				const snapshot = text.trim();
				if (!snapshot || current.trim().startsWith(snapshot)) return;
				if (current && snapshot.startsWith(current.trim())) reasoningBlocks[last] = text;
				else reasoningBlocks.push(text);
			} else if (nextReasoningBlock || last < 0) {
				reasoningBlocks.push(text);
			} else {
				reasoningBlocks[last] += text;
			}
			nextReasoningBlock = false;
			request.onReasoning?.(reasoningBlocks.join('\n\n'));
		};
		const respondApproval = (sessionID: string, choice: 'once' | 'session' | 'deny') => {
			if (socket.readyState !== WebSocket.OPEN) return;
			send('approval.respond', { session_id: sessionID, choice: choice === 'once' ? 'once' : choice });
		};
		const respondClarify = (requestID: string, answer: string) => {
			if (socket.readyState !== WebSocket.OPEN) return;
			send('clarify.respond', { request_id: requestID, answer });
		};
		const respondServerRequest = (requestID: string, result: Record<string, unknown>) => {
			if (socket.readyState !== WebSocket.OPEN) return;
			socket.send(JSON.stringify({ jsonrpc: '2.0', id: requestID, result }));
		};
		const presentBatchClarifyQuestion = () => {
			if (!batchClarify) return;
			const question = batchClarify.questions[batchClarify.index];
			if (!question) {
				respondServerRequest(batchClarify.frameID, { answers: batchClarify.answers });
				batchClarify = undefined;
				return;
			}
			const frameID = batchClarify.frameID;
			request.onClarify?.({
				question: question.question,
				choices: question.choices || [],
				requestID: `${frameID}:${question.qid}`,
			}, (answer) => {
				if (!batchClarify || batchClarify.frameID !== frameID) return;
				batchClarify.answers[question.qid] = answer;
				batchClarify.index += 1;
				presentBatchClarifyQuestion();
			});
		};
		const submitPrompt = () => {
			submitRequestID = send('prompt.submit', { session_id: liveSessionID, text: request.prompt, ...(request.analysisID ? { analysis_id: request.analysisID } : {}) });
		};
		const setupSession = (method = setupMethod) => {
			setupMethod = method;
			setupRequestID = method === 'session.resume'
				? send(method, { session_id: request.hermesSessionID })
				: send(method, {
					...(request.seedMessages?.length ? { messages: request.seedMessages } : {}),
				});
			armTimeout(method === 'session.resume' ? '恢复 Hermes 对话超时' : '创建 Hermes 对话超时');
		};
		function abort() {
			if (socket.readyState === WebSocket.OPEN && liveSessionID) {
				send('session.interrupt', { session_id: liveSessionID });
			}
			const error = new Error('AI 对话已停止');
			error.name = 'AbortError';
			finish(error);
		}

		try {
			socket = new WebSocket(buildHermesWebSocketURL(request.config));
		} catch (error) {
			logRuntimeEvent('error', 'ai-chat', { event: 'websocket_open_failure', error: runtimeErrorDetails(error) });
			reject(error);
			return;
		}
		request.signal?.addEventListener('abort', abort, { once: true });
		if (request.signal?.aborted) {
			abort();
			return;
		}
		armTimeout('连接 Hermes 运行时超时');

		socket.onmessage = (event) => {
			if (settled) return;
			let frame: RPCFrame;
			try {
				frame = JSON.parse(String(event.data)) as RPCFrame;
			} catch {
				return;
			}
			const type = eventType(frame);
			if (type === 'gateway.ready' && !ready) {
				ready = true;
				setupSession();
				return;
			}
			if (frame.id === setupRequestID) {
				if (frame.error) {
					if (setupMethod === 'session.resume' && frame.error.code === 4007) {
						setupSession('session.create');
						return;
					}
					finish(new Error(frame.error.message || 'Hermes 会话初始化失败'));
					return;
				}
				liveSessionID = stringValue(frame.result?.session_id);
				storedSessionID = stringValue(frame.result?.stored_session_id)
					|| stringValue(frame.result?.resumed)
					|| storedSessionID
					|| liveSessionID;
				if (!liveSessionID) {
					finish(new Error('Hermes 未返回会话 ID'));
					return;
				}
				if (timeout) clearTimeout(timeout);
				request.onSession?.(storedSessionID);
				submitPrompt();
				return;
			}
			if (frame.id === submitRequestID && frame.error) {
				finish(new Error(frame.error.message || 'Hermes 提交提示词失败'));
				return;
			}
			if (frame.method === 'approval' && frame.id) {
				const params = frame.params || {};
				request.onApproval?.({
					patternKey: stringValue(params.pattern_key),
					description: stringValue(params.description),
					command: stringValue(params.command),
				}, (choice) => respondServerRequest(frame.id as string, { choice }));
				return;
			}
			if (frame.method === 'clarify' && frame.id) {
				const params = frame.params || {};
				const questions = Array.isArray(params.questions)
					? params.questions.filter((item): item is HermesBatchClarifyQuestion => Boolean(item && typeof item === 'object' && stringValue((item as Record<string, unknown>).qid)))
					: [];
				if (questions.length > 0) {
					batchClarify = { frameID: frame.id, questions, answers: {}, index: 0 };
					presentBatchClarifyQuestion();
				} else {
					const choices = Array.isArray(params.choices) ? params.choices.map((choice) => stringValue(choice)).filter(Boolean) : [];
					request.onClarify?.({ question: stringValue(params.question), choices, requestID: frame.id }, (answer) => respondServerRequest(frame.id as string, { answer }));
				}
				return;
			}
			if (type === 'reasoning.delta') {
				updateReasoning(eventText(frame, 'text') || eventText(frame, 'delta'));
				request.onStatus?.({ kind: 'reasoning', text: '正在思考…' });
				return;
			}
			if (type === 'thinking.delta' || type === 'reasoning.available') {
				// Hermes 0.21 uses thinking.delta for spinner/wait notices and
				// reasoning.available for assistant-text previews, not reasoning tokens.
				request.onStatus?.({ kind: 'process', text: eventText(frame, 'text') || '等待模型回复…' });
				return;
			}
			if (type === 'tool.start' || type === 'tool.complete') {
				nextReasoningBlock = true;
				request.onStatus?.({ kind: 'process', text: type === 'tool.start' ? `正在调用 ${eventText(frame, 'name') || '工具'}…` : '工具调用完成，继续处理…' });
				return;
			}
			if (type === 'message.delta') {
				nextReasoningBlock = true;
				streamed += eventText(frame, 'delta') || eventText(frame, 'text');
				request.onDelta?.(streamed);
				request.onStatus?.({ kind: 'answer', text: '正在生成回复…' });
				return;
			}
			if (type === 'approval.request') {
				const params = frame.params || {};
				const payload = (params.payload && typeof params.payload === 'object' ? params.payload : {}) as Record<string, unknown>;
				const sessionID = stringValue(params.session_id) || liveSessionID;
				request.onApproval?.({
					patternKey: stringValue(payload.pattern_key),
					description: stringValue(payload.description),
					command: stringValue(payload.command),
				}, (choice) => respondApproval(sessionID, choice));
				return;
			}
			if (type === 'clarify.request') {
				const payload = (frame.params?.payload && typeof frame.params.payload === 'object' ? frame.params.payload : {}) as Record<string, unknown>;
				const requestID = stringValue(payload.request_id);
				if (!requestID) return;
				const choices = Array.isArray(payload.choices)
					? payload.choices.map((choice) => stringValue(choice)).filter(Boolean)
					: [];
				request.onClarify?.({ question: stringValue(payload.question), choices, requestID }, (answer) => respondClarify(requestID, answer));
				return;
			}
			if (type === 'status.update') {
				const payload = (frame.params?.payload && typeof frame.params.payload === 'object' ? frame.params.payload : frame.params) as Record<string, unknown>;
				request.onStatus?.({ kind: stringValue(payload.kind) || 'status', text: stringValue(payload.text) });
				return;
			}
			if (type === 'message.complete') {
				updateReasoning(eventText(frame, 'reasoning'), true);
				const rawUsage = (frame.params?.payload && typeof frame.params.payload === 'object' ? frame.params.payload : frame.params) as Record<string, unknown>;
				const usage = (rawUsage.usage && typeof rawUsage.usage === 'object' ? rawUsage.usage : rawUsage) as Record<string, unknown>;
				const prompt_tokens = firstPositiveNumber(usage.prompt_tokens, usage.input_tokens, usage.input, usage.prompt);
				const completion_tokens = firstPositiveNumber(usage.completion_tokens, usage.output_tokens, usage.output, usage.completion);
				const total_tokens = firstPositiveNumber(usage.total_tokens, usage.total) || (prompt_tokens + completion_tokens);
				if (total_tokens > 0) request.onUsage?.({ prompt_tokens, completion_tokens, total_tokens, model: stringValue(usage.model) });
				const status = eventText(frame, 'status');
				const content = (eventText(frame, 'content') || eventText(frame, 'text') || streamed).trim();
				if (status === 'error' || status === 'failed') {
					finish(new Error(content || 'Hermes 执行失败'));
					return;
				}
				if (!content) {
					finish(new Error('Hermes 没有返回有效内容'));
					return;
				}
				request.onDelta?.(content);
				finish(undefined, content);
				return;
			}
			if (type === 'error' || type === 'gateway.error' || type === 'message.error' || type === 'session.error' || type === 'run.error') {
				finish(new Error(eventText(frame, 'message') || 'Hermes 执行失败'));
			}
		};
		socket.onerror = () => finish(new Error('无法连接 Hermes 对话运行时，请检查桌面运行时和模型设置。'));
		socket.onclose = () => {
			if (!settled) finish(new Error('Hermes 对话连接已断开'));
		};
	});
}

function eventType(frame: RPCFrame) {
	if (frame.method === 'event') return stringValue(frame.params?.type);
	return frame.method || '';
}

function eventText(frame: RPCFrame, key: string) {
	const direct = stringValue(frame.params?.[key]);
	if (direct) return direct;
	const payload = frame.params?.payload;
	if (!payload || typeof payload !== 'object') return '';
	return stringValue((payload as Record<string, unknown>)[key]);
}

function stringValue(value: unknown) {
	return typeof value === 'string' ? value : '';
}

function firstPositiveNumber(...values: unknown[]) {
	for (const value of values) {
		const parsed = Number(value);
		if (Number.isFinite(parsed) && parsed > 0) return parsed;
	}
	return 0;
}
