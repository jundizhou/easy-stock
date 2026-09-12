import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { CalendarClock, LoaderCircle, Plus, RefreshCw, Send, Sparkles, X } from 'lucide-react';
import { requestJSON, type BackendConfig, type CorrelationMatrix, type DailyAnalysisConfig, type DailyAnalysisJob, type DailyStockReport, type StockDirectoryData, type StockDirectoryEntry } from '../lib/backend';
import { resolveWatchlistInput } from '../lib/watchlist';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
	onOpenSettings: () => void;
};

type LoadPhase = 'idle' | 'loading' | 'ready' | 'error';

const actionTone = (action: string) => {
	if (action.includes('买入')) return 'up';
	if (action.includes('持有')) return 'flat';
	if (action.includes('减仓')) return 'down-soft';
	if (action.includes('回避')) return 'down';
	return 'flat';
};

// A 股惯例：涨红跌绿
export function DailyReportWorkspace({ config, refreshKey, onOpenSettings }: Props) {
	const [draft, setDraft] = useState<DailyAnalysisConfig>({ watchlist: [], auto_run: false, run_hour: 15, run_minute: 30, ai_enhance: true, push: { enabled: false, wecom_webhook: '', feishu_webhook: '' } });
	const [symbolInput, setSymbolInput] = useState('');
	const directoryRef = useRef<StockDirectoryEntry[] | null>(null);
	const [job, setJob] = useState<DailyAnalysisJob | null>(null);
	const [history, setHistory] = useState<DailyAnalysisJob[]>([]);
	const [correlation, setCorrelation] = useState<CorrelationMatrix | null>(null);
	const [phase, setPhase] = useState<LoadPhase>('idle');
	const [error, setError] = useState('');
	const [notice, setNotice] = useState('');
	const [starting, setStarting] = useState(false);
	const [savingConfig, setSavingConfig] = useState(false);

	const loadWorkspace = useCallback(async () => {
		if (!config) return;
		setPhase('loading');
		setError('');
		try {
			const [configData, historyData] = await Promise.all([
				requestJSON<{ data: DailyAnalysisConfig }>(config, '/api/v1/daily-analysis/config'),
				requestJSON<{ data: DailyAnalysisJob[] }>(config, '/api/v1/daily-analysis?limit=10'),
			]);
			setDraft({
				watchlist: configData.data.watchlist ?? [],
				auto_run: configData.data.auto_run ?? false,
				run_hour: configData.data.run_hour ?? 15,
				run_minute: configData.data.run_minute ?? 30,
				ai_enhance: configData.data.ai_enhance ?? true,
				push: configData.data.push ?? { enabled: false },
			});
			setHistory(historyData.data ?? []);
			const latest = (historyData.data ?? []).find((item) => item.status === 'running' || item.report_available);
			setJob((current) => current?.status === 'running' ? current : latest ?? null);
			setPhase('ready');
		} catch (loadError) {
			setError(loadError instanceof Error ? loadError.message : String(loadError));
			setPhase('error');
		}
	}, [config]);

	useEffect(() => { void loadWorkspace(); }, [loadWorkspace, refreshKey]);

	useEffect(() => {
		if (!config || job?.status !== 'running') return;
		const timer = window.setInterval(async () => {
			try {
				const data = await requestJSON<{ data: DailyAnalysisJob }>(config, `/api/v1/daily-analysis/${job.id}`);
				setJob(data.data);
				if (data.data.status !== 'running') { void loadWorkspace(); if (data.data.report_available) void loadCorrelation(data.data); }
			} catch { /* 轮询失败静默重试 */ }
		}, 3000);
		return () => window.clearInterval(timer);
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [config, job?.id, job?.status]);

	const loadCorrelation = useCallback(async (target: DailyAnalysisJob) => {
		if (!config || !target.report) return;
		const symbols = target.report.stocks.filter((stock) => stock.status === 'succeeded').map((stock) => stock.symbol);
		if (symbols.length < 2) { setCorrelation(null); return; }
		try {
			const data = await requestJSON<{ data: CorrelationMatrix }>(config, `/api/v1/daily-analysis/correlations?symbols=${symbols.join(',')}&days=60`);
			setCorrelation(data.data);
		} catch { setCorrelation(null); }
	}, [config]);

	useEffect(() => {
		if (job?.report_available && job.report && correlation === null) void loadCorrelation(job);
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [job?.id, job?.report_available]);

	const saveConfig = async () => {
		await persistConfig(draft);
	};

	// 自选股增删后立即落盘，避免用户以为「点了没反应」（此前必须手动点保存配置）。
	const persistConfig = async (next: DailyAnalysisConfig) => {
		if (!config) return;
		setSavingConfig(true);
		setError('');
		try {
			const data = await requestJSON<{ data: DailyAnalysisConfig }>(config, '/api/v1/daily-analysis/config', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(next) });
			setDraft({
				watchlist: data.data.watchlist ?? [],
				auto_run: data.data.auto_run ?? false,
				run_hour: data.data.run_hour ?? 15,
				run_minute: data.data.run_minute ?? 30,
				ai_enhance: data.data.ai_enhance ?? true,
				push: data.data.push ?? { enabled: false },
			});
			setNotice('已保存');
		} catch (saveError) {
			setError(saveError instanceof Error ? saveError.message : String(saveError));
		} finally {
			setSavingConfig(false);
		}
	};

	// 允许输入中文名/简称：先从股票目录解析代码，再交给后端归一化。
	const resolveSymbolInput = async (raw: string): Promise<string | null> => {
		const compact = raw.replace(/\s+/g, '').toUpperCase();
		const looksLikeCode = /^(SH|SZ|BJ)?\d{6}$/.test(compact) || /^\d{6}\.(SH|SZ|BJ)$/.test(compact);
		if (!looksLikeCode && config && !directoryRef.current) {
			try {
				const payload = await requestJSON<StockDirectoryData>(config, '/api/v1/stocks/directory');
				directoryRef.current = payload.stocks ?? [];
			} catch {
				directoryRef.current = [];
			}
		}
		const resolution = resolveWatchlistInput(raw, directoryRef.current ?? []);
		if (resolution.notice) {
			setNotice(resolution.notice);
			return null;
		}
		return resolution.symbol ?? null;
	};

	const addSymbol = async () => {
		const raw = symbolInput.trim();
		if (!raw) return;
		const value = await resolveSymbolInput(raw);
		if (!value) return;
		if (draft.watchlist.some((item) => item.toUpperCase() === value.toUpperCase())) { setNotice(`${value} 已在自选列表`); return; }
		if (draft.watchlist.length >= 20) { setNotice('自选股最多 20 只'); return; }
		const next = [...draft.watchlist, value];
		setDraft((current) => ({ ...current, watchlist: next }));
		setSymbolInput('');
		setNotice('');
		await persistConfig({ ...draft, watchlist: next });
	};

	const removeSymbol = async (symbol: string) => {
		const next = draft.watchlist.filter((item) => item !== symbol);
		setDraft((current) => ({ ...current, watchlist: next }));
		await persistConfig({ ...draft, watchlist: next });
	};

	const startAnalysis = async () => {
		if (!config) return;
		setStarting(true);
		setError('');
		setNotice('');
		try {
			const data = await requestJSON<{ data: DailyAnalysisJob }>(config, '/api/v1/daily-analysis', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ symbols: draft.watchlist, ai_enhance: draft.ai_enhance }) });
			setJob(data.data);
		} catch (startError) {
			setError(startError instanceof Error ? startError.message : String(startError));
		} finally {
			setStarting(false);
		}
	};

	const pushReport = async () => {
		if (!config || !job) return;
		try {
			const data = await requestJSON<{ data: DailyAnalysisJob }>(config, `/api/v1/daily-analysis/${job.id}/push`, { method: 'POST' });
			setJob(data.data);
			const ok = (data.data.push_results ?? []).every((item) => item.ok);
			setNotice(ok ? '日报已推送到配置的渠道' : '部分渠道推送失败，请检查 Webhook 地址');
		} catch (pushError) {
			setError(pushError instanceof Error ? pushError.message : String(pushError));
		}
	};

	const report = job?.report_available ? job.report ?? null : null;
	const progress = job && job.total_stocks > 0 ? Math.round((job.completed_stocks / job.total_stocks) * 100) : 0;

	if (phase === 'loading' && !job) {
		return <div className="daily-wrap"><div className="daily-loading"><LoaderCircle className="spin" size={18} /> 正在连接自选股日报引擎…</div></div>;
	}
	if (phase === 'error') {
		return <div className="daily-wrap"><div className="portfolio-error">{error}</div><button type="button" className="portfolio-btn" onClick={onOpenSettings}>打开系统设置</button></div>;
	}

	return (
		<div className="daily-wrap">
			{error && <div className="portfolio-error">{error}</div>}
			{notice && <div className="portfolio-notice">{notice}</div>}

			<section className="daily-config card">
				<div className="daily-config-head">
					<h3><CalendarClock size={16} /> 自选股 · 日报配置</h3>
					<div className="daily-config-actions">
						<button type="button" className="portfolio-btn" disabled={starting || draft.watchlist.length === 0 || job?.status === 'running'} onClick={startAnalysis}>
							{starting || job?.status === 'running' ? <LoaderCircle className="spin" size={14} /> : <Sparkles size={14} />} 立即生成日报
						</button>
						<button type="button" className="portfolio-btn secondary" disabled={savingConfig} onClick={saveConfig}>{savingConfig ? <LoaderCircle className="spin" size={14} /> : <RefreshCw size={14} />} 保存配置</button>
					</div>
				</div>
				<div className="daily-watchlist">
					{draft.watchlist.map((symbol) => (
						<span key={symbol} className="daily-chip">{symbol}<button type="button" aria-label={`移除 ${symbol}`} onClick={() => void removeSymbol(symbol)}><X size={12} /></button></span>
					))}
					<div className="daily-add">
						<input value={symbolInput} placeholder="代码或名称：600519 / 贵州茅台" onChange={(event) => setSymbolInput(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); void addSymbol(); } }} />
						<button type="button" onClick={() => void addSymbol()} aria-label="添加自选股"><Plus size={14} /></button>
					</div>
				</div>
				<small className="daily-watchlist-hint">这里是「日报配置」的自选股，决定生成日报时分析哪些股票。输入代码（600519）或名称（贵州茅台）后回车 / 点 ➕ 添加，点 ✕ 移除；增删会立即保存（最多 20 只）。与下方「自选股实时行情」相互独立。其余配置改动需点右上角「保存配置」。</small>
				<div className="daily-config-grid">
					<label className="daily-toggle"><input type="checkbox" checked={draft.auto_run} onChange={(event) => setDraft((current) => ({ ...current, auto_run: event.target.checked }))} /> 每个交易日自动生成</label>
					<label className="daily-time">运行时间 <input type="number" min={0} max={23} value={draft.run_hour} onChange={(event) => setDraft((current) => ({ ...current, run_hour: Number(event.target.value) }))} /> : <input type="number" min={0} max={59} value={draft.run_minute} onChange={(event) => setDraft((current) => ({ ...current, run_minute: Number(event.target.value) }))} /></label>
					<label className="daily-toggle"><input type="checkbox" checked={draft.ai_enhance} onChange={(event) => setDraft((current) => ({ ...current, ai_enhance: event.target.checked }))} /> AI 综合研判（需配置模型）</label>
					<label className="daily-toggle"><input type="checkbox" checked={draft.push.enabled} onChange={(event) => setDraft((current) => ({ ...current, push: { ...current.push, enabled: event.target.checked } }))} /> 完成后推送</label>
					<input className="daily-webhook" placeholder="企业微信机器人 Webhook" value={draft.push.wecom_webhook ?? ''} onChange={(event) => setDraft((current) => ({ ...current, push: { ...current.push, wecom_webhook: event.target.value } }))} />
					<input className="daily-webhook" placeholder="飞书机器人 Webhook" value={draft.push.feishu_webhook ?? ''} onChange={(event) => setDraft((current) => ({ ...current, push: { ...current.push, feishu_webhook: event.target.value } }))} />
				</div>
			</section>

			{job?.status === 'running' && (
				<section className="daily-progress card">
					<div className="daily-progress-text"><LoaderCircle className="spin" size={16} /> {job.message || '正在生成日报'}</div>
					<div className="daily-progress-bar"><i style={{ width: `${progress}%` }} /></div>
					<div className="daily-progress-meta">{job.completed_stocks}/{job.total_stocks} · 当前：{(job.current_symbols ?? []).join('、') || '—'}</div>
				</section>
			)}

			{report && (
				<>
					<section className="daily-summary card">
						<div className="daily-summary-head">
							<h3>{report.trade_date} 自选股日报 {report.ai_enhanced && <span className="daily-ai-badge">AI 增强</span>}</h3>
							<button type="button" className="portfolio-btn secondary" onClick={pushReport}><Send size={14} /> 手动推送</button>
						</div>
						<div className="daily-summary-grid">
							<div className="theme-metric"><span>市场倾向</span><strong>{report.summary.bias}</strong></div>
							<div className="theme-metric"><span>平均评分</span><strong>{report.summary.average_score}</strong></div>
							<div className="theme-metric"><span>偏多 / 中性 / 偏空</span><strong>{report.summary.bull_count} / {report.summary.neutral_count} / {report.summary.bear_count}</strong></div>
							{job?.push_results && job.push_results.length > 0 && <div className="theme-metric"><span>推送结果</span><strong>{job.push_results.map((item) => `${item.channel}:${item.ok ? '✓' : '✗'}`).join(' ')}</strong></div>}
						</div>
						{report.summary.highlights.length > 0 && (
							<ul className="daily-highlights">{report.summary.highlights.slice(0, 5).map((item, index) => <li key={index}>{item}</li>)}</ul>
						)}
					</section>

					<section className="daily-stocks">
						{report.stocks.map((stock) => <DailyStockCard key={stock.symbol} stock={stock} />)}
					</section>

					{correlation && correlation.symbols.length >= 2 && <CorrelationHeatmap matrix={correlation} />}
				</>
			)}

			<section className="daily-history card">
				<h3><CalendarClock size={15} /> 历史日报</h3>
				{history.length === 0 ? <p className="daily-empty">暂无历史记录，点击「立即生成日报」开始第一次分析</p> : (
					<ul className="daily-history-list">
						{history.map((item) => (
							<li key={item.id}>
								<button type="button" onClick={() => setJob(item)}>
									<span className="daily-history-date">{item.report?.trade_date || item.updated_at?.slice(0, 10)}</span>
									<span className="daily-history-meta">{item.trigger === 'scheduler' ? '定时' : '手动'} · {item.completed_stocks}/{item.total_stocks} 只 · {item.status}</span>
								</button>
							</li>
						))}
					</ul>
				)}
			</section>
		</div>
	);
}

