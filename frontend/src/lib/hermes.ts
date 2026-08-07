import type { BackendConfig } from './backend';

export type HermesStreamResult = {
	content: string;
	hermesSessionID: string;
};

export type HermesStreamRequest = {
	config: BackendConfig;
	prompt: string;
	hermesSessionID?: string;
	seedMessages?: Array<{ role: 'user' | 'assistant'; content: string }>;
	onDelta?: (content: string) => void;
	onSession?: (sessionID: string) => void;
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
		let timeout: ReturnType<typeof setTimeout> | undefined;

		const cleanup = () => {
			if (timeout) clearTimeout(timeout);
			request.signal?.removeEventListener('abort', abort);
		};
		const finish = (error?: unknown, content = '') => {
			if (settled) return;
			settled = true;
			cleanup();
			if (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING) socket.close();
			if (error) reject(error);
			else resolve({ content, hermesSessionID: storedSessionID });
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
		const submitPrompt = () => {
			submitRequestID = send('prompt.submit', { session_id: liveSessionID, text: request.prompt });
		};
		const setupSession = (method = setupMethod) => {
			setupMethod = method;
			setupRequestID = method === 'session.resume'
				? send(method, { session_id: request.hermesSessionID })
				: send(method, {
					client: 'easy-stock-frontend',
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
			if (type === 'message.delta') {
				streamed += eventText(frame, 'delta') || eventText(frame, 'text');
				request.onDelta?.(streamed);
				return;
			}
			if (type === 'message.complete') {
				const content = (eventText(frame, 'content') || eventText(frame, 'text') || streamed).trim();
				if (!content) {
					finish(new Error('Hermes 没有返回有效内容'));
					return;
				}
				request.onDelta?.(content);
				finish(undefined, content);
				return;
			}
			if (type === 'gateway.error' || type === 'message.error' || type === 'session.error' || type === 'run.error') {
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
