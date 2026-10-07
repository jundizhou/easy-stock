import { useCallback, useEffect, useState } from 'react';
import { Activity, AlertTriangle, Gauge, LoaderCircle, RefreshCw, ShieldCheck } from 'lucide-react';
import type { BackendConfig } from '../lib/backend';
import {
	deriveOosGate,
	fetchQuantTrack,
	fetchQuantVerdict,
	type ClipTrackPayload,
	type MerVerdict,
	type OosGate,
} from '../lib/quant-signals';
import { logRuntimeEvent } from '../lib/runtime-log';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
};

const TONE_LABEL: Record<string, string> = {
	bull: '偏多',
	bear: '偏弱',
	neutral: '中性',
};

/**
 * MER 策略信号工作台（原生 React，非 iframe）。
 *
 * 三个区块，全部只读：
 * 1. MER 市场状态 —— Quant Terminal /api/verdict（mer/verdict_latest.json）：
 *    T/B/L/R 滚动分位、状态标签、评判 headline、证伪条件、相似期前向分布。
 *    ⚠ 状态标签经阶段 0 检验对 20 日收益无预测力（p=0.70），只作状态描述。
 * 2. clip_n300 独立前向轨 —— Go 网关 /api/v1/quant/track（write-once 轨道文件只读快照）。
 * 3. OOS 门禁 —— 派生展示：污染区间时间轴 + 前向进度 + LOCKED/OPEN。
 */
