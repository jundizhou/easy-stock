import { portfolioScoringVersion } from '../lib/portfolio-optimization';
import {
	Activity,
	CheckCircle2,
	ChevronRight,
	CircleAlert,
	Clock3,
	ExternalLink,
	History,
	HeartPulse,
	GitBranch,
	ListChecks,
	LoaderCircle,
	PieChart,
	Plus,
	RefreshCw,
	Scale,
	ShieldCheck,
	WalletCards,
	type LucideIcon,
} from 'lucide-react';
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import {
	BackendConfig,
	PortfolioInspectionJob,
	PortfolioInspectionReport,
 PortfolioResearchRequest,
	PortfolioTraderProfile,
	StockAIAnalysis,
	StockDirectoryData,
	StockDirectoryEntry,
	requestJSON,
} from '../lib/backend';
import { portfolioDraftToHoldings, portfolioProfiles, readPortfolioDraft, writePortfolioDraft } from '../lib/portfolio-draft';
import { PortfolioSetupForm } from './PortfolioSetupForm';
import { ResearchPanel } from './StockResearchReport';
import { PortfolioAIReportView, originLabel } from './PortfolioAIReport';
import './portfolio-inspection.css';
import { PortfolioOptimizationWorkspace } from './PortfolioOptimizationReport';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
	onOpenSettings: () => void;
	onOpenStockAnalysis: (analysis: StockAIAnalysis) => void;
};

const directoryStorageKey = 'easy-stock.stock-directory.v1';

