import { useCallback, useEffect, useRef, useState } from 'react';
import { FileSpreadsheet, LoaderCircle, Trash2, Upload, Wallet } from 'lucide-react';
import { requestJSON, type BackendConfig, type JournalAnalysis, type JournalHistoryEntry } from '../lib/backend';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
	onOpenSettings: () => void;
};

type Phase = 'idle' | 'loading' | 'ready' | 'error';

const severityLabel: Record<string, string> = { high: '严重', medium: '中等', low: '轻微', none: '无' };

export function TradeJournalWorkspace({ config, refreshKey, onOpenSettings }: Props) {
	const [csvText, setCsvText] = useState('');
	const [filename, setFilename] = useState('');
	const [analysis, setAnalysis] = useState<JournalAnalysis | null>(null);
	const [history, setHistory] = useState<JournalHistoryEntry[]>([]);
	const [phase, setPhase] = useState<Phase>('idle');
	const [analyzing, setAnalyzing] = useState(false);
	const [error, setError] = useState('');
	const [notice, setNotice] = useState('');
	const fileInputRef = useRef<HTMLInputElement>(null);

	const loadWorkspace = useCallback(async () => {
		if (!config) return;
		setPhase('loading');
		try {
			const data = await requestJSON<{ data: JournalHistoryEntry[] }>(config, '/api/v1/trade-journal?limit=12');
			setHistory(data.data ?? []);
			setPhase('ready');
			setError('');
		} catch (loadError) {
			setError(loadError instanceof Error ? loadError.message : String(loadError));
			setPhase('error');
		}
	}, [config]);

	useEffect(() => { void loadWorkspace(); }, [loadWorkspace, refreshKey]);

	const readFile = async (file: File) => {
		// 后端接口上限 8MB，超限文件会导致整个请求被拒，先在前端拦截。
		if (file.size > 8 * 1024 * 1024) {
			setError('文件超过 8MB 上限，请删除无关列后重新导出');
			return;
		}
		const text = await file.text();
		// 券商导出常见 GBK 编码：UTF-8 解码出现替换符时提示用户改用粘贴。
		if (text.includes('\uFFFD') && /[\u4e00-\u9fa5]/.test(text) === false) {
			setNotice('文件疑似 GBK 编码且出现乱码，请用记事本另存为 UTF-8 后重试，或直接粘贴文本');
		}
		setCsvText(text);
		setFilename(file.name);
		setNotice(`已读取 ${file.name}，点击「开始复盘」分析`);
	};

	const analyze = async () => {
		if (!config || !csvText.trim()) return;
		setAnalyzing(true);
		setError('');
		setNotice('');
		try {
			const data = await requestJSON<{ data: JournalAnalysis }>(config, '/api/v1/trade-journal/analyze', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ csv_content: csvText, filename }),
			});
			setAnalysis(data.data);
			setNotice(`复盘完成：解析 ${data.data.imported} 笔成交，配对 ${data.data.trades.length} 个回合`);
			void loadWorkspace();
		} catch (analyzeError) {
			setError(analyzeError instanceof Error ? analyzeError.message : String(analyzeError));
		} finally {
			setAnalyzing(false);
		}
	};

	const openHistory = (entry: JournalHistoryEntry) => {
		if (entry.result) {
			setAnalysis(entry.result);
			setFilename(entry.filename ?? '');
		}
	};

	const removeEntry = async (id: string) => {
		if (!config) return;
		try {
			await requestJSON(config, `/api/v1/trade-journal/${id}`, { method: 'DELETE' });
			if (analysis && history.find((item) => item.id === id)?.result === analysis) setAnalysis(null);
			void loadWorkspace();
		} catch (deleteError) {
			setError(deleteError instanceof Error ? deleteError.message : String(deleteError));
		}
	};

	if (phase === 'error') {
		return <div className="journal-wrap"><div className="portfolio-error">{error}</div><button type="button" className="portfolio-btn" onClick={onOpenSettings}>打开系统设置</button></div>;
	}

	const stats = analysis?.statistics;
	return (
		<div className="journal-wrap">
			{error && <div className="portfolio-error">{error}</div>}
			{notice && <div className="portfolio-notice">{notice}</div>}

			<section className="journal-upload card">
				<div className="journal-upload-head">
					<h3><FileSpreadsheet size={16} /> 导入交割单 CSV</h3>
					<div className="journal-upload-actions">
						<button type="button" className="portfolio-btn secondary" onClick={() => fileInputRef.current?.click()}><Upload size={14} /> 选择文件</button>
						<button type="button" className="portfolio-btn" disabled={analyzing || !csvText.trim()} onClick={analyze}>
							{analyzing ? <LoaderCircle className="spin" size={14} /> : <Wallet size={14} />} 开始复盘
						</button>
					</div>
					<input ref={fileInputRef} type="file" accept=".csv,.txt" hidden onChange={(event) => { const file = event.target.files?.[0]; if (file) void readFile(file); event.target.value = ''; }} />
				</div>
				<textarea
					className="journal-textarea"
					placeholder={'支持同花顺/东方财富/富途/通用格式，需包含：日期、代码、名称(可选)、操作(买入/卖出)、价格、数量。也可直接粘贴文本…'}
					value={csvText}
					onChange={(event) => { setCsvText(event.target.value); setFilename(''); }}
					rows={5}
				/>
				<p className="daily-muted">数据仅保存在本机。FIFO 口径配对买卖，输出胜率、盈亏比、最大回撤与行为偏差诊断（参考 Vibe-Trading Trade Journal）。</p>
			</section>

			{analysis && stats && (
				<>
					<section className="journal-metrics">
						<div className="theme-metric"><span>完整回合</span><strong>{stats.total_trades ?? 0}</strong><small>{analysis.parsed_range?.[0]} ~ {analysis.parsed_range?.[1]}</small></div>
						<div className="theme-metric"><span>胜率</span><strong className={num(stats.win_rate) >= 50 ? 'up' : 'down'}>{num(stats.win_rate).toFixed(1)}%</strong><small>{stats.win_count ?? 0} 胜 {stats.loss_count ?? 0} 负</small></div>
						<div className="theme-metric"><span>盈亏比</span><strong>{stats.payoff_ratio || '--'}</strong><small>均盈 {num(stats.avg_win)} / 均亏 {num(stats.avg_loss)}</small></div>
						<div className="theme-metric"><span>总净盈亏</span><strong className={num(stats.total_net_profit) >= 0 ? 'up' : 'down'}>{num(stats.total_net_profit) >= 0 ? '+' : ''}{num(stats.total_net_profit).toFixed(0)}</strong><small>期望值/笔 {num(stats.expectancy) >= 0 ? '+' : ''}{num(stats.expectancy)}</small></div>
						<div className="theme-metric"><span>最大回撤</span><strong className="down">{num(stats.max_drawdown_pct).toFixed(1)}%</strong><small>峰值回落 {num(stats.max_drawdown).toFixed(0)}</small></div>
						<div className="theme-metric"><span>平均持仓</span><strong>{num(stats.avg_holding_days).toFixed(1)} 天</strong><small>最长 {num(stats.max_holding_days).toFixed(0)} 天</small></div>
						<div className="theme-metric"><span>交易频率</span><strong>{num(stats.trade_per_week).toFixed(1)} 笔/周</strong><small>{stats.active_days ?? 0} 个交易日</small></div>
						<div className="theme-metric"><span>利润因子</span><strong className={num(stats.profit_factor) >= 1 ? 'up' : 'down'}>{stats.profit_factor || '--'}</strong><small>总盈利/总亏损</small></div>
					</section>

					{analysis.biases.length > 0 && (
						<section className="journal-biases card">
							<h3>行为偏差诊断</h3>
							{analysis.biases.map((bias) => (
								<article key={bias.id} className={`journal-bias severity-${bias.severity}`}>
									<header><strong>{bias.label}</strong><span className={`journal-severity s-${bias.severity}`}>{severityLabel[bias.severity] ?? bias.severity}</span></header>
									<p>{bias.evidence}</p>
									<p className="journal-suggestion">→ {bias.suggestion}</p>
								</article>
							))}
						</section>
					)}

					<section className="journal-charts">
						<div className="card journal-curve">
							<h3>累计盈亏曲线</h3>
							<ProfitCurve points={stats.profit_curve} />
						</div>
					</section>

					<section className="card journal-trades">
						<h3>交易明细（按平仓时间）{analysis.skipped > 0 && <small className="daily-muted"> · 跳过 {analysis.skipped} 条无法配对记录</small>}</h3>
						<div className="journal-table-wrap">
							<table className="journal-table">
								<thead><tr><th>代码</th><th>名称</th><th>开仓</th><th>平仓</th><th>持有天数</th><th>数量</th><th>买价</th><th>卖价</th><th>净盈亏</th><th>收益率</th></tr></thead>
								<tbody>
									{analysis.trades.slice(-30).reverse().map((trip, index) => (
										<tr key={index}>
											<td>{trip.symbol}</td><td>{trip.name}</td><td>{trip.open_date?.slice(0, 10)}</td><td>{trip.close_date?.slice(0, 10)}</td>
											<td>{trip.holding_days.toFixed(1)}</td><td>{trip.volume}</td><td>{trip.open_price.toFixed(2)}</td><td>{trip.close_price.toFixed(2)}</td>
											<td className={trip.net_profit >= 0 ? 'up' : 'down'}>{trip.net_profit >= 0 ? '+' : ''}{trip.net_profit.toFixed(0)}</td>
											<td className={trip.profit_rate >= 0 ? 'up' : 'down'}>{trip.profit_rate >= 0 ? '+' : ''}{trip.profit_rate.toFixed(2)}%</td>
										</tr>
									))}
								</tbody>
							</table>
						</div>
						{analysis.open_positions.length > 0 && (
							<details className="journal-open"><summary>未平仓头寸（{analysis.open_positions.length}）</summary>
								<ul>{analysis.open_positions.map((position, index) => <li key={index}>{position.symbol} {position.name} · {position.volume} 股 @ {position.open_price.toFixed(2)} · {position.open_date?.slice(0, 10)}</li>)}</ul>
							</details>
						)}
					</section>
				</>
			)}

			<section className="card journal-history">
				<h3><FileSpreadsheet size={15} /> 历史复盘</h3>
				{history.length === 0 ? <p className="daily-empty">暂无历史记录</p> : (
					<ul className="journal-history-list">
						{history.map((entry) => (
							<li key={entry.id}>
								<button type="button" onClick={() => openHistory(entry)}>
									<span>{entry.filename || entry.analyzed_at.slice(0, 16).replace('T', ' ')}</span>
									<span className="journal-history-meta">{entry.trades} 回合 · 胜率 {entry.win_rate.toFixed(0)}% · 盈亏 {entry.total_profit >= 0 ? '+' : ''}{entry.total_profit.toFixed(0)} · {entry.bias_count} 项偏差</span>
								</button>
								<button type="button" className="journal-delete" aria-label="删除记录" onClick={() => void removeEntry(entry.id)}><Trash2 size={13} /></button>
							</li>
						))}
					</ul>
				)}
			</section>
		</div>
	);
}

