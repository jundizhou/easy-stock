import type {
	MarketBillboardItem,
	MarketFundFlow,
	MarketIndexSnapshot,
	MarketIndustryMomentum,
	MarketResearchItem,
	NewsItem,
	SourceMeta,
	ThemeOverview,
} from './backend';

export type MarketOverviewView =
	| 'pulse'
	| 'core-indexes'
	| 'industry-momentum'
	| 'industry-flow'
	| 'theme-flow'
	| 'stock-flow'
	| 'billboard'
	| 'announcements'
	| 'institution-reports'
	| 'industry-research';

export type MarketOverviewModule = {
	id: MarketOverviewView;
	name: string;
	description: string;
	status: 'ready';
};

export type MarketOverviewGroup = {
	id: 'overview' | 'flow' | 'research';
	name: string;
	modules: MarketOverviewModule[];
};

export const marketOverviewGroups: MarketOverviewGroup[] = [
	{
		id: 'overview',
		name: '市场总览',
		modules: [
			{ id: 'pulse', name: '新闻快讯', description: '市场新闻、题材与数据源状态', status: 'ready' },
			{ id: 'core-indexes', name: '市场核心指数', description: 'A/H/美核心指数走势与强弱', status: 'ready' },
		],
	},
	{
		id: 'flow',
		name: '资金结构',
		modules: [
			{ id: 'industry-momentum', name: '行业趋势强度', description: '行业多周期趋势强弱与领涨结构', status: 'ready' },
			{ id: 'industry-flow', name: '行业资金', description: '行业资金净流入与持续性', status: 'ready' },
			{ id: 'theme-flow', name: '题材概念', description: '题材概念资金强度与领涨结构', status: 'ready' },
			{ id: 'stock-flow', name: '个股资金流入流出', description: '个股总资金、主力与散户流向排名', status: 'ready' },
			{ id: 'billboard', name: '龙虎榜', description: '机构席位、营业部和上榜原因', status: 'ready' },
		],
	},
	{
		id: 'research',
		name: '研究信号',
		modules: [
			{ id: 'announcements', name: '公告雷达', description: '重要公告分类与影响线索', status: 'ready' },
			{ id: 'institution-reports', name: '机构观点', description: '评级、目标价与观点变化', status: 'ready' },
			{ id: 'industry-research', name: '产业透视', description: '行业景气、产业链与研究证据', status: 'ready' },
		],
	},
];

export const defaultMarketOverviewView: MarketOverviewView = 'pulse';