function DailyStockCard({ stock }: { stock: DailyStockReport }) {
	if (stock.status !== 'succeeded') {
		return <article className="daily-stock card daily-stock-failed"><header><strong>{stock.symbol}</strong><span className="daily-action down">失败</span></header><p className="daily-muted">{stock.error || '数据获取失败'}</p></article>;
	}
	const ind = stock.indicators;
	const change = num(stock.change_percent);
	return (
		<article className="daily-stock card">
			<header>
				<div><strong>{stock.name || stock.symbol}</strong><span className="daily-symbol">{stock.symbol}</span></div>
				<div className="daily-stock-right">
					<span className={`daily-price ${change >= 0 ? 'up' : 'down'}`}>{num(stock.price).toFixed(2)} ({change >= 0 ? '+' : ''}{change.toFixed(2)}%)</span>
					<span className={`daily-action ${actionTone(stock.action)}`}>{stock.action} · {num(stock.score)}分</span>
				</div>
			</header>
			<div className="daily-score-bar"><i style={{ width: `${num(stock.score)}%` }} /></div>
			<p className="daily-trend"><strong>{stock.trend}</strong> · {stock.trend_detail}</p>
			<div className="daily-indicators">
				<span>MA5 <b>{fmt(ind.ma5)}</b></span><span>MA20 <b>{fmt(ind.ma20)}</b></span><span>MA60 <b>{fmt(ind.ma60)}</b></span>
				<span>MACD柱 <b className={num(ind.macd_hist) >= 0 ? 'up' : 'down'}>{num(ind.macd_hist).toFixed(3)}</b></span>
				<span>KDJ <b>{num(ind.kdj_k).toFixed(0)}/{num(ind.kdj_d).toFixed(0)}/{num(ind.kdj_j).toFixed(0)}</b></span>
				<span>RSI14 <b>{num(ind.rsi14).toFixed(1)}</b></span>
				<span>量比 <b>{num(ind.volume_ratio).toFixed(2)}</b></span>
				<span>支撑 <b>{fmt(ind.support)}</b></span><span>压力 <b>{fmt(ind.resistance)}</b></span>
			</div>
			{stock.signals?.length > 0 && <ul className="daily-signals">{stock.signals.map((item, index) => <li key={index}>{item}</li>)}</ul>}
			<div className="daily-risks">{(stock.risks ?? []).map((item, index) => <p key={index} className="daily-risk">⚠ {item}</p>)}</div>
			{stock.commentary && <p className="daily-commentary"><Sparkles size={13} /> {stock.commentary}</p>}
			<details className="daily-checklist"><summary>操作检查清单</summary><ul>{stock.checklist.map((item, index) => <li key={index}>{item}</li>)}</ul></details>
		</article>
	);
}


