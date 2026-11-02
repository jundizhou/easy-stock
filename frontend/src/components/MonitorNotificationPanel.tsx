import { useEffect, useRef, useState } from 'react';
import { LoaderCircle } from 'lucide-react';
import { requestJSON, type BackendConfig, type NotificationSettings } from '../lib/backend';
import { fetchMonitorSettings, saveMonitorSettings, type MonitorPreferences } from '../lib/monitor';
import { notificationSettingsChangedEvent } from '../lib/notifications';

export function MonitorNotificationPanel({ config, refreshKey }: {
	config: BackendConfig | null; refreshKey: number;
}) {
	const [preferences, setPreferences] = useState<MonitorPreferences | null>(null);
	const [robots, setRobots] = useState<NotificationSettings | null>(null);
	const [revision, setRevision] = useState(0);
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState('');
	const generation = useRef(0);
	useEffect(() => {
		const changed = () => setRevision((value) => value + 1);
		window.addEventListener(notificationSettingsChangedEvent, changed);
		return () => window.removeEventListener(notificationSettingsChangedEvent, changed);
	}, []);
	useEffect(() => {
		const current = ++generation.current;
		setPreferences(null); setRobots(null); setError(''); setBusy(false);
		if (config) void Promise.all([
			fetchMonitorSettings(config),
			requestJSON<{ data: NotificationSettings }>(config, '/api/v1/settings/notifications'),
		]).then(([prefs, notifications]) => {
			if (current !== generation.current) return;
			if (!prefs.data?.settings || !notifications.data?.feishu || !notifications.data?.dingtalk) throw new Error('通知设置未读取成功');
			setPreferences(prefs.data); setRobots(notifications.data);
		}).catch(() => { if (current === generation.current) setError('通知设置未读取成功，请重试'); });
		return () => { generation.current++; };
	}, [config?.backendUrl, config?.token, refreshKey, revision]);
	const ready = (['feishu', 'dingtalk'] as const).some((key) => robots?.[key].enabled && robots[key].webhook.configured);
	const toggle = async () => {
		if (!config || !preferences || busy) return;
		const current = generation.current;
		setBusy(true); setError('');
		try {
			const { data } = await saveMonitorSettings(config, { notify_enabled: !preferences.notify_enabled });
			if (current === generation.current) setPreferences(data);
		} catch { if (current === generation.current) setError('通知开关保存失败，请重试'); }
		finally { if (current === generation.current) setBusy(false); }
	};
	const title = error || (!config ? '连接后端后可设置通知' : !preferences ? '正在读取通知设置' : !ready ? '请在系统设置中配置并启用钉钉或飞书机器人' : '使用系统设置中已启用的机器人发送监控提醒');
	return <div className="monitor-notification-control">
		<button type="button" className="monitor-notification-toggle" role="switch" aria-label="钉钉/飞书机器人通知" aria-checked={Boolean(preferences?.notify_enabled)} title={title} disabled={!preferences || busy || (!preferences.notify_enabled && !ready)} onClick={() => void toggle()}>
			<span>钉钉/飞书机器人通知</span>
			<span className="monitor-notification-switch" aria-hidden="true"><span>{busy && <LoaderCircle size={12} className="spin" />}</span></span>
		</button>
		{error && <span role="alert">{error}</span>}
	</div>;
}
