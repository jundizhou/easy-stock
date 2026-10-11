import { useCallback, useEffect, useMemo, useState } from 'react';
import { AlertTriangle, LoaderCircle, RefreshCw, Target } from 'lucide-react';
import type { BackendConfig } from '../lib/backend';
import { fetchCore2Paper, type Core2Paper } from '../lib/strategy-boards';
import { useStockNameMap } from '../lib/stock-directory';
import { fetchSymbolsBoards, type SymbolBoardRef } from '../lib/sidecars';
import { logRuntimeEvent } from '../lib/runtime-log';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
};

type LoadState = 'idle' | 'loading' | 'ready' | 'error';

const SPEC_ROWS: Array<[string, string]> = [
	['score', '打分公式'],
	['rev20', '反转因子 rev20'],
	['amihud', '非流动性 amihud'],
	['universe', '股票池'],
	['pre_screen', '财务排雷'],
	['rebalance', '调仓周期'],
	['execution', '成交时点'],
	['position', '仓位规则'],
];

function text(value: unknown): string {
	if (value === null || value === undefined) return '—';
	if (typeof value === 'object') return JSON.stringify(value);
	return String(value);
}

/**
 * core2 中线量化板块（只读）。
 *
 * 数据来源：Go 后端 /api/v1/strategy/core2/paper —— 只读 paper_forward 的
 * write-once 静态文件（periods/state/params_lock），不在前端触发任何选股计算，
 * 因此不存在「界面看到的是现算的」这类 PIT 污染。
 *
 * 展示的是**信号与前向记录**，不含交易指令；系统 PRODUCTION_ENABLED=false，
 * 本页也不会生成任何下单动作。
 */