export function PortfolioInspectionWorkspace({ config, refreshKey, onOpenSettings, onOpenStockAnalysis }: Props) {
	const [draft, setDraft] = useState(readPortfolioDraft);
	const [directory, setDirectory] = useState<StockDirectoryEntry[]>(loadCachedDirectory);
	const [history, setHistory] = useState<PortfolioInspectionJob[]>([]);
	const [job, setJob] = useState<PortfolioInspectionJob | null>(null);
	const [loading, setLoading] = useState(true);
	const [starting, setStarting] = useState(false);
	const [error, setError] = useState('');
	const [notice, setNotice] = useState('');
 const [optimizationSignal,setOptimizationSignal] = useState(0);

	const totalWeight = useMemo(() => draft.holdings.reduce((total, item) => total + item.weight, 0), [draft.holdings]);
	const running = job?.status === 'running';

	const loadWorkspace = useCallback(async () => {
		if (!config) return;
		setLoading(true);
		setError('');
		try {
			const [directoryPayload, jobsPayload] = await Promise.all([
				requestJSON<{ data: StockDirectoryData }>(config, '/api/v1/stocks/directory'),
				requestJSON<{ data: PortfolioInspectionJob[] }>(config, '/api/v1/portfolio-inspections?limit=12'),
			]);
			setDirectory(directoryPayload.data.stocks || []);
			cacheDirectory(directoryPayload.data.stocks || []);
			setHistory(jobsPayload.data || []);
			const active = jobsPayload.data?.find((item) => item.status === 'running');
			if (active) setJob(active);
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : '持仓巡检数据加载失败');
		} finally {
			setLoading(false);
		}
	}, [config]);

	useEffect(() => {
		void loadWorkspace();
	}, [loadWorkspace, refreshKey]);

	useEffect(() => {
		writePortfolioDraft(draft);
	}, [draft]);

	useEffect(() => {
		if (!config || !job || job.status !== 'running') return;
		let active = true;
		const poll = async () => {
			try {
				const payload = await requestJSON<{ data: PortfolioInspectionJob }>(config, `/api/v1/portfolio-inspections/${encodeURIComponent(job.id)}`);
				if (!active) return;
				setJob(payload.data);
				setHistory((current) => [payload.data, ...current.filter((item) => item.id !== payload.data.id)].slice(0, 12));
				if (payload.data.status === 'succeeded') setNotice('持仓 AI 分析已完成，组合评分与报告已保存');
				if (payload.data.status === 'partial') setNotice(payload.data.results.every((r) => r.status === 'succeeded') ? '个股报告已保存，组合评估尚未完成，可重试组合评估' : '部分个股研究未完成，已有报告已保存，可补齐失败个股');
			} catch {
				// Reopening this workspace recovers the persisted task state.
			}
		};
		void poll();
		const timer = window.setInterval(() => void poll(), 3000);
		return () => { active = false; window.clearInterval(timer); };
	}, [config, job?.id, job?.status]);

	const startInspection = async (forceSymbols: string[] = [], existing?: PortfolioResearchRequest) => {
		if (!config || (!existing && (draft.holdings.length === 0 || totalWeight > 100))) return;
		setStarting(true);
		setError('');
		setNotice('');
		try {
			const payload = await requestJSON<{ data: PortfolioInspectionJob }>(config, '/api/v1/portfolio-inspections', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
     ...(existing || { trader_profile: draft.profile, holdings: portfolioDraftToHoldings(draft.holdings), horizon: draft.horizon || 'swing', research_level: draft.researchLevel || 'standard' }),
     force_symbols: forceSymbols,
    }),
			});
			setJob(payload.data);
			setHistory((current) => [payload.data, ...current.filter((item) => item.id !== payload.data.id)].slice(0, 12));
			setNotice('巡检已在后台开始，离开当前页面不会中断，可以先使用其他功能');
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : '持仓巡检启动失败');
		} finally {
			setStarting(false);
		}
	};

 const resumeInspection = async () => {
  if (!config || !job) return; setStarting(true); setError('');
  try { const payload = await requestJSON<{ data: PortfolioInspectionJob }>(config, `/api/v1/portfolio-inspections/${job.id}/resume`, { method: 'POST' }); setJob(payload.data); setHistory((current) => [payload.data, ...current].slice(0, 12)); setNotice('正在恢复任务，已成功个股报告继续复用'); }
  catch (cause) { setError(cause instanceof Error ? cause.message : '恢复失败'); } finally { setStarting(false); }
 };
	const cancelInspection = async () => {
  if (!config || !job) return; setStarting(true);
  try { await requestJSON(config, `/api/v1/portfolio-inspections/${job.id}/cancel`, { method: 'POST' }); setNotice('已停止组合任务，共享个股研究可继续完成'); }
  catch (cause) { setError(cause instanceof Error ? cause.message : '停止失败'); } finally { setStarting(false); }
 };

 const optimization = job?.report && job.status !== 'running' ? <PortfolioOptimizationWorkspace key={job.id} config={config} sourceId={job.id} canStart={job.status === 'succeeded' && job.report.algorithm_version === portfolioScoringVersion && Boolean(job.report.conclusion.score_available)} startSignal={optimizationSignal} onOpenStockAnalysis={onOpenStockAnalysis} onApply={(request) => void startInspection([],request)} onRefreshSource={() => void startInspection([],job.report?.request || job.request)} applyBusy={starting || Boolean(running)} /> : null;

	return <div className="portfolio-inspection-workspace">
		<aside className="portfolio-history stock-ai-panel" aria-label="巡检历史">
			<header><History size={16} /><div><strong>巡检记录</strong><small>本机保存</small></div></header>
			<div>
				{loading && <span className="portfolio-history-empty"><LoaderCircle className="spin" size={17} />正在读取</span>}
				{!loading && history.length === 0 && <span className="portfolio-history-empty">暂无报告</span>}
				{history.map((item) => <button type="button" className={job?.id === item.id ? 'active' : ''} onClick={() => setJob(item)} key={item.id}>
					<span>{item.status === 'running' ? <LoaderCircle className="spin" size={14} /> : item.status === 'succeeded' ? <CheckCircle2 size={14} /> : <CircleAlert size={14} />}</span>
					<div><strong>{item.request.holdings.length} 只持仓 · {profileLabel(item.request.trader_profile)}</strong><small>{formatDate(item.updated_at || item.started_at)}</small></div>
					<ChevronRight size={14} />
				</button>)}
			</div>
		</aside>

		<section className="portfolio-inspection-main">
			{notice && <div className="portfolio-notice" role="status"><CheckCircle2 size={16} /><span>{notice}</span></div>}
			{error && <div className="portfolio-error" role="alert"><CircleAlert size={16} /><span>{error}</span>{error.includes('模型') && <button type="button" onClick={onOpenSettings}>配置模型</button>}</div>}

			{(!job?.report || job.status === 'running') && <>
				<header className="stock-ai-search-hero portfolio-setup-hero"><div><span>持仓 AI 巡检</span><h2>配置持仓，检查组合风险</h2><p>选择交易风格，填写持仓占比与成本后开始巡检。</p></div><WalletCards size={32} aria-hidden="true" /></header>
				<PortfolioSetupForm showResearchOptions draft={draft} directory={directory} disabled={Boolean(running)} busy={starting || Boolean(running)} actionLabel="开始 AI 巡检" busyLabel={running ? '巡检进行中' : '正在启动'} onChange={setDraft} onSubmit={() => void startInspection()} />
			</>}

			{job?.status === 'running' && <><InspectionProgress job={job} /><button type="button" className="portfolio-task-action" disabled={starting} onClick={() => void cancelInspection()}>停止持仓分析</button></>}
			{job?.resume_available && !running && <div className="portfolio-report-warning portfolio-recovery"><CircleAlert size={16} /><span>{job.error || '上次任务未完成，已有报告已保存'}</span><button type="button" disabled={starting} onClick={() => void resumeInspection()}>{job.results.every((r) => r.status === 'succeeded') ? '重试组合评估' : '补齐失败个股'}</button></div>}
   {job?.report && job.status !== 'running' && (['portfolio-ai-score-v3', portfolioScoringVersion].includes(job.report.algorithm_version || '')
    ? <PortfolioAIReportView report={job.report} afterSummary={optimization} busy={starting} onOptimize={job.status === 'succeeded' && job.report.algorithm_version === portfolioScoringVersion && job.report.conclusion.score_available ? () => setOptimizationSignal((n) => n+1) : undefined} onNew={() => setJob(null)} onOpenStockAnalysis={onOpenStockAnalysis} onRefresh={(symbol) => void startInspection([symbol], job.report?.request || job.request)} />
    : <PortfolioReportView report={job.report} afterSummary={optimization} status={job.status} onNew={() => setJob(null)} onOpenStockAnalysis={onOpenStockAnalysis} />)}

		</section>
	</div>;
}

