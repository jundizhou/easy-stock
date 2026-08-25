type RuntimeLogLevel = 'debug' | 'info' | 'warn' | 'error';

const maxRuntimeMessageLength = 8 * 1024;

export function logRuntimeEvent(level: RuntimeLogLevel, feature: string, details: unknown) {
	if (typeof window === 'undefined' || !window.aStock?.logRuntimeEvent) return;
	const message = sanitizeRuntimeMessage(formatRuntimeDetails(details));
	void window.aStock.logRuntimeEvent({ level, feature, message }).catch(() => {});
}

export function installRuntimeLogging() {
	if (typeof window === 'undefined') return;
	window.addEventListener('error', (event) => {
		logRuntimeEvent('error', 'renderer', {
			event: 'uncaught_error',
			message: event.message,
			source: safeRuntimePath(event.filename),
			line: event.lineno,
			column: event.colno,
			error: runtimeErrorDetails(event.error),
		});
	});
	window.addEventListener('unhandledrejection', (event) => {
		logRuntimeEvent('error', 'renderer', { event: 'unhandled_rejection', error: runtimeErrorDetails(event.reason) });
	});
	logRuntimeEvent('info', 'renderer', { event: 'renderer_start' });
}

export function runtimeFeatureForPath(pathname: string) {
	if (pathname.startsWith('/api/v1/themes') || pathname.startsWith('/api/v1/sector-map')) return 'theme-radar';
	if (pathname.startsWith('/api/v1/short-term')) return 'short-term';
	if (pathname.startsWith('/api/v1/stocks/ai-analysis')) return 'stock-analysis';
	if (pathname.startsWith('/api/v1/stocks')) return 'stocks';
	if (pathname.startsWith('/api/v1/reviews')) return 'reviews';
	if (pathname.startsWith('/api/v1/market') || pathname.startsWith('/api/v1/research')) return 'market-data';
	if (pathname.startsWith('/api/v1/quotes')) return 'quotes';
	if (pathname.startsWith('/api/v1/settings')) return 'settings';
	if (pathname.startsWith('/api/v1/ai')) return 'ai-chat';
	if (pathname.startsWith('/api/v1/strategy')) return 'strategy';
	return 'renderer';
}

export function runtimeErrorDetails(value: unknown) {
	if (value instanceof Error) {
		return {
			name: value.name,
			message: value.message,
			stack: value.stack?.split('\n').slice(0, 8).join('\n') || '',
		};
	}
	return { name: 'Error', message: typeof value === 'string' ? value : 'Unknown runtime error' };
}

function formatRuntimeDetails(details: unknown) {
	if (typeof details === 'string') return details;
	try {
		return JSON.stringify(details);
	} catch {
		return String(details);
	}
}

function sanitizeRuntimeMessage(value: string) {
	return value
		.replace(/\b(https?:\/\/[^\s?#"']+)[?#][^\s"']*/gi, '$1')
		.replace(/\bBearer\s+[A-Za-z0-9._~+/=-]+/gi, 'Bearer <redacted>')
		.replace(/\b(token|api[_-]?key|cookie|credential|authorization)\b["']?\s*[:=]\s*["']?([^"'\s,;&}]+)/gi, '$1=<redacted>')
		.slice(0, maxRuntimeMessageLength);
}

function safeRuntimePath(value: string) {
	try {
		return new URL(value).pathname;
	} catch {
		return '';
	}
}
