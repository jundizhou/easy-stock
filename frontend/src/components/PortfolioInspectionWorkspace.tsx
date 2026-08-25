import {
	Activity,
	BrainCircuit,
	CheckCircle2,
	ChevronRight,
	CircleAlert,
	Clock3,
	ExternalLink,
	History,
	LoaderCircle,
	Plus,
	RefreshCw,
	Search,
	ShieldCheck,
	Trash2,
	WalletCards,
} from 'lucide-react';
import { KeyboardEvent, useCallback, useEffect, useMemo, useState } from 'react';
import {
	BackendConfig,
	PortfolioInspectionJob,
	PortfolioInspectionReport,
	PortfolioTraderProfile,
	StockAIAnalysis,
	StockDirectoryData,
	StockDirectoryEntry,
	requestJSON,
} from '../lib/backend';
import { resolveStockDirectorySymbol, searchStockDirectory } from '../lib/stock-analysis';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
	onOpenSettings: () => void;
	onOpenStockAnalysis: (analysis: StockAIAnalysis) => void;
};

type DraftHolding = {
	symbol: string;
	name: string;
	weight: number;
	costPrice: string;
};

const directoryStorageKey = 'easy-stock.stock-directory.v1';
const draftStorageKey = 'easy-stock.portfolio-inspection-draft.v1';
const maxHoldings = 10;

const profiles: Array<{ id: PortfolioTraderProfile; label: string; description: string; constraint: string }> = [
	{ id: 'aggressive', label: '激进', description: '短线机会与弹性优先', constraint: '单票参考上限 45%' },
	{ id: 'balanced', label: '均衡', description: '收益与回撤保持平衡', constraint: '单票参考上限 35%' },
	{ id: 'steady', label: '稳重', description: '本金保护与趋势确认优先', constraint: '单票参考上限 25%' },
];

