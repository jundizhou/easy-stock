import { Activity, BrainCircuit, LoaderCircle, Plus, RefreshCw, Search, ShieldCheck, Trash2, WalletCards } from 'lucide-react';
import { KeyboardEvent, ReactNode, useEffect, useMemo, useRef, useState } from 'react';
import { BackendConfig, StockDirectoryEntry } from '../lib/backend';
import { defaultPortfolioAssets, maxPortfolioHoldings, PortfolioDraft, portfolioHoldingCost, portfolioProfiles } from '../lib/portfolio-draft';
import { loadPortfolioQuotes, usePortfolioQuotes } from '../lib/use-portfolio-quotes';
import { valuePortfolio } from '../lib/portfolio-valuation';
import './portfolio-valuation.css';
import { resolveStockDirectorySymbol, searchStockDirectory } from '../lib/stock-analysis';

type Props = {
	draft: PortfolioDraft;
	config?: BackendConfig | null;
	directory: StockDirectoryEntry[];
	disabled?: boolean;
	busy?: boolean;
	actionLabel: string;
	busyLabel: string;
	actionIcon?: ReactNode;
 showResearchOptions?: boolean;
	onChange: (draft: PortfolioDraft) => void;
	onSubmit: () => void;
};

export function PortfolioSetupForm({ draft, config, directory, disabled = false, busy = false, actionLabel, busyLabel, actionIcon, showResearchOptions = false, onChange, onSubmit }: Props) {
	const [assetsInput, setAssetsInput] = useState(String(draft.totalAssets ?? defaultPortfolioAssets));
	const [adding, setAdding] = useState(false);
	const addRequest = useRef<AbortController | null>(null);
	const latestDraft = useRef(draft);
	latestDraft.current = draft;
	useEffect(() => setAssetsInput(String(draft.totalAssets ?? defaultPortfolioAssets)), [draft.totalAssets]);
	useEffect(() => () => { addRequest.current?.abort(); }, []);
	const market = usePortfolioQuotes(config, draft.holdings.map((holding) => holding.symbol));
	const totalAssets = Number(assetsInput);
	const assetsValid = Number.isFinite(totalAssets) && totalAssets > 0;
	const valuation = valuePortfolio({ ...draft, totalAssets }, market.quotes);
	const invalidCost = draft.holdings.some((holding) => holding.costPrice.trim() && portfolioHoldingCost(holding) === null);
	const [query, setQuery] = useState('');
	const [suggestionsOpen, setSuggestionsOpen] = useState(false);
	const [activeSuggestion, setActiveSuggestion] = useState(0);
	const [error, setError] = useState('');
	const totalWeight = useMemo(() => draft.holdings.reduce((total, item) => total + item.weight, 0), [draft.holdings]);
	const remainingWeight = 100 - totalWeight;
	const suggestions = useMemo(() => searchStockDirectory(directory, query).filter((item) => !draft.holdings.some((holding) => holding.symbol === item.symbol)), [directory, draft.holdings, query]);

	const addHolding = async (entry?: StockDirectoryEntry) => {
		if (disabled || addRequest.current) return;
		const symbol = entry?.symbol || resolveStockDirectorySymbol(query, directory);
		const stock = entry || directory.find((item) => item.symbol === symbol);
		if (!symbol || !stock) { setError('未找到唯一匹配的股票，请从搜索结果中选择'); return; }
		if (draft.holdings.some((item) => item.symbol === symbol)) { setError('这只股票已经在持仓中'); return; }
		if (draft.holdings.length >= maxPortfolioHoldings) { setError(`最多添加 ${maxPortfolioHoldings} 只持仓股票`); return; }
		if (remainingWeight <= 0) { setError('当前仓位已达到 100%，请先调低已有持仓'); return; }
		const controller = new AbortController();
		addRequest.current = controller;
		setAdding(true);
		setError('');
		let snapshot: { entryPrice?: number; entryPriceTime?: string } = {};
		let priceError = '';
		const timeout = window.setTimeout(() => controller.abort('timeout'), 10000);
		try {
			if (!config) throw new Error('行情尚未连接');
			const quotes = await loadPortfolioQuotes(config, [symbol], controller.signal);
			const quote = quotes[symbol];
			if (!quote || quote.meta?.stale) throw new Error('暂无有效行情');
			snapshot = { entryPrice: quote.price, entryPriceTime: quote.trade_time || quote.meta?.fetched_at };
		} catch {
			priceError = '未取得添加时的有效价格，请手动填写持仓成本';
		} finally {
			window.clearTimeout(timeout);
		}
		// A response from a closed form must not modify another portfolio plan.
		if (controller.signal.aborted && controller.signal.reason !== 'timeout') return;
		addRequest.current = null;
		setAdding(false);
		const current = latestDraft.current;
		const remaining = 100 - current.holdings.reduce((sum, item) => sum + item.weight, 0);
		if (current.holdings.some((item) => item.symbol === symbol) || current.holdings.length >= maxPortfolioHoldings || remaining <= 0) return;
		onChange({ ...current, holdings: [...current.holdings, { symbol, name: stock.name, weight: Math.min(10, remaining), costPrice: snapshot.entryPrice ? String(snapshot.entryPrice) : '', ...snapshot }] });
		setQuery('');
		setSuggestionsOpen(false);
		setError(priceError);
	};

	const updateHolding = (symbol: string, update: Partial<PortfolioDraft['holdings'][number]>) => {
		onChange({ ...draft, holdings: draft.holdings.map((item) => item.symbol === symbol ? { ...item, ...update } : item) });
	};

	const updateWeight = (symbol: string, requested: number) => {
		const current = draft.holdings.find((item) => item.symbol === symbol);
		if (!current) return;
		const available = current.weight + remainingWeight;
		updateHolding(symbol, { weight: Math.max(1, Math.min(available, Math.round(requested || 1))) });
	};

	const handleSearchKey = (event: KeyboardEvent<HTMLInputElement>) => {
		if (!suggestionsOpen || suggestions.length === 0) {
			if (event.key === 'Enter') { event.preventDefault(); void addHolding(); }
			return;
		}
		if (event.key === 'ArrowDown') { event.preventDefault(); setActiveSuggestion((current) => (current + 1) % suggestions.length); }
		else if (event.key === 'ArrowUp') { event.preventDefault(); setActiveSuggestion((current) => (current - 1 + suggestions.length) % suggestions.length); }
		else if (event.key === 'Enter') { event.preventDefault(); void addHolding(suggestions[activeSuggestion] || suggestions[0]); }
		else if (event.key === 'Escape') setSuggestionsOpen(false);
	};

	return <div className="portfolio-builder">
		{error && <div className="portfolio-error"><span>{error}</span></div>}
		<section className="portfolio-assets-section">
			<div className="portfolio-assets-heading"><label>总资产（元）<input type="number" min="0.01" step="1000" inputMode="decimal" value={assetsInput} disabled={disabled} aria-invalid={!assetsValid} onChange={(event) => {
				setAssetsInput(event.target.value);
				const value = Number(event.target.value);
				if (Number.isFinite(value) && value > 0) onChange({ ...draft, totalAssets: value });
			}} /></label><button type="button" onClick={market.refresh} disabled={!config || !draft.holdings.length || market.loading}><RefreshCw size={14} className={market.loading ? 'spin' : ''} />刷新行情</button></div>
			{!assetsValid && <p role="alert">请输入大于 0 的总资产</p>}
			<div className="portfolio-asset-metrics">
				<div><span>持仓市值</span><strong>{money(valuation.marketValue)}</strong></div>
				<div><span>剩余现金</span><strong>{money(valuation.cash)}</strong></div>
				<div><span>总盈亏</span><strong className={profitClass(valuation.profit)}>{money(valuation.profit, true)}</strong></div>
				<div><span>持仓盈亏率</span><strong className={profitClass(valuation.profitPercent)}>{percent(valuation.profitPercent)}</strong></div>
			</div>
			<p className="portfolio-valuation-note">股数按总资产 × 仓位 ÷ 成本价取整，盈亏按最新价估算，不含交易费用。</p>
			{market.error && <p className="portfolio-quote-warning" role="status">{market.error}</p>}
			{draft.holdings.some((holding) => portfolioHoldingCost(holding) === null) && <p className="portfolio-quote-warning">部分持仓缺少有效成本，总盈亏待补齐成本与行情后显示。</p>}
		</section>
		<section className="portfolio-profile-section">
			<header><span>01</span><div><strong>交易风格</strong><small>作为组合风险与集中度的判断标准</small></div></header>
			<div className="portfolio-profile-options">{portfolioProfiles.map((item) => <button type="button" className={draft.profile === item.id ? 'active' : ''} onClick={() => onChange({ ...draft, profile: item.id })} disabled={disabled} aria-pressed={draft.profile === item.id} key={item.id}><ShieldCheck size={17} /><strong>{item.label}</strong><span>{item.description}</span><small>{item.constraint}</small></button>)}</div>
		</section>
  {showResearchOptions && <section className="portfolio-research-options">
   <label>持有周期<select value={draft.horizon || 'swing'} disabled={disabled} onChange={(event) => onChange({ ...draft, horizon: event.target.value as PortfolioDraft['horizon'] })}><option value="short">超短</option><option value="swing">波段</option><option value="medium">中期</option></select></label>
   <label>缺失报告研究深度<select value={draft.researchLevel || 'standard'} disabled={disabled} onChange={(event) => onChange({ ...draft, researchLevel: event.target.value as PortfolioDraft['researchLevel'] })}><option value="standard">标准</option><option value="deep">深度</option></select></label>
   <p>自动复用过去 24 小时内的成功个股 AI 报告，只研究缺失股票。</p>
  </section>}
		<section className="portfolio-holdings-section">
			<header><span>02</span><div><strong>当前持仓</strong><small>{draft.holdings.length}/{maxPortfolioHoldings} 只股票</small></div><div className="portfolio-allocation"><span>持仓 <b>{totalWeight}%</b></span><span>现金 <b>{remainingWeight}%</b></span></div></header>
			<div className="portfolio-stock-search">
				<label><Search size={16} /><input value={query} disabled={disabled || adding || directory.length === 0 || draft.holdings.length >= maxPortfolioHoldings || remainingWeight <= 0} onChange={(event) => { setQuery(event.target.value); setSuggestionsOpen(true); setActiveSuggestion(0); }} onFocus={() => setSuggestionsOpen(true)} onKeyDown={handleSearchKey} placeholder="输入股票名称或代码" aria-label="搜索持仓股票" aria-autocomplete="list" /></label>
				<button type="button" onClick={() => void addHolding()} disabled={disabled || adding || !query.trim() || remainingWeight <= 0} aria-label="添加持仓" title="添加持仓">{adding ? <LoaderCircle size={18} className="spin" /> : <Plus size={18} />}</button>
				{suggestionsOpen && query.trim() && suggestions.length > 0 && <div className="portfolio-stock-suggestions" role="listbox">{suggestions.map((stock, index) => <button type="button" className={index === activeSuggestion ? 'active' : ''} onMouseDown={(event) => event.preventDefault()} onClick={() => void addHolding(stock)} disabled={adding || disabled} role="option" aria-selected={index === activeSuggestion} key={stock.symbol}><strong>{stock.name}</strong><span>{stock.code}</span><small>{marketLabel(stock.symbol)}</small></button>)}</div>}
			</div>
			<div className="portfolio-holding-list">
				{draft.holdings.length === 0 && <div className="portfolio-empty-holdings"><WalletCards size={24} /><strong>尚未录入持仓</strong></div>}
				{draft.holdings.map((holding, index) => {
					const value = valuation.holdings[index];
					const quote = market.quotes[holding.symbol];
					return <article key={holding.symbol}>
						<div className="portfolio-holding-identity"><i>{String(index + 1).padStart(2, '0')}</i><div><strong>{holding.name}</strong><span>{holding.symbol}</span></div></div>
						<div className="portfolio-weight-control"><input type="range" min="1" max="100" value={holding.weight} disabled={disabled} onChange={(event) => updateWeight(holding.symbol, Number(event.target.value))} aria-label={`${holding.name}持仓占比`} /><label><input type="number" min="1" max={holding.weight + remainingWeight} value={holding.weight} disabled={disabled} onChange={(event) => updateWeight(holding.symbol, Number(event.target.value))} aria-label={`${holding.name}仓位百分比`} /><span>%</span></label></div>
						<label className="portfolio-cost-input"><span>持仓成本</span><input inputMode="decimal" value={holding.costPrice} disabled={disabled} aria-label={`${holding.name}持仓成本`} aria-invalid={Boolean(holding.costPrice.trim()) && value.costPrice === null} onChange={(event) => updateHolding(holding.symbol, { costPrice: event.target.value })} onBlur={() => { if (!holding.costPrice.trim() && holding.entryPrice) updateHolding(holding.symbol, { costPrice: String(holding.entryPrice) }); }} placeholder={holding.entryPrice ? String(holding.entryPrice) : '请输入成本'} title={holding.entryPrice ? `添加时价格 ${holding.entryPrice} 元，可修改` : '未取得添加时价格，请填写实际成本'} /></label>
						<button type="button" className="portfolio-remove" onClick={() => onChange({ ...draft, holdings: draft.holdings.filter((item) => item.symbol !== holding.symbol) })} disabled={disabled} aria-label={`删除${holding.name}`} title="删除"><Trash2 size={16} /></button>
						<div className="portfolio-holding-valuation">
							<div><span>最新价格</span><strong>{money(value.price)}</strong><small className={profitClass(quote?.change_percent ?? null)}>日涨跌 {percent(quote?.change_percent ?? null)}</small></div>
							<div><span>持股数量（估算）</span><strong>{value.quantity === null ? '—' : `${value.quantity.toLocaleString('zh-CN')} 股`}</strong></div>
							<div><span>持仓市值</span><strong>{money(value.marketValue)}</strong></div>
							<div><span>个股盈亏</span><strong className={profitClass(value.profit)}>{money(value.profit, true)}</strong></div>
							<div><span>盈亏比例</span><strong className={profitClass(value.profitPercent)}>{percent(value.profitPercent)}</strong></div>
						</div>
						{quote && <small className="portfolio-quote-time">{quoteTime(quote.trade_time || quote.meta?.fetched_at)}{quote.meta?.stale || market.error ? ' · 上次行情快照' : ''}</small>}
					</article>;
				})}
			</div>
		</section>
		<footer className="portfolio-builder-footer"><div><Activity size={17} /><span>总仓位 {totalWeight}%</span><strong>现金 {remainingWeight}%</strong></div><button type="button" onClick={onSubmit} disabled={disabled || busy || adding || !assetsValid || invalidCost || draft.holdings.length === 0 || totalWeight > 100}>{busy ? <LoaderCircle className="spin" size={17} /> : actionIcon || <BrainCircuit size={17} />}{busy ? busyLabel : actionLabel}</button></footer>
	</div>;
}

function marketLabel(symbol: string) {
	if (symbol.endsWith('.SH')) return '沪市';
	if (symbol.endsWith('.SZ')) return '深市';
	if (symbol.endsWith('.BJ')) return '北交所';
	return 'A股';
}

function money(value: number | null, signed = false) {
	if (value === null || !Number.isFinite(value)) return '—';
	const rounded = Math.round(value * 100) / 100;
	return `${signed && rounded > 0 ? '+' : ''}${rounded.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} 元`;
}
function percent(value: number | null) {
	if (value === null || !Number.isFinite(value)) return '—';
	const rounded = Math.round(value * 100) / 100;
	return `${rounded > 0 ? '+' : ''}${rounded.toFixed(2)}%`;
}
function profitClass(value: number | null) { return value !== null && value > 0 ? 'portfolio-profit-up' : value !== null && value < 0 ? 'portfolio-profit-down' : ''; }
function quoteTime(value?: string) {
	const date = new Date(value || '');
	return Number.isFinite(date.getTime()) ? `行情 ${date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })}` : '行情时间未知';
}
