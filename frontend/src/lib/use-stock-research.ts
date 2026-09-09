import { useCallback, useEffect, useRef, useState } from 'react';
import { requestJSON, type BackendConfig } from './backend';
import { isResearchRunning, type ResearchJob, type ResearchJobSummary, type ResearchRequest, type ResearchVerification } from './stock-research';

const selectionKey = 'easy-stock.stock-research.selected.v1';
function readSelection() { try { return window.localStorage.getItem(selectionKey) || ''; } catch { return ''; } }
function rememberSelection(id: string) { try { if (id) window.localStorage.setItem(selectionKey, id); else window.localStorage.removeItem(selectionKey); } catch { /* Backend history remains authoritative. */ } }

export function useStockResearch(config: BackendConfig | null) {
	const [selectedID, setSelectedID] = useState(readSelection);
	const [job, setJob] = useState<ResearchJob | null>(null);
	const [history, setHistory] = useState<ResearchJobSummary[]>([]);
	const [error, setError] = useState('');
	const [starting, setStarting] = useState(false);
	const [verifying, setVerifying] = useState(false);
	const [selectionVersion, setSelectionVersion] = useState(0);
	const sequence = useRef(0);
	const refreshHistory = useCallback(async () => {
		if (!config) return;
		try { const response = await requestJSON<{ data: ResearchJobSummary[] }>(config, '/api/v1/stocks/research'); setHistory(response.data); }
		catch (reason) { setError(reason instanceof Error ? reason.message : '研究历史读取失败'); }
	}, [config]);

	useEffect(() => { void refreshHistory(); }, [refreshHistory]);
	useEffect(() => {
		if (!config || !selectedID) return;
		const controller = new AbortController();
		const currentSequence = sequence.current;
		let timer: ReturnType<typeof setTimeout> | undefined;
		let failures = 0;
		const poll = async () => {
			try {
				const response = await requestJSON<{ data: ResearchJob }>(config, `/api/v1/stocks/research/${selectedID}`, { signal: controller.signal });
				if (controller.signal.aborted || currentSequence !== sequence.current) return;
				setJob(response.data); setError(''); failures = 0;
				if (isResearchRunning(response.data)) timer = setTimeout(poll, 1500);
				else void refreshHistory();
			} catch (reason) {
				if (controller.signal.aborted || currentSequence !== sequence.current) return;
				setError(reason instanceof Error ? reason.message : '研究任务读取失败');
				if (++failures < 5) timer = setTimeout(poll, 3000);
			}
		};
		void poll();
		return () => { controller.abort(); if (timer) clearTimeout(timer); };
	}, [config, selectedID, selectionVersion, refreshHistory]);

	const open = useCallback((id: string) => { sequence.current++; setJob(null); setError(''); setSelectedID(id); setSelectionVersion((value) => value + 1); rememberSelection(id); }, []);
	const clear = useCallback(() => open(''), [open]);
	const start = useCallback(async (request: ResearchRequest) => {
		if (!config) throw new Error('后端尚未连接');
		const current = ++sequence.current;
		setStarting(true); setError(''); setJob(null); setSelectedID('');
		try {
			const response = await requestJSON<{ data: ResearchJob }>(config, '/api/v1/stocks/research', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
			if (sequence.current !== current) return;
			setJob(response.data); setSelectedID(response.data.id); rememberSelection(response.data.id); void refreshHistory();
		} catch (reason) { if (sequence.current === current) setError(reason instanceof Error ? reason.message : '研究提交失败'); throw reason; }
		finally { setStarting(false); }
	}, [config, refreshHistory]);
	const cancel = useCallback(async () => {
		if (!config || !job) return;
		try { await requestJSON(config, `/api/v1/stocks/research/${job.id}/cancel`, { method: 'POST' }); }
		catch (reason) { setError(reason instanceof Error ? reason.message : '停止失败'); }
	}, [config, job]);
	const remove = useCallback(async (id: string) => {
		if (!config) return false;
		try {
			await requestJSON(config, `/api/v1/stocks/research/${id}`, { method: 'DELETE' });
			if (id === selectedID) clear();
			await refreshHistory();
			return true;
		} catch (reason) { setError(reason instanceof Error ? reason.message : '删除失败'); return false; }
	}, [config, selectedID, clear, refreshHistory]);
	const verify = useCallback(async () => {
		if (!config || !job) return;
		setVerifying(true); const id = job.id;
		try {
			const response = await requestJSON<{ data: ResearchVerification }>(config, `/api/v1/stocks/research/${id}/verify`, { method: 'POST' });
			setJob((current) => current?.id === id ? { ...current, verification: response.data } : current);
		} catch (reason) { setError(reason instanceof Error ? reason.message : '核验失败'); }
		finally { setVerifying(false); }
	}, [config, job]);
	return { job, history, error, starting, verifying, selectedID, start, open, clear, cancel, remove, verify, refreshHistory };
}