function InspectionProgress({ job }: { job: PortfolioInspectionJob }) {
	const progress = job.total_stocks > 0 ? Math.round(job.completed_stocks / job.total_stocks * (job.stage === 'aggregating' ? 85 : 75)) : 3;
	const displayProgress = job.stage === 'aggregating' ? Math.max(86, progress) : Math.max(5, progress);
	return <section className="portfolio-progress stock-ai-panel" role="status" aria-live="polite">
		<div className="portfolio-progress-icon"><RefreshCw className="spin" size={24} /></div>
		<div><span>后台任务 · {stageLabel(job.stage)}</span><strong>巡检耗时较长，可以先使用其他功能</strong><p>{job.message}</p><div className="portfolio-progress-bar"><i style={{ width: `${displayProgress}%` }} /><small>{job.completed_stocks}/{job.total_stocks} 只 · {displayProgress}%</small></div>{job.current_symbols?.length > 0 && <em>正在处理 {job.current_symbols.join('、')}</em>}</div>
		<ul className="portfolio-progress-stocks">{job.results.map((r) => <li key={r.holding.symbol}><b>{r.holding.name || r.holding.symbol}</b><span>{r.status === 'succeeded' ? '完成' : r.status === 'failed' ? '失败' : r.status === 'queued' ? '排队' : r.status === 'resolving' ? '查找报告' : '研究中'} · {originLabel(r.research_origin)}</span>{r.error && <small>{r.error}</small>}</li>)}</ul>
  <small><Clock3 size={14} />离开页面不会中断 · 已耗时 {elapsedLabel(job.started_at)}{job.aggregation_started_at && ` · 组合评估 ${elapsedLabel(job.aggregation_started_at)}`}</small>
	</section>;
}

