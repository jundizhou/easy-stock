import { Activity, BrainCircuit, LoaderCircle, Plus, RefreshCw, Search, ShieldCheck, Trash2, WalletCards } from 'lucide-react';
import { KeyboardEvent, ReactNode, useEffect, useRef, useState } from 'react';
import { BackendConfig, PortfolioHolding, StockDirectoryEntry } from '../lib/backend';
import { defaultPortfolioAssets, maxPortfolioHoldings, PortfolioDraft, portfolioDraftToHoldingsWithAssets, portfolioHoldingCost, portfolioRate, portfolioProfiles } from '../lib/portfolio-draft';
import { loadPortfolioQuotes, usePortfolioQuotes } from '../lib/use-portfolio-quotes';
import { valuePortfolio } from '../lib/portfolio-valuation';
import './portfolio-valuation.css';
import { directStockCode, resolveStockDirectorySymbol } from '../lib/stock-analysis';
import { useStockSuggestions } from '../lib/use-stock-suggestions';

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
	onSubmit: (holdings?: PortfolioHolding[]) => void;
};

export function PortfolioSetupForm({ draft, config, directory, disabled = false, busy = false, actionLabel, busyLabel, actionIcon, showResearchOptions = false, onChange, onSubmit }: Props) {
	const [assetsInput, setAssetsInput] = useState(String(draft.totalAssets ?? defaultPortfolioAssets));
	const [editingInput, setEditingInput] = useState<{ symbol: string; field: 'weight' | 'quantity'; value: string } | null>(null);
	const [adding, setAdding] = useState(false);
	const addRequest = useRef<AbortController | null>(null);
	const latestDraft = useRef(draft);
	latestDraft.current = draft;
	useEffect(() => setAssetsInput(String(draft.totalAssets ?? defaultPortfolioAssets)), [draft.totalAssets]);
	useEffect(() => () => { addRequest.current?.abort(); }, []);
	const market = usePortfolioQuotes(config, draft.holdings.map((holding) => holding.symbol));
	// Keep the dated conversion on the plan for schedule snapshots and offline cost estimates.
	useEffect(() => {
		let changed = false;
		const holdings = draft.holdings.map(holding => {
			const quote = market.quotes[holding.symbol];
			if (!holding.symbol.endsWith('.HK') || !quote?.currency) return holding;
			const conversion = quote.cny_conversion || (holding.currency === quote.currency ? holding.cnyConversion : undefined);
			if (holding.currency === quote.currency && holding.cnyConversion?.date === conversion?.date && holding.cnyConversion?.rate === conversion?.rate) return holding;
			changed = true;
			return { ...holding, currency: quote.currency, cnyConversion: conversion };
		});
		if (changed) onChange({ ...draft, holdings });
	}, [market.quotes, draft, onChange]);
	const latestQuotes = useRef(market.quotes);
	latestQuotes.current = market.quotes;
	const totalAssets = Number(assetsInput);
	const assetsValid = Number.isFinite(totalAssets) && totalAssets > 0;
	const valuation = valuePortfolio({ ...draft, totalAssets }, market.quotes);
	const invalidCost = draft.holdings.some((holding) => holding.costPrice.trim() && portfolioHoldingCost(holding) === null);
	const [query, setQuery] = useState('');
	const [suggestionsOpen, setSuggestionsOpen] = useState(false);
	const [activeSuggestion, setActiveSuggestion] = useState(0);
	const [error, setError] = useState('');
	const allocationIncomplete = valuation.holdings.some((item) => item.weightPercent === null);
	const totalWeight = allocationIncomplete ? null : valuation.holdings.reduce((total, item) => total + item.weightPercent!, 0);
	const remainingWeight = totalWeight === null ? null : 100 - totalWeight;
	const emptyQuantity = valuation.holdings.some((item) => item.quantity === 0);
	const quantityExceedsAssets = assetsValid && valuation.holdings.some((item) => item.marketValue !== null && item.marketValue > totalAssets + 1e-6);
	const { suggestions: matches } = useStockSuggestions(config, query, directory);
	const suggestions = matches.filter(item => !draft.holdings.some(holding => holding.symbol === item.symbol));

	const addHolding = async (entry?: StockDirectoryEntry) => {
		if (disabled || addRequest.current) return;
		const resolved = entry?.symbol || resolveStockDirectorySymbol(query, matches);
		const stock = entry || matches.find(item => item.symbol === resolved || item.code === resolved)
			|| directory.find(item => item.symbol === resolved || item.code === resolved);
		const symbol = stock?.symbol || (directStockCode(query).endsWith('.HK') ? directStockCode(query) : '');
		if (!symbol) { setError('未找到唯一匹配的股票 / ETF / 港股，请从搜索结果中选择'); return; }
		if (draft.holdings.some((item) => item.symbol === symbol)) { setError('此标的已经在持仓中'); return; }
		if (draft.holdings.length >= maxPortfolioHoldings) { setError(`最多添加 ${maxPortfolioHoldings} 只持仓标的`); return; }
		const controller = new AbortController();
		addRequest.current = controller;
		setAdding(true);
		setError('');
		let snapshot: { entryPrice?: number; entryPriceTime?: string; currency?: string; cnyConversion?: PortfolioDraft['holdings'][number]['cnyConversion'] } = {};
		let name = stock?.name || symbol;
		let instrumentType = stock?.instrument_type;
		let priceError = '';
		const timeout = window.setTimeout(() => controller.abort('timeout'), 10000);
		try {
			if (!config) throw new Error('行情尚未连接');
			const quotes = await loadPortfolioQuotes(config, [symbol], controller.signal);
			const quote = quotes[symbol];
			if (!quote || quote.meta?.stale) throw new Error('暂无有效行情');
			name = quote.name || name;
			instrumentType = quote.instrument_type || instrumentType;
			snapshot = { currency: quote.currency, cnyConversion: quote.cny_conversion, entryPrice: quote.price, entryPriceTime: quote.trade_time || quote.meta?.fetched_at };
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
		if (current.holdings.some((item) => item.symbol === symbol)) { setError('此标的已经在持仓中'); return; }
		if (current.holdings.length >= maxPortfolioHoldings) { setError(`最多添加 ${maxPortfolioHoldings} 只持仓标的`); return; }
		const values = valuePortfolio(current, latestQuotes.current).holdings;
		const remaining = 100 - values.reduce((sum, item, index) => sum + (item.weightPercent ?? current.holdings[index].weight), 0);
		// Allow editing a full portfolio; enforce the asset budget on submission.
		const weight = remaining > 0 ? Math.min(10, remaining) : 10;
		const assets = current.totalAssets ?? defaultPortfolioAssets;
		const rate = portfolioRate({ symbol, ...snapshot });
		const quantity = snapshot.entryPrice && rate !== null ? String(Math.floor(assets * weight / 100 / (snapshot.entryPrice * rate) + 1e-9)) : '';
		onChange({ ...current, holdings: [...current.holdings, { symbol, name, instrumentType, weight, quantity, costPrice: snapshot.entryPrice ? String(snapshot.entryPrice) : '', ...snapshot }] });
		setQuery('');
		setSuggestionsOpen(false);
		setError(priceError);
	};

	const updateHolding = (symbol: string, update: Partial<PortfolioDraft['holdings'][number]>) => {
		onChange({ ...draft, holdings: draft.holdings.map((item) => item.symbol === symbol ? { ...item, ...update } : item) });
	};

	const handleSearchKey = (event: KeyboardEvent<HTMLInputElement>) => {
		if (!suggestionsOpen || suggestions.length === 0) {
			if (event.key === 'Enter') { event.preventDefault(); void addHolding(); }
			return;
		}
		if (event.key === 'ArrowDown') { event.preventDefault(); setActiveSuggestion((current) => (current + 1) % suggestions.length); }
		else if (event.key === 'ArrowUp') { event.preventDefault(); setActiveSuggestion((current) => (current - 1 + suggestions.length) % suggestions.length); }
		else if (event.key === 'Enter') { event.preventDefault(); void addHolding(directStockCode(query) ? undefined : suggestions[activeSuggestion] || suggestions[0]); }
		else if (event.key === 'Escape') setSuggestionsOpen(false);
	};

	return <div className="portfolio-builder">
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
			<p className="portfolio-valuation-note">持仓占比与数量联动，修改占比时按总资产 × 占比 ÷ 最新价取整，无行情时使用成本价。账户金额以人民币列示。外币成本和市值均按同一参考汇率折算，盈亏不含汇兑损益及交易费用。</p>
			{market.error && <p className="portfolio-quote-warning" role="status">{market.error}</p>}
			{draft.holdings.some((holding) => portfolioHoldingCost(holding) === null) && <p className="portfolio-quote-warning">部分持仓缺少有效成本，总盈亏待补齐成本与行情后显示。</p>}
			{quantityExceedsAssets && <p className="portfolio-quote-warning" role="alert">持仓数量对应的市值超过总资产，请减少持仓数量或增加总资产。</p>}
		</section>
		<section className="portfolio-profile-section">
			<header><span>01</span><div><strong>交易风格</strong><small>作为组合风险与集中度的判断标准</small></div></header>
			<div className="portfolio-profile-options">{portfolioProfiles.map((item) => <button type="button" className={draft.profile === item.id ? 'active' : ''} onClick={() => onChange({ ...draft, profile: item.id })} disabled={disabled} aria-pressed={draft.profile === item.id} key={item.id}><ShieldCheck size={17} /><strong>{item.label}</strong><span>{item.description}</span><small>{item.constraint}</small></button>)}</div>
		</section>
  {showResearchOptions && <section className="portfolio-research-options">
   <label>持有周期<select value={draft.horizon || 'swing'} disabled={disabled} onChange={(event) => onChange({ ...draft, horizon: event.target.value as PortfolioDraft['horizon'] })}><option value="short">超短</option><option value="swing">波段</option><option value="medium">中期</option></select></label>
   <label>缺失报告研究深度<select value={draft.researchLevel || 'standard'} disabled={disabled} onChange={(event) => onChange({ ...draft, researchLevel: event.target.value as PortfolioDraft['researchLevel'] })}><option value="standard">标准</option><option value="deep">深度</option></select></label>
   <p>自动复用过去 24 小时内的成功股票 / ETF AI 报告，只研究缺失标的。</p>
  </section>}
		<section className="portfolio-holdings-section">
			<header><span>02</span><div><strong>当前持仓</strong><small>{draft.holdings.length}/{maxPortfolioHoldings} 只标的</small></div><div className="portfolio-allocation"><span>持仓 <b>{allocationPercent(totalWeight)}</b></span><span>现金 <b>{allocationPercent(remainingWeight)}</b></span></div></header>
			<div className="portfolio-stock-search">
				<label><Search size={16} /><input value={query} disabled={disabled || adding || draft.holdings.length >= maxPortfolioHoldings} onChange={(event) => { setQuery(event.target.value); setSuggestionsOpen(true); setActiveSuggestion(0); }} onFocus={() => setSuggestionsOpen(true)} onKeyDown={handleSearchKey} placeholder="输入股票 / ETF / 港股名称或代码" aria-label="搜索持仓股票或 ETF" aria-autocomplete="list" /></label>
				<button type="button" onClick={() => void addHolding()} disabled={disabled || adding || !query.trim() || draft.holdings.length >= maxPortfolioHoldings} aria-label="添加持仓" title="添加持仓">{adding ? <LoaderCircle size={18} className="spin" /> : <Plus size={18} />}</button>
				{suggestionsOpen && query.trim() && suggestions.length > 0 && <div className="portfolio-stock-suggestions" role="listbox">{suggestions.map((stock, index) => <button type="button" className={index === activeSuggestion ? 'active' : ''} onMouseDown={(event) => event.preventDefault()} onClick={() => void addHolding(stock)} disabled={adding || disabled} role="option" aria-selected={index === activeSuggestion} key={stock.symbol}><strong>{stock.name}</strong><span>{stock.code}</span><small>{stock.instrument_type === 'etf' ? 'ETF · ' : ''}{marketLabel(stock.symbol)}</small></button>)}</div>}
			</div>
			{error && <div className="portfolio-error" role="alert"><span>{error}</span></div>}
			{totalWeight !== null && totalWeight > 100 && <p className="portfolio-quote-warning" role="alert">持仓总市值超过总资产，可继续编辑；请调整持仓数量或总资产后再开始巡检。</p>}
			{emptyQuantity && <p className="portfolio-quote-warning" role="alert">部分持仓数量为 0，请填写有效数量或删除该标的后再开始巡检。</p>}
			<div className="portfolio-holding-list">
				{draft.holdings.length === 0 && <div className="portfolio-empty-holdings"><WalletCards size={24} /><strong>尚未录入持仓</strong></div>}
				{draft.holdings.map((holding, index) => {
					const value = valuation.holdings[index];
					const quote = market.quotes[holding.symbol];
					const otherMarketValue = valuation.holdings.reduce((total, item, itemIndex) => itemIndex === index ? total : total + (item.marketValue ?? 0), 0);
					const nativePrice = value.price ?? value.costPrice;
					const sizingPrice = nativePrice !== null && value.rate !== null ? nativePrice * value.rate : null;
					const maxQuantity = sizingPrice !== null && assetsValid ? Math.max(0, Math.floor((totalAssets - otherMarketValue) / sizingPrice + 1e-9)) : undefined;
					const currency = quote?.currency || holding.currency;
					const isHK = holding.symbol.endsWith('.HK');
					const weightInput = editingInput?.symbol === holding.symbol && editingInput.field === 'weight' ? editingInput.value : String(Math.round((value.weightPercent ?? holding.weight) * 100) / 100);
					const quantityInput = editingInput?.symbol === holding.symbol && editingInput.field === 'quantity' ? editingInput.value : holding.quantity?.trim() ? holding.quantity : String(value.quantity ?? '');
 const isFund = holding.instrumentType === 'etf' || directory.some(v => v.symbol === holding.symbol && v.instrument_type === 'etf');
					return <article key={holding.symbol}>
						<div className="portfolio-holding-identity"><i>{String(index + 1).padStart(2, '0')}</i><div><strong>{holding.name}</strong><span>{holding.symbol}</span></div></div>
						<div className="portfolio-weight-control"><span>持仓占比</span><label><input type="number" min="0" max="100" step="0.01" inputMode="decimal" value={weightInput} disabled={disabled || !assetsValid || sizingPrice === null} aria-label={`${holding.name}持仓占比`} onChange={(event) => {
							const raw = event.target.value;
							setEditingInput({ symbol: holding.symbol, field: 'weight', value: raw });
							const weight = Number(raw);
							if (!raw.trim() || !Number.isFinite(weight) || weight < 0 || weight > 100 || !assetsValid || sizingPrice === null) return;
							const quantity = Math.floor(totalAssets * weight / 100 / sizingPrice + 1e-9);
							updateHolding(holding.symbol, { weight: weight > 0 ? weight : holding.weight, quantity: String(quantity) });
						}} onBlur={() => setEditingInput(null)} /><span>%</span></label></div>
						<label className="portfolio-quantity-input"><span>持仓数量</span><input type="number" min="0" max={maxQuantity} step="1" inputMode="numeric" value={quantityInput} disabled={disabled} aria-label={`${holding.name}持仓数量`} aria-invalid={value.marketValue !== null && value.marketValue > totalAssets + 1e-6} onChange={(event) => {
							const raw = event.target.value;
							const parsed = raw.trim() ? Number(raw) : null;
							const quantity = parsed !== null && Number.isFinite(parsed) && parsed >= 0 ? Math.min(Math.floor(parsed), maxQuantity ?? Infinity) : null;
							setEditingInput({ symbol: holding.symbol, field: 'quantity', value: quantity === null ? raw : String(quantity) });
							if (quantity === null) return;
							const weight = assetsValid && sizingPrice !== null ? quantity * sizingPrice / totalAssets * 100 : null;
							updateHolding(holding.symbol, { quantity: String(quantity), ...(weight !== null && weight > 0 ? { weight } : {}) });
						}} onBlur={() => setEditingInput(null)} placeholder="待补齐价格" /></label>
						<label className="portfolio-cost-input"><span>持仓成本{isHK && currency ? '（' + currency + '）' : ''}</span><input inputMode="decimal" value={holding.costPrice} disabled={disabled} aria-label={`${holding.name}持仓成本`} aria-invalid={Boolean(holding.costPrice.trim()) && value.costPrice === null} onChange={(event) => updateHolding(holding.symbol, { costPrice: event.target.value })} onBlur={() => { if (!holding.costPrice.trim() && holding.entryPrice) updateHolding(holding.symbol, { costPrice: String(holding.entryPrice) }); }} placeholder={holding.entryPrice ? String(holding.entryPrice) : '请输入成本'} title={holding.entryPrice ? `添加时价格 ${holding.entryPrice} ${isHK ? currency || '（币种待确认）' : '元'}，可修改` : '未取得添加时价格，请填写实际成本'} /></label>
						<button type="button" className="portfolio-remove" onClick={() => onChange({ ...draft, holdings: draft.holdings.filter((item) => item.symbol !== holding.symbol) })} disabled={disabled} aria-label={`删除${holding.name}`} title="删除"><Trash2 size={16} /></button>
						<div className="portfolio-holding-valuation">
							<div><span>最新价格</span><strong>{money(value.price, false, isFund ? 3 : 2, isHK ? currency || '币种待确认' : '元')}</strong><small className={profitClass(quote?.change_percent ?? null)}>日涨跌 {percent(quote?.change_percent ?? null)}</small></div>
							<div><span>持有数量（估算）</span><strong>{value.quantity === null ? '—' : `${value.quantity.toLocaleString('zh-CN')} ${isFund ? '份' : '股'}`}</strong></div>
							<div><span>持仓市值</span><strong>{money(value.marketValue)}</strong></div>
							<div><span>持仓盈亏</span><strong className={profitClass(value.profit)}>{money(value.profit, true)}</strong></div>
							<div><span>盈亏比例</span><strong className={profitClass(value.profitPercent)}>{percent(value.profitPercent)}</strong></div>
						</div>
						{isHK && <small className="portfolio-quote-time">{value.rate === null ? '报价币种或人民币汇率缺失，市值与巡检权重待补齐' : currency !== 'CNY' ? 'ECB 参考汇率 ' + (quote?.cny_conversion?.date || holding.cnyConversion?.date) + ' · 1 ' + currency + ' = ' + value.rate.toFixed(5) + ' CNY' : '人民币报价'}</small>}
						{quote && <small className="portfolio-quote-time">{quoteTime(quote.trade_time || quote.meta?.fetched_at)}{quote.meta?.stale || market.error ? ' · 上次行情快照' : ''}</small>}
					</article>;
				})}
			</div>
		</section>
		<footer className="portfolio-builder-footer"><div><Activity size={17} /><span>总仓位 {allocationPercent(totalWeight)}</span><strong>现金 {allocationPercent(remainingWeight)}</strong></div><button type="button" onClick={() => onSubmit(portfolioDraftToHoldingsWithAssets(draft.holdings, totalAssets, market.quotes))} disabled={disabled || busy || adding || !assetsValid || invalidCost || allocationIncomplete || emptyQuantity || quantityExceedsAssets || draft.holdings.length === 0 || (totalWeight ?? 0) > 100}>{busy ? <LoaderCircle className="spin" size={17} /> : actionIcon || <BrainCircuit size={17} />}{busy ? busyLabel : actionLabel}</button></footer>
	</div>;
}

function marketLabel(symbol: string) {
	if (symbol.endsWith('.SH')) return '沪市';
	if (symbol.endsWith('.SZ')) return '深市';
	if (symbol.endsWith('.BJ')) return '北交所';
	if (symbol.endsWith('.HK')) return '港股';
	return 'A股';
}

function money(value: number | null, signed = false, precision = 2, unit = '元') {
	if (value === null || !Number.isFinite(value)) return '—';
	const rounded = Math.round(value * 10 ** precision) / 10 ** precision;
	return `${signed && rounded > 0 ? '+' : ''}${rounded.toLocaleString('zh-CN', { minimumFractionDigits: precision, maximumFractionDigits: precision })} ${unit}`;
}
function percent(value: number | null) {
	if (value === null || !Number.isFinite(value)) return '—';
	const rounded = Math.round(value * 100) / 100;
	return `${rounded > 0 ? '+' : ''}${rounded.toFixed(2)}%`;
}
function allocationPercent(value: number | null) {
	if (value === null || !Number.isFinite(value)) return '—';
	return `${Math.round(value * 100) / 100}%`;
}
function profitClass(value: number | null) { return value !== null && value > 0 ? 'portfolio-profit-up' : value !== null && value < 0 ? 'portfolio-profit-down' : ''; }
function quoteTime(value?: string) {
	const date = new Date(value || '');
	return Number.isFinite(date.getTime()) ? `行情 ${date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })}` : '行情时间未知';
}
