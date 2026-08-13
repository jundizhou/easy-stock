import {
	Bot,
	CheckCircle2,
	Database,
	CircleAlert,
	Globe2,
	Clock3,
	KeyRound,
	LoaderCircle,
	LogIn,
	PlugZap,
	Plus,
	RefreshCw,
	QrCode,
	Save,
	Server,
	ShieldCheck,
	Trash2,
	X,
} from 'lucide-react';
import { FormEvent, useEffect, useMemo, useRef, useState } from 'react';
import { AppSettings, BackendConfig, BrowserAuthStatus, LLMConnectionTestResult, LLMModelOption, LLMModelsResult, LLMProfile, ReviewAutomationProfile, SecretSettingStatus, WechatServiceStatus, requestJSON } from '../lib/backend';
import { llmProviderDefinition, llmProviders } from '../lib/llm-providers';
import { AppUpdatePanel } from './AppUpdatePanel';
import { HermesAgentSettingsPanel } from './HermesAgentSettingsPanel';

type Props = {
	config: BackendConfig | null;
	open: boolean;
	onClose: () => void;
	onSaved?: () => void;
};

type SecretKey = 'llm_api_key' | 'tushare_token' | 'ths_cookie' | 'xueqiu_cookie' | 'eastmoney_cookie' | 'wechat_api_token';
type ReviewSource = 'wechat' | 'xueqiu' | 'taoguba';
type ReviewProfileDraft = Omit<ReviewAutomationProfile, 'credential'> & { credential: SecretSettingStatus; credential_value: string; clear_credential: boolean };
type ModelListState = 'idle' | 'loading' | 'success' | 'error';

const manualModelOption = '__manual_model_input__';

const emptySecrets = (): Record<SecretKey, string> => ({
	llm_api_key: '',
	tushare_token: '',
	ths_cookie: '',
	xueqiu_cookie: '',
	eastmoney_cookie: '',
	wechat_api_token: '',
});