function PortfolioReportView({ report, status, onNew, onOpenStockAnalysis, afterSummary }: { report: PortfolioInspectionReport; afterSummary?: ReactNode; status: string; onNew: () => void; onOpenStockAnalysis: (analysis: StockAIAnalysis) => void }) {
	const { conclusion, metrics } = report;
	const isV2 = report.algorithm_version === 'portfolio-health-v2';
	const names = new Map(report.holdings.map((item) => [item.holding.symbol, item.analysis?.name || item.holding.name || item.holding.symbol]));
	const analyses = new Map<string, StockAIAnalysis>();
	report.holdings.forEach((item) => {
		if (!item.analysis || item.status !== 'succeeded') return;
		analyses.set(item.holding.symbol, item.analysis);
		analyses.set(item.analysis.symbol, item.analysis);
		analyses.set(item.holding.symbol.split('.')[0], item.analysis);
		analyses.set(item.analysis.symbol.split('.')[0], item.analysis);
	});
	return <div className="portfolio-report stock-research-report">
		<header className={`stock-ai-verdict portfolio-report-verdict ${conclusion.risk_level.includes('高') || conclusion.risk_level.includes('中') ? 'risk' : 'strong'}`}>
			<div className="stock-ai-identity"><span>持仓 AI 巡检报告</span><h2>{report.profile.label}型组合</h2><small>{formatDate(report.generated_at)}</small></div>
			<div className="stock-ai-conclusion"><div className="stock-ai-tags"><span>{conclusion.risk_level}风险</span><span>覆盖 {metrics.coverage_percent}%</span><span>{conclusion.source === 'hermes-ai' ? 'AI 综合研判' : '本地规则研判'}</span></div><h3>巡检结论</h3><p>{conclusion.executive_summary}</p></div>
			<div className="stock-ai-verdict-actions"><button type="button" className="primary" onClick={onNew}><Plus size={15} />新建巡检</button></div>
		</header>
		{afterSummary}
		{status === 'partial' && <div className="portfolio-report-warning"><CircleAlert size={15} />报告已生成，但部分个股或组合分析使用降级结果。</div>}
		{metrics.stop_loss_coverage_percent !== undefined && metrics.stop_loss_coverage_percent < 100 && <div className="portfolio-report-warning"><CircleAlert size={15} />有效静态止损仅覆盖 {metrics.stop_loss_coverage_percent}% 的持仓；其余风险未估算，不能视为零风险。</div>}
		{isV2 ? <section className="stock-ai-kpis portfolio-report-overview" aria-label="组合量化指标">
			<PortfolioMetric icon={HeartPulse} label="组合健康度" value={metrics.health_score_available ? conclusion.health_score : '—'} detail={metrics.health_score_available ? '量化基线评分' : '覆盖不足，暂不评分'} tone="blue" />
			<PortfolioMetric icon={Scale} label="个股质量" value={metrics.weighted_stock_score.toFixed(1)} detail="健康度权重 45%" tone="purple" />
			<PortfolioMetric icon={ShieldCheck} label="风险韧性" value={(metrics.stop_loss_coverage_percent ?? 100) < 70 ? '待补证' : metrics.risk_resilience_score} detail={`权重 25% · ${(metrics.stop_loss_coverage_percent ?? 100) < 100 ? `止损覆盖 ${metrics.stop_loss_coverage_percent}%` : `止损风险 ${metrics.stop_loss_risk_percent.toFixed(2)}%`}`} tone="amber" />
			<PortfolioMetric icon={PieChart} label="分散 / 风格" value={`${metrics.diversification_score} / ${metrics.style_match_score}`} detail={`${conclusion.style_match} · 持仓 ${metrics.total_position_percent}% / 现金 ${metrics.cash_percent}%`} tone="green" />
		</section> : <section className="stock-ai-kpis portfolio-report-overview" aria-label="组合量化指标">
			<PortfolioMetric icon={HeartPulse} label="组合健康度" value={conclusion.health_score} detail="历史评分" tone="blue" />
			<PortfolioMetric icon={Scale} label="风格匹配" value={conclusion.style_match} detail={`${metrics.style_match_score} 分`} tone="purple" />
			<PortfolioMetric icon={PieChart} label="持仓 / 现金" value={`${metrics.total_position_percent}% / ${metrics.cash_percent}%`} detail="当前配置" tone="green" />
			<PortfolioMetric icon={ShieldCheck} label="预估止损风险" value={(metrics.stop_loss_coverage_percent ?? 100) < 100 ? '覆盖不完整' : `${metrics.stop_loss_risk_percent.toFixed(2)}%`} detail="占组合资产" tone="amber" />
		</section>}

		<div className="portfolio-report-grid">
			<ResearchPanel label="风险识别" title="主要风险" icon={CircleAlert} className="stock-research-counter"><ListItems items={conclusion.primary_risks} empty="没有识别到突出风险" /></ResearchPanel>
			<ResearchPanel label="组合结构" title="集中与联动" icon={Activity}><ListItems items={conclusion.concentration_findings} empty="没有识别到明显集中风险" /></ResearchPanel>
		</div>

		<section className="portfolio-holding-report" aria-label="逐股巡检"><header><div><span>个股观察</span><h3>逐股巡检</h3></div><small>按组合风险贡献排序</small></header><div className="portfolio-holding-cards">
			{conclusion.holdings.map((item) => {
				const stockAnalysis = analyses.get(item.symbol) || analyses.get(item.symbol.split('.')[0]);
				return <ResearchPanel key={item.symbol} label={item.symbol} title={names.get(item.symbol) || item.symbol} icon={WalletCards} className="portfolio-holding-card" action={stockAnalysis && <button type="button" onClick={() => onOpenStockAnalysis(stockAnalysis)} aria-label={`查看${stockAnalysis.name}个股分析报告`} title="查看已生成的个股分析报告"><ExternalLink size={14} />查看个股报告</button>}>
					<div className="portfolio-holding-meta"><span className={`portfolio-priority ${priorityTone(item.action_priority)}`}>{item.action_priority}</span><span>{item.portfolio_role}</span><span>风险贡献 {item.risk_contribution.toFixed(1)}%</span></div>
					<p>{item.conclusion}</p>
					<div className="portfolio-holding-conditions"><div><strong>动作</strong><p>{item.action}</p></div><div><strong>确认条件</strong><p>{item.confirmation}</p></div><div className="invalidation"><strong>失效条件</strong><p>{item.invalidation}</p></div></div>
				</ResearchPanel>;
			})}
		</div></section>

		<div className="portfolio-report-grid">
			<ResearchPanel label="执行顺序" title="处理顺序" icon={ListChecks}><ListItems items={conclusion.adjustment_order} empty="暂无调整事项" ordered /></ResearchPanel>
			<ResearchPanel label="后续核验" title="下次检查" icon={ShieldCheck}><ListItems items={conclusion.next_checklist} empty="暂无检查事项" /></ResearchPanel>
		</div>
		<ResearchPanel label="条件推演" title="组合情景" icon={GitBranch}><div className="stock-research-scenarios">{conclusion.scenarios.map((scenario) => <article key={scenario.name}><strong>{scenario.name}</strong><p>{scenario.condition}</p><p>{scenario.portfolio_action}</p></article>)}</div></ResearchPanel>
		{conclusion.data_limitations.length > 0 && <ResearchPanel label="研究边界" title="数据限制" icon={CircleAlert}><ListItems items={conclusion.data_limitations} empty="" /></ResearchPanel>}
		<footer className="stock-research-footer">仅用于信息整理、研究与复盘，不构成投资建议或交易指令。</footer>
	</div>;
}

