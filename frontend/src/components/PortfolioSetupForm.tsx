import { Activity, BrainCircuit, LoaderCircle, Plus, Search, ShieldCheck, Trash2, WalletCards } from 'lucide-react';
import { KeyboardEvent, ReactNode, useMemo, useState } from 'react';
import { StockDirectoryEntry } from '../lib/backend';
import { maxPortfolioHoldings, PortfolioDraft, portfolioProfiles } from '../lib/portfolio-draft';
import { resolveStockDirectorySymbol, searchStockDirectory } from '../lib/stock-analysis';

type Props = {
	draft: PortfolioDraft;
	directory: StockDirectoryEntry[];
	disabled?: boolean;
	busy?: boolean;
	actionLabel: string;
	busyLabel: string;
	actionIcon?: ReactNode;
	onChange: (draft: PortfolioDraft) => void;
	onSubmit: () => void;
};

export function PortfolioSetupForm({ draft, directory, disabled = false, busy = false, actionLabel, busyLabel, actionIcon, onChange, onSubmit }: Props) {
	const [query, setQuery] = useState('');
	const [suggestionsOpen, setSuggestionsOpen] = useState(false);
	const [activeSuggestion, setActiveSuggestion] = useState(0);
	const [error, setError] = useState('');
	const totalWeight = useMemo(() => draft.holdings.reduce((total, item) => total + item.weight, 0), [draft.holdings]);
	const remainingWeight = 100 - totalWeight;
	const suggestions = useMemo(() => searchStockDirectory(directory, query).filter((item) => !draft.holdings.some((holding) => holding.symbol === item.symbol)), [directory, draft.holdings, query]);

	const addHolding = (entry?: StockDirectoryEntry) => {
		const symbol = entry?.symbol || resolveStockDirectorySymbol(query, directory);
		const stock = entry || directory.find((item) => item.symbol === symbol);
		if (!symbol || !stock) { setError('未找到唯一匹配的股票，请从搜索结果中选择'); return; }
		if (draft.holdings.some((item) => item.symbol === symbol)) { setError('这只股票已经在持仓中'); return; }
		if (draft.holdings.length >= maxPortfolioHoldings) { setError(`最多添加 ${maxPortfolioHoldings} 只持仓股票`); return; }
		if (remainingWeight <= 0) { setError('当前仓位已达到 100%，请先调低已有持仓'); return; }
		onChange({ ...draft, holdings: [...draft.holdings, { symbol, name: stock.name, weight: Math.min(10, remainingWeight), costPrice: '' }] });
		setQuery('');
		setSuggestionsOpen(false);
		setError('');
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
			if (event.key === 'Enter') { event.preventDefault(); addHolding(); }
			return;
		}
		if (event.key === 'ArrowDown') { event.preventDefault(); setActiveSuggestion((current) => (current + 1) % suggestions.length); }
		else if (event.key === 'ArrowUp') { event.preventDefault(); setActiveSuggestion((current) => (current - 1 + suggestions.length) % suggestions.length); }
		else if (event.key === 'Enter') { event.preventDefault(); addHolding(suggestions[activeSuggestion] || suggestions[0]); }
		else if (event.key === 'Escape') setSuggestionsOpen(false);
	};

	return <div className="portfolio-builder">
		{error && <div className="portfolio-error"><span>{error}</span></div>}
		<section className="portfolio-profile-section">
			<header><span>01</span><div><strong>交易风格</strong><small>作为组合风险与集中度的判断标准</small></div></header>
			<div className="portfolio-profile-options">{portfolioProfiles.map((item) => <button type="button" className={draft.profile === item.id ? 'active' : ''} onClick={() => onChange({ ...draft, profile: item.id })} disabled={disabled} aria-pressed={draft.profile === item.id} key={item.id}><ShieldCheck size={17} /><strong>{item.label}</strong><span>{item.description}</span><small>{item.constraint}</small></button>)}</div>
		</section>
		<section className="portfolio-holdings-section">
			<header><span>02</span><div><strong>当前持仓</strong><small>{draft.holdings.length}/{maxPortfolioHoldings} 只股票</small></div><div className="portfolio-allocation"><span>持仓 <b>{totalWeight}%</b></span><span>现金 <b>{remainingWeight}%</b></span></div></header>
			<div className="portfolio-stock-search">
				<label><Search size={16} /><input value={query} disabled={disabled || directory.length === 0 || draft.holdings.length >= maxPortfolioHoldings || remainingWeight <= 0} onChange={(event) => { setQuery(event.target.value); setSuggestionsOpen(true); setActiveSuggestion(0); }} onFocus={() => setSuggestionsOpen(true)} onKeyDown={handleSearchKey} placeholder="输入股票名称或代码" aria-label="搜索持仓股票" aria-autocomplete="list" /></label>
				<button type="button" onClick={() => addHolding()} disabled={disabled || !query.trim() || remainingWeight <= 0} aria-label="添加持仓" title="添加持仓"><Plus size={18} /></button>
				{suggestionsOpen && query.trim() && suggestions.length > 0 && <div className="portfolio-stock-suggestions" role="listbox">{suggestions.map((stock, index) => <button type="button" className={index === activeSuggestion ? 'active' : ''} onMouseDown={(event) => event.preventDefault()} onClick={() => addHolding(stock)} role="option" aria-selected={index === activeSuggestion} key={stock.symbol}><strong>{stock.name}</strong><span>{stock.code}</span><small>{marketLabel(stock.symbol)}</small></button>)}</div>}
			</div>
			<div className="portfolio-holding-list">
				{draft.holdings.length === 0 && <div className="portfolio-empty-holdings"><WalletCards size={24} /><strong>尚未录入持仓</strong></div>}
				{draft.holdings.map((holding, index) => <article key={holding.symbol}><div className="portfolio-holding-identity"><i>{String(index + 1).padStart(2, '0')}</i><div><strong>{holding.name}</strong><span>{holding.symbol}</span></div></div><div className="portfolio-weight-control"><input type="range" min="1" max={holding.weight + remainingWeight} value={holding.weight} disabled={disabled} onChange={(event) => updateWeight(holding.symbol, Number(event.target.value))} aria-label={`${holding.name}持仓占比`} /><label><input type="number" min="1" max={holding.weight + remainingWeight} value={holding.weight} disabled={disabled} onChange={(event) => updateWeight(holding.symbol, Number(event.target.value))} /><span>%</span></label></div><label className="portfolio-cost-input"><span>持仓成本</span><input inputMode="decimal" value={holding.costPrice} disabled={disabled} onChange={(event) => updateHolding(holding.symbol, { costPrice: event.target.value })} placeholder="选填" /></label><button type="button" className="portfolio-remove" onClick={() => onChange({ ...draft, holdings: draft.holdings.filter((item) => item.symbol !== holding.symbol) })} disabled={disabled} aria-label={`删除${holding.name}`} title="删除"><Trash2 size={16} /></button></article>)}
			</div>
		</section>
		<footer className="portfolio-builder-footer"><div><Activity size={17} /><span>总仓位 {totalWeight}%</span><strong>现金 {remainingWeight}%</strong></div><button type="button" onClick={onSubmit} disabled={disabled || busy || draft.holdings.length === 0 || totalWeight > 100}>{busy ? <LoaderCircle className="spin" size={17} /> : actionIcon || <BrainCircuit size={17} />}{busy ? busyLabel : actionLabel}</button></footer>
	</div>;
}

function marketLabel(symbol: string) {
	if (symbol.endsWith('.SH')) return '沪市';
	if (symbol.endsWith('.SZ')) return '深市';
	if (symbol.endsWith('.BJ')) return '北交所';
	return 'A股';
}
