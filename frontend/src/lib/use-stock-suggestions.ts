import { useEffect, useMemo, useState } from 'react';
import type { BackendConfig, StockDirectoryEntry } from './backend';
import { fetchMonitorStockSuggest } from './monitor';
import { directStockCode, searchStockDirectory } from './stock-analysis';

const empty: StockDirectoryEntry[] = [];

export function useStockSuggestions(config: BackendConfig | null | undefined, query: string, directory = empty) {
	const needle = query.trim().toUpperCase();
	const direct = directStockCode(needle);
	const term = direct.endsWith('.HK') ? direct.slice(0, 5) : needle;
	const [response, setResponse] = useState<{ config: BackendConfig; term: string; rows: StockDirectoryEntry[]; error?: string } | null>(null);
	const local = useMemo(() => direct.endsWith('.HK')
		? directory.filter(item => item.symbol === direct)
		: searchStockDirectory(directory, needle), [directory, direct, needle]);
	const request = Boolean(config && term && !(direct && !direct.endsWith('.HK') && local.length));
	useEffect(() => {
		if (!config || !request) return;
		let active = true;
		const timer = window.setTimeout(async () => {
			try {
				const result = await fetchMonitorStockSuggest(config, term);
				if (active) setResponse({ config, term, rows: result?.data || [] });
			} catch (error) {
				if (active) setResponse({ config, term, rows: [], error: String(error) });
			}
		}, 220);
		return () => { active = false; window.clearTimeout(timer); };
	}, [config, term, request]);
	const current = response?.term === term && response.config === config ? response : null;
	const remote = request && current ? current.rows : empty;
	const suggestions = useMemo(() => {
		const seen = new Set<string>();
		const rows: StockDirectoryEntry[] = [];
		// Keep exact local names and cross-listed matches ahead of fuzzy matches.
		for (const item of [...local.filter(item => item.name.toUpperCase() === needle || item.symbol === direct), ...remote, ...local]) {
			if (seen.has(item.symbol) || (direct.endsWith('.HK') && item.symbol !== direct)) continue;
			seen.add(item.symbol);
			rows.push(directory.find(entry => entry.symbol === item.symbol) || item);
		}
		return rows.slice(0, 10);
	}, [directory, direct, local, needle, remote]);
	return { suggestions, loading: request && !current, error: request ? current?.error || '' : '' };
}
