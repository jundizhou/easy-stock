import { Bell, CheckCircle2, CircleAlert, Eye, EyeOff, LoaderCircle, Save, Send, Trash2 } from 'lucide-react';
import { useEffect, useState } from 'react';
import { BackendConfig, NotificationSettings, requestJSON } from '../lib/backend';
import { emptyNotifications, notificationDraft, notificationUpdate, NotificationChannel, NotificationDraft } from '../lib/notifications';
import { SettingsSection } from './SettingsSection';

const channelDefinitions = {
	feishu: { label: '飞书', placeholder: 'https://open.feishu.cn/open-apis/bot/v2/hook/…', help: '在飞书群设置中添加自定义机器人，复制 Webhook；启用签名校验后填写签名密钥。' },
	dingtalk: { label: '钉钉', placeholder: 'https://oapi.dingtalk.com/robot/send?access_token=…', help: '在钉钉群机器人管理中添加自定义机器人，复制 Webhook；使用加签时填写 SEC 开头的密钥。' },
};

export function NotificationSettingsPanel({ config }: { config: BackendConfig | null }) {
	const [drafts, setDrafts] = useState<Record<NotificationChannel, NotificationDraft>>(() => {
		const values = emptyNotifications();
		return { feishu: notificationDraft(values.feishu), dingtalk: notificationDraft(values.dingtalk) };
	});
	const [events, setEvents] = useState(emptyNotifications().events);
	const [state, setState] = useState<'loading' | 'ready' | 'saving' | 'error'>('loading');
	const [loaded, setLoaded] = useState(false);
	const [message, setMessage] = useState('');
	const [testing, setTesting] = useState<NotificationChannel | null>(null);
	const [testResults, setTestResults] = useState<Partial<Record<NotificationChannel, { ok: boolean; message: string }>>>({});

	const loadValues = (values: NotificationSettings) => {
		setDrafts({ feishu: notificationDraft(values.feishu), dingtalk: notificationDraft(values.dingtalk) });
		setEvents(values.events);
	};

	useEffect(() => {
		if (!config) return;
		let cancelled = false;
		setState('loading');
		setLoaded(false);
		requestJSON<{ data: NotificationSettings }>(config, '/api/v1/settings/notifications')
			.then(({ data }) => { if (!cancelled) { loadValues(data); setState('ready'); setLoaded(true); } })
			.catch((error) => { if (!cancelled) { setState('error'); setMessage(error instanceof Error ? error.message : '读取通知配置失败'); } });
		return () => { cancelled = true; };
	}, [config]);

	const busy = state === 'loading' || state === 'saving' || testing !== null;
	const update = (channel: NotificationChannel, patch: Partial<NotificationDraft>) => {
		setDrafts((current) => ({ ...current, [channel]: { ...current[channel], ...patch } }));
		setMessage('');
		setTestResults((current) => ({ ...current, [channel]: undefined }));
	};
	const save = async () => {
		if (!config) return;
		setState('saving');
		setMessage('');
		try {
			const { data } = await requestJSON<{ data: NotificationSettings }>(config, '/api/v1/settings/notifications', {
				method: 'PUT', headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ feishu: notificationUpdate(drafts.feishu), dingtalk: notificationUpdate(drafts.dingtalk), events }),
			});
			loadValues(data);
			setState('ready');
			setMessage('通知配置已保存');
		} catch (error) {
			setState('error');
			setMessage(error instanceof Error ? error.message : '保存通知配置失败');
		}
	};
	const test = async (channel: NotificationChannel) => {
		if (!config) return;
		setTesting(channel);
		try {
			const { data } = await requestJSON<{ data: { message: string } }>(config, '/api/v1/settings/notifications/test', {
				method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ channel, config: notificationUpdate(drafts[channel]) }),
			});
			setTestResults((current) => ({ ...current, [channel]: { ok: true, message: data.message } }));
		} catch (error) {
			setTestResults((current) => ({ ...current, [channel]: { ok: false, message: error instanceof Error ? error.message : '测试通知发送失败' } }));
		} finally {
			setTesting(null);
		}
	};

	return <SettingsSection title="消息通知" description="通过飞书、钉钉群机器人接收个股研究和持仓巡检结果。" icon={<Bell size={18} />}>
		{state === 'loading' && <div className="agent-settings-loading"><LoaderCircle className="spin" size={18} />读取通知配置</div>}
		<fieldset className="notification-settings-fields" disabled={!loaded || busy}>
			{(['feishu', 'dingtalk'] as const).map((channel) => {
				const definition = channelDefinitions[channel];
				const draft = drafts[channel];
				const result = testResults[channel];
				return <article className="notification-channel-card" key={channel}>
					<header><div><strong>{definition.label}机器人</strong><small>{draft.webhook.configured && !draft.clear_webhook ? '已配置 Webhook' : '尚未配置 Webhook'}</small></div><label className="notification-toggle"><input type="checkbox" checked={draft.enabled} onChange={(event) => update(channel, { enabled: event.target.checked })} /><span>启用通知</span></label></header>
					<p>{definition.help}</p>
					<NotificationSecretField label={`${definition.label} Webhook`} value={draft.webhook_value} configured={draft.webhook.configured} clearing={draft.clear_webhook} placeholder={definition.placeholder} onChange={(value) => update(channel, { webhook_value: value, clear_webhook: false })} onClear={() => update(channel, { clear_webhook: !draft.clear_webhook, webhook_value: '', enabled: false })} />
					<NotificationSecretField label={`${definition.label}签名密钥（可选）`} value={draft.secret_value} configured={draft.secret.configured} clearing={draft.clear_secret} placeholder="未开启签名校验时留空" onChange={(value) => update(channel, { secret_value: value, clear_secret: false })} onClear={() => update(channel, { clear_secret: !draft.clear_secret, secret_value: '' })} />
					<label><span>安全关键词（可选）</span><input value={draft.keyword} maxLength={100} placeholder="与机器人安全设置中的关键词一致" onChange={(event) => update(channel, { keyword: event.target.value })} /></label>
					<div className="notification-test-row"><span className={result?.ok ? 'success' : 'error'} role={result?.ok === false ? 'alert' : 'status'}>{result ? <>{result.ok ? <CheckCircle2 size={14} /> : <CircleAlert size={14} />}{result.message}</> : '测试会向群发送一条消息，不会保存配置。'}</span><button type="button" onClick={() => void test(channel)} disabled={!draft.webhook_value.trim() && (!draft.webhook.configured || draft.clear_webhook)}>{testing === channel ? <LoaderCircle className="spin" size={14} /> : <Send size={14} />}测试发送</button></div>
				</article>;
			})}
			<div className="notification-events"><strong>通知事件</strong><label className="notification-toggle"><input type="checkbox" checked={events.stock_research} onChange={(event) => setEvents({ ...events, stock_research: event.target.checked })} /><span>个股 AI 研究完成</span></label><label className="notification-toggle"><input type="checkbox" checked={events.portfolio_inspection} onChange={(event) => setEvents({ ...events, portfolio_inspection: event.target.checked })} /><span>持仓 AI 巡检完成</span></label><label className="notification-toggle"><input type="checkbox" checked={events.task_failed} onChange={(event) => setEvents({ ...events, task_failed: event.target.checked })} /><span>同时提醒所选任务失败或未完成</span></label></div>
		</fieldset>
		<p className="settings-field-note">Webhook 和签名密钥仅保存在本机，留空保留原值。通知发送报告摘要；量化速览和手动取消的任务不发送。应用运行期间生效。</p>
		<div className={`agent-settings-footer ${state === 'error' ? 'error' : ''}`}><span role={state === 'error' ? 'alert' : 'status'}>{message || '通知配置单独保存，两个渠道可同时启用。'}</span><button type="button" onClick={() => void save()} disabled={!config || !loaded || busy}>{state === 'saving' ? <LoaderCircle className="spin" size={15} /> : <Save size={15} />}保存通知配置</button></div>
	</SettingsSection>;
}

function NotificationSecretField({ label, value, configured, clearing, placeholder, onChange, onClear }: { label: string; value: string; configured: boolean; clearing: boolean; placeholder: string; onChange: (value: string) => void; onClear: () => void }) {
	const [visible, setVisible] = useState(false);
	return <label><span>{label}</span><div className="notification-secret-input"><input aria-label={label} type={visible ? 'text' : 'password'} autoComplete="off" spellCheck={false} value={value} placeholder={clearing ? '保存后清除' : configured ? '已保存，留空保留；输入新值替换' : placeholder} onChange={(event) => onChange(event.target.value)} /><button type="button" aria-label={`${visible ? '隐藏' : '显示'}${label}`} disabled={!value} onClick={() => setVisible(!visible)}>{visible ? <EyeOff size={15} /> : <Eye size={15} />}</button>{configured && <button type="button" aria-label={`${clearing ? '取消清除' : '清除'}${label}`} aria-pressed={clearing} onClick={onClear}><Trash2 size={15} /></button>}</div></label>;
}
