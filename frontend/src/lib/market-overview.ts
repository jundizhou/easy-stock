import type {
	MarketBillboardItem,
	MarketBillboardDetail,
	MarketFundFlow,
	MarketIndexSnapshot,
	MarketIndustryMomentum,
	MarketResearchItem,
	NewsItem,
	SourceMeta,
	ThemeOverview,
	LimitUpLadderData,
} from './backend';
import { BILLBOARD_SEAT_MAPPINGS } from '../data/billboard-seat-mappings';

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

export type MarketBillboardPromptEvidence = {
	items: MarketBillboardItem[];
	details: Record<string, MarketBillboardDetail | undefined>;
	limitUp?: LimitUpLadderData | null;
	meta?: SourceMeta | null;
};

export function buildMarketBillboardPrompt(evidence: MarketBillboardPromptEvidence, asOf: string): string {
	const itemGroups = groupBillboardItems(evidence.items, 20);
	const items = itemGroups.map((records) => records[0]);
	const limitUpBySymbol = new Map<string, LimitUpLadderData['current']['levels'][number]['stocks'][number]>();
	const billboardDate = items[0]?.trade_date || '';
	const limitUpDay = [evidence.limitUp?.current, evidence.limitUp?.previous].find((day) => day?.trade_date === billboardDate);
	for (const level of limitUpDay?.levels || []) {
		for (const stock of level.stocks) limitUpBySymbol.set(stock.symbol, stock);
	}
	const stockLines = itemGroups.map((records, index) => {
		const item = records[0];
		const detail = records.map((record) => evidence.details[marketBillboardEvidenceKey(record)]).find(Boolean);
		const ladderStock = item.trade_date === limitUpDay?.trade_date ? limitUpBySymbol.get(item.symbol) : undefined;
		const buySeats = detail?.buy_seats || [];
		const sellSeats = detail?.sell_seats || [];
		const buyAmount = buySeats.reduce((sum, seat) => sum + seat.buy_amount, 0);
		const sellAmount = sellSeats.reduce((sum, seat) => sum + seat.sell_amount, 0);
		const institutionNet = uniqueSeats([...buySeats, ...sellSeats]).filter((seat) => seat.institution).reduce((sum, seat) => sum + seat.net_amount, 0);
		const buyTopOneConcentration = seatConcentration(buySeats.map((seat) => seat.buy_amount), item.buy_amount || buyAmount, 1);
		const buyTopThreeConcentration = seatConcentration(buySeats.map((seat) => seat.buy_amount), item.buy_amount || buyAmount, 3);
		const sellTopOneConcentration = seatConcentration(sellSeats.map((seat) => seat.sell_amount), item.sell_amount || sellAmount, 1);
		const sellTopThreeConcentration = seatConcentration(sellSeats.map((seat) => seat.sell_amount), item.sell_amount || sellAmount, 3);
		const activeSeatNames = [...buySeats, ...sellSeats].map((seat) => `${seat.name}${seat.institution ? '（机构席位）' : ''}`).filter((name, seatIndex, names) => names.indexOf(name) === seatIndex);
		const namedTraders = detectSeatLabels(activeSeatNames);
		const reasons = records.map((record) => record.reason).filter(Boolean).filter((reason, reasonIndex, allReasons) => allReasons.indexOf(reason) === reasonIndex).join('；');
		const topic = ladderStock
			? [ladderStock.primary_theme, ...(ladderStock.secondary_themes || []), ...(ladderStock.raw_concepts || [])].filter(Boolean).filter((name, topicIndex, topics) => topics.indexOf(name) === topicIndex).slice(0, 6).join('、')
			: '';
		const ladder = ladderStock
			? `连板高度 ${ladderStock.streak}板，${ladderStock.streak_label || '涨停梯队'}，行业 ${ladderStock.industry || '未提供'}，题材 ${topic || '未提供'}，题材角色 ${ladderStock.theme_leader_role || '未提供'}，换手率 ${formatPercentValue(ladderStock.turnover_rate)}，成交额 ${formatMoney(ladderStock.amount)}`
			: '未在同交易日连板梯队中命中，连板高度和题材关联暂无可验证数据';
		const seats = detail
			? `买方席位 ${buySeats.length} 个：${formatSeats(buySeats)}；卖方席位 ${sellSeats.length} 个：${formatSeats(sellSeats)}；机构席位净额 ${formatMoney(institutionNet)}；买方买一/前三集中度 ${formatPercentValue(buyTopOneConcentration)} / ${formatPercentValue(buyTopThreeConcentration)}，卖方卖一/前三集中度 ${formatPercentValue(sellTopOneConcentration)} / ${formatPercentValue(sellTopThreeConcentration)}；买卖席位结构（买入 ${formatMoney(buyAmount)} / 卖出 ${formatMoney(sellAmount)} / 席位净额 ${formatMoney(buyAmount - sellAmount)}）；活跃席位 ${activeSeatNames.length ? activeSeatNames.join('、') : '未提供'}；市场常用游资/活跃资金席位标签（非官方身份核验）${namedTraders.length ? namedTraders.join('、') : '未匹配到可靠标签，身份未确认'}`
			: '买卖五席明细获取失败，无法验证机构净买入、席位集中度和买卖双方结构';
		return `${index + 1}. ${item.name}（${item.symbol}）：上榜日 ${item.trade_date}，收盘 ${formatNumber(item.close_price)}，涨跌 ${formatSigned(item.change_percent)}%，换手率 ${formatPercentValue(item.turnover_rate)}，龙虎榜买入 ${formatMoney(item.buy_amount)}，卖出 ${formatMoney(item.sell_amount)}，净买额 ${formatMoney(item.net_amount)}，机构买方数量 ${item.institution_buyers}，买卖原因 ${reasons || '未提供'}；${ladder}；${seats}`;
	});
	const conceptHeat = evidence.limitUp?.current.trade_date === billboardDate ? evidence.limitUp.concept_heat : [];
	const marketContext = limitUpDay
		? `同交易日连板环境：涨停 ${limitUpDay.limit_up_count} 家，连板 ${limitUpDay.board_count} 家，最高 ${limitUpDay.max_streak} 板，首板 ${limitUpDay.first_board_count} 家；题材热度前五：${(conceptHeat || []).slice(0, 5).map((item) => `${item.name}（${item.board_count}板，最高${item.max_streak}板，热度${item.heat.toFixed(1)}）`).join('、') || '该交易日未提供聚合题材热度，以逐股题材归因为准'}`
		: '同交易日连板环境和题材热度未获取，不得补造';
	const source = evidence.meta?.source || items[0]?.meta.source || '未知';
	const fetchedAt = evidence.meta?.fetched_at || items[0]?.meta.fetched_at || '未知';
	const fallback = evidence.meta?.fallback_reason ? `；降级说明：${evidence.meta.fallback_reason}` : '';
	return [
		`请基于以下 easy-stock「龙虎榜」结构化证据，生成截至 ${asOf} 的次日交易观察报告。`,
		'你只能使用下方事实，不得补造席位名称、知名游资身份、连板高度、题材关系、股价或实时状态；所有判断必须标注“事实”或“推断”。',
		'分析目标：识别机构和活跃资金的真实偏好，区分主动买入、被动接力、冲高兑现和潜在派发，给出可验证的次日观察与风险处置名单。',
		'输出格式必须严格包含：一、市场结论；二、逐股龙虎榜拆解；三、机构/活跃席位与买卖结构；四、连板高度与题材关联；五、明日最值得观察的3个名单；六、明日需要优先止损/割肉评估的3个名单；七、验证条件与总风险提示。',
		'“明日最值得观察的3个名单”与“明日需要优先止损/割肉评估的3个名单”必须各列出3只不同股票，且两份名单互不重复；每只必须写名称、代码、入选事实、明日观察触发条件和失效条件。若证据不足，明确写“证据不足”，不得为了凑数虚构理由。后一个标题中的“割肉”仅表示“若用户已经持有，则优先进行风险处置评估”，不得写成确定性收益或交易指令。',
		'席位集中度按买一/买前三占龙虎榜买入总额、卖一/卖前三占龙虎榜卖出总额计算；机构席位净额按所有标记为机构席位的买卖净额合计。营业部名称不能证明某位游资身份，只有席位名称明确包含可识别标签时才可称为知名游资，否则写“活跃营业部/身份未确认”。',
		`数据来源：${source}；抓取时间：${fetchedAt}${fallback}`,
		'',
		'【同交易日市场环境】',
		marketContext,
		'',
		'【龙虎榜逐股证据】',
		...(stockLines.length ? stockLines : ['暂无可用龙虎榜证据']),
		'',
		'【结论约束】',
		'不得把龙虎榜净买额直接等同于次日上涨；必须讨论高位接力、连板断板、题材退潮、机构兑现、买卖席位失衡和数据延迟风险。',
	].join('\n');
}