export function SettingsDrawer({ config, open, onClose, onSaved }: Props) {
	const [settings, setSettings] = useState<AppSettings | null>(null);
	const [llmProfiles, setLLMProfiles] = useState<LLMProfile[]>([]);
	const [activeLLMProfileID, setActiveLLMProfileID] = useState('');
	const [profileName, setProfileName] = useState('');
	const [profileKeyValues, setProfileKeyValues] = useState<Record<string, string>>({});
	const [clearProfileKeys, setClearProfileKeys] = useState<Set<string>>(new Set());
	const [provider, setProvider] = useState('openai');
	const [baseURL, setBaseURL] = useState(llmProviderDefinition('openai').baseURL);
	const [model, setModel] = useState('');
	const [apiMode, setAPIMode] = useState('chat_completions');
	const [reviewSource, setReviewSource] = useState<ReviewSource>('xueqiu');
	const [reviewProfiles, setReviewProfiles] = useState<ReviewProfileDraft[]>([]);
	const [secrets, setSecrets] = useState<Record<SecretKey, string>>(emptySecrets);
	const [clearSecrets, setClearSecrets] = useState<Set<SecretKey>>(new Set());
	const [state, setState] = useState<'idle' | 'loading' | 'saving' | 'saved' | 'error'>('idle');
	const [message, setMessage] = useState('');
	const [testState, setTestState] = useState<'idle' | 'testing' | 'success' | 'error'>('idle');
	const [testResult, setTestResult] = useState<LLMConnectionTestResult | null>(null);
	const [modelOptions, setModelOptions] = useState<LLMModelOption[]>([]);
	const [modelListState, setModelListState] = useState<ModelListState>('idle');
	const [modelListMessage, setModelListMessage] = useState('');
	const [manualModel, setManualModel] = useState(true);
	const [browserAuthStatuses, setBrowserAuthStatuses] = useState<Record<string, BrowserAuthStatus>>({});
	const [openingBrowserProfile, setOpeningBrowserProfile] = useState('');
	const [wechatServiceStatus, setWechatServiceStatus] = useState<WechatServiceStatus>({ available: false, configured: false, authenticated: false, state: 'starting', message: '内置微信公众号服务正在启动' });
	const [wechatLoginURL, setWechatLoginURL] = useState('');
	const [wechatLoginBaseline, setWechatLoginBaseline] = useState('');
	const modelFetchSequence = useRef(0);

	useEffect(() => {
		if (!open) return;
		const onKeyDown = (event: KeyboardEvent) => {
			if (event.key === 'Escape') onClose();
		};
		window.addEventListener('keydown', onKeyDown);
		return () => window.removeEventListener('keydown', onKeyDown);
	}, [onClose, open]);

	useEffect(() => {
		if (!open || !config) return;
		let cancelled = false;
		modelFetchSequence.current += 1;
		setState('loading');
		setMessage('');
		setTestState('idle');
		setTestResult(null);
		setModelOptions([]);
		setModelListState('idle');
		setModelListMessage('');
		setManualModel(true);
		requestJSON<{ data: AppSettings }>(config, '/api/v1/settings')
			.then((payload) => {
				if (cancelled) return;
				setSettings(payload.data);
				const modelProfiles = normalizeLLMProfiles(payload.data);
				setLLMProfiles(modelProfiles);
				const selected = modelProfiles.find((profile) => profile.id === payload.data.active_llm_profile_id) || modelProfiles[0];
				setActiveLLMProfileID(selected.id);
				loadProfileFields(selected);
				setProfileKeyValues({});
				setClearProfileKeys(new Set());
				const profiles = toProfileDrafts(payload.data.review_automation?.profiles || []);
				setReviewProfiles(profiles);
				void refreshBrowserAuthStatuses(profiles);
				setSecrets(emptySecrets());
				setClearSecrets(new Set());
				setState('idle');
			})
			.catch((error) => {
				if (cancelled) return;
				setState('error');
				setMessage(error instanceof Error ? error.message : '读取设置失败');
			});
		return () => {
			cancelled = true;
			modelFetchSequence.current += 1;
		};
	}, [config, open]);

	useEffect(() => {
		if (!open) {
			setWechatLoginURL('');
			return;
		}
		let cancelled = false;
		const refresh = async () => {
			const status = await getWechatServiceStatus();
			if (cancelled) return;
			setWechatServiceStatus(status);
			if (wechatLoginURL && status.authenticated && (!wechatLoginBaseline || status.expires_at !== wechatLoginBaseline)) {
				setWechatLoginURL('');
				setWechatLoginBaseline('');
			}
		};
		void refresh();
		const timer = window.setInterval(refresh, wechatLoginURL ? 2000 : 8000);
		return () => {
			cancelled = true;
			window.clearInterval(timer);
		};
	}, [open, wechatLoginBaseline, wechatLoginURL]);

	const configuredCount = useMemo(() => {
		if (!settings) return 0;
		const sharedCredentials = Object.entries(settings.credentials).filter(([key]) => key !== 'xueqiu_cookie' && key !== 'wechat_api_token').map(([, value]) => value);
		const browserSessions = Object.values(browserAuthStatuses).filter((item) => item.configured).length;
		return [...settings.llm_profiles.map((profile) => profile.api_key), ...sharedCredentials].filter((item) => item.configured).length + browserSessions + (wechatServiceStatus.authenticated ? 1 : 0);
	}, [browserAuthStatuses, settings, wechatServiceStatus.authenticated]);

	const selectedLLMProfile = useMemo(() => llmProfiles.find((profile) => profile.id === activeLLMProfileID), [activeLLMProfileID, llmProfiles]);

	const patchSelectedLLMProfile = (patch: Partial<LLMProfile>) => setLLMProfiles((current) => current.map((profile) => profile.id === activeLLMProfileID ? { ...profile, ...patch } : profile));

	const loadProfileFields = (profile: LLMProfile) => {
		setProfileName(profile.name);
		setProvider(profile.provider || 'openai');
		setBaseURL(profile.base_url || llmProviderDefinition(profile.provider || 'openai').baseURL);
		setModel(profile.model || '');
		setAPIMode(profile.api_mode === 'responses' ? 'codex_responses' : profile.api_mode || (profile.provider === 'anthropic' ? 'anthropic_messages' : 'chat_completions'));
	};

	const selectLLMProfile = (id: string) => {
		const profile = llmProfiles.find((item) => item.id === id);
		if (!profile) return;
		setActiveLLMProfileID(id);
		loadProfileFields(profile);
		resetModelList();
		setTestState('idle');
		setTestResult(null);
	};

	const addLLMProfile = () => {
		const id = `llm-${Date.now()}-${Math.random().toString(16).slice(2)}`;
		const definition = llmProviderDefinition('openai');
		const profile: LLMProfile = { id, name: '新模型配置', provider: 'openai', base_url: definition.baseURL, model: definition.defaultModel, api_mode: definition.apiMode, api_key: { configured: false } };
		setLLMProfiles((current) => [...current, profile]);
		setActiveLLMProfileID(id);
		loadProfileFields(profile);
		resetModelList();
	};

	const removeLLMProfile = () => {
		if (llmProfiles.length <= 1) return;
		const remaining = llmProfiles.filter((profile) => profile.id !== activeLLMProfileID);
		const next = remaining[0];
		setLLMProfiles(remaining);
		setActiveLLMProfileID(next.id);
		loadProfileFields(next);
		setProfileKeyValues((current) => { const copy = { ...current }; delete copy[activeLLMProfileID]; return copy; });
		setClearProfileKeys((current) => { const copy = new Set(current); copy.delete(activeLLMProfileID); return copy; });
		resetModelList();
	};

	const selectableModels = useMemo(() => {
		const currentModel = model.trim();
		if (!currentModel || modelOptions.some((option) => option.id === currentModel)) return modelOptions;
		return [{ id: currentModel, display_name: '当前配置' }, ...modelOptions];
	}, [model, modelOptions]);

	const resetModelList = () => {
		modelFetchSequence.current += 1;
		setModelOptions([]);
		setModelListState('idle');
		setModelListMessage('');
		setManualModel(true);
	};

	const updateProvider = (nextProvider: string) => {
		const nextDefinition = llmProviderDefinition(nextProvider);
		setProvider(nextProvider);
		setBaseURL(nextDefinition.baseURL);
		setModel(nextDefinition.defaultModel);
		setAPIMode(nextDefinition.apiMode);
		patchSelectedLLMProfile({ provider: nextProvider, base_url: nextDefinition.baseURL, model: nextDefinition.defaultModel, api_mode: nextDefinition.apiMode });
		resetModelList();
		setTestState('idle');
		setTestResult(null);
		setModelListMessage(nextDefinition.baseURL
			? '输入模型 API Key 后将自动获取模型，也可手动点击“获取模型”。'
			: '请输入兼容接口的 Base URL 和 API Key，再获取模型列表。');
	};

	const updateSecret = (key: SecretKey, value: string) => {
		if (key === 'llm_api_key') {
			setProfileKeyValues((current) => ({ ...current, [activeLLMProfileID]: value }));
			setClearProfileKeys((current) => { const next = new Set(current); if (value) next.delete(activeLLMProfileID); return next; });
			resetModelList(); setTestState('idle'); setTestResult(null); return;
		}
		setSecrets((current) => ({ ...current, [key]: value }));
		if (value) {
			setClearSecrets((current) => {
				const next = new Set(current);
				next.delete(key);
				return next;
			});
		}
	};

	const toggleClear = (key: SecretKey) => {
		if (key === 'llm_api_key') {
			setClearProfileKeys((current) => { const next = new Set(current); if (next.has(activeLLMProfileID)) next.delete(activeLLMProfileID); else next.add(activeLLMProfileID); return next; });
			setProfileKeyValues((current) => ({ ...current, [activeLLMProfileID]: '' }));
			resetModelList(); return;
		}
		setClearSecrets((current) => {
			const next = new Set(current);
			if (next.has(key)) next.delete(key);
			else next.add(key);
			return next;
		});
		setSecrets((current) => ({ ...current, [key]: '' }));
	};

	const updateModel = (nextModel: string) => {
		setModel(nextModel);
		patchSelectedLLMProfile({ model: nextModel });
		setTestState('idle');
		setTestResult(null);
	};

	const updateBaseURL = (nextBaseURL: string) => {
		setBaseURL(nextBaseURL);
		patchSelectedLLMProfile({ base_url: nextBaseURL });
		resetModelList();
		setTestState('idle');
		setTestResult(null);
	};

	const fetchModels = async (overrides: { provider?: string; baseURL?: string; apiKey?: string; automatic?: boolean } = {}) => {
		if (!config) return;
		const fetchID = ++modelFetchSequence.current;
		const requestProvider = overrides.provider || provider;
		const requestBaseURL = overrides.baseURL ?? baseURL.trim();
		const requestAPIKey = overrides.apiKey ?? (profileKeyValues[activeLLMProfileID] || '').trim();
		setModelListState('loading');
		setModelListMessage(overrides.automatic ? `正在自动读取 ${llmProviderDefinition(requestProvider).label} 的模型列表…` : '正在读取模型服务的模型列表…');
		try {
			const request: { provider: string; base_url: string; api_key?: string } = { provider: requestProvider, base_url: requestBaseURL };
			if (requestAPIKey || clearProfileKeys.has(activeLLMProfileID)) request.api_key = requestAPIKey;
			(request as { profile_id?: string }).profile_id = activeLLMProfileID;
			const payload = await requestJSON<{ data: LLMModelsResult }>(config, '/api/v1/settings/llm/models', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(request),
			});
			if (fetchID !== modelFetchSequence.current) return;
			setModelOptions(payload.data.models);
			setModelListState('success');
			setModelListMessage(`已从 ${payload.data.source_url} 获取 ${payload.data.models.length} 个模型`);
			setManualModel(false);
		} catch (error) {
			if (fetchID !== modelFetchSequence.current) return;
			setModelOptions([]);
			setModelListState('error');
			setModelListMessage(error instanceof Error ? error.message : '获取模型列表失败');
			setManualModel(true);
		}
	};

	const fetchModelsAfterAPIKeyInput = (apiKey: string) => {
		const trimmedAPIKey = apiKey.trim();
		if (!trimmedAPIKey) return;
		if (!baseURL.trim()) {
			setModelListState('error');
			setModelListMessage('请先填写 API Base URL，再自动获取模型列表。');
			return;
		}
		void fetchModels({ apiKey: trimmedAPIKey, automatic: true });
	};

	const updateReviewProfile = (id: string, patch: Partial<ReviewProfileDraft>) => setReviewProfiles((current) => current.map((profile) => profile.id === id ? { ...profile, ...patch } : profile));
	const addReviewProfile = (source: ReviewSource) => setReviewProfiles((current) => [...current, newProfileDraft(source)]);
	const removeReviewProfile = (id: string) => setReviewProfiles((current) => current.filter((profile) => profile.id !== id));

	const refreshBrowserAuthStatuses = async (profiles: ReviewProfileDraft[]) => {
		const browserProfiles = profiles.filter((profile) => profile.source === 'xueqiu' || profile.source === 'taoguba');
		if (!window.aStock?.getBrowserAuthStatus) {
			setBrowserAuthStatuses(Object.fromEntries(browserProfiles.map((profile) => [profile.id, { configured: false, message: '请在桌面应用中配置浏览器登录态' }])));
			return;
		}
		const statuses = await Promise.all(browserProfiles.map(async (profile) => {
			try {
				return [profile.id, await window.aStock!.getBrowserAuthStatus!(profile.id, profile.source as 'xueqiu' | 'taoguba')] as const;
			} catch (error) {
				return [profile.id, { configured: false, message: error instanceof Error ? error.message : '读取浏览器登录态失败' }] as const;
			}
		}));
		setBrowserAuthStatuses(Object.fromEntries(statuses));
	};

	const openReviewSourceLogin = async (profile: ReviewProfileDraft) => {
		const source = profile.source as 'xueqiu' | 'taoguba';
		const label = reviewSourceLabel(source);
		const opener = window.aStock?.openReviewSourceLogin
			? (id: string, homepageURL: string) => window.aStock!.openReviewSourceLogin!(source, id, homepageURL)
			: source === 'xueqiu' && window.aStock?.openXueqiuLogin
				? (id: string, homepageURL: string) => window.aStock!.openXueqiuLogin!(id, homepageURL)
				: null;
		if (!opener) {
			setBrowserAuthStatuses((current) => ({ ...current, [profile.id]: { configured: false, message: '内置登录窗口仅在桌面应用中可用' } }));
			return;
		}
		setOpeningBrowserProfile(profile.id);
		setBrowserAuthStatuses((current) => ({ ...current, [profile.id]: { ...current[profile.id], configured: current[profile.id]?.configured || false, message: `登录窗口已打开；完成${label}登录或安全验证后，点击“我已完成登录”` } }));
		try {
			const status = await opener(profile.id, profile.base_url || (source === 'xueqiu' ? 'https://xueqiu.com' : 'https://www.tgb.cn'));
			setBrowserAuthStatuses((current) => ({ ...current, [profile.id]: status }));
		} catch (error) {
			setBrowserAuthStatuses((current) => ({ ...current, [profile.id]: { configured: false, message: error instanceof Error ? error.message : `打开${label}登录窗口失败` } }));
		} finally {
			setOpeningBrowserProfile('');
		}
	};

	const openWechatLogin = async () => {
		if (!window.aStock?.openWechatLogin) {
			setWechatServiceStatus({ available: false, configured: false, authenticated: false, state: 'error', message: '扫码登录仅在桌面应用中可用' });
			return;
		}
		try {
			const status = await window.aStock.openWechatLogin();
			setWechatServiceStatus(status);
			setWechatLoginBaseline(status.expires_at || '');
			setWechatLoginURL(status.login_url || '');
		} catch (error) {
			setWechatServiceStatus((current) => ({ ...current, state: 'error', message: error instanceof Error ? error.message : '打开微信公众号扫码页面失败' }));
		}
	};

	const persistSettings = async () => {
		if (!config) throw new Error('后端尚未连接');
		const credentials: Record<string, string> = {};
		for (const key of ['tushare_token', 'ths_cookie', 'xueqiu_cookie', 'eastmoney_cookie'] as SecretKey[]) {
			if (secrets[key].trim()) credentials[key] = secrets[key].trim();
		}
		const modelProfiles = llmProfiles.map((profile) => {
			const current = profile.id === activeLLMProfileID ? { ...profile, name: profileName.trim(), provider, base_url: baseURL.trim(), model: model.trim(), api_mode: apiMode } : profile;
			return { id: current.id, name: current.name.trim(), provider: current.provider, base_url: current.base_url.trim(), model: current.model.trim(), api_mode: current.api_mode, api_key: (profileKeyValues[current.id] || '').trim() || undefined, clear_api_key: clearProfileKeys.has(current.id) };
		});
		const payload = await requestJSON<{ data: AppSettings }>(config, '/api/v1/settings', {
			method: 'PUT',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ llm_profiles: modelProfiles, active_llm_profile_id: activeLLMProfileID, credentials, review_automation: { profiles: reviewProfiles.map((profile) => ({ id: profile.id, source: profile.source, name: profile.name.trim(), base_url: profile.source === 'wechat' ? '' : profile.base_url.trim(), credential: profile.source === 'wechat' ? undefined : profile.credential_value.trim() || undefined, clear_credential: profile.source === 'wechat' || profile.clear_credential, sync_hour: profile.sync_hour, auto_analyze: profile.auto_analyze, enabled: profile.enabled })) }, clear_secrets: [...clearSecrets].filter((key) => key !== 'llm_api_key').concat('wechat_api_token') }),
		});
		setSettings(payload.data);
		const savedProfiles = normalizeLLMProfiles(payload.data);
		setLLMProfiles(savedProfiles);
		const savedActive = savedProfiles.find((profile) => profile.id === payload.data.active_llm_profile_id) || savedProfiles[0];
		setActiveLLMProfileID(savedActive.id);
		loadProfileFields(savedActive);
		setProfileKeyValues({});
		setClearProfileKeys(new Set());
		const savedReviewProfiles = toProfileDrafts(payload.data.review_automation?.profiles || []);
		setReviewProfiles(savedReviewProfiles);
		void refreshBrowserAuthStatuses(savedReviewProfiles);
		setSecrets(emptySecrets());
		setClearSecrets(new Set());
		return payload.data;
	};

	const save = async (event: FormEvent) => {
		event.preventDefault();
		if (!config) return;
		setState('saving');
		setMessage('');
		try {
			await persistSettings();
			setState('saved');
			setMessage('设置已同步到本机 Hermes');
			onSaved?.();
		} catch (error) {
			setState('error');
			setMessage(error instanceof Error ? error.message : '保存设置失败');
		}
	};

	const testConnection = async () => {
		if (!config) return;
		setState('saving');
		setMessage('正在保存设置并通过 Hermes 调用模型探针');
		setTestState('testing');
		setTestResult(null);
		try {
			await persistSettings();
			const payload = await requestJSON<{ data: LLMConnectionTestResult }>(config, '/api/v1/settings/llm/test', { method: 'POST' });
			setTestResult(payload.data);
			setTestState('success');
			setState('saved');
			setMessage(`Hermes 模型连接成功，耗时 ${payload.data.latency_ms}ms`);
		} catch (error) {
			setTestState('error');
			setState('error');
			setMessage(error instanceof Error ? error.message : '模型连接测试失败');
		}
	};

	if (!open) return null;

	return (
		<div className="settings-overlay" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
			<aside className="settings-drawer" role="dialog" aria-modal="true" aria-label="系统设置">
				<header className="settings-header">
					<div><span>HERMES LOCAL RUNTIME</span><h2>系统设置</h2><p>管理 Hermes 模型运行时与外部数据源凭据</p></div>
					<button type="button" onClick={onClose} aria-label="关闭设置"><X size={20} /></button>
				</header>

				{state === 'loading' && <div className="settings-loading"><LoaderCircle className="spin" size={22} /><span>读取本机设置</span></div>}
				{state !== 'loading' && (
					<form className="settings-form" onSubmit={save}>
						<section className="settings-security-note">
							<ShieldCheck size={19} />
							<div><strong>模型密钥由 Hermes 管理</strong><span>API Key 只写入 Hermes 的本机 .env；页面仅读取是否已配置，不会取回密钥原文。</span></div>
							<em>{configuredCount} 项已配置</em>
						</section>

						<HermesAgentSettingsPanel config={config} open={open} />

						<section className="settings-section">
							<div className="settings-section-title"><Bot size={18} /><div><h3>Hermes 模型运行时</h3><p>侧边栏对话、模型探针和复盘 AI 提炼统一由本机 Hermes 驱动。</p></div></div>
							<div className={`llm-connection-test ${settings?.hermes.available ? settings.hermes.configured ? 'success' : '' : 'error'}`}>
								<div><Bot size={17} /><span><strong>{settings?.hermes.available ? `Hermes ${settings.hermes.version || 'Runtime'} 已安装` : 'Hermes 运行时不可用'}</strong><small>{settings?.hermes.message || (settings?.hermes.configured ? '运行时和模型配置均已就绪。' : '运行时已就绪，请继续配置模型连接。')}</small></span></div>
							</div>
							<div className="llm-profile-toolbar">
								<label><span>模型配置</span><select value={activeLLMProfileID} onChange={(event) => selectLLMProfile(event.target.value)}>{llmProfiles.map((profile) => <option value={profile.id} key={profile.id}>{profile.name} · {profile.model || llmProviderDefinition(profile.provider).label}</option>)}</select></label>
								<button type="button" onClick={addLLMProfile}><Plus size={14} />新增配置</button>
								<button type="button" className="danger" onClick={removeLLMProfile} disabled={llmProfiles.length <= 1}><Trash2 size={14} />删除</button>
							</div>
							<label><span>配置名称</span><input value={profileName} onChange={(event) => { setProfileName(event.target.value); patchSelectedLLMProfile({ name: event.target.value }); }} placeholder="例如 DeepSeek 日常 / GPT-5.6 Sol 深度分析" /></label>
							<div className="settings-grid two-columns">
								<label><span>服务商</span><select value={provider} onChange={(event) => updateProvider(event.target.value)}>{llmProviders.map((item) => <option value={item.id} key={item.id}>{item.label}</option>)}</select></label>
								<label><span>接口协议</span><select value={apiMode} onChange={(event) => { setAPIMode(event.target.value); patchSelectedLLMProfile({ api_mode: event.target.value }); setTestState('idle'); setTestResult(null); }}><option value="chat_completions">Chat Completions</option><option value="codex_responses">Responses API</option><option value="anthropic_messages">Anthropic Messages</option></select></label>
							</div>
							<SecretField label="模型 API Key" secretKey="llm_api_key" status={selectedLLMProfile?.api_key} value={profileKeyValues[activeLLMProfileID] || ''} clearing={clearProfileKeys.has(activeLLMProfileID)} onChange={updateSecret} onBlur={(_key, value) => fetchModelsAfterAPIKeyInput(value)} onClear={toggleClear} hint="每套配置独立安全保存；切换配置不会覆盖其他密钥" />
							<div className="settings-grid two-columns">
								<div className="model-field">
									<span className="model-field-heading"><span>模型</span>{modelListState === 'success' && <button type="button" onClick={() => setManualModel((current) => !current)}>{manualModel ? '使用下拉' : '手动输入'}</button>}</span>
									<span className="model-picker-row">
										{modelListState === 'success' && !manualModel ? <select value={model} onChange={(event) => { if (event.target.value === manualModelOption) setManualModel(true); else updateModel(event.target.value); }}><option value="">请选择模型</option>{selectableModels.map((option) => <option value={option.id} key={option.id}>{modelOptionLabel(option)}</option>)}<option value={manualModelOption}>手动输入其他模型…</option></select> : <input value={model} onChange={(event) => updateModel(event.target.value)} placeholder="例如 gpt-5.5 或 deepseek-chat" />}
										<button type="button" className="model-refresh-button" onClick={() => void fetchModels()} disabled={!config || state === 'saving' || modelListState === 'loading'}>{modelListState === 'loading' ? <LoaderCircle className="spin" size={14} /> : <RefreshCw size={14} />}{modelListState === 'success' ? '刷新' : '获取模型'}</button>
									</span>
									<small className={`model-list-message ${modelListState}`}>{modelListMessage || '从当前 Base URL 的 /models 接口读取，也可继续手动输入。'}</small>
								</div>
								<label><span>API Base URL</span><input value={baseURL} onChange={(event) => updateBaseURL(event.target.value)} placeholder="https://api.example.com/v1" /></label>
							</div>
							<div className={`llm-connection-test ${testState}`}>
								<div><PlugZap size={17} /><span><strong>{testState === 'success' ? 'Hermes 模型连接可用' : testState === 'error' ? '连接测试未通过' : testState === 'testing' ? 'Hermes 正在请求模型' : 'Hermes 真实模型探针'}</strong><small>{testResult ? `Hermes · ${testResult.model} · ${testResult.api_mode} · ${testResult.latency_ms}ms · ${testResult.response}` : '保存当前配置后由 Hermes 发送最小提示词，并验证模型确实返回内容。'}</small></span></div>
								<button type="button" onClick={testConnection} disabled={!config || state === 'saving' || testState === 'testing'}>{testState === 'testing' ? <LoaderCircle className="spin" size={15} /> : <PlugZap size={15} />}保存并测试连接</button>
							</div>
						</section>

						<section className="settings-section">
							<div className="settings-section-title"><Clock3 size={18} /><div><h3>大V复盘自动化</h3><p>按平台管理多套采集配置，每个大V订阅可以绑定其中一套。</p></div></div>
							<div className="review-profile-tabs">{(['xueqiu', 'taoguba', 'wechat'] as ReviewSource[]).map((source) => <button type="button" className={reviewSource === source ? 'active' : ''} onClick={() => setReviewSource(source)} key={source}>{reviewSourceLabel(source)}<em>{reviewProfiles.filter((profile) => profile.source === source).length}</em></button>)}</div>
							{reviewSource === 'wechat' && <WechatServiceCard status={wechatServiceStatus} loginURL={wechatLoginURL} onLogin={() => void openWechatLogin()} onCloseLogin={() => { setWechatLoginURL(''); setWechatLoginBaseline(''); }} />}
							<div className="review-profile-list">
								{reviewProfiles.filter((profile) => profile.source === reviewSource).map((profile) => <ReviewProfileCard profile={profile} browserAuthStatus={browserAuthStatuses[profile.id]} openingBrowser={openingBrowserProfile === profile.id} onOpenBrowserLogin={() => void openReviewSourceLogin(profile)} onChange={(patch) => updateReviewProfile(profile.id, patch)} onRemove={() => removeReviewProfile(profile.id)} canRemove={profile.source !== 'wechat' || reviewProfiles.filter((item) => item.source === 'wechat').length > 1} key={profile.id} />)}
								{!reviewProfiles.some((profile) => profile.source === reviewSource) && <div className="review-profile-empty"><strong>还没有{reviewSourceLabel(reviewSource)}配置</strong><span>可以添加多套配置，并让不同大V订阅使用不同登录态或同步时段。</span></div>}
							</div>
							{reviewSource !== 'wechat' && <button type="button" className="review-profile-add" onClick={() => addReviewProfile(reviewSource)}><Plus size={14} />添加{reviewSourceLabel(reviewSource)}配置</button>}
							<p className="settings-field-note">雪球和淘股吧通过内置浏览器保存独立登录态；微信公众号扫码仅用于解析已知文章链接，历史文章列表接口已停用，暂不提供自动订阅。所有登录凭据仅保存在本机。</p>
						</section>

						<section className="settings-section">
							<div className="settings-section-title"><Database size={18} /><div><h3>行情与内容数据源</h3><p>现有公共接口继续直接使用；以下凭据为增强数据与后续接入准备。</p></div></div>
							<div className="public-source-status">
								<span><CheckCircle2 size={14} />东方财富：公共行情已接入</span>
								<span><CheckCircle2 size={14} />新浪财经：公共行情已接入</span>
								<span><CheckCircle2 size={14} />财联社：公开资讯已接入</span>
							</div>
							<SecretField label="Tushare Pro Token" secretKey="tushare_token" status={settings?.credentials.tushare_token} value={secrets.tushare_token} clearing={clearSecrets.has('tushare_token')} onChange={updateSecret} onClear={toggleClear} hint="预留：基础数据、指数和日线增强" />
							<SecretField label="同花顺 Cookie / Token" secretKey="ths_cookie" status={settings?.credentials.ths_cookie} value={secrets.ths_cookie} clearing={clearSecrets.has('ths_cookie')} onChange={updateSecret} onClear={toggleClear} hint="预留：涨停原因与题材催化数据" />
							<SecretField label="东方财富 Cookie" secretKey="eastmoney_cookie" status={settings?.credentials.eastmoney_cookie} value={secrets.eastmoney_cookie} clearing={clearSecrets.has('eastmoney_cookie')} onChange={updateSecret} onClear={toggleClear} hint="当前公共行情不需要，预留登录态接口" />
						</section>

						<AppUpdatePanel />

						<footer className="settings-footer">
							<div className={`settings-message ${state}`}>{state === 'saved' && <CheckCircle2 size={15} />}{state === 'error' && <KeyRound size={15} />}<span>{message || '留空的模型密钥会保留 Hermes .env 中的现有值。'}</span></div>
							<button type="button" onClick={onClose}>取消</button>
							<button type="submit" className="settings-save" disabled={!config || state === 'saving' || testState === 'testing'}>{state === 'saving' ? <LoaderCircle className="spin" size={16} /> : <Save size={16} />}保存设置</button>
						</footer>
					</form>
				)}
			</aside>
		</div>
	);
}

