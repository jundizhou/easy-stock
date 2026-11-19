import { requestJSON, type BackendConfig, type SourceMeta } from './backend';
import type { ETFSubscriptionDashboard } from './etf-subscription';

export type ETFSubscriptionPayload = { data: ETFSubscriptionDashboard; meta: SourceMeta };
const prefix = 'easy-stock:etf-subscriptions:v2:';
const maxAge = 7 * 24 * 60 * 60 * 1000;
const requests = new Map<string, Promise<ETFSubscriptionPayload>>();

function storageKey(config: BackendConfig) { return prefix + config.backendUrl.replace(/\/+$/, ''); }

function isPayload(value: unknown): value is ETFSubscriptionPayload {
	if (!value || typeof value !== 'object') return false;
	const { data, meta } = value as Partial<ETFSubscriptionPayload>;
	if (!data || !meta || !Array.isArray(data.days) || !Array.isArray(data.series) || !Array.isArray(data.checks) || !Array.isArray(data.warnings)) return false;
	const age = Date.now() - Date.parse(meta.fetched_at);
	const optionalNumber = (number: unknown) => number == null || (typeof number === 'number' && Number.isFinite(number));
	return data.expected === 22 && typeof data.latest_complete_date === 'string' && Number.isFinite(age) && age >= -60_000 && age <= maxAge
		&& data.days.every((day) => day && typeof day.date === 'string' && optionalNumber(day.estimated_net))
		&& data.checks.every((check) => check && typeof check.code === 'string' && typeof check.date === 'string')
		&& data.series.every((item) => item && typeof item.code === 'string' && typeof item.name === 'string' && optionalNumber(item.latest_price) && optionalNumber(item.change_percent) && Array.isArray(item.points)
			&& item.points.every((point) => point && typeof point.date === 'string' && [point.shares, point.share_change, point.close, point.estimated_net].every(optionalNumber)))
		&& (!data.today_trial || (typeof data.today_trial.status === 'string' && Array.isArray(data.today_trial.items)
			&& data.today_trial.items.every((item) => item && typeof item.code === 'string' && typeof item.name === 'string' && optionalNumber(item.price) && optionalNumber(item.estimated_net))));
}

export function readETFSubscriptionSnapshot(config: BackendConfig | null): ETFSubscriptionPayload | null {
	if (!config) return null;
	try {
		const value: unknown = JSON.parse(window.localStorage.getItem(storageKey(config)) || 'null');
		if (!isPayload(value)) return null;
		return { ...value, meta: { ...value.meta, stale: true } };
	} catch { return null; }
}

// One bounded request survives a tab remount/StrictMode replay and populates the
// next visit's public-market snapshot. Polling still belongs to the visible view.
export function loadETFSubscriptionSnapshot(config: BackendConfig): Promise<ETFSubscriptionPayload> {
	const key = storageKey(config);
	const requestKey = `${key}|${config.token}`;
	const pending = requests.get(requestKey);
	if (pending) return pending;
	const controller = new AbortController();
	const timer = window.setTimeout(() => controller.abort(), 36_000);
	const promise = requestJSON<ETFSubscriptionPayload>(config, '/api/v1/market/national-team-etfs/subscriptions', { signal: controller.signal }).then((payload) => {
		if (!payload.meta.stale && isPayload(payload)) {
			try { window.localStorage.setItem(key, JSON.stringify(payload)); } catch { /* Refresh works when storage is unavailable. */ }
		}
		return payload;
	}).finally(() => {
		window.clearTimeout(timer);
		requests.delete(requestKey);
	});
	requests.set(requestKey, promise);
	return promise;
}
