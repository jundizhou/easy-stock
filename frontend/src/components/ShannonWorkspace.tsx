import { useCallback, useEffect, useState } from 'react';
import {
	Activity,
	AlertTriangle,
	LoaderCircle,
	RefreshCw,
	TrendingUp,
	TrendingDown,
	Layers,
} from 'lucide-react';
import type { BackendConfig } from '../lib/backend';
import {
	ALIGN_LABEL,
	BUCKET_LABEL,
	LIGHT_LABEL,
	TREND_LABEL,
	fetchShannonAnalyze,
	fetchShannonBoards,
	fetchShannonScan,
	type ShannonPlanPick,
	type ShannonResult,
	type ShannonScanResponse,
} from '../lib/shannon';
import { logRuntimeEvent } from '../lib/runtime-log';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
};

type ScanState = 'idle' | 'loading' | 'ready' | 'error';
type UniverseMode = 'all' | 'board' | 'codes';

const BUCKET_CLASS: Record<string, string> = {
	ENTRY: 'bucket-entry',
	WATCH: 'bucket-watch',
	LOW_RR: 'bucket-lowrr',
	CHASE: 'bucket-chase',
	NO_STRUCT: 'bucket-nostruct',
	SIZE_ZERO: 'bucket-sizezero',
	L2_BLOCK: 'bucket-l2block',
	RED: 'bucket-red',
	OTHER: 'bucket-other',
	ERROR: 'bucket-error',
};

const LIGHT_CLASS: Record<string, string> = {
	GREEN: 'light-green',
	YELLOW: 'light-yellow',
	RED: 'light-red',
};

