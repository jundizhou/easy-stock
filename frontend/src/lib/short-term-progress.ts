import { BackendConfig, requestJSON } from './backend';

export type ShortTermProgress<T> = {
	data: T | null;
	refresh_id: string;
	revision: number;
	refreshing: boolean;
	stale: boolean;
	steps: Record<string, string>;
	errors: Record<string, string>;
};

// One request at a time, with an absolute budget (including a hung HTTP request).
// Aborted generations never publish, even if a transport ignores cancellation.
export async function pollShortTermProgress<T>(config: BackendConfig, path: string, publish: (value: ShortTermProgress<T>) => void, options: { signal: AbortSignal; refresh: boolean; budgetMS: number }) {
	const abort = new AbortController();
	const cancel = () => abort.abort();
	options.signal.addEventListener('abort', cancel, { once: true });
	if (options.signal.aborted) cancel();
	let timedOut = false;
	const timeout = setTimeout(() => { timedOut = true; abort.abort(); }, options.budgetMS);
	let id = '';
	let revision = -1;
	let interval = 250;
	try {
		while (!abort.signal.aborted) {
			const query = new URLSearchParams({ delivery: 'progressive' });
			if (id) query.set('refresh_id', id);
			else if (options.refresh) query.set('refresh', '1');
			const received = await abortable(requestJSON<ShortTermProgress<T>>(config, `${path}?${query}`, { signal: abort.signal }), abort.signal);
			if (abort.signal.aborted) break;
			// A desktop backend awaiting restart may still return the complete legacy payload.
			const value: ShortTermProgress<T> = { ...received, refresh_id: received.refresh_id || '', revision: received.revision || 0, refreshing: !!received.refreshing, stale: !!received.stale, steps: received.steps || {}, errors: received.errors || {} };
			if (value.refresh_id !== id || value.revision >= revision) publish(value);
			id = value.refresh_id;
			revision = value.revision;
			if (!value.refreshing) return;
			await new Promise<void>(resolve => {
				const finish = () => { clearTimeout(timer); abort.signal.removeEventListener('abort', finish); resolve(); };
				const timer = setTimeout(finish, interval);
				abort.signal.addEventListener('abort', finish, { once: true });
			});
			interval = Math.min(1500, interval * 1.4);
		}
	} catch (error) {
		if (!abort.signal.aborted) throw error;
	} finally {
		clearTimeout(timeout);
		options.signal.removeEventListener('abort', cancel);
	}
	if (timedOut && !options.signal.aborted) throw new Error('数据更新超时，已保留可用快照，请重试');
}

function abortable<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
	return new Promise((resolve, reject) => {
		const cancel = () => { signal.removeEventListener('abort', cancel); reject(new DOMException('Aborted', 'AbortError')); };
		if (signal.aborted) cancel(); else signal.addEventListener('abort', cancel, { once: true });
		promise.then(resolve, reject).finally(() => signal.removeEventListener('abort', cancel));
	});
}
