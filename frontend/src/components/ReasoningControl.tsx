import { useEffect, useRef, useState } from 'react';
import { BackendConfig, HermesAgentSettings, requestJSON } from '../lib/backend';

/** Both analysis and chat consume the backend's model/route capability policy. */
export function ReasoningControl({ config, refreshKey, disabled, onSaved, label = '选择思考等级' }: {
	config: BackendConfig | null;
	refreshKey: string;
	disabled?: boolean;
	onSaved?: () => void;
	label?: string;
}) {
	const [snapshot, setSnapshot] = useState<{ key: string; config: BackendConfig; data: HermesAgentSettings } | null>(null);
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState('');
	const sequence = useRef(0);
	useEffect(() => {
		const id = ++sequence.current;
		setSnapshot(null);
		setError('');
		if (!config) { setBusy(false); return; }
		setBusy(true);
		requestJSON<{ data: HermesAgentSettings }>(config, '/api/v1/settings/agent')
			.then(({ data }) => { if (sequence.current === id) setSnapshot({ key: refreshKey, config, data }); })
			.catch(() => { if (sequence.current === id) setError('无法读取模型思考能力，请刷新后重试'); })
			.finally(() => { if (sequence.current === id) setBusy(false); });
		return () => { ++sequence.current; };
	}, [config, refreshKey]);
	const data = snapshot?.key === refreshKey && snapshot.config === config ? snapshot.data : null;
	const options = data?.reasoning?.options || [];
	const selected = options.some((option) => option.value === data?.reasoning_effort) ? data!.reasoning_effort : '';
	const save = async (value: string) => {
		if (!config || busy || disabled || !data || !options.some((option) => option.value === value)) return;
		const id = ++sequence.current;
		setBusy(true); setError('');
		try {
			const response = await requestJSON<{ data: HermesAgentSettings }>(config, '/api/v1/settings/agent', {
				method: 'PUT', headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ reasoning_effort: value, reasoning_context: data.reasoning_context }),
			});
			if (sequence.current !== id) return;
			setSnapshot({ key: refreshKey, config, data: response.data });
			onSaved?.();
		} catch (cause) {
			if (sequence.current === id) setError(cause instanceof Error ? cause.message : '保存思考设置失败');
		} finally { if (sequence.current === id) setBusy(false); }
	};
	return <label className="reasoning-control" title={error || data?.reasoning?.note || '尚未确认当前模型的思考能力'}>
		<span>思考</span>
		<select aria-label={label} value={selected} disabled={disabled || busy || !selected || options.length < 2} onChange={(event) => void save(event.target.value)}>
			{!selected && <option value="">{busy ? '读取能力中…' : '暂不支持调节'}</option>}
			{options.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
		</select>
		{error && <small role="alert">{error}</small>}
	</label>;
}
