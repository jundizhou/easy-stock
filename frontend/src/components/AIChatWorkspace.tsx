import {
	Bot,
	Check,
	Copy,
	LoaderCircle,
	MessageSquarePlus,
	PanelLeft,
	RefreshCw,
	Send,
	Settings,
	Square,
	Trash2,
	UserRound,
} from 'lucide-react';
import { FormEvent, KeyboardEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { AppSettings, BackendConfig, LLMModelOption, LLMModelsResult, requestJSON } from '../lib/backend';
import { llmProviderDefaultModel, llmProviderName } from '../lib/llm-providers';
import {
	ChatConversation,
	ChatMessage,
	clearHermesSessionIDs,
	createChatConversation,
	createChatID,
	deriveChatTitle,
	parseStoredConversations,
	storeableConversations,
} from '../lib/chat';
import { streamHermesPrompt } from '../lib/hermes';
import { MessageContent } from './MarkdownContent';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
	initialPrompt?: string;
	onInitialPromptConsumed?: () => void;
	onOpenSettings: () => void;
};

type ModelState = 'loading' | 'ready' | 'missing' | 'error';
type ModelListState = 'idle' | 'loading' | 'ready' | 'error';
type ModelSwitchState = 'idle' | 'switching' | 'saved' | 'error';
type ChatLLMConfig = Pick<AppSettings['llm'], 'provider' | 'base_url' | 'model' | 'api_mode'>;

const STORAGE_KEY = 'easy-stock.ai-conversations.v1';
const LEGACY_STORAGE_KEY = 'a-stock-ai.ai-conversations.v1';
const manualModelOption = '__manual_model_input__';

function loadStoredConversations() {
	const current = window.localStorage.getItem(STORAGE_KEY);
	if (current !== null) return parseStoredConversations(current);
	const legacy = window.localStorage.getItem(LEGACY_STORAGE_KEY);
	if (legacy === null) return [];
	const conversations = parseStoredConversations(legacy);
	window.localStorage.setItem(STORAGE_KEY, JSON.stringify(storeableConversations(conversations)));
	return conversations;
}

const starterPrompts = [
	'结合这套交易体系，梳理大拐点、小拐点和锚定物之间的关系。',
	'把一个主观交易判断拆成可回测的量化因子，并说明验证方法。',
	'审查当前项目的拐点识别方案，找出可能造成未来函数的地方。',
	'帮我设计今天的复盘清单，区分事实、预期与明日验证条件。',
];