export function ShannonWorkspace({ config, refreshKey }: Props) {
	const [equity, setEquity] = useState<number>(100000);
	// 默认「指定代码」：进入工作台不自动发起全市场扫描（5000+ 只，慢且浪费后端资源）。
	const [mode, setMode] = useState<UniverseMode>('codes');
	const [board, setBoard] = useState('');
	// 预填示例代码，首次点击「扫描」即可看到结果。
	const [codesText, setCodesText] = useState('600519, 000001, 300750');
	const [boards, setBoards] = useState<string[]>([]);
	const [response, setResponse] = useState<ShannonScanResponse | null>(null);
	const [state, setState] = useState<ScanState>('idle');
	const [error, setError] = useState('');
	const [expanded, setExpanded] = useState<Set<string>>(new Set());

	const loadBoards = useCallback(() => {
		if (!config) return;
		fetchShannonBoards(config, { signal: new AbortController().signal }).then(setBoards).catch(() => {});
	}, [config]);

	const runScan = useCallback(() => {
		if (!config) return;
		const abort = new AbortController();
		setState('loading');
		setError('');
		const request: Record<string, unknown> = { equity, workers: 8 };
		if (mode === 'codes') {
			const codes = codesText
				.split(/[\s,]+/)
				.map((c) => c.trim())
				.filter(Boolean);
			request.codes = codes;
		} else if (mode === 'board') {
			request.board = board;
		} else {
			request.universe = 'all';
		}
		fetchShannonScan(config, request as never, { signal: abort.signal })
			.then((payload) => {
				if (abort.signal.aborted) return;
				setResponse(payload);
				setState('ready');
			})
			.catch((reason) => {
				if (abort.signal.aborted) return;
				logRuntimeEvent('warn', 'strategy', { event: 'shannon_scan_failed', error: String(reason) });
				setError(reason instanceof Error ? reason.message : '扫描失败');
				setState('error');
			});
		return () => abort.abort();
	}, [config, equity, mode, board, codesText]);

	useEffect(() => {
		loadBoards();
	}, [loadBoards, refreshKey]);

	// 注意：扫描完全由用户点击「扫描」按钮触发，不随挂载/参数变化自动执行。

	const toggleExpand = (code: string) => {
		setExpanded((previous) => {
			const next = new Set(previous);
			if (next.has(code)) next.delete(code);
			else next.add(code);
			return next;
		});
	};

	const plan = response?.plan;

	if (!config) {
		return (
			<div className="quant-signal-empty">
				<Activity size={22} />
				<span>等待后端配置…</span>
			</div>
		);
	}

	return (
		<section className="shannon" aria-label="Brian Shannon 六层规则工作台">
			<header className="quant-signal-heading">
				<div>
					<h2>Brian Shannon 六层规则</h2>
					<p>
						Anchored VWAP · 5日线极性 · 多周期对齐 · 红绿灯 · 三步入场 · 仓位阶梯 ·
						来源 /api/v1/strategy/shannon/scan（Free-StockDB 7899）
					</p>
				</div>
				<button type="button" className="icon-button" onClick={runScan} aria-label="重新扫描">
					<RefreshCw size={16} aria-hidden="true" />
				</button>
			</header>

			<div className="shannon-controls">
				<label className="shannon-field">
					<span>账户净值（元）</span>
					<input
						type="number"
						className="shannon-input"
						value={equity}
						min={10000}
						step={10000}
						onChange={(event) => setEquity(Number(event.target.value) || 0)}
					/>
				</label>
				<label className="shannon-field">
					<span>扫描范围</span>
					<select
						className="shannon-input"
						value={mode}
						onChange={(event) => setMode(event.target.value as UniverseMode)}
					>
						<option value="all">全市场</option>
						<option value="board">板块</option>
						<option value="codes">指定代码</option>
					</select>
				</label>
				{mode === 'board' && (
					<label className="shannon-field shannon-field-wide">
						<span>板块</span>
						<select className="shannon-input" value={board} onChange={(event) => setBoard(event.target.value)}>
							<option value="">— 选择板块 —</option>
							{boards.map((name) => (
								<option key={name} value={name}>
									{name}
								</option>
							))}
						</select>
					</label>
				)}
				{mode === 'codes' && (
					<label className="shannon-field shannon-field-wide">
						<span>代码（逗号或空格分隔）</span>
						<textarea
							className="shannon-input shannon-codes"
							value={codesText}
							placeholder="600519, 000001, 300750"
							onChange={(event) => setCodesText(event.target.value)}
							rows={1}
						/>
					</label>
				)}
				<button type="button" className="shannon-scan-btn" onClick={runScan} disabled={state === 'loading'}>
					{state === 'loading' ? <LoaderCircle size={16} className="spin" /> : <Layers size={16} />}
					扫描
				</button>
			</div>

			{state === 'idle' && (
				<div className="quant-signal-status" role="status">
					设定净值与扫描范围后，点击「扫描」开始
				</div>
			)}
			{state === 'loading' && (
				<div className="quant-signal-status" role="status">
					<LoaderCircle size={16} className="spin" /> 扫描中…
				</div>
			)}
			{state === 'error' && (
				<div className="quant-signal-status error" role="alert">
					{error || '扫描失败'}
				</div>
			)}

			{state === 'ready' && response && (
				<div className="shannon-body">
					{plan && (
						<div className="quant-card shannon-plan">
							<div className="quant-card-title">
								<TrendingUp size={15} /> 组合资金预算
							</div>
							<div className="shannon-plan-grid">
								<PlanStat label="净值" value={`¥${plan.equity.toLocaleString('zh-CN')}`} />
								<PlanStat label="可入场候选" value={`${plan.candidates}`} />
								<PlanStat label="实际建仓" value={`${plan.picked.length}/${plan.max_positions}`} />
								<PlanStat label="总仓位%" value={`${plan.total_position_pct.toFixed(2)}%`} />
								<PlanStat label="总风险%" value={`${plan.total_risk_pct.toFixed(3)}%`} />
								<PlanStat label="现金%" value={`${plan.cash_pct.toFixed(2)}%`} />
							</div>
							{plan.stopped_by && (
								<div className="shannon-stopped">
									<AlertTriangle size={14} /> {plan.stopped_by}
								</div>
							)}
							{plan.picked.length > 0 && (
								<table className="shannon-pick-table">
									<thead>
										<tr>
											<th>代码</th>
											<th>名称</th>
											<th>仓位%</th>
											<th>股数</th>
											<th>金额</th>
											<th>风险%</th>
										</tr>
									</thead>
									<tbody>
										{plan.picked.map((pick: ShannonPlanPick) => (
											<tr key={pick.code}>
												<td>{pick.code}</td>
												<td>{pick.name || '—'}</td>
												<td>{pick.pos_pct.toFixed(2)}</td>
												<td>{pick.shares}</td>
												<td>¥{pick.notional.toLocaleString('zh-CN')}</td>
												<td>{pick.risk_pct.toFixed(3)}</td>
											</tr>
										))}
									</tbody>
								</table>
							)}
						</div>
					)}

					<div className="quant-card">
						<div className="quant-card-title">
							<Activity size={15} /> 扫描结果（按评分排序，共 {response.count} 只）
							{response.errors && response.errors.length > 0 && (
								<span className="shannon-err-count"> · {response.errors.length} 只取数失败</span>
							)}
						</div>
						<div className="shannon-table-wrap">
							<table className="shannon-table">
								<thead>
									<tr>
										<th>代码</th>
										<th>名称</th>
										<th>分组</th>
										<th>灯</th>
										<th>趋势</th>
										<th>对齐</th>
										<th>评分</th>
										<th>收盘</th>
										<th>仓位%</th>
										<th>股数</th>
										<th></th>
									</tr>
								</thead>
								<tbody>
									{response.results.map((r: ShannonResult) => (
										<ShannonRow
											key={r.code}
											result={r}
											open={expanded.has(r.code)}
											onToggle={() => toggleExpand(r.code)}
										/>
									))}
									{response.results.length === 0 && (
										<tr>
											<td colSpan={11} className="shannon-empty-row">
												当前范围无符合条件的标的
											</td>
										</tr>
									)}
								</tbody>
							</table>
						</div>
					</div>
				</div>
			)}
		</section>
	);
}

