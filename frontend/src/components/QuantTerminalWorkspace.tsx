import { useCallback, useEffect, useMemo, useState } from 'react';
import { ExternalLink, LoaderCircle, RefreshCw, Server } from 'lucide-react';
import type { BackendConfig } from '../lib/backend';
import { fetchSidecars, type SidecarEndpoints } from '../lib/sidecars';
import { logRuntimeEvent } from '../lib/runtime-log';

type ProbeState = 'idle' | 'probing' | 'ok' | 'down';

type QuantPage = { key: string; label: string; path: string };

/** 8766 上已核实的页面入口；自定义路径可自由输入。 */
const QUANT_PAGES: QuantPage[] = [
	{ key: 'hub', label: '终端首页', path: '/' },
	{ key: 'stockdb-viewer', label: '数据浏览器', path: '/hub/static/stockdb_viewer.html' },
	{ key: 'ticks', label: 'Ticks 代理', path: '/hub/static/ticks.html' },
];

export function QuantTerminalWorkspace({ config, refreshKey }: { config: BackendConfig | null; refreshKey: number }) {
	const [sidecars, setSidecars] = useState<SidecarEndpoints | null>(null);
	const [sidecarsError, setSidecarsError] = useState('');
	const [activePage, setActivePage] = useState<QuantPage>(QUANT_PAGES[0]);
	const [customPath, setCustomPath] = useState('');
	const [frameKey, setFrameKey] = useState(0);
	const [quantProbe, setQuantProbe] = useState<ProbeState>('idle');
	const [stockdbProbe, setStockdbProbe] = useState<ProbeState>('idle');

	useEffect(() => {
		if (!config) return;
		const abort = new AbortController();
		setSidecarsError('');
		fetchSidecars(config, { signal: abort.signal })
			.then(payload => { if (!abort.signal.aborted) setSidecars(payload); })
			.catch((error) => {
				if (abort.signal.aborted) return;
				setSidecarsError(error instanceof Error ? error.message : 'sidecar 配置暂不可用');
				logRuntimeEvent('warn', 'quant-terminal', { event: 'sidecars_unavailable' });
			});
		return () => abort.abort();
	}, [config, refreshKey]);

	const probe = useCallback(async (path: '/api/v1/quant/hub-status' | '/api/v1/stockdb/status', apply: (state: ProbeState) => void) => {
		if (!config) return;
		apply('probing');
		try {
			const payload = await fetch(new URL(path, config.backendUrl).toString(), { signal: AbortSignal.timeout(8000) });
			const body = await payload.json() as { ok?: boolean };
			apply(body.ok ? 'ok' : 'down');
		} catch {
			apply('down');
		}
	}, [config]);

	useEffect(() => {
		void probe('/api/v1/quant/hub-status', setQuantProbe);
		void probe('/api/v1/stockdb/status', setStockdbProbe);
	}, [probe, refreshKey]);

	const frameSrc = useMemo(() => {
		if (!sidecars) return '';
		const base = sidecars.quant_url.replace(/\/+$/, '');
		const path = activePage.key === 'custom' ? customPath.trim() : activePage.path;
		if (!path || path === '/') return base + '/';
		return base + (path.startsWith('/') ? path : '/' + path);
	}, [sidecars, activePage, customPath]);

	if (!config) {
		return <div className="quant-terminal-empty"><Server size={22} /><span>等待后端配置…</span></div>;
	}

	return (
		<section className="quant-terminal-workspace" aria-label="量化终端">
			<div className="quant-terminal-bar">
				<div className="quant-terminal-pages" role="tablist" aria-label="终端页面">
					{QUANT_PAGES.map(page => (
						<button
							type="button"
							key={page.key}
							role="tab"
							aria-selected={activePage.key === page.key}
							className={activePage.key === page.key ? 'active' : ''}
							onClick={() => { setActivePage(page); setFrameKey(key => key + 1); }}
						>{page.label}</button>
					))}
					<button
						type="button"
						role="tab"
						aria-selected={activePage.key === 'custom'}
						className={activePage.key === 'custom' ? 'active' : ''}
						onClick={() => { setActivePage({ key: 'custom', label: '自定义', path: '' }); setFrameKey(key => key + 1); }}
					>自定义</button>
				</div>
				{activePage.key === 'custom' && (
					<input
						className="quant-terminal-path"
						value={customPath}
						onChange={(event) => setCustomPath(event.target.value)}
						onKeyDown={(event) => { if (event.key === 'Enter') setFrameKey(key => key + 1); }}
						placeholder="输入 8766 路径，如 /hub/static/stockdb_viewer.html"
						aria-label="自定义终端路径"
					/>
				)}
				<div className="quant-terminal-status">
					<span className={`sidecar-chip ${quantProbe}`} title="GET /api/v1/quant/hub-status">终端 8766 {probeLabel(quantProbe)}</span>
					<span className={`sidecar-chip ${stockdbProbe}`} title="GET /api/v1/stockdb/status">StockDB 7899 {probeLabel(stockdbProbe)}</span>
					<button type="button" className="icon-button" onClick={() => {
						void probe('/api/v1/quant/hub-status', setQuantProbe);
						void probe('/api/v1/stockdb/status', setStockdbProbe);
						setFrameKey(key => key + 1);
					}} aria-label="重新探活并刷新"><RefreshCw size={16} aria-hidden="true" /></button>
					<a className="icon-button" href={frameSrc || '#'} target="_blank" rel="noreferrer" aria-label="新窗口打开" title="新窗口打开"><ExternalLink size={16} aria-hidden="true" /></a>
				</div>
			</div>
			{sidecarsError && <p className="load-notice">{sidecarsError}</p>}
			<div className="quant-terminal-frame-wrap">
				{sidecars ? (
					<iframe
						key={frameKey}
						className="quant-terminal-frame"
						src={frameSrc}
						title="Quant Terminal"
						loading="lazy"
					/>
				) : (
					<div className="chart-placeholder"><LoaderCircle className="spin" size={20} /><span>解析 sidecar 地址中…</span></div>
				)}
			</div>
		</section>
	);
}

function probeLabel(state: ProbeState): string {
	if (state === 'ok') return '在线';
	if (state === 'down') return '离线';
	if (state === 'probing') return '探活中';
	return '待探活';
}
