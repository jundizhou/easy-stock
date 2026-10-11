import { useCallback, useEffect, useRef, useState } from 'react';
import { AlertTriangle, LoaderCircle, RefreshCw, Waves } from 'lucide-react';
import type { BackendConfig } from '../lib/backend';
import {
	fetchInflectionAuto,
	type InflectionAutoResponse,
	type InflectionSignal,
} from '../lib/strategy-boards';
import { logRuntimeEvent } from '../lib/runtime-log';

type Props = {
	config: BackendConfig | null;
	refreshKey?: number;
};

type LoadState = 'idle' | 'loading' | 'ready' | 'error';

const KIND_LABEL: Record<string, string> = {
	market_exhaustion: '市场出清',
	sentiment_repair: '情绪修复',
	high_low_switch: '高低切换',
	sector_rotation: '板块轮动',
	individual_reversal: '个股反转',
	trend_reversal: '趋势反转',
	index_reversal: '指数反转',
	expectation_gap: '预期差',
};

const ANCHOR_LABEL: Record<string, string> = {
	old_profit: '旧盈利锚',
	old_negative: '旧负面锚',
	new_carrier: '新载体',
};

const STATUS_CLASS: Record<string, string> = {
	confirmed: 'ok',
	candidate: 'warn',
	none: 'mute',
};

const STATUS_LABEL: Record<string, string> = {
	confirmed: '已确认',
	candidate: '预期（未确认）',
	none: '无信号',
};

const TYPE_LABEL: Record<string, string> = {
	big: '大拐点',
	small: '小拐点',
	none: '无',
};

function text(value: unknown): string {
	if (value === null || value === undefined || value === '') return '—';
	if (typeof value === 'object') return JSON.stringify(value);
	return String(value);
}

function number(value: unknown): string {
	return typeof value === 'number' ? value.toFixed(1) : '—';
}

function SignalCard({ title, signal }: { title: string; signal?: InflectionSignal }) {
	if (!signal) return null;
	return (
		<div className="strategy-card">
			<h3>
				{title}
				{' '}
				<span className={`strategy-badge ${STATUS_CLASS[signal.status] || 'mute'}`}>{STATUS_LABEL[signal.status] || text(signal.status)}</span>
			</h3>
			<div className="strategy-stat-grid" style={{ marginBottom: 10 }}>
				<div className="strategy-stat"><span>信号强度</span><strong>{number(signal.score)}</strong></div>
				<div className="strategy-stat"><span>旧锚</span><strong>{text(signal.old_anchor_symbol)}</strong></div>
				<div className="strategy-stat"><span>新载体</span><strong>{text(signal.new_carrier_symbol)}</strong></div>
				<div className="strategy-stat"><span>形态</span><strong>{(signal.kinds || []).map(kind => KIND_LABEL[kind] || kind).join(' · ') || '—'}</strong></div>
			</div>
			{(signal.factors || []).length > 0 && (
				<div className="strategy-table-wrap" style={{ marginBottom: 8 }}>
					<table className="strategy-table">
						<thead><tr><th>判据</th><th>得分</th><th>权重</th><th>说明</th></tr></thead>
						<tbody>
							{signal.factors.map(factor => (
								<tr key={factor.name}>
									<td>{factor.name}</td>
									<td>{number(factor.score)}</td>
									<td>{number(factor.weight)}</td>
									<td style={{ whiteSpace: 'normal' }}>{text(factor.detail)}</td>
								</tr>
							))}
						</tbody>
					</table>
				</div>
			)}
			{(signal.evidence || []).length > 0 && (
				<>
					<strong style={{ fontSize: 12 }}>证据</strong>
					<ul className="strategy-list">{signal.evidence!.map((item, index) => <li key={index}>{item}</li>)}</ul>
				</>
			)}
			{(signal.risks || []).length > 0 && (
				<>
					<strong style={{ fontSize: 12 }}>风险</strong>
					<ul className="strategy-list">{signal.risks!.map((item, index) => <li key={index}>{item}</li>)}</ul>
				</>
			)}
		</div>
	);
}