function WechatServiceCard({ status, loginURL, onLogin, onCloseLogin }: { status: WechatServiceStatus; loginURL: string; onLogin: () => void; onCloseLogin: () => void }) {
	const ready = status.authenticated;
	const statusLabel = ready ? '已登录' : status.state === 'expired' ? '登录已过期' : status.state === 'starting' ? '正在启动' : status.available ? '等待扫码' : '服务异常';
	const expiresAt = status.expires_at ? new Date(status.expires_at).toLocaleString('zh-CN', { hour12: false }) : '';
	return <div className={`wechat-service-panel ${status.state}`}>
		<div className="wechat-service-summary">
			<div className="wechat-service-icon">{status.available ? ready ? <CheckCircle2 size={20} /> : <QrCode size={20} /> : <CircleAlert size={20} />}</div>
			<span><strong>内置微信公众号服务<em>{statusLabel}</em></strong><small>{status.message}</small>{ready && expiresAt && <i>登录有效期至 {expiresAt}</i>}</span>
			<button type="button" onClick={onLogin} disabled={!status.available || status.state === 'starting'}>{loginURL ? <LoaderCircle className="spin" size={14} /> : <QrCode size={14} />}{loginURL ? '等待扫码' : ready ? '重新扫码' : '扫码登录'}</button>
		</div>
		{loginURL && <div className="wechat-login-embedded">
			<header><span><QrCode size={14} />请使用微信扫码并在手机上确认</span><button type="button" onClick={onCloseLogin}>关闭扫码</button></header>
			<webview src={loginURL} partition="persist:a-stock-wechat-login" title="微信公众号扫码登录" />
		</div>}
		<p><Server size={12} />服务随 App 自动启动，扫码登录仅用于解析你粘贴的具体文章链接；登录凭据只保存在本机用户数据目录。</p>
	</div>;
}

