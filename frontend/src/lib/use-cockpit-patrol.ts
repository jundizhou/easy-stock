import { useCallback, useEffect, useRef, useState } from 'react';
import type { BackendConfig } from './backend';
import { type AutoPatrolUpdate, type PatrolBrief, type PatrolStatus, fetchPatrolBriefs, fetchPatrolStatus, runPatrol, updateAutoPatrol } from './cockpit-patrol';

const STATUS_INTERVAL_MS = 30_000;
const RUNNING_INTERVAL_MS = 3_000;

// 巡查状态与记录由后端调度，前端只负责读取和展示；有运行中的巡查时短轮询直到完成。
export function useCockpitPatrol(config: BackendConfig | null, refreshKey: number) {
	const [status, setStatus] = useState<PatrolStatus | null>(null);
	const [briefs, setBriefs] = useState<PatrolBrief[]>([]);
	const [error, setError] = useState('');
	const [loading, setLoading] = useState(false);
	const [starting, setStarting] = useState(false);
	const [savingAutoPatrol, setSavingAutoPatrol] = useState(false);
	const [autoPatrolError, setAutoPatrolError] = useState('');
	const savingAutoPatrolRef = useRef(false);
	const statusRevision = useRef(0);
	const mounted = useRef(true);

	useEffect(() => {
		mounted.current = true;
		return () => {
			mounted.current = false;
		};
	}, []);

	const load = useCallback(async (signal?: AbortSignal) => {
		if (!config) return;
		const revision = statusRevision.current;
		setLoading(true);
		try {
			const [nextStatus, nextBriefs] = await Promise.all([fetchPatrolStatus(config, signal), fetchPatrolBriefs(config, signal)]);
			if (!mounted.current || signal?.aborted) return;
			// 切换前或保存中的轮询响应不能覆盖刚保存的开关状态。
			if (revision === statusRevision.current && !savingAutoPatrolRef.current) setStatus(nextStatus);
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

	const saveAutoPatrol = useCallback(async (update: AutoPatrolUpdate) => {
		if (!config || savingAutoPatrolRef.current) return;
		savingAutoPatrolRef.current = true;
		statusRevision.current += 1;
		setSavingAutoPatrol(true);
		setAutoPatrolError('');
		try {
			const nextStatus = await updateAutoPatrol(config, update);
			if (mounted.current) setStatus(nextStatus);
		} catch (reason) {
			if (mounted.current) setAutoPatrolError(reason instanceof Error ? reason.message : '保存自动巡查设置失败');
		} finally {
			statusRevision.current += 1;
			savingAutoPatrolRef.current = false;
			if (mounted.current) setSavingAutoPatrol(false);
		}
	}, [config]);

	return { status, briefs, error: autoPatrolError || error, loading, starting, running, start, savingAutoPatrol, saveAutoPatrol, reload: load } as const;
}
