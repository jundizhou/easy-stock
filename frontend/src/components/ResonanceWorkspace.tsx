import { useCallback, useEffect, useState } from 'react';
import { AlertTriangle, LoaderCircle, RefreshCw, ShieldAlert, Sparkles } from 'lucide-react';
import type { BackendConfig } from '../lib/backend';
import { fetchResonanceBoard, type ResonanceAxis, type ResonanceBoard } from '../lib/strategy-boards';
import { logRuntimeEvent } from '../lib/runtime-log';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
};

type LoadState = 'idle' | 'loading' | 'ready' | 'error';

const QUADRANT_LABEL: Record<string, string> = {
	A: 'A · 资讯热 + 资金强',
	B: 'B · 资讯热 + 资金弱',
	C: 'C · 资讯冷 + 资金强',
	D: 'D · 资讯冷 + 资金弱',
	'?': '? · 数据不足',
};

const QUADRANT_CLASS: Record<string, string> = {
	A: 'ok',
	B: 'warn',
	C: 'warn',
	D: 'mute',
	'?': 'mute',
};

function text(value: unknown): string {
	if (value === null || value === undefined || value === '') return '—';
	if (typeof value === 'object') return JSON.stringify(value);
	return String(value);
}

function number(value: unknown, digits = 1): string {
	return typeof value === 'number' ? value.toFixed(digits) : '—';
}

function AxisCell({ axis }: { axis?: ResonanceAxis }) {
	if (!axis) return <>—</>;
	const blocked = axis.status === 'INSUFFICIENT_DATA';
	return (
		<span title={blocked ? axis.reason : undefined}>
			{number(axis.value)}
			{' '}
			<span className={`strategy-badge ${blocked ? 'mute' : axis.is_hot ? 'ok' : 'warn'}`}>
				{blocked ? '不可用' : axis.is_hot ? '热/强' : '不热'}
			</span>
		</span>
	);
}

/**
 * 双确认共振板块（只读）。
 *
 * 数据来源：GET /api/v1/strategy/resonance/board —— 后端只读 news_ferment 产出的
 * 双确认看板.json，不重算、不解读。
 *
 * ⚠ PIT 纪律：资讯轴与行情轴任一未通过 PIT 闸门时，校准后象限一律为「?（数据不足）」，
 * 此时页面顶部给出醒目警告 —— 共振标签不得用于历史绩效评估，不得生成实盘指令。
 */
