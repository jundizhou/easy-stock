import { useCallback, useEffect, useRef, useState } from 'react';
import type { BackendConfig } from './backend';
import { type PatrolLiveFacts, fetchPatrolFacts } from './cockpit-patrol';

// 固定模块按分钟级刷新：图上数据始终是「刚才」，不再跟着巡查记录走。
const LIVE_INTERVAL_MS = 60_000;

export type CockpitLiveState = {
	data: PatrolLiveFacts | null;
	error: string;
	loading: boolean;
	reload: () => Promise<void>;
};

export function useCockpitLiveFacts(config: BackendConfig | null, refreshKey: number): CockpitLiveState {
	const [data, setData] = useState<PatrolLiveFacts | null>(null);
	const [error, setError] = useState('');
	const [loading, setLoading] = useState(false);
	const mounted = useRef(true);

	useEffect(() => {
		mounted.current = true;
		return () => {
			mounted.current = false;
		};
	}, []);

	const load = useCallback(async (signal?: AbortSignal) => {
		if (!config) return;
		setLoading(true);
		try {
			const next = await fetchPatrolFacts(config, signal);
			if (!mounted.current || signal?.aborted) return;
			setData(next);
			setError('');
		} catch (reason) {
			if (!mounted.current || signal?.aborted) return;
			setError(reason instanceof Error ? reason.message : '盘面数据加载失败');
		} finally {
			if (mounted.current) setLoading(false);
		}
	}, [config]);

	useEffect(() => {
		const abort = new AbortController();
		void load(abort.signal);
		const timer = window.setInterval(() => void load(), LIVE_INTERVAL_MS);
		return () => {
			abort.abort();
			window.clearInterval(timer);
		};
	}, [load, refreshKey]);

	return { data, error, loading, reload: load } as const;
}