async function getWechatServiceStatus(): Promise<WechatServiceStatus> {
	if (!window.aStock?.getWechatServiceStatus) {
		return { available: false, configured: false, authenticated: false, state: 'error', message: '内置微信公众号服务仅在桌面应用中可用' };
	}
	try {
		return await window.aStock.getWechatServiceStatus();
	} catch (error) {
		return { available: false, configured: false, authenticated: false, state: 'error', message: error instanceof Error ? error.message : '读取微信公众号服务状态失败' };
	}
}

function ReviewProfileCard({ profile, browserAuthStatus, openingBrowser, onOpenBrowserLogin, onChange, onRemove, canRemove }: { profile: ReviewProfileDraft; browserAuthStatus?: BrowserAuthStatus; openingBrowser: boolean; onOpenBrowserLogin: () => void; onChange: (patch: Partial<ReviewProfileDraft>) => void; onRemove: () => void; canRemove: boolean }) {
	const sourceLabel = reviewSourceLabel(profile.source as ReviewSource);
	const isWechat = profile.source === 'wechat';
	const usesBrowserLogin = profile.source === 'xueqiu' || profile.source === 'taoguba';
	return <article className={`review-profile-card ${profile.enabled ? '' : 'disabled'}`}>
		<header><div><span className={`review-profile-source ${profile.source}`}>{reviewSourceLabel(profile.source as ReviewSource)}</span><strong>{profile.name || '未命名配置'}</strong></div><div><button type="button" className={profile.enabled ? 'profile-enabled active' : 'profile-enabled'} onClick={() => onChange({ enabled: !profile.enabled })}>{profile.enabled ? '已启用' : '已停用'}</button>{canRemove && <button type="button" className="profile-remove" title="删除配置" onClick={onRemove}><Trash2 size={14} /></button>}</div></header>
		<div className="review-profile-grid">
			<label><span>配置名称</span><input value={profile.name} onChange={(event) => onChange({ name: event.target.value })} placeholder={`${reviewSourceLabel(profile.source as ReviewSource)}配置名称`} /></label>
			{!isWechat && <label><span>每日同步时间</span><select value={profile.sync_hour} onChange={(event) => onChange({ sync_hour: Number(event.target.value) })}>{Array.from({ length: 24 }, (_, hour) => <option value={hour} key={hour}>{String(hour).padStart(2, '0')}:00</option>)}</select></label>}
		</div>
		{!isWechat && <label><span>平台地址</span><input value={profile.base_url} onChange={(event) => onChange({ base_url: event.target.value })} placeholder={profile.source === 'xueqiu' ? 'https://xueqiu.com' : 'https://www.tgb.cn'} /></label>}
		{usesBrowserLogin ? <div className={`profile-browser-auth ${browserAuthStatus?.configured ? 'ready' : ''}`}><Globe2 size={18} /><span><strong>{browserAuthStatus?.configured ? `${sourceLabel}浏览器已登录` : `使用内置浏览器登录${sourceLabel}`}</strong><small>{browserAuthStatus?.message || '打开后由你亲自完成登录或安全验证，然后点击“我已完成登录”保存登录态。'}</small>{browserAuthStatus?.updated_at && <em>最近保存 {new Date(browserAuthStatus.updated_at).toLocaleString('zh-CN', { hour12: false })}</em>}</span><button type="button" onClick={onOpenBrowserLogin} disabled={openingBrowser}>{openingBrowser ? <LoaderCircle className="spin" size={14} /> : <LogIn size={14} />}{openingBrowser ? '等待确认登录' : browserAuthStatus?.configured ? '重新登录' : '打开登录窗口'}</button></div> : isWechat ? <div className="profile-builtin-service"><Server size={16} /><span><strong>内置文章解析 API</strong><small>无需填写服务地址或 Token；扫码登录仅用于解析已知文章链接，不会读取公众号历史文章列表。</small></span></div> : null}
		{!isWechat && <label className="profile-ai-toggle"><span>抓取后自动 AI 提炼</span><button type="button" className={profile.auto_analyze ? 'active' : ''} onClick={() => onChange({ auto_analyze: !profile.auto_analyze })}>{profile.auto_analyze ? '已开启' : '已关闭'}</button></label>}
	</article>;
}