function marketBillboardEvidenceKey(item: MarketBillboardItem) {
	return `${item.trade_date}|${item.symbol}|${item.reason}`;
}

function groupBillboardItems(items: MarketBillboardItem[], limit: number) {
	const groups: MarketBillboardItem[][] = [];
	const groupBySymbol = new Map<string, MarketBillboardItem[]>();
	for (const item of items) {
		let group = groupBySymbol.get(item.symbol);
		if (!group) {
			if (groups.length >= limit) continue;
			group = [];
			groupBySymbol.set(item.symbol, group);
			groups.push(group);
		}
		group.push(item);
	}
	return groups;
}

function formatSeats(seats: MarketBillboardDetail['buy_seats']) {
	return seats.length ? seats.map((seat) => `${seat.rank}.${seat.name}${seat.institution ? '[机构]' : ''} 买${formatMoney(seat.buy_amount)} 卖${formatMoney(seat.sell_amount)} 净${formatMoney(seat.net_amount)}`).join('；') : '未提供';
}

function seatConcentration(amounts: number[], total: number, seatCount: number) {
	if (!Number.isFinite(total) || total <= 0) return Number.NaN;
	return amounts.slice().sort((a, b) => b - a).slice(0, seatCount).reduce((sum, amount) => sum + amount, 0) / total * 100;
}

function uniqueSeats(seats: MarketBillboardDetail['buy_seats']) {
	return seats.filter((seat, index, allSeats) => allSeats.findIndex((candidate) => `${candidate.name}|${candidate.buy_amount}|${candidate.sell_amount}` === `${seat.name}|${seat.buy_amount}|${seat.sell_amount}`) === index);
}

function detectSeatLabels(names: string[]) {
	const result = names.flatMap((name) => BILLBOARD_SEAT_MAPPINGS.filter((item) => item.keywords.some((keyword) => name.replace(/\s+/g, '').includes(keyword.replace(/\s+/g, '')))).map((item) => `${item.label}（${item.confidence}置信度，${item.note}）`));
	return result.filter((label, index) => result.indexOf(label) === index);
}

function formatPercentValue(value: number) {
	return Number.isFinite(value) ? `${value.toFixed(2)}%` : '--';
}

function formatNumber(value: number) {
	return Number.isFinite(value) ? value.toFixed(2) : '--';
}

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