/**
 * 拐点识别引擎板块（只读评估）。
 *
 * 数据来源：POST /api/v1/strategy/inflections/auto —— 后端用当日/前一日涨停梯队
 * 与盘中高标指标自动装配快照后调用引擎，前端不传任何参数。
 * 引擎 V1 只做单快照评估：不下单、不回测、不生成交易指令。
 */
export function InflectionWorkspace({ config, refreshKey }: Props) {
	const [response, setResponse] = useState<InflectionAutoResponse | null>(null);
	const [state, setState] = useState<LoadState>('idle');
	const [error, setError] = useState('');
	const evaluatedRef = useRef(false);

	const run = useCallback(() => {
		if (!config) return;
		setState('loading');
		setError('');
		fetchInflectionAuto(config)
			.then(payload => {
				evaluatedRef.current = true;
				setResponse(payload);
				setState('ready');
			})
			.catch(reason => {
				logRuntimeEvent('warn', 'inflection', { event: 'auto_evaluate_failed' });
				setError(reason instanceof Error ? reason.message : '拐点评估失败');
				setState('error');
			});
	}, [config]);

	// 首次进入仍需手动点击评估（POST 会装配当日快照）；已评估过则随 refreshKey 自动重评。
	useEffect(() => {
		if (!evaluatedRef.current) return;
		run();
	}, [refreshKey, run]);

	if (!config) return <div className="strategy-empty"><Waves size={22} /><span>等待后端配置…</span></div>;

	const evaluation = response?.data;
	const source = response?.source;
	const assumptions = response?.assumptions || [];
	const candidates = evaluation?.candidates || [];
	const anchors = evaluation?.anchors || [];

	return (
		<section className="strategy-panel" aria-label="拐点识别引擎">
			<header className="strategy-heading">
				<div>
					<h2>拐点识别引擎</h2>
					<p>旧共识锚出清 → 新载体接收流动性 · 大拐点四要素 gate · 单快照评估</p>
				</div>
				<button type="button" className="strategy-action" onClick={run} disabled={state === 'loading'}>
					{state === 'loading' ? <LoaderCircle size={14} aria-hidden="true" /> : <RefreshCw size={14} aria-hidden="true" />}
					{state === 'loading' ? '评估中…' : '用当日梯队评估'}
				</button>
			</header>

			{state === 'idle' && (
				<div className="strategy-alert neutral">
					<Waves size={15} aria-hidden="true" />
					<div>点击右上角按钮开始评估。后端会用当日涨停梯队自动装配市场环境与候选锚，无需输入。</div>
				</div>
			)}
			{state === 'error' && (
				<div className="strategy-alert">
					<AlertTriangle size={15} aria-hidden="true" />
					<div><strong>评估失败</strong><div>{error}</div></div>
				</div>
			)}

			{source && (
				<div className="strategy-card">
					<h3>装配来源</h3>
					<div className="strategy-stat-grid">
						<div className="strategy-stat"><span>当日 / 前一日</span><strong>{text(source.current_trade_date)} / {text(source.previous_trade_date)}</strong></div>
						<div className="strategy-stat"><span>涨停家数</span><strong>{text(source.limit_up_count)}</strong></div>
						<div className="strategy-stat"><span>最高板 / 前日最高板</span><strong>{text(source.max_streak)} / {text(source.previous_max_streak)}</strong></div>
						<div className="strategy-stat"><span>候选数</span><strong>{text(source.candidate_count)}</strong></div>
						<div className="strategy-stat"><span>场次状态</span><strong>{text(source.session_status)}</strong></div>
					</div>
				</div>
			)}

			{evaluation && (
				<>
					<div className="strategy-card">
						<h3>
							主信号
							{' '}
							<span className={`strategy-badge ${evaluation.primary_signal === 'none' ? 'mute' : 'ok'}`}>
								{TYPE_LABEL[evaluation.primary_signal || 'none'] || text(evaluation.primary_signal)}
							</span>
						</h3>
						<div className="strategy-stat-grid">
							<div className="strategy-stat"><span>市场承压分</span><strong>{number(evaluation.market_stress_score)}</strong></div>
							<div className="strategy-stat"><span>环境转向分</span><strong>{number(evaluation.environment_turn_score)}</strong></div>
						</div>
						{(evaluation.warnings || []).length > 0 && (
							<ul className="strategy-list" style={{ marginTop: 8 }}>{evaluation.warnings!.map((item, index) => <li key={index}>{item}</li>)}</ul>
						)}
					</div>

					<div className="strategy-columns">
						<SignalCard title="大拐点（旧周期出清）" signal={evaluation.big} />
						<SignalCard title="小拐点（高低切换 / 个股反转）" signal={evaluation.small} />
					</div>

					{anchors.length > 0 && (
						<div className="strategy-card">
							<h3>锚定物选择</h3>
							<div className="strategy-table-wrap">
								<table className="strategy-table">
									<thead><tr><th>类型</th><th>选中</th><th>识别度</th><th>出清分</th><th>次选</th><th>差距</th><th>出清</th></tr></thead>
									<tbody>
										{anchors.map(anchor => (
											<tr key={anchor.kind}>
												<td>{ANCHOR_LABEL[anchor.kind || ''] || text(anchor.kind)}</td>
												<td>{anchor.selected?.candidate?.name || anchor.selected?.candidate?.symbol || '—'}</td>
												<td>{number(anchor.selected?.recognition_score)}</td>
												<td>{number(anchor.selected?.clearance_score)}</td>
												<td>{anchor.runner_up?.candidate?.name || anchor.runner_up?.candidate?.symbol || '—'}</td>
												<td>{number(anchor.score_gap)}</td>
												<td>{anchor.clear ? '是' : '否'}</td>
											</tr>
										))}
									</tbody>
								</table>
							</div>
						</div>
					)}

					{candidates.length > 0 && (
						<div className="strategy-card">
							<h3>候选评估（{candidates.length}）</h3>
							<div className="strategy-table-wrap">
								<table className="strategy-table">
									<thead>
										<tr><th>类型</th><th>名称</th><th>代码</th><th>识别度</th><th>主动走弱</th><th>主动走强</th><th>出清</th><th>涨跌%</th></tr>
									</thead>
									<tbody>
										{candidates.map((item, index) => (
											<tr key={`${item.candidate?.symbol}-${index}`}>
												<td>{ANCHOR_LABEL[item.candidate?.kind || ''] || text(item.candidate?.kind)}</td>
												<td>{text(item.candidate?.name)}</td>
												<td>{text(item.candidate?.symbol)}</td>
												<td>{number(item.recognition_score)}</td>
												<td>{number(item.active_weakness_score)}</td>
												<td>{number(item.active_strength_score)}</td>
												<td>{number(item.clearance_score)}</td>
												<td>{number(item.candidate?.change_percent)}</td>
											</tr>
										))}
									</tbody>
								</table>
							</div>
						</div>
					)}
				</>
			)}

			{assumptions.length > 0 && (
				<div className="strategy-card">
					<h3>装配口径与缺省声明</h3>
					<p className="strategy-card-note">下列字段无法从现有数据源可靠获得或只能近似，判据因此偏保守，请勿当作精确输入解读。</p>
					<ul className="strategy-list">{assumptions.map((item, index) => <li key={index}>{item}</li>)}</ul>
				</div>
			)}

			<div className="strategy-alert neutral">
				<Waves size={15} aria-hidden="true" />
				<div>引擎 V1 只做<strong>单快照评估</strong>：不下单、不回测、不生成交易指令。「已确认」指四要素 gate 全部通过，不等于历史已验证有效。</div>
			</div>
		</section>
	);
}