function ProfitCurve({ points }: { points: JournalCurvePointForChart[] }) {
	if (points.length < 2) return <p className="daily-empty">数据点不足，无法绘制曲线</p>;
	const values = points.map((point) => point.cumulative);
	const min = Math.min(0, ...values);
	const max = Math.max(0, ...values);
	const span = max - min || 1;
	const width = 640;
	const height = 160;
	const stepX = width / (points.length - 1);
	const toY = (value: number) => height - ((value - min) / span) * (height - 12) - 6;
	const path = points.map((point, index) => `${index === 0 ? 'M' : 'L'}${(index * stepX).toFixed(1)},${toY(point.cumulative).toFixed(1)}`).join(' ');
	const last = values[values.length - 1];
	const zeroY = toY(0);
	return (
		<svg viewBox={`0 0 ${width} ${height}`} className="journal-curve-svg" role="img" aria-label="累计盈亏曲线">
			<line x1={0} x2={width} y1={zeroY} y2={zeroY} className="journal-zero-line" />
			<path d={path} fill="none" strokeWidth={2} className={last >= 0 ? 'journal-line up-stroke' : 'journal-line down-stroke'} />
			<circle cx={(points.length - 1) * stepX} cy={toY(last)} r={3.5} className={last >= 0 ? 'up-fill' : 'down-fill'} />
		</svg>
	);
}

type JournalCurvePointForChart = import('../lib/backend').JournalCurvePoint;

// 后端字段缺失时退回 0，避免单个数值异常导致整页渲染失败。
function num(value: number | undefined): number {
	return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}
