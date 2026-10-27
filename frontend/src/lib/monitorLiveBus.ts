import { useCallback, useSyncExternalStore } from 'react';
import type { BackendConfig } from './backend';

export type LiveQuote = { price: number; changePercent: number; ts: number; stale?: boolean };
export type LiveStreamStatus = { connection: 'closed' | 'connecting' | 'open' | 'reconnecting'; stale: boolean; sessionOpen: boolean; source: string; warning: string };
type Listener = () => void;

// 断线或过期时优先采用本次 HTTP 回退；没有回退值才保留上次推送值。
export function selectMonitorQuoteValue(quote: LiveQuote | undefined, status: LiveStreamStatus, kind: 'price' | 'change', fallback?: number | null) {
	const valid = (value?: number | null): value is number => typeof value === 'number' && Number.isFinite(value) && (kind !== 'price' || value > 0);
	const live = quote && (kind === 'price' ? quote.price : quote.changePercent);
	if (status.connection === 'open' && !status.stale && !quote?.stale && valid(live)) return live;
	if (valid(fallback)) return fallback;
	return valid(live) ? live : undefined;
}

function symbolKey(symbol: string) {
	return symbol.trim().toUpperCase().replace(/^(SH|SZ|BJ)/, '').replace(/\.(SH|SZ|BJ)$/, '');
}