export function resolveMarketOverviewView(hash: string): MarketOverviewView {
	const candidate = hash.replace(/^#market\/?/, '').split(/[?&]/)[0] as MarketOverviewView;
	return marketOverviewGroups.some((group) => group.modules.some((module) => module.id === candidate))
		? candidate
		: defaultMarketOverviewView;
}

export function findMarketOverviewModule(view: MarketOverviewView): MarketOverviewModule {
	return marketOverviewGroups.flatMap((group) => group.modules).find((module) => module.id === view)
		|| marketOverviewGroups[0].modules[0];
}

export function buildMarketPulsePrompt(news: NewsItem[], themes: ThemeOverview[], asOf: string): string {
	const newsLines = news.slice(0, 10).map((item, index) => {
		const publishedAt = item.published_at || item.meta?.fetched_at || '时间未知';
		return `${index + 1}. [${publishedAt}] ${item.title}（来源：${item.meta?.source || '未知'}）`;
	});
	const themeLines = themes.slice(0, 8).map((item, index) => {
		const leaders = item.leaders?.slice(0, 3).join('、') || item.top_node || '暂无';
		return `${index + 1}. ${item.name}：涨跌 ${formatSigned(item.change_percent)}%，主力净流入 ${formatMoney(item.main_net_inflow)}，核心标的/节点 ${leaders}`;
	});

	return [
		`请基于以下 easy-stock 行情证据，生成截至 ${asOf} 的盘面解读。`,
		'要求：严格区分事实与推断；先总结市场状态，再列主线、潜在风险、反方证据和下一交易日验证条件；不得把缺失或延迟数据描述成实时事实。',
		'',
		'【市场快讯】',
		...(newsLines.length ? newsLines : ['暂无可用快讯']),
		'',
		'【题材快照】',
		...(themeLines.length ? themeLines : ['暂无可用题材快照']),
	].join('\n');
}

export type MarketOverviewEvidence = {
	indexes?: MarketIndexSnapshot[];
	industries?: MarketIndustryMomentum[];
	flows?: MarketFundFlow[];
	billboard?: MarketBillboardItem[];
	research?: MarketResearchItem[];
	meta?: SourceMeta | null;
};

export function buildMarketModulePrompt(view: Exclude<MarketOverviewView, 'pulse'>, evidence: MarketOverviewEvidence, asOf: string): string {
	const module = findMarketOverviewModule(view);
	const lines: string[] = [];
	if (evidence.indexes?.length) {
		lines.push(...evidence.indexes.slice(0, 20).map((item, index) => `${index + 1}. ${item.name}（${item.region}/${item.market}）：${item.price}，涨跌 ${formatSigned(item.change_percent)}%，状态 ${item.status}，行情时间 ${item.trade_time || '未知'}`));
	}
	if (evidence.industries?.length) {
		lines.push(...evidence.industries.slice(0, 20).map((item, index) => `${index + 1}. ${item.name}：动能 ${item.score.toFixed(1)}，当日 ${formatSigned(item.change_percent)}%，5日 ${formatSigned(item.five_day_change_percent)}%，20日 ${formatSigned(item.twenty_day_change_percent)}%，上涨/下跌 ${item.rising_count}/${item.falling_count}，主力 ${formatMoney(item.main_net_inflow)}，领涨 ${item.leader_name || '未知'}`));
	}
	if (evidence.flows?.length) {
		lines.push(...evidence.flows.slice(0, 25).map((item, index) => {
			const fields = evidence.meta?.available_fields || item.meta.available_fields || [];
			const available = (field: string) => !fields.length || fields.includes(field);
			const facts: string[] = [];
			if (available('change_percent')) facts.push(`涨跌 ${formatSigned(item.change_percent)}%`);
			if (available('net_inflow')) facts.push(`净流入 ${formatMoney(item.net_inflow)}（${formatSigned(item.net_inflow_ratio)}%）`);
			if (available('main_net_inflow')) facts.push(`主力净流入 ${formatMoney(item.main_net_inflow)}（${formatSigned(item.main_net_inflow_ratio)}%）`);
			if (available('retail_net_inflow')) facts.push(`散户净流入 ${formatMoney(item.retail_net_inflow)}（${formatSigned(item.retail_net_inflow_ratio)}%）`);
			if (available('inflow')) facts.push(`流入 ${formatMoney(item.inflow)}，流出 ${formatMoney(item.outflow)}`);
			if (available('leader_name') && item.leader_name) facts.push(`领涨 ${item.leader_name}${item.leader_symbol ? `（${item.leader_symbol}）` : ''} ${formatSigned(item.leader_change_percent)}%`);
			if (available('super_large_net_inflow')) facts.push(`超大单 ${formatMoney(item.super_large_net_inflow)}，大单 ${formatMoney(item.large_net_inflow)}`);
			return `${index + 1}. ${item.name}${item.symbol ? `（${item.symbol}）` : ''}：${facts.length ? facts.join('，') : '当前来源未提供可验证明细'}`;
		}));
	}
	if (evidence.billboard?.length) {
		lines.push(...evidence.billboard.slice(0, 20).map((item, index) => `${index + 1}. ${item.name}（${item.symbol}）：${item.trade_date} 上榜，净买额 ${formatMoney(item.net_amount)}，买入 ${formatMoney(item.buy_amount)}，卖出 ${formatMoney(item.sell_amount)}，机构买方 ${item.institution_buyers}，原因 ${item.reason}`));
	}
	if (evidence.research?.length) {
		lines.push(...evidence.research.slice(0, 25).map((item, index) => `${index + 1}. [${item.published_at || '时间未知'}] ${item.title}；标的 ${item.stock_name || item.industry_name || item.symbol || '行业/市场'}；机构 ${item.organization || '未标注'}；评级 ${item.rating || '未标注'}；分类 ${item.category || item.kind}；原文 ${item.url}`));
	}
	const source = evidence.meta?.source || firstEvidenceSource(evidence) || '未知';
	const fetchedAt = evidence.meta?.fetched_at || '未知';
	const fallback = evidence.meta?.fallback_reason ? `；降级说明：${evidence.meta.fallback_reason}` : '';
	return [
		`请基于以下 easy-stock「${module.name}」证据，生成截至 ${asOf} 的分析。`,
		'要求：严格区分事实与推断；给出核心结论、排序依据、反方证据、风险点和下一步验证条件；不得补造席位、价格、评级或实时状态。',
		`数据来源：${source}；抓取时间：${fetchedAt}${fallback}`,
		'',
		`【${module.name}证据】`,
		...(lines.length ? lines : ['暂无可用证据']),
	].join('\n');
}

function firstEvidenceSource(evidence: MarketOverviewEvidence) {
	return evidence.indexes?.[0]?.meta.source
		|| evidence.industries?.[0]?.meta.source
		|| evidence.flows?.[0]?.meta.source
		|| evidence.billboard?.[0]?.meta.source
		|| evidence.research?.[0]?.meta.source;
}

function formatSigned(value: number) {
	if (!Number.isFinite(value)) return '--';
	return `${value > 0 ? '+' : ''}${value.toFixed(2)}`;
}

function formatMoney(value: number) {
	if (!Number.isFinite(value)) return '--';
	const absolute = Math.abs(value);
	if (absolute >= 100_000_000) return `${(value / 100_000_000).toFixed(2)}亿`;
	if (absolute >= 10_000) return `${(value / 10_000).toFixed(1)}万`;
	return value.toFixed(0);
}
