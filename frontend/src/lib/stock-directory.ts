import { useEffect, useState } from 'react';
import { requestJSON, type BackendConfig, type StockDirectoryData, type StockDirectoryEntry } from './backend';

// 与 StockAIAnalysisWorkspace 共用同一份 localStorage 缓存（同 key 同格式），
// 多个工作台（core2 选股 / 板块图谱）共享一次目录请求，24h 内不重复拉取。
const storageKey = 'easy-stock.stock-directory.v1';
const STORAGE_TTL = 24 * 60 * 60 * 1000;

type CacheEntry = { map: Map<string, string>; fetchedAt: number };

let memory: CacheEntry | null = null;

function readStorage(): CacheEntry | null {
	try {
		const raw = window.localStorage.getItem(storageKey);
		if (!raw) return null;
		const cached = JSON.parse(raw) as { cachedAt?: number; stocks?: StockDirectoryEntry[] };
		if (!cached.cachedAt || Date.now() - cached.cachedAt > STORAGE_TTL || !Array.isArray(cached.stocks)) return null;
		const map = new Map<string, string>();
		for (const stock of cached.stocks) {
			if (stock && typeof stock.code === 'string' && typeof stock.name === 'string') map.set(stock.code, stock.name);
		}
		return { map, fetchedAt: cached.cachedAt };
	} catch {
		return null;
	}
}

function writeStorage(entry: CacheEntry) {
	try {
		const stocks = [...entry.map.entries()].map(([code, name]) => ({ symbol: code, code, name }));
		window.localStorage.setItem(storageKey, JSON.stringify({ cachedAt: entry.fetchedAt, stocks }));
	} catch {
		// 配额不足时静默放弃持久化，内存缓存仍然有效。
	}
}

/** 当前可用的 代码→名称 映射（可能来自缓存，尚未拉取时为空 Map）。 */
export function stockNameMap(): Map<string, string> {
	return memory?.map || readStorage()?.map || new Map<string, string>();
}

/**
 * 股票名录 Hook：全库 代码→名称。
 * 名字是展示增强而不是数据依赖：拉取失败静默保留旧缓存，不让页面报错。
 */
export function useStockNameMap(config: BackendConfig | null): Map<string, string> {
	const [map, setMap] = useState<Map<string, string>>(() => stockNameMap());
	useEffect(() => {
		if (!config) return;
		const cached = memory || readStorage();
		if (cached && Date.now() - cached.fetchedAt < STORAGE_TTL) {
			memory = cached;
			setMap(cached.map);
			return;
		}
		let cancelled = false;
		requestJSON<{ data: StockDirectoryData }>(config, '/api/v1/stocks/directory')
			.then(payload => {
				if (cancelled) return;
				const next = new Map<string, string>();
				for (const stock of payload.data.stocks || []) {
					if (stock && stock.code && stock.name) next.set(stock.code, stock.name);
				}
				memory = { map: next, fetchedAt: Date.now() };
				writeStorage(memory);
				setMap(next);
			})
			.catch(() => undefined);
		return () => { cancelled = true; };
	}, [config]);
	return map;
}