function PortfolioMetric({ icon: Icon, label, value, detail, tone }: { icon: LucideIcon; label: string; value: string | number; detail: string; tone: string }) {
	return <article className={`stock-ai-kpi ${tone}`}><div><Icon size={17} aria-hidden="true" />{label}</div><strong>{value}</strong><small>{detail}</small></article>;
}

function ListItems({ items, empty, ordered = false }: { items: string[]; empty: string; ordered?: boolean }) {
	const Tag = ordered ? 'ol' : 'ul';
	return items.length ? <Tag>{items.map((item, index) => <li key={`${index}-${item}`}>{item}</li>)}</Tag> : <p className="portfolio-list-empty">{empty}</p>;
}

function loadCachedDirectory(): StockDirectoryEntry[] {
	try {
		const parsed = JSON.parse(window.localStorage.getItem(directoryStorageKey) || '{}');
		return Array.isArray(parsed.stocks) ? parsed.stocks : [];
	} catch {
		return [];
	}
}

function cacheDirectory(stocks: StockDirectoryEntry[]) {
	try { window.localStorage.setItem(directoryStorageKey, JSON.stringify({ cachedAt: Date.now(), stocks })); } catch { /* in-memory directory remains available */ }
}

function stageLabel(stage: string) {
	if (stage === 'queued') return '排队准备';
 if (stage === 'resolving_reports') return '查找可复用报告';
	if (stage === 'analyzing_stocks') return '逐股分析';
	if (stage === 'aggregating') return '组合研判';
	return '处理中';
}

function profileLabel(profile: PortfolioTraderProfile) {
	return portfolioProfiles.find((item) => item.id === profile)?.label || '均衡';
}

function formatDate(value?: string) {
	if (!value) return '刚刚';
	const date = new Date(value);
	return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
}

function priorityTone(value: string) {
	if (value === '优先处理') return 'danger';
	if (value === '保持') return 'positive';
	return 'neutral';
}

function elapsedLabel(value?: string) { if (!value) return '0 分钟'; const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000)); return `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`; }