function PlanStat({ label, value }: { label: string; value: string }) {
	return (
		<div className="shannon-plan-stat">
			<span>{label}</span>
			<strong>{value}</strong>
		</div>
	);
}

function ShannonRow({
	result,
	open,
	onToggle,
}: {
	result: ShannonResult;
	open: boolean;
	onToggle: () => void;
}) {
	const bucket = result.bucket || 'OTHER';
	const light = result.light?.light || 'YELLOW';
	return (
		<>
			<tr className="shannon-row" onClick={onToggle}>
				<td>{result.code}</td>
				<td>{result.name || '—'}</td>
				<td>
					<span className={`shannon-badge ${BUCKET_CLASS[bucket] || 'bucket-other'}`}>
						{BUCKET_LABEL[bucket] || bucket}
					</span>
				</td>
				<td>
					<span className={`shannon-light ${LIGHT_CLASS[light] || 'light-yellow'}`}>
						{LIGHT_LABEL[light] || light}
					</span>
				</td>
				<td>{TREND_LABEL[result.trend?.state] || result.trend?.state || '—'}</td>
				<td>{ALIGN_LABEL[result.alignment?.alignment] || result.alignment?.alignment || '—'}</td>
				<td className="shannon-score">{result.score.toFixed(1)}</td>
				<td>{result.close ?? '—'}</td>
				<td>{result.sizing?.position_pct.toFixed(2)}</td>
				<td>{result.sizing?.total_shares}</td>
				<td className="shannon-caret">{open ? '▾' : '▸'}</td>
			</tr>
			{open && (
				<tr className="shannon-detail-row">
					<td colSpan={11}>
						<ShannonDetail result={result} />
					</td>
				</tr>
			)}
		</>
	);
}