export function Core2Workspace({ config, refreshKey }: Props) {
	const [paper, setPaper] = useState<Core2Paper | null>(null);
	const [state, setState] = useState<LoadState>('idle');
	const [error, setError] = useState('');
	// 就绪后再刷新保留内容不卸载（避免闪烁），另用独立标志给按钮反馈。
	const [refreshing, setRefreshing] = useState(false);
	const names = useStockNameMap(config);
	// 持仓板块归属（stockdb 批量反查，后端进程级索引 O(1)）；仅最新一期 10 只，失败静默降级。
	const [boards, setBoards] = useState<Record<string, SymbolBoardRef[]>>({});

	const load = useCallback(() => {
		if (!config) return;
		const abort = new AbortController();
		setState(previous => (previous === 'ready' ? previous : 'loading'));
		setError('');
		setRefreshing(true);
		fetchCore2Paper(config, { signal: abort.signal })
			.then(payload => {
				setRefreshing(false);
				if (abort.signal.aborted) return;
				setPaper(payload);
				setState('ready');
			})
			.catch(reason => {
				setRefreshing(false);
				if (abort.signal.aborted) return;
				logRuntimeEvent('warn', 'core2', { event: 'paper_unavailable' });
				setError(reason instanceof Error ? reason.message : 'core2 前向记录加载失败');
				setState('error');
			});
		return () => abort.abort();
	}, [config]);

	useEffect(() => {
		const cleanup = load();
		return cleanup;
	}, [load, refreshKey]);

	const latestHoldings = paper?.latest?.holdings;
	const holdingCodes = useMemo(
		() => (Array.isArray(latestHoldings) ? latestHoldings!.map(holding => holding.code).filter(Boolean) : []),
		[latestHoldings],
	);
	const holdingKey = holdingCodes.join(',');
	useEffect(() => {
		if (!config || !holdingKey) return;
		let cancelled = false;
		fetchSymbolsBoards(config, holdingKey.split(','), 6)
			.then(payload => { if (!cancelled) setBoards(payload); })
			.catch(() => { if (!cancelled) setBoards({}); });
		return () => { cancelled = true; };
	}, [config, holdingKey]);

	const boardsFor = useCallback((code: string): string => {
		const refs = boards[code];
		if (!refs || !refs.length) return '';
		return refs.slice(0, 3).map(ref => ref.name).join(' · ');
	}, [boards]);

	if (!config) return <div className="strategy-empty"><Target size={22} /><span>等待后端配置…</span></div>;

	const spec = (paper?.spec || {}) as Record<string, unknown>;
	const latest = paper?.latest;
	const holdings = Array.isArray(latest?.holdings) ? latest!.holdings! : [];
	const periods = paper?.periods || [];

	return (
		<section className="strategy-panel" aria-label="core2 中线量化">
			<header className="strategy-heading">
				<div>
					<h2>core2 中线量化</h2>
					<p>沪深主板 · rev20 反转 + amihud 非流动性双因子 · 20 个交易日定期换仓</p>
				</div>
				<button type="button" className="strategy-action ghost" onClick={() => load()} disabled={refreshing || state === 'loading'}>
					<RefreshCw size={14} className={refreshing ? 'spin' : undefined} aria-hidden="true" />{refreshing ? '同步中…' : '刷新'}
				</button>
			</header>

			{state === 'loading' && <div className="strategy-card-note"><LoaderCircle size={13} /> 正在读取前向记录…</div>}
			{state === 'error' && (
				<div className="strategy-alert">
					<AlertTriangle size={15} aria-hidden="true" />
					<div>
						<strong>无法读取 paper 前向记录</strong>
						<div>{error}</div>
						<div style={{ marginTop: 4 }}>确认 <code>C:\quant\paper_forward</code> 存在 periods/ 与 state/ 目录，或设置 A_STOCK_CORE2_PAPER_DIR。</div>
					</div>
				</div>
			)}

			<div className="strategy-card">
				<h3>冻结口径（core2_v1.yaml · {text(spec.version)} · {text(spec.status)}）</h3>
				<dl className="strategy-kv">
					{SPEC_ROWS.map(([key, label]) => (
						<div key={key} style={{ display: 'contents' }}>
							<dt>{label}</dt>
							<dd>{text(spec[key])}</dd>
						</div>
					))}
					<div style={{ display: 'contents' }}>
						<dt>top_n</dt>
						<dd>{text(spec.top_n)}（候选不足 {text(spec.min_candidates)} 只则持币）</dd>
					</div>
					<div style={{ display: 'contents' }}>
						<dt>出场规则</dt>
						<dd>{text(spec.exit_rules)}</dd>
					</div>
				</dl>
				<p className="strategy-sub">规范文件：{text(spec.spec_file)}</p>
			</div>

			{latest && (
				<div className="strategy-card">
					<h3>最新一期信号 · {text(latest.signal_date)}</h3>
					<div className="strategy-stat-grid" style={{ marginBottom: 10 }}>
						<div className="strategy-stat"><span>期次 / 角色</span><strong>{text(latest.period)} · {text(latest.role)}</strong></div>
						<div className="strategy-stat"><span>状态</span><strong>{text(latest.status)}</strong></div>
						<div className="strategy-stat"><span>持仓数</span><strong>{text(latest.n_holdings)}</strong></div>
						<div className="strategy-stat"><span>总权重</span><strong>{text(latest.gross_weight)}</strong></div>
						<div className="strategy-stat"><span>执行日</span><strong>{text(latest.execute_date)}</strong></div>
					</div>
					{holdings.length > 0 ? (
						<div className="strategy-table-wrap">
							<table className="strategy-table">
								<thead>
									<tr>
										<th>#</th><th>代码</th><th>名称</th><th>所属板块</th><th>信号分</th><th>目标权重</th><th>假设成交价</th>
									</tr>
								</thead>
								<tbody>
									{holdings.map((holding, index) => {
										const code = holding.code || '';
										return (
											<tr key={`${code}-${index}`}>
												<td>{index + 1}</td>
												<td>{code || '—'}</td>
												<td style={{ whiteSpace: 'nowrap' }}>{code ? names.get(code) || '—' : '—'}</td>
												<td style={{ whiteSpace: 'normal' }}>{code ? boardsFor(code) || '—' : '—'}</td>
												<td>{holding.signal_score !== undefined ? holding.signal_score.toFixed(3) : '—'}</td>
												<td>{holding.target_weight !== undefined ? `${(holding.target_weight * 100).toFixed(1)}%` : '—'}</td>
												<td>{holding.assumed_price !== undefined ? holding.assumed_price.toFixed(2) : '—'}</td>
											</tr>
										);
									})}
								</tbody>
							</table>
						</div>
					) : (
						<p className="strategy-card-note">该期无持仓明细。</p>
					)}
					<p className="strategy-sub">价格基准 ASSUMED_T_CLOSE = 以信号日收盘价假设成交（后复权口径），实盘按 T+1 开盘价执行 · 板块归属来自 Free-StockDB，仅供参考。</p>
				</div>
			)}

			<div className="strategy-card">
				<h3>历史期次（{periods.length}）</h3>
				{periods.length > 0 ? (
					<div className="strategy-table-wrap">
						<table className="strategy-table">
							<thead>
								<tr><th>期次</th><th>信号日</th><th>执行日</th><th>角色</th><th>状态</th><th>持仓数</th></tr>
							</thead>
							<tbody>
								{[...periods].reverse().map(period => (
									<tr key={text(period.file)}>
										<td>{text(period.period)}</td>
										<td>{text(period.signal_date)}</td>
										<td>{text(period.execute_date)}</td>
										<td>{text(period.role)}</td>
										<td>{text(period.status)}</td>
										<td>{text(period.n_holdings)}</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>
				) : (
					<p className="strategy-card-note">尚无期次记录。</p>
				)}
			</div>

			<div className="strategy-alert neutral">
				<Target size={15} aria-hidden="true" />
				<div>本页为前向信号记录，<strong>不是交易指令</strong>；系统未开启实盘（PRODUCTION_ENABLED=false）。任何绩效结论须先通过 PIT 审计。</div>
			</div>
		</section>
	);
}