function toProfileDrafts(profiles: ReviewAutomationProfile[]): ReviewProfileDraft[] {
	const drafts = profiles.map((profile) => ({ ...profile, base_url: profile.source === 'wechat' ? '' : profile.base_url, credential_value: '', clear_credential: profile.source === 'wechat' }));
	if (!drafts.some((profile) => profile.source === 'wechat')) {
		drafts.unshift({ id: 'wechat-default', source: 'wechat', name: '微信公众号默认配置', base_url: '', credential: { configured: false }, credential_value: '', clear_credential: true, sync_hour: 7, auto_analyze: true, enabled: true });
	}
	return drafts;
}

function normalizeLLMProfiles(settings: AppSettings): LLMProfile[] {
	if (settings.llm_profiles?.length) return settings.llm_profiles;
	return [{ id: 'llm-default', name: settings.llm.model || llmProviderDefinition(settings.llm.provider).label, provider: settings.llm.provider, base_url: settings.llm.base_url, model: settings.llm.model, api_mode: settings.llm.api_mode, api_key: settings.llm.api_key }];
}

function newProfileDraft(source: ReviewSource): ReviewProfileDraft {
	const suffix = `${Date.now()}-${Math.random().toString(16).slice(2)}`;
	return { id: `${source}-${suffix}`, source, name: `${reviewSourceLabel(source)}配置`, base_url: source === 'wechat' ? '' : source === 'xueqiu' ? 'https://xueqiu.com' : 'https://www.tgb.cn', credential: { configured: false }, credential_value: '', clear_credential: false, sync_hour: 7, auto_analyze: true, enabled: true };
}

