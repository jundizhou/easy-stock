import { useEffect, useMemo, useReducer } from 'react';
import { BackendConfig, KLine, requestJSON } from './backend';
import { ThemeKLineQueue } from './theme-kline-queue';
import { KLineLookup } from './short-term';

export function useThemeKLines(config: BackendConfig | null, active: boolean, symbols: string[], selected: string, prefetch: string[], refreshKey: number) {
	const [, changed] = useReducer(n => n + 1, 0);
	const queue = useMemo(() => new ThemeKLineQueue(async (symbol, signal) => {
		if (!config) return [];
		const result = await requestJSON<{ data: KLine[] }>(config, `/api/v1/quotes/kline?symbol=${encodeURIComponent(symbol)}&period=day&limit=60`, { signal });
		return result.data;
	}, changed), [config]);
	const key = symbols.join(',');
	const prefetchKey = prefetch.join(',');
	useEffect(() => {
		if (active && config) queue.setWanted(key.split(',').filter(Boolean), selected, prefetchKey.split(',').filter(Boolean));
	}, [active, config, queue, key, selected, prefetchKey, refreshKey]);
	useEffect(() => {
		if (!active) queue.pause();
		return () => queue.pause();
	}, [active, queue]);
	const histories: KLineLookup = {};
	const failed = new Set<string>();
	let pending = 0;
	for (const symbol of new Set([...symbols, selected].filter(Boolean))) {
		const entry = queue.entries.get(symbol);
		if (entry?.lines?.length) histories[symbol] = entry.lines.slice(-40);
		if (entry?.error) failed.add(symbol);
		if (entry?.pending || !entry) pending++;
	}
	useEffect(() => { if (refreshKey > 0 && active) queue.refresh(); }, [refreshKey, queue]);
	const entry = queue.entries.get(selected);
	const ready = symbols.filter(symbol => histories[symbol]?.length).length;
	const historyState = !symbols.length ? 'idle' : pending ? 'loading' : failed.size ? (ready ? 'partial' : 'error') : 'ready';
	const klineState = !selected ? 'idle' : entry?.lines?.length ? 'ready' : entry?.error ? 'error' : 'loading';
	return { histories, failed, ready, historyState, klineState, lines: selected ? entry?.lines || [] : [], retry: () => queue.retryFailed() } as const;
}