export function AIChatWorkspace({ config, refreshKey, initialPrompt, onInitialPromptConsumed, onOpenSettings }: Props) {
	const [conversations, setConversations] = useState<ChatConversation[]>(() => {
		try {
			return loadStoredConversations();
		} catch {
			return [];
		}
	});
	const [activeID, setActiveID] = useState(() => conversations[0]?.id || '');
	const [draft, setDraft] = useState('');
	const [sending, setSending] = useState(false);
	const [modelState, setModelState] = useState<ModelState>('loading');
	const [modelLabel, setModelLabel] = useState('读取模型配置');
	const [llmConfig, setLLMConfig] = useState<ChatLLMConfig | null>(null);
	const [modelOptions, setModelOptions] = useState<LLMModelOption[]>([]);
	const [modelListState, setModelListState] = useState<ModelListState>('idle');
	const [modelListMessage, setModelListMessage] = useState('');
	const [modelSwitchState, setModelSwitchState] = useState<ModelSwitchState>('idle');
	const [modelSwitchMessage, setModelSwitchMessage] = useState('');
	const [manualModelEditing, setManualModelEditing] = useState(false);
	const [manualModelDraft, setManualModelDraft] = useState('');
	const [copiedID, setCopiedID] = useState('');
	const [pendingMessageID, setPendingMessageID] = useState('');
	const abortRef = useRef<AbortController | null>(null);
	const textareaRef = useRef<HTMLTextAreaElement | null>(null);
	const messageEndRef = useRef<HTMLDivElement | null>(null);
	const modelMessageTimerRef = useRef<number | null>(null);

	const activeConversation = useMemo(
		() => conversations.find((conversation) => conversation.id === activeID) || null,
		[activeID, conversations],
	);
	const selectableModels = useMemo(() => {
		const currentModel = llmConfig?.model.trim() || '';
		if (!currentModel || modelOptions.some((option) => option.id === currentModel)) return modelOptions;
		return [{ id: currentModel, display_name: '当前配置' }, ...modelOptions];
	}, [llmConfig?.model, modelOptions]);

	useEffect(() => {
		try {
			window.localStorage.setItem(STORAGE_KEY, JSON.stringify(storeableConversations(conversations)));
		} catch {
			// Local history is a convenience; storage failures must not block chat.
		}
	}, [conversations]);

	useEffect(() => {
		messageEndRef.current?.scrollIntoView({ block: 'end', behavior: 'smooth' });
	}, [activeConversation?.messages.length, sending]);

	useEffect(() => {
		const prompt = initialPrompt?.trim();
		if (!prompt) return;
		setDraft(prompt);
		window.setTimeout(() => textareaRef.current?.focus(), 0);
		onInitialPromptConsumed?.();
	}, [initialPrompt, onInitialPromptConsumed]);

	useEffect(() => {
		const textarea = textareaRef.current;
		if (!textarea) return;
		textarea.style.height = 'auto';
		textarea.style.height = `${Math.min(textarea.scrollHeight, 180)}px`;
	}, [draft]);

	const loadModel = useCallback(async () => {
		if (!config) {
			setModelState('error');
			setModelLabel('后端尚未连接');
			setLLMConfig(null);
			setModelOptions([]);
			setModelListState('error');
			setModelListMessage('后端尚未连接，无法获取模型列表');
			return;
		}
		setModelState('loading');
		setModelListState('loading');
		setModelListMessage('正在读取模型列表…');
		setModelSwitchState('idle');
		setModelSwitchMessage('');
		setManualModelEditing(false);
		try {
			const payload = await requestJSON<{ data: AppSettings }>(config, '/api/v1/settings');
			const { hermes, llm } = payload.data;
			const provider = llm.provider || 'openai';
			const model = llm.model || llmProviderDefaultModel(provider);
			const nextLLM = { provider, base_url: llm.base_url, model, api_mode: llm.api_mode };
			const usable = hermes.available && hermes.configured;
			setLLMConfig(nextLLM);
			setModelState(usable ? 'ready' : hermes.available ? 'missing' : 'error');
			setModelLabel(usable
				? `Hermes · ${llmProviderName(provider)} · ${model}`
				: hermes.message || (hermes.available ? '需要配置 Hermes 模型' : 'Hermes 运行时不可用'));
			try {
				const models = await requestChatModels(config, nextLLM);
				setModelOptions(models.models);
				setModelListState('ready');
				setModelListMessage(`已从模型服务获取 ${models.models.length} 个模型`);
			} catch (error) {
				setModelOptions([]);
				setModelListState('error');
				setModelListMessage(error instanceof Error ? error.message : '获取模型列表失败');
			}
		} catch (error) {
			setModelState('error');
			setModelLabel(error instanceof Error ? error.message : '模型配置读取失败');
			setLLMConfig(null);
			setModelOptions([]);
			setModelListState('error');
			setModelListMessage('模型配置读取失败');
		}
	}, [config]);

	useEffect(() => {
		void loadModel();
	}, [loadModel, refreshKey]);

	useEffect(() => () => {
		abortRef.current?.abort();
		if (modelMessageTimerRef.current !== null) window.clearTimeout(modelMessageTimerRef.current);
	}, []);

	const refreshModels = async () => {
		if (!config || !llmConfig || modelListState === 'loading') return;
		setModelListState('loading');
		setModelListMessage('正在刷新模型列表…');
		try {
			const models = await requestChatModels(config, llmConfig);
			setModelOptions(models.models);
			setModelListState('ready');
			setModelListMessage(`已从模型服务获取 ${models.models.length} 个模型`);
		} catch (error) {
			setModelListState('error');
			setModelListMessage(error instanceof Error ? error.message : '获取模型列表失败');
		}
	};

	const switchModel = async (nextModel: string) => {
		if (!config || !llmConfig || !nextModel || nextModel === llmConfig.model || sending || modelSwitchState === 'switching') return;
		const previous = llmConfig;
		setLLMConfig({ ...llmConfig, model: nextModel });
		setModelSwitchState('switching');
		setModelSwitchMessage(`正在切换到 ${nextModel}…`);
		if (modelMessageTimerRef.current !== null) window.clearTimeout(modelMessageTimerRef.current);
		try {
			const payload = await requestJSON<{ data: AppSettings }>(config, '/api/v1/settings', {
				method: 'PUT',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ llm: { provider: llmConfig.provider, base_url: llmConfig.base_url, model: nextModel, api_mode: llmConfig.api_mode } }),
			});
			const { hermes, llm } = payload.data;
			const savedLLM = { provider: llm.provider, base_url: llm.base_url, model: llm.model, api_mode: llm.api_mode };
			setLLMConfig(savedLLM);
			setConversations((current) => clearHermesSessionIDs(current));
			const usable = hermes.available && hermes.configured;
			setModelState(usable ? 'ready' : hermes.available ? 'missing' : 'error');
			setModelLabel(usable
				? `Hermes · ${llmProviderName(llm.provider)} · ${llm.model}`
				: hermes.message || (hermes.available ? '需要配置 Hermes 模型' : 'Hermes 运行时不可用'));
			setModelSwitchState('saved');
			setModelSwitchMessage(`已切换为 ${llm.model}，下一条消息生效`);
			modelMessageTimerRef.current = window.setTimeout(() => {
				setModelSwitchState('idle');
				setModelSwitchMessage('');
				modelMessageTimerRef.current = null;
			}, 3500);
		} catch (error) {
			setLLMConfig(previous);
			setModelSwitchState('error');
			setModelSwitchMessage(error instanceof Error ? error.message : '切换模型失败');
		}
	};

	const chooseModel = (nextModel: string) => {
		if (nextModel === manualModelOption) {
			setManualModelDraft(llmConfig?.model || '');
			setManualModelEditing(true);
			return;
		}
		void switchModel(nextModel);
	};

	const applyManualModel = () => {
		const nextModel = manualModelDraft.trim();
		if (!nextModel) return;
		setManualModelEditing(false);
		if (nextModel !== llmConfig?.model) void switchModel(nextModel);
	};

	const upsertConversation = (next: ChatConversation) => {
		setConversations((current) => [next, ...current.filter((conversation) => conversation.id !== next.id)]);
	};

	const newConversation = () => {
		abortRef.current?.abort();
		setSending(false);
		setPendingMessageID('');
		const next = createChatConversation();
		upsertConversation(next);
		setActiveID(next.id);
		setDraft('');
		window.setTimeout(() => textareaRef.current?.focus(), 0);
	};

	const deleteConversation = (id: string) => {
		if (sending && id === activeID) abortRef.current?.abort();
		setConversations((current) => {
			const next = current.filter((conversation) => conversation.id !== id);
			if (id === activeID) setActiveID(next[0]?.id || '');
			return next;
		});
	};

	const sendMessage = async (event?: FormEvent) => {
		event?.preventDefault();
		const content = draft.trim();
		if (!content || !config || sending) return;

		const now = new Date().toISOString();
		const current = activeConversation || createChatConversation(now);
		const userMessage: ChatMessage = {
			id: createChatID('message'),
			role: 'user',
			content,
			created_at: now,
		};
		const pendingConversation: ChatConversation = {
			...current,
			title: current.messages.length ? current.title : deriveChatTitle(content),
			messages: [...current.messages, userMessage],
			updated_at: now,
		};
		upsertConversation(pendingConversation);
		setActiveID(pendingConversation.id);
		setDraft('');
		setSending(true);
		const assistantID = createChatID('message');
		const assistantMessage: ChatMessage = {
			id: assistantID,
			role: 'assistant',
			content: '',
			created_at: now,
		};
		setPendingMessageID(assistantID);
		setConversations((items) => items.map((conversation) => conversation.id === pendingConversation.id
			? { ...conversation, messages: [...conversation.messages, assistantMessage] }
			: conversation));

		const controller = new AbortController();
		abortRef.current = controller;
		try {
			const seedMessages = current.messages
				.filter((message) => !message.error)
				.slice(-40)
				.map(({ role, content: messageContent }) => ({ role, content: messageContent }));
			const result = await streamHermesPrompt({
				config,
				prompt: content,
				hermesSessionID: current.hermes_session_id,
				seedMessages,
				signal: controller.signal,
				onSession: (hermesSessionID) => setConversations((items) => items.map((conversation) => conversation.id === pendingConversation.id
					? { ...conversation, hermes_session_id: hermesSessionID }
					: conversation)),
				onDelta: (nextContent) => setConversations((items) => items.map((conversation) => conversation.id === pendingConversation.id
					? { ...conversation, messages: conversation.messages.map((message) => message.id === assistantID ? { ...message, content: nextContent } : message) }
					: conversation)),
			});
			const completedAt = new Date().toISOString();
			setConversations((items) => items.map((conversation) => conversation.id === pendingConversation.id
				? { ...conversation, hermes_session_id: result.hermesSessionID || conversation.hermes_session_id, messages: conversation.messages.map((message) => message.id === assistantID ? { ...message, content: result.content, created_at: completedAt } : message), updated_at: completedAt }
				: conversation));
			setModelState('ready');
		} catch (error) {
			if ((error as Error)?.name !== 'AbortError') {
				const failedAt = new Date().toISOString();
				setConversations((items) => items.map((conversation) => conversation.id === pendingConversation.id
					? { ...conversation, messages: conversation.messages.map((message) => message.id === assistantID ? { ...message, content: error instanceof Error ? error.message : 'Hermes 对话请求失败，请稍后重试。', created_at: failedAt, error: true } : message), updated_at: failedAt }
					: conversation));
			} else {
				setConversations((items) => items.map((conversation) => conversation.id === pendingConversation.id
					? { ...conversation, messages: conversation.messages.filter((message) => message.id !== assistantID) }
					: conversation));
			}
		} finally {
			if (abortRef.current === controller) abortRef.current = null;
			setSending(false);
			setPendingMessageID('');
		}
	};

	const stop = () => {
		abortRef.current?.abort();
		abortRef.current = null;
		setSending(false);
		setPendingMessageID('');
	};

	const handleComposerKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
		if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
			event.preventDefault();
			void sendMessage();
		}
	};

	const copyMessage = async (message: ChatMessage) => {
		await navigator.clipboard?.writeText(message.content);
		setCopiedID(message.id);
		window.setTimeout(() => setCopiedID((current) => current === message.id ? '' : current), 1500);
	};

	return (
		<section className="ai-chat-workspace">
			<aside className="ai-thread-rail">
				<header><div><span>AI WORKSPACE</span><strong>对话记录</strong></div><button type="button" onClick={newConversation} title="新建对话"><MessageSquarePlus size={17} /></button></header>
				<div className="ai-thread-list">
					{conversations.map((conversation) => (
						<button type="button" className={conversation.id === activeID ? 'active' : ''} onClick={() => setActiveID(conversation.id)} key={conversation.id}>
							<PanelLeft size={14} />
							<span><strong>{conversation.title}</strong><small>{conversation.messages.length ? `${conversation.messages.length} 条消息` : '空白对话'} · {formatRelativeTime(conversation.updated_at)}</small></span>
							<i role="button" tabIndex={0} aria-label="删除对话" onClick={(event) => { event.stopPropagation(); deleteConversation(conversation.id); }} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); event.stopPropagation(); deleteConversation(conversation.id); } }}><Trash2 size={13} /></i>
						</button>
					))}
					{!conversations.length && <div className="ai-thread-empty"><Bot size={22} /><span>新对话会保存在本机</span></div>}
				</div>
				<div className="ai-local-note"><span className={`status-dot ${modelState}`} /><div><strong>{modelLabel}</strong><small>Hermes 会话与对话历史保存在当前设备</small></div></div>
			</aside>

			<div className="ai-conversation-panel">
				<header className="ai-conversation-header">
					<div className="ai-assistant-avatar"><Bot size={20} /></div>
					<div><strong>{activeConversation?.title || 'AI 研究助手'}</strong><span className={modelSwitchState === 'error' ? 'error' : modelState}>{modelSwitchMessage || modelLabel}</span></div>
					<div className="ai-conversation-tools">
						<div className={`ai-chat-model-picker ${modelListState} ${manualModelEditing ? 'manual' : ''}`} title={modelListMessage || '选择当前 AI 对话使用的模型'}>
							<span>模型</span>
							{manualModelEditing ? (
								<>
									<input aria-label="手动输入对话模型" autoFocus value={manualModelDraft} onChange={(event) => setManualModelDraft(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); applyManualModel(); } else if (event.key === 'Escape') setManualModelEditing(false); }} placeholder="输入模型 ID" />
									<button type="button" onClick={applyManualModel} disabled={!manualModelDraft.trim()} title="应用模型"><Check size={14} /></button>
								</>
							) : (
								<>
									<select aria-label="选择对话模型" value={llmConfig?.model || ''} onChange={(event) => chooseModel(event.target.value)} disabled={!config || !llmConfig || sending || modelSwitchState === 'switching' || modelListState === 'loading'}>
										{!selectableModels.length && <option value="">{modelListState === 'loading' ? '读取模型中…' : '暂无可选模型'}</option>}
										{selectableModels.map((option) => <option value={option.id} key={option.id}>{modelOptionLabel(option)}</option>)}
										{llmConfig && <option value={manualModelOption}>手动输入其他模型…</option>}
									</select>
									{modelSwitchState === 'switching' && <LoaderCircle className="spin" size={14} />}
								</>
							)}
						</div>
						<button type="button" className={`ai-chat-model-refresh ${modelListState}`} onClick={() => void refreshModels()} disabled={!config || !llmConfig || sending || modelSwitchState === 'switching' || modelListState === 'loading'} title={modelListState === 'error' ? `模型列表获取失败：${modelListMessage}` : '刷新模型列表'}>{modelListState === 'loading' ? <LoaderCircle className="spin" size={15} /> : <RefreshCw size={15} />}</button>
						<button type="button" className="ai-chat-settings-button" onClick={onOpenSettings}><Settings size={15} />Hermes 设置</button>
					</div>
				</header>

				<div className={`ai-message-stage ${activeConversation?.messages.length ? 'has-messages' : ''}`}>
					{!activeConversation?.messages.length ? (
						<div className="ai-welcome">
							<div className="ai-welcome-mark"><Bot size={28} /></div>
							<span>AI RESEARCH COPILOT</span>
							<h2>今天想一起研究什么？</h2>
							<p>由本机 Hermes 驱动，并使用系统设置中的模型，像 Codex 一样围绕目标持续对话、拆解问题并形成可执行结果。</p>
							<div className="ai-starter-grid">{starterPrompts.map((prompt) => <button type="button" key={prompt} onClick={() => { setDraft(prompt); textareaRef.current?.focus(); }}>{prompt}<Send size={14} /></button>)}</div>
						</div>
					) : (
						<div className="ai-message-list">
							{activeConversation.messages.map((message) => {
								const pending = message.id === pendingMessageID;
								return (
								<article className={`ai-message ${message.role} ${message.error ? 'error' : ''} ${pending ? 'pending' : ''}`} key={message.id}>
									<div className="ai-message-avatar">{message.role === 'user' ? <UserRound size={16} /> : <Bot size={16} />}</div>
									<div className="ai-message-body">
										<header><strong>{message.role === 'user' ? '你' : 'AI 助手'}</strong><time>{formatMessageTime(message.created_at)}</time></header>
										{message.content && <MessageContent content={message.content} markdown={message.role === 'assistant' && !message.error} />}
										{pending && <div className="ai-answering" role="status" aria-live="polite"><span className="ai-answering-bars" aria-hidden="true"><i /><i /><i /><i /></span><strong>AI 正在回答</strong><small>{message.content ? '正在继续分析并生成后续内容…' : '正在理解问题并组织答案…'}</small></div>}
										{!pending && <button type="button" className="ai-copy-message" onClick={() => void copyMessage(message)}>{copiedID === message.id ? <Check size={13} /> : <Copy size={13} />}{copiedID === message.id ? '已复制' : '复制'}</button>}
									</div>
								</article>
								);
							})}
							<div ref={messageEndRef} />
						</div>
					)}
				</div>

				<form className="ai-composer-wrap" onSubmit={(event) => void sendMessage(event)}>
					<div className={`ai-composer ${sending ? 'sending' : ''}`}>
						<textarea ref={textareaRef} value={draft} onChange={(event) => setDraft(event.target.value)} onKeyDown={handleComposerKeyDown} placeholder={modelState === 'missing' ? '请先配置 Hermes 模型后开始对话' : modelState === 'error' ? 'Hermes 运行时不可用，请检查安装或设置' : '向 Hermes AI 描述任务，Enter 发送，Shift + Enter 换行'} disabled={!config || modelState !== 'ready'} rows={1} />
						<div className="ai-composer-actions"><span>AI 可能会犯错，请核对关键事实与交易数据。</span>{sending ? <button type="button" className="stop" onClick={stop} title="停止生成"><Square size={14} />停止</button> : <button type="submit" disabled={!draft.trim() || !config || modelState !== 'ready'} title="发送消息"><Send size={15} />发送</button>}</div>
					</div>
					{modelState !== 'ready' && <button type="button" className="ai-configure-hint" onClick={onOpenSettings}><Settings size={14} />{modelState === 'error' ? 'Hermes 运行时不可用，查看系统设置' : '尚未配置 Hermes 模型，打开系统设置'}</button>}
				</form>
			</div>
		</section>
	);
}

async function requestChatModels(config: BackendConfig, llm: ChatLLMConfig) {
	const payload = await requestJSON<{ data: LLMModelsResult }>(config, '/api/v1/settings/llm/models', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ provider: llm.provider, base_url: llm.base_url }),
	});
	return payload.data;
}

function modelOptionLabel(option: LLMModelOption) {
	if (option.display_name && option.display_name !== option.id) return `${option.display_name} · ${option.id}`;
	return option.id;
}

function formatRelativeTime(value: string) {
	const elapsed = Date.now() - new Date(value).getTime();
	if (elapsed < 60_000) return '刚刚';
	if (elapsed < 3_600_000) return `${Math.floor(elapsed / 60_000)}分钟前`;
	if (elapsed < 86_400_000) return `${Math.floor(elapsed / 3_600_000)}小时前`;
	return new Date(value).toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' });
}

function formatMessageTime(value: string) {
	return new Date(value).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
}
