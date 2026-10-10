import { useCallback, useEffect, useState } from 'react';
import { Activity, AlertTriangle, LoaderCircle, RefreshCw } from 'lucide-react';
import type { BackendConfig } from '../lib/backend';
import { fetchQuantVerdict, type MerVerdict } from '../lib/quant-signals';
import { logRuntimeEvent } from '../lib/runtime-log';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
};

type LoadState = 'idle' | 'loading' | 'ready' | 'error';

const TONE_LABEL: Record<string, string> = {
	bull: '偏多',
	bear: '偏弱',
	neutral: '中性',
};

const TONE_CLASS: Record<string, string> = {
	bull: 'ok',
	bear: 'bad',
	neutral: 'mute',
};

function text(value: unknown): string {
	if (value === null || value === undefined || value === '') return '—';
	if (typeof value === 'object') return JSON.stringify(value);
	return String(value);
}

function number(value: unknown, digits = 1): string {
	return typeof value === 'number' ? value.toFixed(digits) : '—';
}

/**
 * MER 市场环境中面板（只读）。
 *
 * 数据来源：Quant Terminal /api/verdict（mer/verdict_latest.json），经 Go 网关透传。
 *
 * ⚠ 经阶段 0 检验，状态标签对 20 日收益**无预测力**（p=0.70），只作状态描述；
 * 四维分位只能当逆向/估值指标用。本页不进订单链、不改任何策略参数。
 */
export function MarketRegimeWorkspace({ config, refreshKey }: Props) {
	const [verdict, setVerdict] = useState<MerVerdict | null>(null);
	const [state, setState] = useState<LoadState>('idle');
	const [error, setError] = useState('');

	const load = useCallback(() => {
		if (!config) return;
		const abort = new AbortController();
		setState(previous => (previous === 'ready' ? previous : 'loading'));
		setError('');
		fetchQuantVerdict(config, { signal: abort.signal })
			.then(payload => {
				if (abort.signal.aborted) return;
				setVerdict(payload);
				setState('ready');
			})
			.catch(reason => {
				if (abort.signal.aborted) return;
				logRuntimeEvent('warn', 'market-regime', { event: 'verdict_unavailable' });
				setError(reason instanceof Error ? reason.message : 'MER 评判加载失败');
				setState('error');
			});
		return () => abort.abort();
	}, [config]);

	useEffect(() => {
		const cleanup = load();
		return cleanup;
	}, [load, refreshKey]);

	if (!config) return <div className="strategy-empty"><Activity size={22} /><span>等待后端配置…</span></div>;

	const dims = verdict?.dims || [];
	const info = verdict?.verdict || {};
	const index = verdict?.index || {};
	const falsify = info.falsify || {};
	const similar = verdict?.similar || {};
	const riskBudget = (similar.risk_budget || {}) as Record<string, unknown>;

	return (
		<section className="strategy-panel" aria-label="市场环境">
			<header className="strategy-heading">
				<div>
					<h2>市场环境 · MER</h2>
					<p>趋势 T · 广度 B · 参与 L · 风险 R 四维滚动分位 · 仅作状态描述</p>
				</div>
				<button type="button" className="strategy-action ghost" onClick={() => load()} disabled={state === 'loading'}>
					<RefreshCw size={14} aria-hidden="true" />刷新
				</button>
			</header>

			{state === 'loading' && <div className="strategy-card-note"><LoaderCircle size={13} /> 正在读取 MER 评判…</div>}
			{state === 'error' && (
				<div className="strategy-alert">
					<AlertTriangle size={15} aria-hidden="true" />
					<div>
						<strong>无法读取 MER 评判</strong>
						<div>{error}</div>
						<div style={{ marginTop: 4 }}>确认本地 Quant Terminal（8766）在运行，或检查 <code>C:\quant\mer\verdict_latest.json</code>。</div>
					</div>
				</div>
			)}

			<div className="strategy-card">
				<h3>
					当前状态
					{' '}
					<span className={`strategy-badge ${TONE_CLASS[verdict?.tone || ''] || 'mute'}`}>
						{TONE_LABEL[verdict?.tone || ''] || text(verdict?.tone)}
					</span>
				</h3>
				<div className="strategy-stat-grid">
					<div className="strategy-stat"><span>数据截止</span><strong>{text(verdict?.asof)}</strong></div>
					<div className="strategy-stat"><span>状态判定</span><strong>{text(verdict?.state?.confirmed || verdict?.state?.raw)}</strong></div>
					<div className="strategy-stat"><span>环境分</span><strong>{number(info.env_score)}</strong></div>
					<div className="strategy-stat"><span>最弱 / 最强</span><strong>{text(info.worst)} / {text(info.best)}</strong></div>
				</div>
				{info.headline && <p className="strategy-sub">{text(info.headline)}</p>}
			</div>

			{dims.length > 0 && (
				<div className="strategy-card">
					<h3>四维分位（0-100，逆向 / 估值语义）</h3>
					<div className="strategy-stat-grid">
						{dims.map(dim => (
							<div className="strategy-stat" key={dim.key}>
								<span>{dim.name} · {text(dim.weights)}</span>
								<strong>{number(dim.score)}</strong>
								<div className="strategy-meter"><i style={{ width: `${Math.max(0, Math.min(100, dim.score))}%` }} /></div>
								{dim.read && <span style={{ fontSize: 10 }}>{dim.read}</span>}
							</div>
						))}
					</div>
				</div>
			)}

			<div className="strategy-columns">
				<div className="strategy-card">
					<h3>指数参照</h3>
					<dl className="strategy-kv">
						<dt>基准</dt><dd>{text(index.name)}</dd>
						<dt>沪深300</dt><dd>{number(index.hs300, 2)}</dd>
						<dt>MA60</dt><dd>{number(index.ma60, 2)}</dd>
						<dt>偏离 MA60</dt><dd>{number(index.ma60_dev_pct, 2)}%</dd>
						<dt>5 日 / 60 日变动</dt><dd>{number(index.r5_pct, 2)}% / {number(index.r60_pct, 2)}%</dd>
						<dt>60 日回撤</dt><dd>{number(index.dd60_pct, 2)}%</dd>
					</dl>
				</div>

				<div className="strategy-card">
					<h3>证伪条件</h3>
					{Object.keys(falsify).length > 0 ? (
						<dl className="strategy-kv">
							{Object.entries(falsify).map(([key, value]) => (
								<div key={key} style={{ display: 'contents' }}>
									<dt>{key}</dt>
									<dd>{text(value)}</dd>
								</div>
							))}
						</dl>
					) : (
						<p className="strategy-card-note">当前无显式证伪条件。</p>
					)}
					{riskBudget.note && <p className="strategy-sub">风险预算：{text(riskBudget.note)}</p>}
				</div>
			</div>

			{info.coverage && <div className="strategy-card"><h3>覆盖度</h3><p className="strategy-card-note">{text(info.coverage)}</p></div>}

			<div className="strategy-alert neutral">
				<Activity size={15} aria-hidden="true" />
				<div>
					<strong>状态标签对 20 日收益无预测力（p=0.70）</strong>，只作状态描述；四维分位只能当逆向 / 估值指标用。
					本页不进订单链、不改任何策略参数、不构成交易信号。
				</div>
			</div>
		</section>
	);
}