export function QuantSignalWorkspace({ config, refreshKey }: Props) {
	const [verdict, setVerdict] = useState<MerVerdict | null>(null);
	const [track, setTrack] = useState<ClipTrackPayload | null>(null);
	const [state, setState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
	const [error, setError] = useState('');

	const load = useCallback(() => {
		if (!config) return;
		const abort = new AbortController();
		setState(previous => (previous === 'ready' ? previous : 'loading'));
		setError('');
		Promise.all([
			fetchQuantVerdict(config, { signal: abort.signal }).catch(reason => {
				logRuntimeEvent('warn', 'quant-signals', { event: 'verdict_unavailable' });
				return { ok: false, error: reason instanceof Error ? reason.message : 'verdict 加载失败' } as MerVerdict;
			}),
			fetchQuantTrack(config, { signal: abort.signal }).catch(reason => {
				logRuntimeEvent('warn', 'quant-signals', { event: 'track_unavailable' });
				return null;
			}),
		]).then(([verdictPayload, trackPayload]) => {
			if (abort.signal.aborted) return;
			setVerdict(verdictPayload);
			setTrack(trackPayload);
			if (!trackPayload && verdictPayload.error) {
				setError(verdictPayload.error);
			}
			setState('ready');
		});
		return () => abort.abort();
	}, [config]);

	useEffect(() => { const cleanup = load(); return cleanup; }, [load, refreshKey]);

	if (!config) return <div className="quant-signal-empty"><Gauge size={22} /><span>等待后端配置…</span></div>;

	const gate: OosGate | null = deriveOosGate(track);
	const dims = verdict?.dims || [];
	const fwd = (track?.summary?.forward_only || {}) as Record<string, Record<string, Record<string, number>>>;

	return (
		<section className="quant-signal" aria-label="MER 策略信号">
			<header className="quant-signal-heading">
				<div>
					<h2>MER 策略信号</h2>
					<p>
						市场状态 mer/verdict_latest.json（asof {verdict?.asof || '—'}）· 前向轨 clip_n300（write-once）· 来源 /api/v1/quant/proxy → 8766、/api/v1/quant/track
					</p>
				</div>
				<button type="button" className="icon-button" onClick={load} aria-label="刷新策略信号"><RefreshCw size={16} aria-hidden="true" /></button>
			</header>

			{state === 'loading' && <div className="quant-signal-status" role="status"><LoaderCircle size={16} className="spin" /> 加载中…</div>}
			{state === 'error' && <div className="quant-signal-status error" role="alert">{error || '信号数据不可用'}</div>}

			{state === 'ready' && (
				<div className="quant-signal-grid">
					{/* ---- MER 状态 ---- */}
					<div className="quant-card quant-card-state">
						<div className="quant-card-title"><Activity size={15} /> 市场状态 · {verdict?.index?.name || '沪深300'}</div>
						{verdict?.ok ? (
							<>
								<div className="quant-state-main">
									<span className={`quant-state-badge tone-${verdict.tone || 'neutral'}`}>{verdict.state?.confirmed || '—'}</span>
									<span className="quant-state-tone">{TONE_LABEL[verdict.tone || ''] || verdict.tone || ''} · 环境分 {verdict.verdict?.env_score ?? '—'}</span>
								</div>
								<div className="quant-dims">
									{dims.map(dim => (
										<div key={dim.key} className="quant-dim" title={dim.weights}>
											<div className="quant-dim-head">
												<span className="quant-dim-key">{dim.key} {dim.name}</span>
												<span className="quant-dim-score">{dim.score}</span>
											</div>
											<div className="quant-dim-bar" role="img" aria-label={`${dim.name}分位 ${dim.score}`}>
												<div className="quant-dim-fill" style={{ width: `${Math.max(0, Math.min(100, dim.score))}%` }} />
											</div>
											{dim.read && <div className="quant-dim-read">{dim.read}</div>}
										</div>
									))}
								</div>
								<div className="quant-index-line">
									现值 {verdict.index?.hs300 ?? '—'} · 距MA60 {fmtPct(verdict.index?.ma60_dev_pct)} · 5日 {fmtPct(verdict.index?.r5_pct)} · 60日 {fmtPct(verdict.index?.r60_pct)} · 距60日高 {fmtPct(verdict.index?.dd60_pct)} · 样本 {verdict.index?.n_valid ?? '—'}
								</div>
								{verdict.verdict?.headline && <div className="quant-headline">{verdict.verdict.headline}</div>}
								{verdict.verdict?.coverage && <p className="quant-note">{verdict.verdict.coverage}</p>}
								{verdict.verdict?.risk && <p className="quant-note">{verdict.verdict.risk}</p>}
							</>
						) : (
							<div className="quant-signal-status error">{verdict?.error || 'verdict 数据不可用（quant_terminal 8766 或 mer/verdict_latest.json 缺失）'}</div>
						)}
					</div>

					{/* ---- 证伪条件 ---- */}
					{verdict?.ok && verdict.verdict?.falsify && (
						<div className="quant-card">
							<div className="quant-card-title"><ShieldCheck size={15} /> 证伪条件（可核验边界）</div>
							<ul className="quant-falsify">
								{Object.entries(verdict.verdict.falsify).map(([key, text]) => (
									<li key={key}><strong>{key}</strong>{text}</li>
								))}
							</ul>
							<p className="quant-note">{verdict.verdict.basis_note || '全部为依据列举，非因果证明。'}</p>
						</div>
					)}

					{/* ---- clip_n300 轨道 ---- */}
					<div className="quant-card">
						<div className="quant-card-title"><Gauge size={15} /> clip_n300 独立前向轨（write-once）</div>
						{track ? (
							<>
								<div className="quant-track-meta">
									锚点 {track.config?.anchor_date || '—'} · 调仓 {track.config?.rebalance_days ?? '—'} 日 · TOP_N {track.config?.selection?.top_n ?? '—'} · 尾部裁剪 q={track.config?.selection?.tail_clip_q ?? '—'} · 目标 {track.oos.target_periods} 期
								</div>
								<div className="quant-track-progress">
									<div className="quant-track-bar">
										<div className="quant-track-fill" style={{ width: `${Math.round((gate?.progress || 0) * 100)}%` }} />
									</div>
									<span>前向已结算 {gate?.forwardSettled ?? 0}/{gate?.target ?? track.oos.target_periods} 期（信号 {gate?.forwardSignals ?? 0} 期）</span>
								</div>
								<table className="quant-track-table">
									<thead>
										<tr><th>期</th><th>信号日</th><th>角色</th><th>证据级</th><th>状态</th><th>持仓</th></tr>
									</thead>
									<tbody>
										{(track.periods || []).map(period => (
											<tr key={period.period}>
												<td>{period.period}</td>
												<td>{period.signal_date}</td>
												<td>{period.role}</td>
												<td><span className={`quant-evidence ${period.evidence_class === 'FORWARD' ? 'forward' : 'pre-anchor'}`}>{period.evidence_class === 'FORWARD' ? 'FORWARD' : 'PRE_ANCHOR'}</span></td>
												<td>{period.status}</td>
												<td>{period.n_holdings}</td>
											</tr>
										))}
										{!track.periods?.length && <tr><td colSpan={6}>轨道尚无信号期</td></tr>}
									</tbody>
								</table>
								{track.settled?.length > 0 && (
									<div className="quant-note">已结算 {track.settled.length} 期（settled/ write-once，绝不回改信号）</div>
								)}
								{(fwd.gross_close2close || fwd.close2close) && (
									<div className="quant-note">
										summary.forward_only：n={sumForwardN(fwd)}（forward 段仅统计 signal_date ≥ {track.oos.anchor_date}；PRE_ANCHOR 段不构成样本外证据）
									</div>
								)}
								{track.config_error && <div className="quant-note warn">配置缺失：{track.config_error}</div>}
							</>
						) : (
							<div className="quant-signal-status error">轨道快照不可用（/api/v1/quant/track 或本机 C:\quant\paper_forward\shadow\clip_n300 缺失）</div>
						)}
					</div>

					{/* ---- OOS 门禁 ---- */}
					{gate && (
						<div className="quant-card quant-card-gate">
							<div className="quant-card-title"><AlertTriangle size={15} /> OOS 门禁 · {gate.state === 'OPEN' ? '已解锁' : '锁定中'}</div>
							<div className={`quant-gate-badge ${gate.state === 'OPEN' ? 'open' : 'locked'}`}>
								{gate.state === 'OPEN' ? '样本外证据充足（≥24 期前向已结算）' : `前向证据累积中：${gate.forwardSettled}/${gate.target} 期 — 在此之前不得宣称样本外有效`}
							</div>
							<div className="quant-oos-timeline" role="img" aria-label="样本可用性时间轴">
								<div className="quant-oos-seg ok" style={{ width: '34%' }}><span>2018-02 起可用<br />（IS 已用尽）</span></div>
								<div className="quant-oos-seg bad" style={{ width: '33%' }}><span>受污染 {gate.contaminatedStart}~{gate.contaminatedEnd}<br />永久失去样本外资格</span></div>
								<div className="quant-oos-seg pending" style={{ width: '33%' }}><span>{gate.contaminatedEnd.slice(0, 4)+1} 前向区间<br />唯一合法验证</span></div>
							</div>
							<p className="quant-note">依据：governance/ADR-20261001 · backtest_5strategies/oos_gate.py。受污染区间内的任何回测数字只描述过去拟合，不构成样本外证据。</p>
						</div>
					)}

					{/* ---- 免责 ---- */}
					{verdict?.disclaimer && (
						<div className="quant-card quant-card-disclaimer">
							<div className="quant-card-title"><AlertTriangle size={15} /> 使用边界</div>
							<p className="quant-note">{verdict.disclaimer}</p>
						</div>
					)}
				</div>
			)}
		</section>
	);
}

function fmtPct(value: number | undefined): string {
	if (typeof value !== 'number' || !Number.isFinite(value)) return '—';
	return `${value > 0 ? '+' : ''}${value.toFixed(2)}%`;
}

function sumForwardN(fwd: Record<string, Record<string, Record<string, number>>>): number {
	let total = 0;
	for (const metric of Object.values(fwd)) {
		for (const capital of Object.values(metric)) {
			total += Number(capital?.n) || 0;
		}
	}
	return total;
}
