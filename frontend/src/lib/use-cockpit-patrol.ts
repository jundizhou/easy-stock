import { useCallback, useEffect, useRef, useState } from 'react';
import type { BackendConfig } from './backend';
import { type PatrolBrief, type PatrolStatus, fetchPatrolBriefs, fetchPatrolStatus, runPatrol } from './cockpit-patrol';

const STATUS_INTERVAL_MS = 30_000;
const RUNNING_INTERVAL_MS = 3_000;

// 巡查状态与记录由后端调度，前端只负责读取和展示；有运行中的巡查时短轮询直到完成。
export function useCockpitPatrol(config: BackendConfig | null, refreshKey: number) {
	const [status, setStatus] = useState<PatrolStatus | null>(null);
	const [briefs, setBriefs] = useState<PatrolBrief[]>([]);
	const [error, setError] = useState('');
	const [loading, setLoading] = useState(false);
	const [starting, setStarting] = useState(false);
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
			const [nextStatus, nextBriefs] = await Promise.all([fetchPatrolStatus(config, signal), fetchPatrolBriefs(config, signal)]);
			if (!mounted.current || signal?.aborted) return;
			setStatus(nextStatus);
			setBriefs(nextBriefs);
			setError('');
		} catch (reason) {
			if (!mounted.current || signal?.aborted) return;
			setError(reason instanceof Error ? reason.message : '巡查状态加载失败');
		} finally {
			if (mounted.current) setLoading(false);
		}
	}, [config]);

	useEffect(() => {
		const abort = new AbortController();
		void load(abort.signal);
		const timer = window.setInterval(() => void load(), STATUS_INTERVAL_MS);
		return () => {
			abort.abort();
			window.clearInterval(timer);
		};
	}, [load, refreshKey]);

	const running = briefs.some((brief) => brief.status === 'running') || Boolean(status?.running);
	useEffect(() => {
		if (!running) return;
		const timer = window.setInterval(() => void load(), RUNNING_INTERVAL_MS);
		return () => window.clearInterval(timer);
	}, [running, load]);

	const start = useCallback(async (phase: Parameters<typeof runPatrol>[1]) => {
		if (!config) return null;
		setStarting(true);
		try {
			const brief = await runPatrol(config, phase);
			await load();
			setError('');
			return brief;
		} catch (reason) {
			setError(reason instanceof Error ? reason.message : '启动巡查失败');
			return null;
		} finally {
			if (mounted.current) setStarting(false);
		}
	}, [config, load]);

	return { status, briefs, error, loading, starting, running, start, reload: load } as const;
}
