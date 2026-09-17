import { useEffect, useRef, useState } from 'react';
import { BackendConfig, BackendRequestError, SectorMap, ThemeOverview, ThemeScreenData, ThemeScreenLane, ThemeScreenSort, requestJSON } from './backend';

export type ThemeFilter = { page: number; node: string; lane: ThemeScreenLane; sort: ThemeScreenSort; query: string };
export function sameTheme(item: ThemeOverview, id: string) { return item.theme === id || item.aliases?.includes(id); }

// A preview has known membership, never an authoritative full-pool count.
export function leaderPreview(theme: ThemeOverview, filter: ThemeFilter): ThemeScreenData | null {
	if (!theme.leader_stocks?.length || filter.page !== 1 || filter.node !== 'all' || filter.lane !== 'all' || filter.query) return null;
	const meta = { source: theme.source || '', fetched_at: '', latency_ms: 0, stale: Boolean(theme.carry_forward), trade_date: theme.trade_date, snapshot_id: theme.snapshot_id };
	const industry = theme.theme.startsWith('industry:');
	const map: SectorMap = { theme: theme.theme, name: theme.name, tabs: [], meta, groups: [{ id: industry ? 'industry_members' : 'kaipanla', name: '已知领涨', nodes: [{ id: industry ? 'industry_core' : 'kaipanla_leaders', name: '已知领涨', change_percent: 0, main_net_inflow: 0, match_status: 'matched', stocks: theme.leader_stocks.map(stock => ({ ...stock, price: stock.price || 0, change: stock.change || 0, change_percent: stock.change_percent || 0, amount: stock.amount || 0, volume: stock.volume || 0, meta: stock.meta || meta })) }] }] };
	return { map, order: theme.leader_stocks.map(stock => stock.symbol), complete: false, coverage: 'leaders', source_snapshot_id: theme.snapshot_id, snapshot_id: theme.snapshot_id || '', sort: filter.sort, pagination: { page: 1, page_size: 20, total: theme.leader_stocks.length, total_pages: 0, has_more: false } };
}

export function useThemeConstituents(config: BackendConfig | null, active: boolean, theme: ThemeOverview | null, filter: ThemeFilter, refreshKey: number, recover: () => Promise<ThemeOverview[]>) {
	const requestKey = JSON.stringify([config?.backendUrl, theme?.theme, theme?.snapshot_id, filter]);
	const [state, setState] = useState<{ key: string; data: ThemeScreenData | null; fetching: boolean; error: string }>({ key: '', data: null, fetching: false, error: '' });
	const cache = useRef(new Map<string, ThemeScreenData>());
	const recoveries = useRef(new Set<string>());
	const recoveryEpoch = useRef(refreshKey);
	const input = useRef({ theme, filter, recover }); input.current = { theme, filter, recover };
	useEffect(() => {
		if (!config || !active || !input.current.theme) return;
		if (recoveryEpoch.current !== refreshKey) { recoveries.current.clear(); recoveryEpoch.current = refreshKey; }
		const selected = input.current.theme;
		const filters = input.current.filter;
		const abort = new AbortController();
		let data = cache.current.get(requestKey) || leaderPreview(selected, filters);
		setState({ key: requestKey, data, fetching: true, error: '' });
		const run = async (current: ThemeOverview, retry: boolean): Promise<void> => {
			let complete = false; let expired = false; let fullError = '';
			const params = new URLSearchParams({ theme: current.theme, page: String(filters.page), page_size: '20', node: filters.node, lane: filters.lane, sort: filters.sort });
			if (current.snapshot_id) params.set('snapshot_id', current.snapshot_id);
			if (filters.query) params.set('q', filters.query);
			const load = async (leaders: boolean) => {
				const query = new URLSearchParams(params); if (leaders) query.set('phase', 'leaders');
				try {
					const result = await requestJSON<{ data: ThemeScreenData }>(config, `/api/v1/themes/screen?${query}`, { signal: abort.signal });
					if (abort.signal.aborted || expired || (leaders && (complete || data?.complete))) return;
					if (leaders && !result.data.map.groups?.some(group => group.nodes.some(node => node.stocks.length))) return;
					data = { ...result.data, complete: result.data.complete ?? !leaders }; complete = !leaders;
					cache.current.set(requestKey, data);
					if (cache.current.size > 32) cache.current.delete(cache.current.keys().next().value!);
					setState({ key: requestKey, data, fetching: !complete, error: '' });
				} catch (reason) {
					if (abort.signal.aborted) return;
					if (reason instanceof BackendRequestError && reason.code === 'SNAPSHOT_EXPIRED') expired = true;
					if (!leaders) fullError = reason instanceof Error ? reason.message : '完整成分加载失败';
				}
			};
			// Each response publishes itself; this join only handles terminal state/recovery.
			await Promise.allSettled([load(false), ...(filters.page === 1 ? [load(true)] : [])]);
			if (abort.signal.aborted) return;
			// Recovery changes the overview and can restart this effect. Track the
			// theme (including fusion aliases), so a new snapshot gets only one retry.
			const identities = [current.theme, ...(current.aliases || [])];
			if (expired && !retry && !identities.some(id => recoveries.current.has(id))) {
				identities.forEach(id => recoveries.current.add(id));
				const latest = await input.current.recover();
				if (abort.signal.aborted) return;
				const replacement = latest.find(item => sameTheme(item, current.theme) || item.aliases?.some(id => current.aliases?.includes(id)));
				if (replacement) {
					[replacement.theme, ...(replacement.aliases || [])].forEach(id => recoveries.current.add(id));
					await run(replacement, true); return;
				}
			}
			if (!expired && complete) identities.forEach(id => recoveries.current.delete(id));
			setState({ key: requestKey, data, fetching: false, error: expired ? '题材快照已更新，请重试' : fullError });
		};
		const timeout = setTimeout(() => { abort.abort(); setState(current => current.key === requestKey ? { ...current, fetching: false, error: '成分加载超时，请重试' } : current); }, 50_000);
		void run(selected, false).finally(() => clearTimeout(timeout));
		return () => { clearTimeout(timeout); abort.abort(); };
	}, [config, active, requestKey, refreshKey]);
	const current = state.key === requestKey ? state : { key: requestKey, data: cache.current.get(requestKey) || (theme ? leaderPreview(theme, filter) : null), fetching: active && Boolean(theme), error: '' };
	const status = current.fetching ? 'loading' : current.error ? (current.data ? 'partial' : 'error') : current.data ? 'ready' : 'idle';
	return { ...current, status } as const;
}
