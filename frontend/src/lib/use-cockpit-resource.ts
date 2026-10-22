import { useEffect, useRef, useState } from 'react';
import { BackendConfig } from './backend';

export type CockpitResourceState = 'idle' | 'loading' | 'ready' | 'error';

export type CockpitResource<T> = {
	data: T | null;
	state: CockpitResourceState;
	error: string;
	receivedAt: string;
};

type Loader<T> = (config: BackendConfig, signal: AbortSignal, publish: (data: T) => void) => Promise<void>;

// 每个驾驶舱模块独立加载：一个接口慢或失败，只影响自己的卡片。
export function useCockpitResource<T>(config: BackendConfig | null, refreshKey: number, load: Loader<T>): CockpitResource<T> {
	const [value, setValue] = useState<CockpitResource<T>>({ data: null, state: 'idle', error: '', receivedAt: '' });
	const loaderRef = useRef(load);
	loaderRef.current = load;

	useEffect(() => {
		if (!config) return;
		const abort = new AbortController();
		setValue((current) => ({ ...current, state: 'loading', error: '' }));
		loaderRef.current(config, abort.signal, (data) => {
			if (abort.signal.aborted) return;
			setValue({ data, state: 'ready', error: '', receivedAt: new Date().toISOString() });
		})
			.then(() => {
				if (abort.signal.aborted) return;
				setValue((current) => (current.state === 'loading' ? { ...current, state: current.data ? 'ready' : 'error', error: current.data ? '' : '暂无数据', receivedAt: new Date().toISOString() } : current));
			})
			.catch((reason: unknown) => {
				if (abort.signal.aborted) return;
				setValue((current) => ({ ...current, state: 'error', error: reason instanceof Error ? reason.message : '加载失败', receivedAt: new Date().toISOString() }));
			});
		return () => abort.abort();
	}, [config, refreshKey]);

	return value;
}
