import { useCallback, useEffect, useState } from 'react';
import { AlertTriangle, LoaderCircle, Lock, PlayCircle, RefreshCw, Send, ShieldAlert, ShieldCheck, Target } from 'lucide-react';
import type { BackendConfig } from '../lib/backend';
import {
	fetchExecutionStatus,
	fetchExecutionTarget,
	postExecutionIntent,
	postExecutionKillSwitch,
	type ExecutionFill,
	type ExecutionIntentAck,
	type ExecutionPlanTarget,
	type ExecutionStatus,
	type ExecutionTargetPlan,
} from '../lib/strategy-boards';
import { logRuntimeEvent } from '../lib/runtime-log';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
};

type LoadState = 'idle' | 'loading' | 'ready' | 'error';

function text(value: unknown): string {
	if (value === null || value === undefined) return '—';
	if (typeof value === 'object') return JSON.stringify(value);
	return String(value);
}

function pct(value: number | undefined): string {
	return typeof value === 'number' ? `${(value * 100).toFixed(1)}%` : '—';
}

/**
 * 执行网关板块（P0+P1：sim 模式，不接真实下单）。
 *
 * 数据来源：Go 后端 /api/v1/execution/status 与 /api/v1/execution/target。
 * 后端网关从 paper_forward 的 write-once 静态文件读取 core2 最新期，产出可执行
 * 目标权重表；仅 core2 在 PIT 白名单内，共振/拐点/双确认不进执行。
 *
 * 本页所有动作均经后端护栏（PIT 白名单、急停、生产开关、单票≤10%）：
 *  - 受理 sim 意图：后端生成模拟成交（不触达任何券商）；
 *  - live 意图：后端守卫拒绝（适配层未接线 / 生产开关未开）；
 *  - 拉闸：拒绝一切下单动作。
 */