function reviewSourceLabel(source: ReviewSource) { return source === 'wechat' ? '微信公众号' : source === 'xueqiu' ? '雪球' : '淘股吧'; }

function modelOptionLabel(option: LLMModelOption) {
	const detail = option.display_name || option.owned_by;
	return detail && detail !== option.id ? `${option.id} · ${detail}` : option.id;
}

function SecretField({ label, secretKey, status, value, clearing, onChange, onBlur, onClear, hint }: {
	label: string;
	secretKey: SecretKey;
	status?: SecretSettingStatus;
	value: string;
	clearing: boolean;
	onChange: (key: SecretKey, value: string) => void;
	onBlur?: (key: SecretKey, value: string) => void;
	onClear: (key: SecretKey) => void;
	hint?: string;
}) {
	return (
		<label className={`secret-field ${clearing ? 'clearing' : ''}`}>
			<span className="secret-field-heading"><span>{label}{hint && <small>{hint}</small>}</span>{status?.configured && <em>{clearing ? '等待清除' : `已配置 ${status.masked || ''}`}</em>}</span>
			<span className="secret-input-row">
				<KeyRound size={15} />
				<input type="password" autoComplete="new-password" value={value} disabled={clearing} onChange={(event) => onChange(secretKey, event.target.value)} onBlur={(event) => onBlur?.(secretKey, event.target.value)} placeholder={status?.configured ? '输入新值可覆盖，留空保持不变' : '输入凭据'} />
				{status?.configured && <button type="button" onClick={() => onClear(secretKey)} title={clearing ? '取消清除' : '清除已保存凭据'}><Trash2 size={14} />{clearing ? '撤销' : '清除'}</button>}
			</span>
		</label>
	);
}
