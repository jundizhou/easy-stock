import { useCallback, useEffect, useRef, useState } from 'react';
import { BackendConfig, SourceMeta, ThemeOverview, requestJSON } from './backend';

export type ThemeProgress = {
	data: ThemeOverview[]; meta: SourceMeta; revision: number; refresh_id: string;
	refreshing: boolean; stage: string; steps: Record<string, string>; errors: Record<string, string>;
};

function waitForPoll(ms: number, signal: AbortSignal) {
	return new Promise<void>((resolve, reject) => {
		const cancel = () => { clearTimeout(timer); reject(new DOMException('Aborted', 'AbortError')); };
		const timer = setTimeout(() => { signal.removeEventListener('abort', cancel); resolve(); }, ms);
		if (signal.aborted) cancel(); else signal.addEventListener('abort', cancel, { once: true });
	});
}

export function useThemeOverview(config: BackendConfig | null, active: boolean) {
	const [value, setValue] = useState<ThemeProgress | null>(null);
	const [error, setError] = useState('');
	const [fetching, setFetching] = useState(false);
	const controller = useRef<AbortController | null>(null);
	const flight = useRef<Promise<ThemeOverview[]> | null>(null);
	const run = useCallback((force: boolean): Promise<ThemeOverview[]> => {
		if (!config || !active) return Promise.resolve([]);
		if (flight.current) return flight.current;
		const abort = new AbortController(); controller.current = abort;
		setError(''); setFetching(true);
		const operation = (async () => {
			let refreshID = ''; let previousRevision = -1; let delay = 800;
			const timeout = setTimeout(() => abort.abort('timeout'), 30_000);
			try {
				for (;;) {
					const params = new URLSearchParams({ delivery: 'progressive' });
					if (refreshID) params.set('refresh_id', refreshID); else if (force) params.set('refresh', '1');
					const result = await requestJSON<ThemeProgress>(config, `/api/v1/themes/overview?${params}`, { signal: abort.signal });
					if (abort.signal.aborted) return [];
					setValue(result);
					if (!result.refreshing) {
						setError(Object.values(result.errors || {}).join('；'));
						return result.data;
					}
					refreshID = result.refresh_id;
					delay = result.revision === previousRevision ? Math.min(2000, delay + 400) : 800;
					previousRevision = result.revision;
					await waitForPoll(delay, abort.signal);
				}
			} catch (reason) {
				if (!abort.signal.aborted || abort.signal.reason === 'timeout') setError(abort.signal.reason === 'timeout' ? '题材更新超时，请重试' : reason instanceof Error ? reason.message : '题材加载失败');
				return [];
			} finally {
				clearTimeout(timeout);
				if (controller.current === abort) { flight.current = null; setFetching(false); }
			}
		})();
		flight.current = operation;
		return operation;
	}, [config, active]);
	useEffect(() => {
		if (active) void run(false);
		return () => { controller.current?.abort(); controller.current = null; flight.current = null; };
	}, [active, run]);
	const refresh = useCallback(() => run(true), [run]);
	const state = fetching ? 'loading' : error ? (value?.data.length ? 'partial' : 'error') : value ? 'ready' : 'idle';
	return { data: value?.data || [], meta: value?.meta || null, state, fetching, error, refresh } as const;
}
