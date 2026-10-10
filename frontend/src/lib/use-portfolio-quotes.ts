import { useCallback, useEffect, useState } from 'react';
import { requestJSON, type BackendConfig, type Quote } from './backend';

export async function loadPortfolioQuotes(config: BackendConfig, symbols: string[], signal?: AbortSignal) {
	const payload = await requestJSON<{ data: Quote[] }>(config, `/api/v1/quotes/realtime?symbols=${encodeURIComponent(symbols.join(','))}`, { signal });
	return Object.fromEntries((Array.isArray(payload.data) ? payload.data : []).filter((quote) => symbols.includes(quote.symbol) && Number.isFinite(quote.price) && quote.price > 0).map((quote) => [quote.symbol, quote]));
}

export function usePortfolioQuotes(config: BackendConfig | null | undefined, symbols: string[]) {
	const symbolsKey = [...symbols].sort().join(',');
	const [quotes, setQuotes] = useState<Record<string, Quote>>({});
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState('');
	const [refreshKey, setRefreshKey] = useState(0);
	const refresh = useCallback(() => setRefreshKey((key) => key + 1), []);
	useEffect(() => {
		if (!config || !symbolsKey) { setQuotes({}); setError(''); setLoading(false); return; }
		let active = true;
		let pending = false;
		let controller: AbortController | undefined;
		const update = async () => {
			if (pending) return;
			pending = true;
			controller = new AbortController();
			const timeout = window.setTimeout(() => controller?.abort(), 10000);
			setLoading(true);
			try {
				const symbols = symbolsKey.split(',');
				const next = await loadPortfolioQuotes(config, symbols, controller.signal);
				if (!active) return;
				setQuotes(next);
				setError(symbols.every((symbol) => next[symbol]) ? '' : '部分股票暂无有效行情，相关盈亏暂不计算');
			} catch {
				if (active) setError('行情刷新失败，已有价格为上次快照');
			} finally {
				window.clearTimeout(timeout);
				pending = false;
				if (active) setLoading(false);
			}
		};
		void update();
		const timer = window.setInterval(() => { if (document.visibilityState !== 'hidden') void update(); }, 15000);
		const onVisible = () => { if (document.visibilityState === 'visible') void update(); };
		document.addEventListener('visibilitychange', onVisible);
		return () => { active = false; controller?.abort(); window.clearInterval(timer); document.removeEventListener('visibilitychange', onVisible); };
	}, [config?.backendUrl, config?.token, symbolsKey, refreshKey]);
	return { quotes, loading, error, refresh };
}