function ShannonDetail({ result }: { result: ShannonResult }) {
	const t = result.trend;
	const p = result.polarity;
	const a = result.alignment;
	const e = result.entry;
	const s = result.sizing;
	return (
		<div className="shannon-detail">
			<div className="shannon-detail-grid">
				<DetailCard title="L1 趋势结构">
					<Line k="状态" v={TREND_LABEL[t.state] || t.state} />
					<Line k="更高高" v={t.higher_highs ? '✓' : '✗'} />
					<Line k="更高低" v={t.higher_lows ? '✓' : '✗'} />
					<Line k="最近摆动低" v={t.last_swing_low ?? '—'} />
					<Line k="最近摆动高" v={t.last_swing_high ?? '—'} />
				</DetailCard>
				<DetailCard title="L2 5日线极性">
					<Line k="极性" v={p.polarity} />
					<Line k="MA5" v={p.ma5 ?? '—'} />
					<Line k="斜率%" v={p.slope_pct ?? '—'} />
					<Line k="价在MA5上" v={p.price_above_ma5 ? '✓' : '✗'} />
					<Line k="硬过滤拦截" v={p.is_filter_blocking ? '🚫' : '—'} />
				</DetailCard>
				<DetailCard title="L3 多周期对齐">
					<Line k="对齐" v={ALIGN_LABEL[a.alignment] || a.alignment} />
					<Line k="月/周/日" v={`${a.monthly}/${a.weekly}/${a.daily}`} />
					<Line k="对齐分" v={`${a.score}（多${a.ups}/空${a.downs}）`} />
					<Line k="满仓系数" v={a.allow_full_size ? '✓' : '—'} />
					<Line k="仓位缩放" v={a.position_scale} />
				</DetailCard>
				<DetailCard title="L4 红绿灯 + L5 入场">
					<Line k="灯色" v={LIGHT_LABEL[result.light.light] || result.light.light} />
					<Line k="有效入场" v={e.valid ? '✓' : '✗'} />
					<Line k="触发位" v={e.trigger ?? '—'} />
					<Line k="止损位" v={e.stop ?? '—'} />
					<Line k="RR" v={e.rr_ratio ?? '—'} />
					<Line k="结构OK" v={e.structure_ok ? '✓' : '✗'} />
					{e.reason && <Line k="理由" v={e.reason} wide />}
				</DetailCard>
				<DetailCard title="L6 仓位与止损阶梯">
					<Line k="净值" v={`¥${s.equity.toLocaleString('zh-CN')}`} />
					<Line k="名义仓位%" v={s.position_pct.toFixed(2)} />
					<Line k="实际风险%" v={s.risk_pct_actual.toFixed(3)} />
					<Line k="股数" v={s.total_shares} />
					<Line k="金额" v={`¥${s.notional.toLocaleString('zh-CN')}`} />
					<Line k="生效约束" v={s.binding_constraint} />
					{s.tranches?.length > 0 && <Line k="分批股数" v={s.tranches.join(' / ')} wide />}
				</DetailCard>
				<DetailCard title="信号源">
					<div className="shannon-sources">{result.sources.join(' · ')}</div>
				</DetailCard>
			</div>
			{e.steps?.length > 0 && (
				<div className="shannon-steps">
					<strong>三步入场模板</strong>
					<ol>{e.steps.map((step, i) => <li key={i}>{step}</li>)}</ol>
				</div>
			)}
			{s.stop_rules?.length > 0 && (
				<div className="shannon-steps">
					<strong>止损规则</strong>
					<ul>{s.stop_rules.map((rule, i) => <li key={i}>{rule}</li>)}</ul>
				</div>
			)}
		</div>
	);
}

function DetailCard({ title, children }: { title: string; children: React.ReactNode }) {
	return (
		<div className="shannon-detail-card">
			<div className="shannon-detail-title">{title}</div>
			{children}
		</div>
	);
}

function Line({ k, v, wide }: { k: string; v: React.ReactNode; wide?: boolean }) {
	return (
		<div className={`shannon-line ${wide ? 'wide' : ''}`}>
			<span className="shannon-line-k">{k}</span>
			<span className="shannon-line-v">{String(v)}</span>
		</div>
	);
}
