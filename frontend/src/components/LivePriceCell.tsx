import { createElement, type ReactNode } from 'react';
import { useLiveQuote, useLiveStreamStatus, liveStatusLabel, selectMonitorQuoteValue } from '../lib/monitorLiveBus';

type QuoteProps = {
	symbol: string;
	kind: 'price' | 'change';
	fallback?: number | null;
};

function quoteValue(kind: QuoteProps['kind'], value?: number | null) {
	if (typeof value !== 'number' || !Number.isFinite(value) || (kind === 'price' && value <= 0)) return '—';
	return kind === 'price' ? value.toFixed(2) : `${value >= 0 ? '+' : ''}${value.toFixed(2)}%`;
}

// 输出原来的 td / b / em 元素，价格更新不会替换行或改变表格结构。
export function LivePriceCell({ symbol, kind, fallback, as = 'td' }: QuoteProps & { as?: 'td' | 'b' | 'em' }) {
	const quote = useLiveQuote(symbol);
	const status = useLiveStreamStatus();
	const value = selectMonitorQuoteValue(quote, status, kind, fallback);
	const className = kind === 'change'
		? typeof value === 'number' && Number.isFinite(value) ? value >= 0 ? 'up' : 'down' : 'muted'
		: undefined;
	const title = liveStatusLabel(status) || (quote?.stale ? '该标的行情已过期，显示上次成功值' : quote ? `行情时间：${new Date(quote.ts).toLocaleTimeString('zh-CN')}` : undefined);
	return createElement(as, { className, title }, quoteValue(kind, value));
}

// 行内现价仅替换文本，不增加包裹元素。
export function LivePriceText({ symbol, kind, fallback }: QuoteProps): ReactNode {
	const quote = useLiveQuote(symbol);
	const status = useLiveStreamStatus();
	return quoteValue(kind, selectMonitorQuoteValue(quote, status, kind, fallback));
}
