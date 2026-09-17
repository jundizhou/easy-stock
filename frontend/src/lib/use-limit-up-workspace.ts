import { useCallback, useEffect, useRef, useState } from 'react';
import { BackendConfig, LimitUpLadderData, MarketEmotionHistory } from './backend';
import { pollShortTermProgress, ShortTermProgress } from './short-term-progress';
import { logRuntimeEvent } from './runtime-log';

type LoadState = 'idle' | 'loading' | 'ready' | 'error';
function useShortTermResource<T>(config: BackendConfig | null, active: boolean, refreshKey: number, path: string, budgetMS: number) {
	const [data, setData] = useState<T | null>(null);
	const [progress, setProgress] = useState<ShortTermProgress<T> | null>(null);
	const [state, setState] = useState<LoadState>('idle');
	const [error, setError] = useState('');
	const previousConfig = useRef(config);
	const previousRefresh = useRef(refreshKey);
	useEffect(() => {
		if (previousConfig.current !== config) {
			setData(null); setProgress(null); setError(''); setState('idle');
			previousConfig.current = config;
		}
		if (!config || !active) return;
		const abort = new AbortController();
		let disposed = false;
		const started = performance.now();
		let first = true;
		const force = previousRefresh.current !== refreshKey;
		previousRefresh.current = refreshKey;
		setState('loading'); setError('');
		void pollShortTermProgress<T>(config, path, value => {
			if (disposed) return;
			if (value.data) {
				setData(value.data);
				if (first) {
					first = false;
					logRuntimeEvent('info', 'short-term', { event: 'first_region_available', path, duration_ms: Math.round(performance.now() - started) });
				}
			}
			setProgress(value);
			const errors = Object.values(value.errors).filter(Boolean).join('；');
			setError(errors);
			setState(value.refreshing ? 'loading' : errors ? 'error' : 'ready');
			if (!value.refreshing) logRuntimeEvent('info', 'short-term', { event: 'refresh_complete', path, duration_ms: Math.round(performance.now() - started), failed: !!errors });
		}, { signal: abort.signal, refresh: force, budgetMS }).catch(error => {
			if (disposed) return;
			setState('error');
			setError(error instanceof Error ? error.message : '数据加载失败');
		});
		return () => { disposed = true; abort.abort(); };
	}, [config, active, refreshKey, path, budgetMS]);
	// Hide the old backend's data even before the reset effect runs.
	const currentConfig = previousConfig.current === config;
	return { data: currentConfig ? data : null, progress: currentConfig ? progress : null, state, error };
}

export function useLimitUpWorkspace(config: BackendConfig | null, active: boolean) {
	const [refreshKey, setRefreshKey] = useState(0);
	const refresh = useCallback(() => setRefreshKey(key => key + 1), []);
	const ladder = useShortTermResource<LimitUpLadderData>(config, active, refreshKey, '/api/v1/short-term/limit-up-ladder', 35_000);
	const history = useShortTermResource<MarketEmotionHistory>(config, active, refreshKey, '/api/v1/short-term/emotion-history', 160_000);
	return { ladder, history, refresh };
}
