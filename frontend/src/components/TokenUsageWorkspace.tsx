import { BarChart3, CalendarDays, Database, LoaderCircle, RefreshCw, Sigma } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { BackendConfig, TokenUsageSummary, requestJSON } from '../lib/backend';

type UsagePeriod = 'day' | 'month';

const moduleLabels: Record<string, string> = {
	'ai-chat': 'AI 对话',
	'stock-analysis': '个股分析',
	'portfolio-inspection': '持仓 AI 巡检',
	'review-diary': '大V复盘日记',
	'market-overview': '行情总览',
	'mastery': '游资心法',
};

const defaultModules = Object.keys(moduleLabels);

function localDateValue(date: Date) {
	const year = date.getFullYear();
	const month = String(date.getMonth() + 1).padStart(2, '0');
	const day = String(date.getDate()).padStart(2, '0');
	return `${year}-${month}-${day}`;
}

function firstDayOfMonth(date: Date) {
	return localDateValue(new Date(date.getFullYear(), date.getMonth(), 1));
}

function formatNumber(value: number) {
	return value.toLocaleString('zh-CN');
}

export function TokenUsageWorkspace({ config, refreshKey }: { config: BackendConfig | null; refreshKey: number }) {
	const today = useMemo(() => new Date(), []);
	const [period, setPeriod] = useState<UsagePeriod>('day');
	const [from, setFrom] = useState(() => firstDayOfMonth(today));
	const [to, setTo] = useState(() => localDateValue(today));
	const [module, setModule] = useState('');
	const [usage, setUsage] = useState<TokenUsageSummary | null>(null);
	const [state, setState] = useState<'idle' | 'loading' | 'error'>('idle');

	const loadUsage = async () => {
		if (!config) return;
		setState('loading');
		const params = new URLSearchParams({ period, from, to });
		if (module) params.set('module', module);
		try {
			const payload = await requestJSON<{ data: TokenUsageSummary }>(config, `/api/v1/settings/token-usage?${params.toString()}`);
			setUsage(payload.data);
			setState('idle');
		} catch {
			setUsage(null);
			setState('error');
		}
	};

	useEffect(() => { void loadUsage(); }, [config, refreshKey]);

	const modules = useMemo(() => [...new Set([...defaultModules, ...(usage?.modules || [])])], [usage?.modules]);
	const rows = usage?.rows || [];
	const maxRowTotal = Math.max(...rows.map((row) => row.total_tokens), 1);

	return (
		<section className="token-usage-workspace" aria-label="Token统计">
			<header className="token-usage-hero">
				<div>
					<span className="workspace-kicker">USAGE ANALYTICS</span>
					<h2>Token 统计</h2>
					<p>按时间范围和功能模块查看模型输入、输出及总消耗。</p>
				</div>
				<button type="button" className="token-usage-refresh" onClick={() => void loadUsage()} disabled={state === 'loading'}>
					{state === 'loading' ? <LoaderCircle className="spin" size={16} /> : <RefreshCw size={16} />}刷新数据
				</button>
			</header>

			<section className="token-usage-control-panel">
				<div className="token-usage-period-tabs" role="tablist" aria-label="统计周期">
					<button type="button" className={period === 'day' ? 'active' : ''} onClick={() => setPeriod('day')}>按日</button>
					<button type="button" className={period === 'month' ? 'active' : ''} onClick={() => setPeriod('month')}>按月</button>
				</div>
				<label><span>开始日期</span><span className="token-usage-input"><CalendarDays size={14} /><input type="date" value={from} onChange={(event) => setFrom(event.target.value)} /></span></label>
				<label><span>结束日期</span><span className="token-usage-input"><CalendarDays size={14} /><input type="date" value={to} onChange={(event) => setTo(event.target.value)} /></span></label>
				<label><span>功能模块</span><span className="token-usage-input"><Database size={14} /><select value={module} onChange={(event) => setModule(event.target.value)}><option value="">全部功能模块</option>{modules.map((item) => <option value={item} key={item}>{moduleLabels[item] || item}</option>)}</select></span></label>
				<button type="button" className="token-usage-query" onClick={() => void loadUsage()} disabled={state === 'loading'}><BarChart3 size={15} />查询</button>
			</section>

			{state === 'error' && <div className="token-usage-error">Token 统计暂时无法读取，请稍后重试。</div>}
			<section className="token-usage-stat-grid">
				<TokenStat label="总消耗" value={formatNumber(usage?.total.total_tokens || 0)} detail="输入 + 输出" tone="primary" />
				<TokenStat label="输入 Token" value={formatNumber(usage?.total.prompt_tokens || 0)} detail="Prompt tokens" />
				<TokenStat label="输出 Token" value={formatNumber(usage?.total.completion_tokens || 0)} detail="Completion tokens" />
				<TokenStat label="功能模块" value={String(usage?.modules.length || 0)} detail="当前范围内有消耗记录" />
			</section>

			<section className="token-usage-detail-grid">
				<div className="token-usage-panel">
					<header><div><span>消耗趋势</span><strong>{period === 'day' ? '按日明细' : '按月明细'}</strong></div><Sigma size={18} /></header>
					{rows.length ? <div className="token-usage-bars">{rows.slice(-14).map((row) => <div className="token-usage-bar-row" key={`${row.date}-${row.module}`}><span>{row.date}</span><div><i style={{ width: `${Math.max((row.total_tokens / maxRowTotal) * 100, 2)}%` }} /></div><strong>{formatNumber(row.total_tokens)}</strong></div>)}</div> : <EmptyUsage />}
				</div>
				<div className="token-usage-panel token-usage-module-panel">
					<header><div><span>模块分布</span><strong>功能模块消耗</strong></div><Database size={18} /></header>
					{rows.length ? <div className="token-usage-module-list">{aggregateModules(rows).map((row) => <div className="token-usage-module-row" key={row.module}><div><span>{moduleLabels[row.module] || row.module}</span><small>{row.module}</small></div><strong>{formatNumber(row.total_tokens)}</strong></div>)}</div> : <EmptyUsage />}
				</div>
			</section>

			<section className="token-usage-panel token-usage-table-panel">
				<header><div><span>明细记录</span><strong>{period === 'day' ? '每日 Token 消耗' : '每月 Token 消耗'}</strong></div><small>{from} 至 {to}</small></header>
				{rows.length ? <div className="token-usage-data-table"><div className="token-usage-data-row token-usage-data-head"><span>日期</span><span>功能模块</span><span>输入</span><span>输出</span><span>总量</span></div>{rows.slice().reverse().map((row) => <div className="token-usage-data-row" key={`${row.date}-${row.module}`}><span>{row.date}</span><span>{moduleLabels[row.module] || row.module}</span><span>{formatNumber(row.prompt_tokens)}</span><span>{formatNumber(row.completion_tokens)}</span><strong>{formatNumber(row.total_tokens)}</strong></div>)}</div> : <EmptyUsage />}
			</section>
		</section>
	);
}

function TokenStat({ label, value, detail, tone = '' }: { label: string; value: string; detail: string; tone?: string }) {
	return <div className={`token-usage-stat ${tone}`}><span>{label}</span><strong>{value}</strong><small>{detail}</small></div>;
}

function EmptyUsage() {
	return <div className="token-usage-empty-page"><Database size={20} /><span>当前范围暂无 Token usage 数据</span><small>模型返回 usage 后会自动出现在这里。</small></div>;
}

function aggregateModules(rows: TokenUsageSummary['rows']) {
	const totals = new Map<string, number>();
	rows.forEach((row) => totals.set(row.module, (totals.get(row.module) || 0) + row.total_tokens));
	return [...totals.entries()].map(([module, total_tokens]) => ({ module, total_tokens })).sort((a, b) => b.total_tokens - a.total_tokens);
}