export function ExecutionWorkspace({ config, refreshKey }: Props) {
	const [status, setStatus] = useState<ExecutionStatus | null>(null);
	const [plan, setPlan] = useState<ExecutionTargetPlan | null>(null);
	const [loadState, setLoadState] = useState<LoadState>('idle');
	const [error, setError] = useState('');
	const [ack, setAck] = useState<ExecutionIntentAck | null>(null);
	const [submitting, setSubmitting] = useState(false);
	const [killBusy, setKillBusy] = useState(false);
	const [actionMsg, setActionMsg] = useState('');
	// 就绪后再刷新保留内容不卸载（避免闪烁），另用独立标志给按钮反馈。
	const [refreshing, setRefreshing] = useState(false);

	const load = useCallback(() => {
		if (!config) return;
		const abort = new AbortController();
		setLoadState(previous => (previous === 'ready' ? previous : 'loading'));
		setError('');
		setRefreshing(true);
		setAck(null);
		const statusReq = fetchExecutionStatus(config, { signal: abort.signal });
		const targetReq = fetchExecutionTarget(config, 'core2', { signal: abort.signal });
		Promise.all([statusReq, targetReq])
			.then(([statusPayload, targetPayload]) => {
				setRefreshing(false);
				if (abort.signal.aborted) return;
				setStatus((statusPayload as { status?: ExecutionStatus }).status || null);
				setPlan((targetPayload as { plan?: ExecutionTargetPlan }).plan || null);
				setLoadState('ready');
			})
			.catch(reason => {
				setRefreshing(false);
				if (abort.signal.aborted) return;
				logRuntimeEvent('warn', 'execution', { event: 'load_failed' });
				setError(reason instanceof Error ? reason.message : '执行网关加载失败');
				setLoadState('error');
			});
		return () => abort.abort();
	}, [config]);

	useEffect(() => {
		const cleanup = load();
		return cleanup;
	}, [load, refreshKey]);

	const acceptSim = useCallback(() => {
		if (!config || !plan) return;
		const abort = new AbortController();
		setSubmitting(true);
		setActionMsg('');
		setAck(null);
		const intent = {
			schema_version: plan.schema_version,
			strategy: plan.strategy,
			pit_audit_passed: plan.pit_audit_passed,
			signal_id: plan.signal_id,
			execute_on: 'sim',
			account: '',
			targets: (plan.targets || []).map((t: ExecutionPlanTarget) => ({
				code: t.code,
				target_weight: t.target_weight,
				assumed_price: t.assumed_price,
			})),
			constraints: {
				max_single_weight: 0.10,
				max_daily_turnover: 0.30,
				round_lot: 100,
				block_st: true,
				block_suspended: true,
				block_limit: true,
			},
		};
		postExecutionIntent(config, intent, { signal: abort.signal })
			.then(payload => {
				if (abort.signal.aborted) return;
				setAck((payload as { ack?: ExecutionIntentAck }).ack || null);
				setActionMsg((payload as { ack?: ExecutionIntentAck }).ack?.message || '已受理');
			})
			.catch(reason => {
				if (abort.signal.aborted) return;
				setActionMsg(reason instanceof Error ? reason.message : '受理失败');
			})
			.finally(() => {
				if (!abort.signal.aborted) setSubmitting(false);
			});
		return () => abort.abort();
	}, [config, plan]);

	const toggleKillSwitch = useCallback((engaged: boolean) => {
		if (!config) return;
		const abort = new AbortController();
		setKillBusy(true);
		postExecutionKillSwitch(config, engaged, { signal: abort.signal })
			.then(() => {
				if (abort.signal.aborted) return;
				load();
			})
			.catch(reason => {
				if (abort.signal.aborted) return;
				setActionMsg(reason instanceof Error ? reason.message : '急停操作失败');
			})
			.finally(() => {
				if (!abort.signal.aborted) setKillBusy(false);
			});
		return () => abort.abort();
	}, [config, load]);

	if (!config) return <div className="strategy-empty"><Target size={22} /><span>等待后端配置…</span></div>;

	const targets = plan?.targets || [];
	const fills = ack?.fills || [];
	const gross = plan?.gross_weight || 0;
	const mode = status?.mode || 'sim';

	return (
		<section className="strategy-panel" aria-label="执行网关">
			<header className="strategy-heading">
				<div>
					<h2>执行网关 · {mode === 'live' ? '实盘' : 'sim 模拟'}</h2>
					<p>core2 信号 → 券商 QMT / Ptrade 执行通道 · PIT 白名单仅 core2 · 单票 ≤ 10%</p>
				</div>
			<button type="button" className="strategy-action ghost" onClick={() => load()} disabled={refreshing || loadState === 'loading'}>
				<RefreshCw size={14} className={refreshing ? 'spin' : undefined} aria-hidden="true" />{refreshing ? '同步中…' : '刷新'}
			</button>
			</header>

			{loadState === 'loading' && <div className="strategy-card-note"><LoaderCircle size={13} /> 正在读取网关状态与目标…</div>}
			{loadState === 'error' && (
				<div className="strategy-alert">
					<AlertTriangle size={15} aria-hidden="true" />
					<div>
						<strong>无法读取执行网关</strong>
						<div>{error}</div>
						<div style={{ marginTop: 4 }}>确认后端已启用执行网关（internal/execution）并监听 /api/v1/execution/*。</div>
					</div>
				</div>
			)}

			{status && (
				<div className="strategy-card">
					<h3>网关状态</h3>
					<dl className="strategy-kv">
						<div style={{ display: 'contents' }}>
							<dt>执行模式</dt>
							<dd><span className={`pill ${mode === 'live' ? 'live' : 'sim'}`}>{mode === 'live' ? '实盘 LIVE' : 'sim 模拟'}</span></dd>
						</div>
						<div style={{ display: 'contents' }}>
							<dt>生产开关</dt>
							<dd>{status.production_enabled ? '已开启（A_STOCK_PRODUCTION_ENABLED）' : '未开启'}</dd>
						</div>
						<div style={{ display: 'contents' }}>
							<dt>急停开关</dt>
							<dd>
								{status.kill_switch_engaged
									? <span className="pill danger">已拉闸</span>
									: <span className="pill ok">正常</span>}
							</dd>
						</div>
						<div style={{ display: 'contents' }}>
							<dt>券商适配层</dt>
							<dd>{status.adapter_ready ? text(status.adapter) : '未就绪（P2 待实现）'}</dd>
						</div>
						<div style={{ display: 'contents' }}>
							<dt>审计记录</dt>
							<dd>{status.audit_count ?? 0} 条 · append-only JSONL</dd>
						</div>
						<div style={{ display: 'contents' }}>
							<dt>PIT 白名单</dt>
							<dd>{(status.pit_whitelist || []).join(' / ')}</dd>
						</div>
					</dl>
					{(status.warnings || []).map((w, i) => (
						<p key={i} className="strategy-card-note warn"><AlertTriangle size={12} /> {w}</p>
					))}
				</div>
			)}

			{plan && (
				<div className="strategy-card">
					<h3>可执行目标 · core2 最新期（{text(plan.period)}）</h3>
					<div className="strategy-stat-grid" style={{ marginBottom: 10 }}>
						<div className="strategy-stat"><span>信号指纹</span><strong style={{ fontSize: 13 }}>{text(plan.signal_id)}</strong></div>
						<div className="strategy-stat"><span>信号日</span><strong>{text(plan.signal_date)}</strong></div>
						<div className="strategy-stat"><span>执行日</span><strong>{text(plan.execute_date)}</strong></div>
						<div className="strategy-stat"><span>持仓数</span><strong>{text(plan.n_holdings)}</strong></div>
						<div className="strategy-stat"><span>合计权重</span><strong>{pct(gross)}</strong></div>
						<div className="strategy-stat"><span>PIT 审计</span><strong>{plan.pit_audit_passed ? '通过' : '未过'}</strong></div>
					</div>
					{targets.length > 0 ? (
						<div className="strategy-table-wrap">
							<table className="strategy-table">
								<thead>
									<tr>
										<th>#</th><th>代码</th><th>目标权重</th><th>假定成交价</th><th>整手股数</th><th>预计金额</th><th>预过滤</th>
									</tr>
								</thead>
								<tbody>
									{targets.map((t, index) => (
										<tr key={`${t.code}-${index}`}>
											<td>{index + 1}</td>
											<td>{text(t.code)}</td>
											<td>{pct(t.target_weight)}</td>
											<td>{t.assumed_price?.toFixed(2)}</td>
											<td>{t.shares}</td>
											<td>{t.est_notional?.toFixed(2)}</td>
											<td>{t.prefilter ? <span className="pill warn">{t.prefilter}</span> : <span className="pill ok">放行</span>}</td>
										</tr>
									))}
								</tbody>
							</table>
						</div>
					) : (
						<p className="strategy-card-note">该期无持仓明细。</p>
					)}
					{(plan.warnings || []).map((w, i) => (
						<p key={i} className="strategy-card-note warn"><AlertTriangle size={12} /> {w}</p>
					))}
					<p className="strategy-sub">整手股数 = round(假定金额 / 假定价 / 100)；预计金额 = 整手股数 × 假定价。假定价取信号日收盘价，实盘按 T+1 开盘价执行。</p>
				</div>
			)}

			<div className="strategy-card">
				<h3>执行动作（sim 预览）</h3>
				<div className="execution-actions">
					<button type="button" className="strategy-action primary" onClick={acceptSim} disabled={submitting || !plan}>
						<PlayCircle size={15} aria-hidden="true" />{submitting ? '受理中…' : '受理 sim 执行（模拟成交）'}
					</button>
					{status?.kill_switch_engaged ? (
						<button type="button" className="strategy-action" onClick={() => toggleKillSwitch(false)} disabled={killBusy}>
							<ShieldCheck size={15} aria-hidden="true" />解除拉闸
						</button>
					) : (
						<button type="button" className="strategy-action danger" onClick={() => toggleKillSwitch(true)} disabled={killBusy}>
							<Lock size={15} aria-hidden="true" />紧急拉闸
						</button>
					)}
				</div>
				{actionMsg && <p className="strategy-card-note">{actionMsg}</p>}
			</div>

			{ack && (
				<div className="strategy-card">
					<h3>受理回执 · {ack.accepted ? <span className="pill ok">已受理</span> : <span className="pill danger">未受理</span>}</h3>
					<div className="strategy-stat-grid" style={{ marginBottom: 10 }}>
						<div className="strategy-stat"><span>意图编号</span><strong style={{ fontSize: 13 }}>{text(ack.intent_id)}</strong></div>
						<div className="strategy-stat"><span>执行方式</span><strong>{text(ack.execute_on)}</strong></div>
						<div className="strategy-stat"><span>模拟成交</span><strong>{fills.length} 笔</strong></div>
					</div>
					{(ack.warnings || []).map((w, i) => (
						<p key={i} className="strategy-card-note warn"><AlertTriangle size={12} /> {w}</p>
					))}
					{fills.length > 0 && (
						<div className="strategy-table-wrap">
							<table className="strategy-table">
								<thead>
									<tr><th>代码</th><th>方向</th><th>价格</th><th>股数</th><th>金额</th><th>来源</th></tr>
								</thead>
								<tbody>
									{fills.map((f: ExecutionFill, i) => (
										<tr key={`${f.fill_id || i}`}>
											<td>{text(f.code)}</td>
											<td><span className={`pill ${f.side === 'BUY' ? 'buy' : 'sell'}`}>{f.side}</span></td>
											<td>{f.price?.toFixed(2)}</td>
											<td>{f.shares}</td>
											<td>{f.notional?.toFixed(2)}</td>
											<td>{text(f.source)}</td>
										</tr>
									))}
								</tbody>
							</table>
						</div>
					)}
					<p className="strategy-sub">以上为<strong>模拟成交</strong>，未触达任何券商；仅用于验证信号 → 订单契约链路。实盘需 P2 适配层 + 生产开关 + 未拉闸。</p>
				</div>
			)}

			<div className="strategy-alert neutral">
				<ShieldAlert size={15} aria-hidden="true" />
				<div>
					P0+P1 为<strong>纯工程预览</strong>：网关默认 sim 模式，<strong>不接真实下单</strong>；live 执行被护栏守卫拒绝（适配层未接线 / 生产开关未开）。
					共振 / 拐点 / 双确认板块不进执行链。任何绩效结论须先通过 PIT 审计。
				</div>
			</div>
		</section>
	);
}
