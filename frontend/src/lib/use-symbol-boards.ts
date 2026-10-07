import { useEffect, useState } from 'react';
import type { BackendConfig } from './backend';
import { fetchSymbolsBoards, normalizeSymbol, type SymbolBoardRef } from './sidecars';

// 模块级缓存：板块表每日同步一次，30 分钟 TTL 足够；同代码的 chip 不重复请求。
const cache = new Map<string, { boards: SymbolBoardRef[]; fetchedAt: number }>();
const TTL_MS = 30 * 60 * 1000;

function readCache(symbol: string): SymbolBoardRef[] | null {
	const entry = cache.get(symbol);
	if (entry && Date.now() - entry.fetchedAt < TTL_MS) return entry.boards;
	if (entry) cache.delete(symbol);
	return null;
}

/**
 * 个股所属板块反查（懒触发）。
 * enabled=false 时不发请求——用于「开盘啦概念缺失的 chip」兜底场景，
 * 概念齐全的雷达行完全零请求。
 */
export function useSymbolBoards(config: BackendConfig | null, symbol: string, enabled: boolean): SymbolBoardRef[] | null {
	const normalized = normalizeSymbol(symbol);
	const [boards, setBoards] = useState<SymbolBoardRef[] | null>(() => (normalized ? readCache(normalized) : null));

	useEffect(() => {
		if (!config || !enabled || !normalized) return;
		const cached = readCache(normalized);
		if (cached) {
			setBoards(cached);
			return;
		}
		let cancelled = false;
		fetchSymbolsBoards(config, [normalized], 12)
			.then(payload => {
				if (cancelled) return;
				const refs = payload[normalized] || [];
				cache.set(normalized, { boards: refs, fetchedAt: Date.now() });
				if (cache.size > 500) {
					const oldest = cache.keys().next().value;
					if (oldest) cache.delete(oldest);
				}
				setBoards(refs);
			})
			.catch(() => {
				if (!cancelled) setBoards(null);
			});
		return () => { cancelled = true; };
	}, [config, enabled, normalized]);

	return boards;
}