// 一条连接供监控工作区共享；只通知数字改变的标的，整表不订阅价格状态。
export function createMonitorLiveBus() {
	const quotes = new Map<string, LiveQuote>();
	const listeners = new Map<string, Set<Listener>>();
	const allListeners = new Set<Listener>();
	const statusListeners = new Set<Listener>();
	let status: LiveStreamStatus = { connection: 'closed', stale: false, sessionOpen: false, source: '', warning: '' };
	let lastQuoteAt = 0;
	let expiryTimer: ReturnType<typeof setInterval> | undefined;
	const updateStatus = (patch: Partial<LiveStreamStatus>) => {
		const next = { ...status, ...patch };
		if (JSON.stringify(next) === JSON.stringify(status)) return;
		status = next;
		for (const listener of statusListeners) listener();
	};
	let version = 0;
	const notifyAll = () => { version++; for (const listener of allListeners) listener(); };
	let active: { key: string; source: EventSource; users: Set<symbol> } | undefined;

	const clear = () => {
		const symbols = [...quotes.keys()];
		quotes.clear();
		lastQuoteAt = 0;
		for (const key of symbols) for (const listener of listeners.get(key) ?? []) listener();
		notifyAll();
	};

	const receive = (data: string) => {
		let frame: { ticks?: unknown; status?: { snapshot_source?: string; snapshot_stale?: boolean; snapshot_warning?: string; snapshot_error?: string; session_open?: boolean } };
		try { frame = JSON.parse(data); } catch { return; }
		if (!frame) return;
		if (frame.status) updateStatus({ connection: 'open', stale: Boolean(frame.status.snapshot_stale), sessionOpen: Boolean(frame.status.session_open), source: frame.status.snapshot_source ?? '', warning: frame.status.snapshot_warning || frame.status.snapshot_error || '' });
		if (!Array.isArray(frame.ticks)) return;
		const changed = new Set<string>();
		for (const tick of frame.ticks) {
			if (!tick || typeof tick.symbol !== 'string' || !/^\d{6}$/.test(symbolKey(tick.symbol))
				|| typeof tick.price !== 'number' || !Number.isFinite(tick.price) || tick.price <= 0
				|| typeof tick.change_percent !== 'number' || !Number.isFinite(tick.change_percent)
				|| typeof tick.ts !== 'number' || !Number.isFinite(tick.ts) || tick.ts <= 0) continue;
			const key = symbolKey(tick.symbol);
			const previous = quotes.get(key);
			if (previous && tick.ts < previous.ts) continue;
			if (previous?.price !== tick.price || previous?.changePercent !== tick.change_percent || previous?.stale) changed.add(key);
			quotes.set(key, { price: tick.price, changePercent: tick.change_percent, ts: tick.ts });
			lastQuoteAt = Math.max(lastQuoteAt, tick.ts);
		}
		for (const key of changed) for (const listener of listeners.get(key) ?? []) listener();
		if (changed.size) notifyAll();
	};

	return {
		connect(config: BackendConfig) {
			const url = new URL('/api/v1/monitor/stream', config.backendUrl);
			if (config.token) url.searchParams.set('token', config.token);
			const key = url.toString();
			if (active?.key !== key) {
				active?.source.close();
				if (expiryTimer) clearInterval(expiryTimer);
				clear();
				updateStatus({ connection: 'connecting', stale: false, sessionOpen: false, source: '', warning: '' });
				const source = new EventSource(key);
				source.onopen = () => { if (active?.source === source) updateStatus({ connection: 'open' }); };
				source.onerror = () => { if (active?.source === source) updateStatus({ connection: 'reconnecting', stale: true }); };
				source.onmessage = (event) => { if (active?.source === source) receive(event.data); };
				active = { key, source, users: new Set() };
				expiryTimer = setInterval(() => {
					if (status.sessionOpen && lastQuoteAt > 0 && Date.now() - lastQuoteAt > 60000) updateStatus({ stale: true });
					let expired = false;
					for (const [symbol, quote] of quotes) {
						if (status.sessionOpen && !quote.stale && Date.now() - quote.ts > 60000) {
							quotes.set(symbol, { ...quote, stale: true });
							for (const listener of listeners.get(symbol) ?? []) listener();
							expired = true;
						}
					}
					if (expired) notifyAll();
				}, 15000);
			}
			const connection = active;
			const user = Symbol();
			connection.users.add(user);
			// EventSource 原生重连；最后一个工作区卸载时关闭连接和缓存。
			return () => {
				connection.users.delete(user);
				if (active === connection && connection.users.size === 0) {
					connection.source.close();
					active = undefined;
					if (expiryTimer) clearInterval(expiryTimer);
					expiryTimer = undefined;
					updateStatus({ connection: 'closed', stale: false, source: '', warning: '', sessionOpen: false });
					clear();
				}
			};
		},
		get(symbol: string) { return quotes.get(symbolKey(symbol)); },
		getVersion() { return version; },
		getStatus() { return status; },
		subscribeStatus(listener: Listener) { statusListeners.add(listener); return () => { statusListeners.delete(listener); }; },
		subscribeAll(listener: Listener) { allListeners.add(listener); return () => { allListeners.delete(listener); }; },
		subscribe(symbol: string, listener: Listener) {
			const key = symbolKey(symbol);
			let bucket = listeners.get(key);
			if (!bucket) listeners.set(key, bucket = new Set());
			bucket.add(listener);
			return () => {
				bucket.delete(listener);
				if (bucket.size === 0) listeners.delete(key);
			};
		},
	};
}

export const monitorLiveBus = createMonitorLiveBus();

export function useLiveStreamStatus() {
	return useSyncExternalStore(monitorLiveBus.subscribeStatus, monitorLiveBus.getStatus, monitorLiveBus.getStatus);
}

export function liveStatusLabel(status: LiveStreamStatus) {
	if (status.connection === 'reconnecting') return '行情连接中断，优先显示备用值';
	if (status.warning) return `行情降级：${status.warning}`;
	if (status.stale) return '行情已过期，优先显示备用值';
	if (status.connection === 'connecting') return '行情连接中';
	return '';
}

export function useLiveQuoteVersion() {
	return useSyncExternalStore(monitorLiveBus.subscribeAll, monitorLiveBus.getVersion, () => 0);
}
const emptyQuote = () => undefined;

export function useLiveQuote(symbol: string) {
	const subscribe = useCallback((listener: Listener) => monitorLiveBus.subscribe(symbol, listener), [symbol]);
	const snapshot = useCallback(() => monitorLiveBus.get(symbol), [symbol]);
	return useSyncExternalStore(subscribe, snapshot, emptyQuote);
}