export function PortfolioInspectionWorkspace({ config, refreshKey, onOpenSettings, onOpenStockAnalysis }: Props) {
	const initialDraft = useMemo(loadDraft, []);
	const [profile, setProfile] = useState<PortfolioTraderProfile>(initialDraft.profile);
	const [holdings, setHoldings] = useState<DraftHolding[]>(initialDraft.holdings);
	const [directory, setDirectory] = useState<StockDirectoryEntry[]>(loadCachedDirectory);
	const [query, setQuery] = useState('');
	const [suggestionsOpen, setSuggestionsOpen] = useState(false);
	const [activeSuggestion, setActiveSuggestion] = useState(0);
	const [history, setHistory] = useState<PortfolioInspectionJob[]>([]);
	const [job, setJob] = useState<PortfolioInspectionJob | null>(null);
	const [loading, setLoading] = useState(true);
	const [starting, setStarting] = useState(false);
	const [error, setError] = useState('');
	const [notice, setNotice] = useState('');

	const totalWeight = useMemo(() => holdings.reduce((total, item) => total + item.weight, 0), [holdings]);
	const remainingWeight = 100 - totalWeight;
	const suggestions = useMemo(() => searchStockDirectory(directory, query).filter((item) => !holdings.some((holding) => holding.symbol === item.symbol)), [directory, holdings, query]);
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
		window.localStorage.setItem(draftStorageKey, JSON.stringify({ profile, holdings }));
	}, [holdings, profile]);

	useEffect(() => {
		if (!config || !job || job.status !== 'running') return;
		let active = true;
		const poll = async () => {
			try {
				const payload = await requestJSON<{ data: PortfolioInspectionJob }>(config, `/api/v1/portfolio-inspections/${encodeURIComponent(job.id)}`);
				if (!active) return;
				setJob(payload.data);
				setHistory((current) => [payload.data, ...current.filter((item) => item.id !== payload.data.id)].slice(0, 12));
				if (payload.data.status === 'succeeded') setNotice('持仓 AI 巡检已完成，报告已保存在本机');
				if (payload.data.status === 'partial') setNotice('巡检报告已生成，部分个股或组合分析使用降级结果');
			} catch {
				// Reopening this workspace recovers the persisted task state.
			}
		};
		void poll();
		const timer = window.setInterval(() => void poll(), 3000);
		return () => { active = false; window.clearInterval(timer); };
	}, [config, job?.id, job?.status]);

	const addHolding = (entry?: StockDirectoryEntry) => {
		const symbol = entry?.symbol || resolveStockDirectorySymbol(query, directory);
		const stock = entry || directory.find((item) => item.symbol === symbol);
		if (!symbol || !stock) {
			setError('未找到唯一匹配的股票，请从搜索结果中选择');
			return;
		}
		if (holdings.some((item) => item.symbol === symbol)) {
			setError('这只股票已经在持仓中');
			return;
		}
		if (holdings.length >= maxHoldings) {
			setError(`最多添加 ${maxHoldings} 只持仓股票`);
			return;
		}
		if (remainingWeight <= 0) {
			setError('当前仓位已达到 100%，请先调低已有持仓');
			return;
		}
		setHoldings((current) => [...current, { symbol, name: stock.name, weight: Math.min(10, remainingWeight), costPrice: '' }]);
		setQuery('');
		setSuggestionsOpen(false);
		setError('');
	};

	const updateHolding = (symbol: string, update: Partial<DraftHolding>) => {
		setHoldings((current) => current.map((item) => item.symbol === symbol ? { ...item, ...update } : item));
	};

	const updateWeight = (symbol: string, requested: number) => {
		const current = holdings.find((item) => item.symbol === symbol);
		if (!current) return;
		const available = current.weight + remainingWeight;
		updateHolding(symbol, { weight: Math.max(1, Math.min(available, Math.round(requested || 1))) });
	};

	const handleSearchKey = (event: KeyboardEvent<HTMLInputElement>) => {
		if (!suggestionsOpen || suggestions.length === 0) {
			if (event.key === 'Enter') { event.preventDefault(); addHolding(); }
			return;
		}
		if (event.key === 'ArrowDown') {
			event.preventDefault();
			setActiveSuggestion((current) => (current + 1) % suggestions.length);
		} else if (event.key === 'ArrowUp') {
			event.preventDefault();
			setActiveSuggestion((current) => (current - 1 + suggestions.length) % suggestions.length);
		} else if (event.key === 'Enter') {
			event.preventDefault();
			addHolding(suggestions[activeSuggestion] || suggestions[0]);
		} else if (event.key === 'Escape') {
			setSuggestionsOpen(false);
		}
	};

	const startInspection = async () => {
		if (!config || holdings.length === 0 || totalWeight > 100) return;
		setStarting(true);
		setError('');
		setNotice('');
		try {
			const payload = await requestJSON<{ data: PortfolioInspectionJob }>(config, '/api/v1/portfolio-inspections', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					trader_profile: profile,
					holdings: holdings.map((item) => ({ symbol: item.symbol, name: item.name, weight_percent: item.weight, ...(Number(item.costPrice) > 0 ? { cost_price: Number(item.costPrice) } : {}) })),
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

	return <div className="portfolio-inspection-workspace">
		<aside className="portfolio-history" aria-label="巡检历史">
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
			{notice && <div className="portfolio-notice"><CheckCircle2 size={16} /><span>{notice}</span></div>}
			{error && <div className="portfolio-error"><CircleAlert size={16} /><span>{error}</span>{error.includes('模型') && <button type="button" onClick={onOpenSettings}>配置模型</button>}</div>}

			{(!job?.report || job.status === 'running') && <PortfolioBuilder
				profile={profile} holdings={holdings} totalWeight={totalWeight} remainingWeight={remainingWeight}
				query={query} suggestions={suggestions} suggestionsOpen={suggestionsOpen} activeSuggestion={activeSuggestion}
				running={Boolean(running)} starting={starting} directoryReady={directory.length > 0}
				onProfile={setProfile} onQuery={(value) => { setQuery(value); setSuggestionsOpen(true); setActiveSuggestion(0); }}
				onSearchFocus={() => setSuggestionsOpen(true)} onSearchKey={handleSearchKey} onAdd={addHolding}
				onUpdateWeight={updateWeight} onUpdateCost={(symbol, costPrice) => updateHolding(symbol, { costPrice })}
				onRemove={(symbol) => setHoldings((current) => current.filter((item) => item.symbol !== symbol))}
				onStart={() => void startInspection()}
			/>}

			{job?.status === 'running' && <InspectionProgress job={job} />}
			{job?.report && job.status !== 'running' && <PortfolioReportView report={job.report} status={job.status} onNew={() => setJob(null)} onOpenStockAnalysis={onOpenStockAnalysis} />}
		</section>
	</div>;
}

function PortfolioBuilder({ profile, holdings, totalWeight, remainingWeight, query, suggestions, suggestionsOpen, activeSuggestion, running, starting, directoryReady, onProfile, onQuery, onSearchFocus, onSearchKey, onAdd, onUpdateWeight, onUpdateCost, onRemove, onStart }: {
	profile: PortfolioTraderProfile; holdings: DraftHolding[]; totalWeight: number; remainingWeight: number; query: string; suggestions: StockDirectoryEntry[]; suggestionsOpen: boolean; activeSuggestion: number; running: boolean; starting: boolean; directoryReady: boolean;
	onProfile: (profile: PortfolioTraderProfile) => void; onQuery: (value: string) => void; onSearchFocus: () => void; onSearchKey: (event: KeyboardEvent<HTMLInputElement>) => void; onAdd: (entry?: StockDirectoryEntry) => void; onUpdateWeight: (symbol: string, weight: number) => void; onUpdateCost: (symbol: string, value: string) => void; onRemove: (symbol: string) => void; onStart: () => void;
}) {
	return <div className="portfolio-builder">
		<section className="portfolio-profile-section">
			<header><span>01</span><div><strong>交易风格</strong><small>作为组合风险与集中度的判断标准</small></div></header>
			<div className="portfolio-profile-options">
				{profiles.map((item) => <button type="button" className={profile === item.id ? 'active' : ''} onClick={() => onProfile(item.id)} disabled={running} aria-pressed={profile === item.id} key={item.id}>
					<ShieldCheck size={17} /><strong>{item.label}</strong><span>{item.description}</span><small>{item.constraint}</small>
				</button>)}
			</div>
		</section>

		<section className="portfolio-holdings-section">
			<header><span>02</span><div><strong>当前持仓</strong><small>{holdings.length}/{maxHoldings} 只股票</small></div><div className="portfolio-allocation"><span>持仓 <b>{totalWeight}%</b></span><span>现金 <b>{remainingWeight}%</b></span></div></header>
			<div className="portfolio-stock-search">
				<label><Search size={16} /><input value={query} disabled={running || !directoryReady || holdings.length >= maxHoldings || remainingWeight <= 0} onChange={(event) => onQuery(event.target.value)} onFocus={onSearchFocus} onKeyDown={onSearchKey} placeholder="输入股票名称或代码" aria-label="搜索持仓股票" aria-autocomplete="list" /></label>
				<button type="button" onClick={() => onAdd()} disabled={running || !query.trim() || remainingWeight <= 0} aria-label="添加持仓" title="添加持仓"><Plus size={18} /></button>
				{suggestionsOpen && query.trim() && suggestions.length > 0 && <div className="portfolio-stock-suggestions" role="listbox">
					{suggestions.map((stock, index) => <button type="button" className={index === activeSuggestion ? 'active' : ''} onMouseDown={(event) => event.preventDefault()} onClick={() => onAdd(stock)} role="option" aria-selected={index === activeSuggestion} key={stock.symbol}><strong>{stock.name}</strong><span>{stock.code}</span><small>{marketLabel(stock.symbol)}</small></button>)}
				</div>}
			</div>
			<div className="portfolio-holding-list">
				{holdings.length === 0 && <div className="portfolio-empty-holdings"><WalletCards size={24} /><strong>尚未录入持仓</strong></div>}
				{holdings.map((holding, index) => <article key={holding.symbol}>
					<div className="portfolio-holding-identity"><i>{String(index + 1).padStart(2, '0')}</i><div><strong>{holding.name}</strong><span>{holding.symbol}</span></div></div>
					<div className="portfolio-weight-control"><input type="range" min="1" max={holding.weight + remainingWeight} value={holding.weight} disabled={running} onChange={(event) => onUpdateWeight(holding.symbol, Number(event.target.value))} aria-label={`${holding.name}持仓占比`} /><label><input type="number" min="1" max={holding.weight + remainingWeight} value={holding.weight} disabled={running} onChange={(event) => onUpdateWeight(holding.symbol, Number(event.target.value))} /><span>%</span></label></div>
					<label className="portfolio-cost-input"><span>持仓成本</span><input inputMode="decimal" value={holding.costPrice} disabled={running} onChange={(event) => onUpdateCost(holding.symbol, event.target.value)} placeholder="选填" /></label>
					<button type="button" className="portfolio-remove" onClick={() => onRemove(holding.symbol)} disabled={running} aria-label={`删除${holding.name}`} title="删除"><Trash2 size={16} /></button>
				</article>)}
			</div>
		</section>

		<footer className="portfolio-builder-footer"><div><Activity size={17} /><span>总仓位 {totalWeight}%</span><strong>现金 {remainingWeight}%</strong></div><button type="button" onClick={onStart} disabled={running || starting || holdings.length === 0 || totalWeight > 100}>{starting || running ? <LoaderCircle className="spin" size={17} /> : <BrainCircuit size={17} />}{running ? '巡检进行中' : '开始 AI 巡检'}</button></footer>
	</div>;
}

function InspectionProgress({ job }: { job: PortfolioInspectionJob }) {
	const progress = job.total_stocks > 0 ? Math.round(job.completed_stocks / job.total_stocks * (job.stage === 'aggregating' ? 85 : 75)) : 3;
	const displayProgress = job.stage === 'aggregating' ? Math.max(86, progress) : Math.max(5, progress);
	return <section className="portfolio-progress" role="status" aria-live="polite">
		<div className="portfolio-progress-icon"><RefreshCw className="spin" size={24} /></div>
		<div><span>后台任务 · {stageLabel(job.stage)}</span><strong>巡检耗时较长，可以先使用其他功能</strong><p>{job.message}</p><div className="portfolio-progress-bar"><i style={{ width: `${displayProgress}%` }} /><small>{job.completed_stocks}/{job.total_stocks} 只 · {displayProgress}%</small></div>{job.current_symbols?.length > 0 && <em>正在分析 {job.current_symbols.join('、')}</em>}</div>
		<small><Clock3 size={14} />离开页面不会中断</small>
	</section>;
}

function PortfolioReportView({ report, status, onNew, onOpenStockAnalysis }: { report: PortfolioInspectionReport; status: string; onNew: () => void; onOpenStockAnalysis: (analysis: StockAIAnalysis) => void }) {
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
	return <div className="portfolio-report">
		<header className="portfolio-report-header"><div><span><WalletCards size={15} />持仓 AI 巡检报告</span><h2>{report.profile.label}型组合 · {conclusion.risk_level}风险</h2><p>{formatDate(report.generated_at)} · 覆盖 {metrics.coverage_percent}% · {conclusion.source === 'hermes-ai' ? 'AI 综合研判' : '本地规则研判'}</p></div><button type="button" onClick={onNew}><Plus size={15} />新建巡检</button></header>
		{status === 'partial' && <div className="portfolio-report-warning"><CircleAlert size={15} />报告已生成，但部分个股或组合分析使用降级结果。</div>}
		{isV2 ? <section className="portfolio-report-overview">
			<div className="portfolio-health-score"><span>组合健康度</span><strong>{metrics.health_score_available ? conclusion.health_score : '—'}</strong><small>{metrics.health_score_available ? 'V2 确定性评分' : '覆盖不足，暂不评分'}</small></div>
			<div><span>个股质量</span><strong>{metrics.weighted_stock_score.toFixed(1)}</strong><small>健康度权重 45%</small></div>
			<div><span>风险韧性</span><strong>{metrics.risk_resilience_score}</strong><small>权重 25% · 止损风险 {metrics.stop_loss_risk_percent.toFixed(2)}%</small></div>
			<div><span>分散 / 风格</span><strong>{metrics.diversification_score} / {metrics.style_match_score}</strong><small>{conclusion.style_match} · 持仓 {metrics.total_position_percent}% / 现金 {metrics.cash_percent}%</small></div>
		</section> : <section className="portfolio-report-overview">
			<div className="portfolio-health-score"><span>组合健康度</span><strong>{conclusion.health_score}</strong><small>历史评分</small></div>
			<div><span>风格匹配</span><strong>{conclusion.style_match}</strong><small>{metrics.style_match_score} 分</small></div>
			<div><span>持仓 / 现金</span><strong>{metrics.total_position_percent}% / {metrics.cash_percent}%</strong><small>当前配置</small></div>
			<div><span>预估止损风险</span><strong>{metrics.stop_loss_risk_percent.toFixed(2)}%</strong><small>占组合资产</small></div>
		</section>}
		<section className="portfolio-executive"><h3>巡检结论</h3><p>{conclusion.executive_summary}</p></section>

		<div className="portfolio-report-grid">
			<section><header><CircleAlert size={16} /><strong>主要风险</strong></header><ListItems items={conclusion.primary_risks} empty="没有识别到突出风险" /></section>
			<section><header><Activity size={16} /><strong>集中与联动</strong></header><ListItems items={conclusion.concentration_findings} empty="没有识别到明显集中风险" /></section>
		</div>

		<section className="portfolio-holding-report"><header><strong>逐股巡检</strong><small>按组合风险贡献排序</small></header><div>
			{conclusion.holdings.map((item) => {
				const stockAnalysis = analyses.get(item.symbol) || analyses.get(item.symbol.split('.')[0]);
				return <article key={item.symbol}><header><div><strong>{names.get(item.symbol) || item.symbol}</strong><span>{item.symbol} · {item.portfolio_role}</span></div><em className={priorityTone(item.action_priority)}>{item.action_priority}</em><b>{item.risk_contribution.toFixed(1)}% 风险贡献</b>{stockAnalysis && <button type="button" className="portfolio-open-stock" onClick={() => onOpenStockAnalysis(stockAnalysis)} aria-label={`查看${stockAnalysis.name}个股分析报告`} title="查看已生成的个股分析报告"><ExternalLink size={14} />查看个股报告</button>}</header><p>{item.conclusion}</p><div><span><b>动作</b>{item.action}</span><span><b>确认</b>{item.confirmation}</span><span><b>失效</b>{item.invalidation}</span></div></article>;
			})}
		</div></section>

		<div className="portfolio-report-grid">
			<section><header><ChevronRight size={16} /><strong>处理顺序</strong></header><ListItems items={conclusion.adjustment_order} empty="暂无调整事项" ordered /></section>
			<section><header><ShieldCheck size={16} /><strong>下次检查</strong></header><ListItems items={conclusion.next_checklist} empty="暂无检查事项" /></section>
		</div>
		<section className="portfolio-scenarios"><header><strong>组合情景</strong></header><div>{conclusion.scenarios.map((scenario) => <article key={scenario.name}><strong>{scenario.name}</strong><span>{scenario.condition}</span><p>{scenario.portfolio_action}</p></article>)}</div></section>
		{conclusion.data_limitations.length > 0 && <section className="portfolio-limitations"><strong>数据限制</strong><ListItems items={conclusion.data_limitations} empty="" /></section>}
		<footer>仅用于信息整理、研究与复盘，不构成投资建议或交易指令。</footer>
	</div>;
}

function ListItems({ items, empty, ordered = false }: { items: string[]; empty: string; ordered?: boolean }) {
	const Tag = ordered ? 'ol' : 'ul';
	return items.length ? <Tag>{items.map((item, index) => <li key={`${index}-${item}`}>{item}</li>)}</Tag> : <p className="portfolio-list-empty">{empty}</p>;
}

function loadDraft(): { profile: PortfolioTraderProfile; holdings: DraftHolding[] } {
	try {
		const parsed = JSON.parse(window.localStorage.getItem(draftStorageKey) || '{}');
		const profile = profiles.some((item) => item.id === parsed.profile) ? parsed.profile as PortfolioTraderProfile : 'balanced';
		const holdings = Array.isArray(parsed.holdings) ? parsed.holdings.filter((item: DraftHolding) => item?.symbol && item.weight > 0).slice(0, maxHoldings) : [];
		return { profile, holdings };
	} catch {
		return { profile: 'balanced', holdings: [] };
	}
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
	if (stage === 'analyzing_stocks') return '逐股分析';
	if (stage === 'aggregating') return '组合研判';
	return '处理中';
}

function profileLabel(profile: PortfolioTraderProfile) {
	return profiles.find((item) => item.id === profile)?.label || '均衡';
}

function marketLabel(symbol: string) {
	if (symbol.endsWith('.SH')) return '沪市';
	if (symbol.endsWith('.SZ')) return '深市';
	if (symbol.endsWith('.BJ')) return '北交所';
	return 'A股';
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
