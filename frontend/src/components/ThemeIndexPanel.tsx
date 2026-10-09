import { useEffect, useMemo, useRef, useState } from 'react';
import { BackendConfig, requestJSON, ThemeOverview } from '../lib/backend';
import { aggregateThemeIndex, ThemeIndexPeriod, ThemeIndexSeries } from '../lib/theme-index';
import { KLineChart } from './KLineChart';
import './theme-index.css';

const seriesCache = new Map<string, { value: ThemeIndexSeries; expires: number }>();
export function ThemeIndexPanel({ config, theme, refreshKey }: { config: BackendConfig | null; theme: ThemeOverview | null | undefined; refreshKey: number }) {
	const [period, setPeriod] = useState<ThemeIndexPeriod>('day');
	const [result, setResult] = useState<{ key: string; value: ThemeIndexSeries } | null>(null);
	const [state, setState] = useState<'loading' | 'ready' | 'error' | 'idle'>('idle');
	const [error, setError] = useState('');
	const [retry, setRetry] = useState(0);
	const versions = useRef({ refreshKey, retry });
	const key = config && theme ? `${config.backendUrl}:${config.token}:${theme.theme}@${theme.snapshot_id || ''}` : '';
	useEffect(() => {
		if (!config || !theme) return;
		const force = versions.current.refreshKey !== refreshKey || versions.current.retry !== retry;
		versions.current = { refreshKey, retry };
		const cached = seriesCache.get(key);
		if (!force && cached && cached.expires > Date.now()) { setResult({ key, value: cached.value }); setState('ready'); setError(''); return; }
		const abort = new AbortController();
		const timer = setTimeout(() => abort.abort('timeout'), 25_000);
		setState('loading'); setError('');
		const params = new URLSearchParams({ theme: theme.theme, limit: '240' });
		if (force) params.set('refresh', '1');
		if (theme.snapshot_id) params.set('snapshot_id', theme.snapshot_id);
		void requestJSON<{ data: ThemeIndexSeries }>(config, `/api/v1/themes/index?${params}`, { signal: abort.signal }).then(({ data }) => {
			if (abort.signal.aborted) return;
			if (!data.lines.length) throw new Error('暂无题材指数历史行情');
			if (seriesCache.size >= 32) seriesCache.delete(seriesCache.keys().next().value!);
			seriesCache.set(key, { value: data, expires: Date.now() + 5 * 60_000 });
			setResult({ key, value: data }); setState('ready');
		}).catch(reason => {
			if (abort.signal.aborted && abort.signal.reason !== 'timeout') return;
			setError(abort.signal.reason === 'timeout' ? '题材指数加载超时，请重试' : reason instanceof Error ? reason.message : '题材指数暂不可用'); setState('error');
		}).finally(() => clearTimeout(timer));
		return () => { clearTimeout(timer); abort.abort(); };
	}, [config, key, theme?.theme, theme?.snapshot_id, refreshKey, retry]);
	const series = result?.key === key ? result.value : null;
	const lines = useMemo(() => aggregateThemeIndex(series?.lines || [], period).slice(period === 'day' ? -45 : -120), [series, period]);
	const latest = lines.at(-1);
	const label = { day: '日K', week: '周K', month: '月K' }[period];
	return <section className="theme-index-panel" aria-label="题材指数走势">
		<div className="theme-index-heading">
			<div><span>题材指数</span><h3>{theme?.name || '选择题材'}</h3><small>{series ? series.method === 'provider-index' ? `${series.index_name} · ${series.index_code}` : '等权参考指数' : '查看题材整体走势'}</small></div>
			<div className="theme-index-controls"><div role="group" aria-label="题材指数K线周期">{(['day', 'week', 'month'] as const).map(value => <button type="button" key={value} aria-pressed={period === value} className={period === value ? 'active' : ''} onClick={() => setPeriod(value)}>{{ day: '日K', week: '周K', month: '月K' }[value]}</button>)}</div>{latest && <strong className={latest.change_percent && latest.change_percent < 0 ? 'down' : 'up'}>{latest.close.toFixed(2)}<small>{latest.change_percent == null ? '--' : `${latest.change_percent >= 0 ? '+' : ''}${latest.change_percent.toFixed(2)}%`}</small></strong>}</div>
		</div>
		<KLineChart key={`${key}:${period}`} lines={lines} state={series ? 'ready' : state} periodLabel={label} compact timeZone="Asia/Shanghai" />
		{error && <p className="load-notice">{error} <button type="button" onClick={() => setRetry(value => value + 1)}>重试题材指数</button></p>}
	</section>;
}