export function ResonanceWorkspace({ config, refreshKey }: Props) {
	const [board, setBoard] = useState<ResonanceBoard | null>(null);
	const [state, setState] = useState<LoadState>('idle');
	const [error, setError] = useState('');

	const load = useCallback(() => {
		if (!config) return;
		const abort = new AbortController();
		setState(previous => (previous === 'ready' ? previous : 'loading'));
		setError('');
		fetchResonanceBoard(config, { signal: abort.signal })
			.then(payload => {
				if (abort.signal.aborted) return;
				setBoard(payload);
				setState('ready');
			})
			.catch(reason => {
				if (abort.signal.aborted) return;
				logRuntimeEvent('warn', 'resonance', { event: 'board_unavailable' });
				setError(reason instanceof Error ? reason.message : '双确认看板加载失败');
				setState('error');
			});
		return () => abort.abort();
	}, [config]);

	useEffect(() => {
		const cleanup = load();
		return cleanup;
	}, [load, refreshKey]);

	if (!config) return <div className="strategy-empty"><Sparkles size={22} /><span>等待后端配置…</span></div>;

	const payload = board?.payload;
	const market = payload?.market;
	const pit = payload?.pit;
	const calibrated = payload?.calibrated || [];
	const quadrants = payload?.quadrants || [];
	const pitPassed = Boolean(pit?.news_pit_ok && pit?.money_pit_ok);

	return (
		<section className="strategy-panel" aria-label="双确认共振">
			<header className="strategy-heading">
				<div>
					<h2>双确认共振</h2>
					<p>消息热度 × 涨停家数 · 四象限 A/B/C/D · 阈值取当日候选 60 分位</p>
				</div>
				<button type="button" className="strategy-action ghost" onClick={() => load()} disabled={state === 'loading'}>
					<RefreshCw size={14} aria-hidden="true" />刷新
				</button>
			</header>

			{!pitPassed && (
				<div className="strategy-alert">
					<ShieldAlert size={16} aria-hidden="true" />
					<div>
						<strong>PIT 闸门未通过 · 本页仅供观察</strong>
						<div>
							资讯轴 {pit?.news_pit_ok ? '通过' : '未通过'}（新鲜 {text(pit?.available_news)} 条） · 行情轴 {pit?.money_pit_ok ? '通过' : '未通过'}。
							校准后象限已全部置为「? 数据不足」。
						</div>
						<div style={{ marginTop: 4 }}>
							<strong>不得</strong>用于历史交易绩效评估，<strong>不得</strong>生成实盘交易指令。
						</div>
					</div>
				</div>
			)}

			{state === 'loading' && <div className="strategy-card-note"><LoaderCircle size={13} /> 正在读取看板…</div>}
			{state === 'error' && (
				<div className="strategy-alert">
					<AlertTriangle size={15} aria-hidden="true" />
					<div>
						<strong>无法读取双确认看板</strong>
						<div>{error}</div>
						<div style={{ marginTop: 4 }}>先在 <code>C:\quant\news_ferment</code> 运行 <code>report_resonance.py</code>（会同时产出 HTML 与 JSON）。</div>
					</div>
				</div>
			)}

			{market && (
				<div className="strategy-card">
					<h3>行情快照 · {text(payload?.trade_date)} · {text(market.session_status)}</h3>
					<div className="strategy-stat-grid">
						<div className="strategy-stat"><span>涨停家数</span><strong>{text(market.limit_up_count)}</strong></div>
						<div className="strategy-stat"><span>连板家数</span><strong>{text(market.board_count)}</strong></div>
						<div className="strategy-stat"><span>首板家数</span><strong>{text(market.first_board_count)}</strong></div>
						<div className="strategy-stat"><span>最高连板</span><strong>{text(market.max_streak)}</strong></div>
						<div className="strategy-stat"><span>炸板家数</span><strong>{text(market.reopened_count)}</strong></div>
						<div className="strategy-stat"><span>炸板率</span><strong>{typeof market.broken_rate === 'number' ? `${(market.broken_rate * 100).toFixed(1)}%` : '—'}</strong></div>
					</div>
					<p className="strategy-sub">{text(payload?.note)} · 资讯窗口 {text(payload?.window_hours)}h · 看板生成于 {text(payload?.generated_at)}</p>
				</div>
			)}

			{calibrated.length > 0 && (
				<div className="strategy-card">
					<h3>校准后四象限（{calibrated.length}）</h3>
					<p className="strategy-card-note">「热/强」判定为绝对基准 OR 当日截面 60 分位；PIT 闸门未过的轴直接判不可用。</p>
					<div className="strategy-table-wrap">
						<table className="strategy-table">
							<thead><tr><th>题材</th><th>资讯轴</th><th>资金轴</th><th>象限</th><th>原因</th></tr></thead>
							<tbody>
								{calibrated.map(row => (
									<tr key={text(row.bucket)}>
										<td>{text(row.bucket)}</td>
										<td><AxisCell axis={row.news} /></td>
										<td><AxisCell axis={row.money} /></td>
										<td>
											<span className={`strategy-badge ${QUADRANT_CLASS[text(row.quadrant)] || 'mute'}`}>
												{QUADRANT_LABEL[text(row.quadrant)] || text(row.quadrant)}
											</span>
										</td>
										<td style={{ whiteSpace: 'normal' }}>{text(row.news?.reason || row.money?.reason)}</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>
				</div>
			)}

			{quadrants.length > 0 && (
				<div className="strategy-card">
					<h3>原始共振（未校准 · {quadrants.length}）</h3>
					<div className="strategy-table-wrap">
						<table className="strategy-table">
							<thead>
								<tr><th>题材</th><th>象限</th><th>热度</th><th>资讯数</th><th>阶段</th><th>资金分</th><th>涨停</th><th>最高板</th><th>共振分</th><th>龙头</th></tr>
							</thead>
							<tbody>
								{quadrants.map(row => (
									<tr key={text(row.bucket)}>
										<td>{text(row.bucket)}</td>
										<td><span className={`strategy-badge ${QUADRANT_CLASS[text(row.quadrant)] || 'mute'}`}>{text(row.quadrant)}</span></td>
										<td>{number(row.news_heat)}</td>
										<td>{text(row.news_count)}</td>
										<td>{text(row.news_stage)}</td>
										<td>{number(row.money_score)}</td>
										<td>{text(row.limit_up_count)}</td>
										<td>{text(row.max_streak)}</td>
										<td>{number(row.resonance)}</td>
										<td style={{ whiteSpace: 'normal' }}>{(row.leaders || []).slice(0, 4).join(' · ') || '—'}</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>
					<p className="strategy-sub">原始分未经 PIT 闸门与基准校准，只反映当期数值大小，不能作为绩效依据。</p>
				</div>
			)}

			<div className="strategy-alert neutral">
				<Sparkles size={15} aria-hidden="true" />
				<div>{text(payload?.disclaimer) || 'PIT 审计通过前，共振标签仅作观察，不得用于历史绩效评估或实盘指令'}</div>
			</div>
		</section>
	);
}
