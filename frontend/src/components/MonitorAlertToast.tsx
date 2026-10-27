import { useCallback, useEffect, useRef, useState } from 'react';
import { AlertTriangle, BellRing, X } from 'lucide-react';
import { fetchMonitorEvents, type MonitorEvent } from '../lib/monitor';
import type { BackendConfig } from '../lib/backend';

// 全局触发提醒条：任何页面都能看到监控刚命中了什么。
// 它不负责落库——事件在触发那一刻就已经写进 monitor_events 了，这里只是
// 把「已经发生的事」搬到用户眼前，所以关闭或超时都不会丢记录。
//
// 两条纪律：
// 1. 首次只建基线不补弹历史，否则一进页面就会被几十条旧事件糊满。
// 2. 轮询失败静默吞掉。提醒条自己不该成为新的打扰源。

const POLL_MS = 5000;
const HOLD_MS = 5000;
const EXIT_MS = 240;

// 触发文案里的数值才是要一眼拿到的东西（涨了多少、阈值多少），逐个加粗。
// 只对纯数字/百分比生效，其余文字原样输出。
function emphasiseNumbers(text: string) {
	return text
		.split(/(\d+(?:\.\d+)?%?)/g)
		.map((part, index) => (/^\d/.test(part) ? <b key={index}>{part}</b> : part));
}

export function MonitorAlertToast({ config }: {
	config: BackendConfig | null;
	onOpenMonitor: () => void;
}) {
	const contextKey = JSON.stringify([config?.backendUrl.replace(/\/$/, '') ?? '', config?.token ?? '']);
	return <MonitorAlertQueue key={contextKey} config={config} />;
}

function MonitorAlertQueue({ config }: { config: BackendConfig | null }) {
	const [queue, setQueue] = useState<MonitorEvent[]>([]);
	const [leaving, setLeaving] = useState(false);
	const [paused, setPaused] = useState(false);
	const sinceRef = useRef(0);
	const primedRef = useRef(false);
	const seenRef = useRef<Set<number>>(new Set());

	const current = queue[0] ?? null;
	const dismiss = useCallback(() => setLeaving(true), []);

	// 退场动画放完再摘队首，队列里还有就接着弹下一条。
	useEffect(() => {
		if (!leaving) return;
		const timer = window.setTimeout(() => {
			setQueue((list) => list.slice(1));
			setLeaving(false);
		}, EXIT_MS);
		return () => window.clearTimeout(timer);
	}, [leaving]);

	// 悬停 5 秒无响应自动收起；鼠标停在上面时不计时，移开重新数 5 秒。
	useEffect(() => {
		if (!current || leaving || paused) return;
		const timer = window.setTimeout(dismiss, HOLD_MS);
		return () => window.clearTimeout(timer);
	}, [current?.id, leaving, paused, dismiss, current]);

	useEffect(() => {
		if (!config) return;
		let alive = true;
		let inFlight = false;
		const tick = async () => {
			// 每次读当前状态，不缓存挂载时的快照：页面刚加载那一瞬浏览器可能
			// 还报告 hidden，缓存下来就再也等不到 visibilitychange 纠正。
			if (!alive || document.hidden || inFlight) return;
			inFlight = true;
			try {
				if (!primedRef.current) {
					const payload = await fetchMonitorEvents(config, 'limit=1');
					if (!alive) return;
					// 第一趟只记住「读到哪了」，历史事件不补弹。
					primedRef.current = true;
					sinceRef.current = Math.max(sinceRef.current, payload.since_id || 0);
					return;
				}
				// 按ID正序逐批补齐，游标不能越过尚未读取的事件。
				for (let page = 0; page < 5 && alive; page++) {
					const payload = await fetchMonitorEvents(config, `since_id=${sinceRef.current}&limit=100&order=asc`);
					if (!alive) return;
					const items = payload.items ?? [];
					if (!items.length) break;
					sinceRef.current = Math.max(sinceRef.current, ...items.map((item) => item.id));
					const fresh = items.filter((item) => item.severity !== 'info' && !seenRef.current.has(item.id));
					for (const item of fresh) seenRef.current.add(item.id);
					if (fresh.length) setQueue((list) => [...list, ...fresh.slice().sort((a, b) => a.id - b.id)]);
					if (items.length < 100) break;
				}
			} catch {
				// 轮询失败不打扰用户。
			} finally {
				inFlight = false;
			}
		};
		void tick();
		const timer = window.setInterval(tick, POLL_MS);
		// 切回前台立刻补一次，不用等满一个轮询周期。
		const onVisibility = () => { if (!document.hidden) void tick(); };
		document.addEventListener('visibilitychange', onVisibility);
		return () => {
			alive = false;
			window.clearInterval(timer);
			document.removeEventListener('visibilitychange', onVisibility);
		};
	}, [config?.backendUrl, config?.token]);

	if (!current) return null;
	const critical = current.severity === 'critical';
	const title = current.name || current.symbol || current.rule_name;
	const subject = current.name?.trim() ? `${current.name}（${current.symbol}）` : current.symbol;
	const message = current.symbol
		? current.message.replace(`${current.rule_name} · ${subject}：`, `${current.rule_name} · `)
		: current.message;
	return (
		<div
			className={`monitor-alert-toast${critical ? ' critical' : ''}${leaving ? ' leaving' : ''}`}
			role="alert"
			aria-live="assertive"
			onMouseEnter={() => setPaused(true)}
			onMouseLeave={() => setPaused(false)}
		>
			<span className="monitor-alert-icon" aria-hidden="true">
				{critical ? <AlertTriangle size={15} /> : <BellRing size={15} />}
			</span>
			<div className="monitor-alert-body">
				<strong>{title}{current.symbol && current.name ? <em>{current.symbol}</em> : null}</strong>
				<span>{emphasiseNumbers(message)}</span>
			</div>
			<div className="monitor-alert-actions">
				<button type="button" className="monitor-alert-close" onClick={dismiss} aria-label="关闭提醒">
					<X size={14} aria-hidden="true" />
				</button>
			</div>
		</div>
	);
}
