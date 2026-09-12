import type { StockDirectoryEntry } from './backend';

export type WatchlistResolution = {
	symbol?: string;
	notice?: string;
};

const CODE_PATTERN = /^(SH|SZ|BJ)?\d{6}$/;
const SUFFIX_PATTERN = /^\d{6}\.(SH|SZ|BJ)$/;

/**
 * 把自选股输入解析成后端可接受的代码。
 * - 6 位代码 / 带前缀后缀的代码：原样交给后端归一化；
 * - 中文名或简称：到股票目录里查代码；命中多只时给出候选提示而不是瞎猜。
 */
export function resolveWatchlistInput(raw: string, stocks: StockDirectoryEntry[]): WatchlistResolution {
	const trimmed = raw.trim();
	if (!trimmed) {
		return {};
	}
	const compact = trimmed.replace(/\s+/g, '').toUpperCase();
	if (CODE_PATTERN.test(compact) || SUFFIX_PATTERN.test(compact)) {
		return { symbol: compact };
	}
	const exact = stocks.find((item) => item.name === trimmed);
	if (exact) {
		return { symbol: exact.symbol };
	}
	const partial = stocks.filter((item) => item.name.includes(trimmed));
	if (partial.length === 1) {
		return { symbol: partial[0].symbol };
	}
	if (partial.length > 1) {
		const preview = partial.slice(0, 5).map((item) => `${item.name}(${item.code})`).join('、');
		return { notice: `“${trimmed}” 匹配到 ${partial.length} 只：${preview}${partial.length > 5 ? ' 等' : ''}，请改用代码添加` };
	}
	// 目录里没有：交给后端报错，避免前端静默失败。
	return { symbol: compact };
}