function CorrelationHeatmap({ matrix }: { matrix: CorrelationMatrix }) {
	const cell = useMemo(() => {
		const lookup = new Map<string, number>();
		for (const pair of matrix.pairs) {
			lookup.set(`${pair.left_symbol}|${pair.right_symbol}`, pair.correlation);
			lookup.set(`${pair.right_symbol}|${pair.left_symbol}`, pair.correlation);
		}
		return lookup;
	}, [matrix]);
	const color = (value: number) => {
		const alpha = Math.min(Math.abs(value), 1) * 0.85 + 0.08;
		return value >= 0 ? `rgba(227, 59, 70, ${alpha})` : `rgba(20, 134, 95, ${alpha})`;
	};
	return (
		<section className="card daily-correlation">
			<h3>60 日收益相关性矩阵 <small>红=同涨同跌，绿=跷跷板（越深越强）</small></h3>
			<table className="daily-corr-table">
				<thead><tr><th></th>{matrix.symbols.map((symbol) => <th key={symbol}>{shortSymbol(symbol)}</th>)}</tr></thead>
				<tbody>
					{matrix.symbols.map((row) => (
						<tr key={row}>
							<th>{shortSymbol(row)}</th>
							{matrix.symbols.map((col) => {
								if (row === col) return <td key={col} className="diag">—</td>;
								const value = cell.get(`${row}|${col}`);
								return <td key={col} style={{ background: value === undefined ? 'transparent' : color(value) }}>{value === undefined ? '?' : value.toFixed(2)}</td>;
							})}
						</tr>
					))}
				</tbody>
			</table>
		</section>
	);
}

function shortSymbol(symbol: string) {
	return symbol.split('.')[0];
}

function fmt(value: number) {
	return value > 0 ? value.toFixed(2) : '--';
}

// 后端字段缺失时退回 0，避免单个数值异常导致整页渲染失败。
function num(value: number | undefined): number {
	return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}
