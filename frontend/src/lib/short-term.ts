import { BoardStock, KLine, Quote, SectorMap, ThemeOverview } from './backend';

export type EmotionTone = 'hot' | 'warm' | 'mixed' | 'cool' | 'cold';

export type ThemeEmotion = {
	score: number;
	label: '亢奋' | '活跃' | '分歧' | '偏弱' | '冰点';
	stage: '主升' | '扩散' | '发酵' | '分歧' | '退潮' | '冰点';
	tone: EmotionTone;
	breadth: number;
	coverage: number;
};

export type ThemeStrengthWindow = 'daily' | 'five_day';

export type StockRole =
	| '高度龙头候选'
	| '先锋候选'
	| '容量核心候选'
	| '补涨候选'
	| '核心候选'
	| '中位跟随'
	| '低位观察'
	| '掉队';

export type LeadershipState = '待确认' | '启动' | '发酵' | '加速' | '分歧' | '修复' | '退潮';
export type ConfirmationLevel = '观察' | '候选' | '强候选' | '已确认';

export type LeadershipMetrics = {
	history_days: number;
	return_3d: number;
	return_5d: number;
	return_10d: number;
	relative_strength_5d: number;
	position_20d: number;
	strong_days_10d: number;
	limit_up_days_20d: number;
	max_limit_streak_20d: number;
	start_lag_days?: number;
	first_strong_date?: string;
	avg_amount_5d: number;
	avg_turnover_5d: number;
	average_next_premium: number;
	down_day_relative_strength: number;
	latest_return: number;
	previous_return: number;
};

export type LeadershipBreakdown = {
	height: number;
	timing: number;
	persistence: number;
	attention: number;
	influence_proxy: number;
	resilience: number;
	purity: number;
};

export type ThemeStock = BoardStock & {
	nodes: string[];
	occurrence: number;
	role: StockRole;
	role_reason: string;
	leader_score: number;
	tradability_score: number;
	confidence: number;
	confirmation: ConfirmationLevel;
	state: LeadershipState;
	evidence: string[];
	risks: string[];
	metrics: LeadershipMetrics;
	breakdown: LeadershipBreakdown;
	limit_regime: '10cm' | '20cm' | '30cm';
	live: boolean;
};

export type QuoteLookup = Record<string, Pick<Quote, 'price' | 'change' | 'change_percent'>>;
export type KLineLookup = Record<string, KLine[]>;

type BaseThemeStock = BoardStock & { nodes: string[]; occurrence: number; live: boolean };

type RawLeadership = {
	stock: BaseThemeStock;
	history: KLine[];
	dailyReturns: Array<{ date: string; value: number }>;
	metrics: LeadershipMetrics;
	latestRangePercent: number;
	lockedLimit: boolean;
};

export function calculateThemeEmotion(theme: ThemeOverview, scoreOverride?: number): ThemeEmotion {
	const breadth = theme.matched_nodes > 0 ? theme.rising_nodes / theme.matched_nodes : 0.5;
	const coverage = theme.total_nodes > 0 ? theme.matched_nodes / theme.total_nodes : 0;
	if (typeof scoreOverride === 'number' || typeof theme.trend_score === 'number') {
		const score = Math.round(clamp(scoreOverride ?? theme.trend_score ?? 0, 0, 100));
		const stage = normalizeTrendStage(theme.trend_stage, score);
		if (score >= 75) return { score, label: '亢奋', stage, tone: 'hot', breadth, coverage };
		if (score >= 60) return { score, label: '活跃', stage, tone: 'warm', breadth, coverage };
		if (score >= 45) return { score, label: '分歧', stage, tone: 'mixed', breadth, coverage };
		if (score >= 30) return { score, label: '偏弱', stage, tone: 'cool', breadth, coverage };
		return { score, label: '冰点', stage: '冰点', tone: 'cold', breadth, coverage };
	}
	const changeScore = 50 + clamp(theme.change_percent, -5, 5) * 8;
	const breadthScore = breadth * 100;
	const flowDirection = Math.sign(theme.main_net_inflow);
	const flowMagnitude = Math.min(30, Math.log10(1 + Math.abs(theme.main_net_inflow) / 1_000_000) * 10);
	const flowScore = 50 + flowDirection * flowMagnitude;
	const score = Math.round(clamp(changeScore * 0.55 + breadthScore * 0.3 + flowScore * 0.15, 0, 100));

	if (score >= 75) {
		return { score, label: '亢奋', stage: '主升', tone: 'hot', breadth, coverage };
	}
	if (score >= 60) {
		return { score, label: '活跃', stage: theme.change_percent >= 1.5 ? '主升' : '扩散', tone: 'warm', breadth, coverage };
	}
	if (score >= 45) {
		return { score, label: '分歧', stage: '分歧', tone: 'mixed', breadth, coverage };
	}
	if (score >= 30) {
		return { score, label: '偏弱', stage: '退潮', tone: 'cool', breadth, coverage };
	}
	return { score, label: '冰点', stage: '冰点', tone: 'cold', breadth, coverage };
}

export function themeStrengthScore(theme: ThemeOverview, window: ThemeStrengthWindow): number {
	const value = window === 'daily' ? theme.daily_strength_score : theme.five_day_strength_score;
	if (typeof value === 'number') {
		return Math.round(clamp(value, 0, 100));
	}
	return calculateThemeEmotion(theme).score;
}

function normalizeTrendStage(value: ThemeOverview['trend_stage'], score: number): ThemeEmotion['stage'] {
	switch (value) {
		case '主升':
		case '扩散':
		case '发酵':
		case '分歧':
		case '退潮':
			return value;
		default:
			if (score >= 75) return '主升';
			if (score >= 60) return '扩散';
			if (score >= 45) return '分歧';
			return '退潮';
	}
}

export function rankThemeOverviews(items: ThemeOverview[], window: ThemeStrengthWindow = 'daily'): ThemeOverview[] {
	if (items.some((item) => typeof item.source_rank === 'number' && item.source_rank > 0)) {
		const compare = (a: ThemeOverview, b: ThemeOverview) => {
			const aWindowScore = window === 'daily' ? a.daily_strength_score : a.five_day_strength_score;
			const bWindowScore = window === 'daily' ? b.daily_strength_score : b.five_day_strength_score;
			if (typeof aWindowScore === 'number' && typeof bWindowScore === 'number' && aWindowScore !== bWindowScore) {
				return bWindowScore - aWindowScore;
			}
			const aRank = a.source_rank && a.source_rank > 0 ? a.source_rank : Number.MAX_SAFE_INTEGER;
			const bRank = b.source_rank && b.source_rank > 0 ? b.source_rank : Number.MAX_SAFE_INTEGER;
			return aRank - bRank || calculateThemeEmotion(b).score - calculateThemeEmotion(a).score;
		};
		const provisional = items.filter((item) => item.provisional).sort(compare);
		const primary = items.filter((item) => !item.provisional && (item.source === 'duanxianxia:kaipanla' || item.theme.startsWith('kpl:'))).sort(compare);
		const fallback = items.filter((item) => !item.provisional && !(item.source === 'duanxianxia:kaipanla' || item.theme.startsWith('kpl:'))).sort(compare);
		const ranked = [...primary, ...fallback];
		if (provisional.length > 0) {
			ranked.splice(Math.min(1, ranked.length), 0, ...provisional);
		}
		return ranked;
	}
	return [...items].sort((a, b) => themeStrengthScore(b, window) - themeStrengthScore(a, window));
}

export function buildThemeStocks(
	map: SectorMap | null,
	liveQuotes: QuoteLookup = {},
	histories: KLineLookup = {},
): ThemeStock[] {
	const baseStocks = flattenThemeStocks(map, liveQuotes);
	if (!baseStocks.length) {
		return [];
	}

	const historyBySymbol = new Map<string, KLine[]>();
	for (const stock of baseStocks) {
		historyBySymbol.set(stock.symbol, normalizeHistory(histories[stock.symbol] || []));
	}
	const dailyReturnsBySymbol = new Map<string, Array<{ date: string; value: number }>>();
	for (const stock of baseStocks) {
		dailyReturnsBySymbol.set(stock.symbol, calculateDailyReturns(historyBySymbol.get(stock.symbol) || []));
	}
	const themeDailyReturns = buildThemeDailyReturns(dailyReturnsBySymbol);
	const tradingDates = [...themeDailyReturns.keys()].sort();
	const themeStartDate = detectThemeStartDate(dailyReturnsBySymbol, tradingDates, baseStocks.length);

	const rawItems = baseStocks.map<RawLeadership>((stock) => {
		const history = historyBySymbol.get(stock.symbol) || [];
		const dailyReturns = dailyReturnsBySymbol.get(stock.symbol) || [];
		const metrics = calculateLeadershipMetrics(stock, history, dailyReturns, themeDailyReturns, tradingDates, themeStartDate);
		const latest = history.at(-1);
		const latestRangePercent = latest?.close ? ((latest.high - latest.low) / latest.close) * 100 : 0;
		const lockedLimit = Boolean(
			latest
			&& metrics.latest_return >= approximateLimitThreshold(stock.symbol)
			&& Math.abs(latest.high - latest.low) < 0.001,
		);
		return { stock, history, dailyReturns, metrics, latestRangePercent, lockedLimit };
	});

	const peers = buildPeerRanks(rawItems);
	const lanePeers = new Map<string, ReturnType<typeof buildPeerRanks>>();
	for (const regime of ['10cm', '20cm', '30cm']) {
		const laneItems = rawItems.filter((item) => limitRegimeForStock(item.stock) === regime);
		if (laneItems.length) lanePeers.set(regime, buildPeerRanks(laneItems));
	}
	return rawItems
		.map<ThemeStock>((item) => {
			const regime = limitRegimeForStock(item.stock);
			return scoreLeadership(item, peers, lanePeers.get(regime) || peers, regime);
		})
		.sort((a, b) => b.leader_score - a.leader_score || b.tradability_score - a.tradability_score);
}

function flattenThemeStocks(map: SectorMap | null, liveQuotes: QuoteLookup): BaseThemeStock[] {
	if (!map) {
		return [];
	}
	const stocks = new Map<string, BaseThemeStock>();
	for (const group of map.groups) {
		for (const node of group.nodes) {
			for (const stock of node.stocks) {
				const previous = stocks.get(stock.symbol);
				const quote = liveQuotes[stock.symbol];
				const hydrated = quote
					? { ...stock, price: quote.price, change: quote.change, change_percent: quote.change_percent }
					: stock;
				if (!previous) {
					stocks.set(stock.symbol, { ...hydrated, nodes: [node.name], occurrence: 1, live: Boolean(quote) });
					continue;
				}
				stocks.set(stock.symbol, {
					...previous,
					...hydrated,
					amount: Math.max(previous.amount, hydrated.amount),
					main_net_inflow: Math.abs(hydrated.main_net_inflow) > Math.abs(previous.main_net_inflow)
						? hydrated.main_net_inflow
						: previous.main_net_inflow,
					nodes: previous.nodes.includes(node.name) ? previous.nodes : [...previous.nodes, node.name],
					occurrence: previous.occurrence + 1,
					live: previous.live || Boolean(quote),
				});
			}
		}
	}
	return [...stocks.values()];
}

function calculateLeadershipMetrics(
	stock: BaseThemeStock,
	history: KLine[],
	dailyReturns: Array<{ date: string; value: number }>,
	themeDailyReturns: Map<string, number>,
	tradingDates: string[],
	themeStartDate?: string,
): LeadershipMetrics {
	const recent20 = history.slice(-20);
	const recent10Returns = dailyReturns.slice(-10);
	const recent5 = history.slice(-5);
	const recent5Returns = dailyReturns.slice(-5);
	const threshold = approximateLimitThreshold(stock.symbol);
	const limitFlags = dailyReturns.slice(-20).map((item) => item.value >= threshold);
	const kLineFirstStrong = dailyReturns.slice(-20).find((item) => item.value >= 5)?.date;
	const firstStrong = earliestDate(kLineFirstStrong, stock.first_limit_date);
	const lastLine = history.at(-1);
	const minLow = recent20.length ? Math.min(...recent20.map((line) => line.low)) : 0;
	const maxHigh = recent20.length ? Math.max(...recent20.map((line) => line.high)) : 0;
	const position20 = lastLine && maxHigh > minLow ? ((lastLine.close - minLow) / (maxHigh - minLow)) * 100 : 50;
	const theme5 = themeReturnForDates(themeDailyReturns, recent5Returns.map((item) => item.date));
	const return5 = cumulativeReturn(history, 5);
	const downDayEdges = recent10Returns
		.filter((item) => (themeDailyReturns.get(item.date) || 0) < 0)
		.map((item) => item.value - (themeDailyReturns.get(item.date) || 0));

	return {
		history_days: history.length,
		return_3d: cumulativeReturn(history, 3),
		return_5d: return5,
		return_10d: cumulativeReturn(history, 10),
		relative_strength_5d: return5 - theme5,
		position_20d: clamp(position20, 0, 100),
		strong_days_10d: recent10Returns.filter((item) => item.value >= 3).length,
		limit_up_days_20d: Math.max(limitFlags.filter(Boolean).length, stock.limit_up_count || stock.limit_up_days || 0),
		max_limit_streak_20d: Math.max(maxBooleanStreak(limitFlags), stock.limit_up_streak || 0),
		start_lag_days: calculateStartLag(firstStrong, themeStartDate, tradingDates),
		first_strong_date: firstStrong,
		avg_amount_5d: average(recent5.map((line) => line.amount || 0)),
		avg_turnover_5d: average(recent5.map((line) => line.turnover_rate || 0)),
		average_next_premium: calculateAverageNextPremium(history, dailyReturns, threshold),
		down_day_relative_strength: downDayEdges.length ? average(downDayEdges) : 0,
		latest_return: dailyReturns.at(-1)?.value ?? stock.change_percent,
		previous_return: dailyReturns.at(-2)?.value ?? 0,
	};
}

function buildPeerRanks(items: RawLeadership[]) {
	return {
		return3: items.map((item) => item.metrics.return_3d),
		return5: items.map((item) => item.metrics.return_5d),
		return10: items.map((item) => item.metrics.return_10d),
		relative5: items.map((item) => item.metrics.relative_strength_5d),
		strongDays: items.map((item) => item.metrics.strong_days_10d),
		limitDays: items.map((item) => item.metrics.limit_up_days_20d),
		limitStreak: items.map((item) => item.metrics.max_limit_streak_20d),
		amount: items.map((item) => item.metrics.avg_amount_5d || item.stock.amount || 0),
		turnover: items.map((item) => item.metrics.avg_turnover_5d),
		occurrence: items.map((item) => item.stock.occurrence),
		resilience: items.map((item) => item.metrics.down_day_relative_strength),
	};
}

function scoreLeadership(
	item: RawLeadership,
	peers: ReturnType<typeof buildPeerRanks>,
	lanePeers: ReturnType<typeof buildPeerRanks>,
	regime: '10cm' | '20cm' | '30cm',
): ThemeStock {
	const { stock, history, metrics } = item;
	const height = weightedAverage([
		[percentileScore(lanePeers.return10, metrics.return_10d), 0.38],
		[percentileScore(lanePeers.return5, metrics.return_5d), 0.25],
		[percentileScore(lanePeers.limitStreak, metrics.max_limit_streak_20d), 0.25],
		[percentileScore(lanePeers.limitDays, metrics.limit_up_days_20d), 0.12],
	]);
	const timing = metrics.start_lag_days === undefined ? 25 : clamp(100 - metrics.start_lag_days * 18, 20, 100);
	const persistence = weightedAverage([
		[percentileScore(peers.strongDays, metrics.strong_days_10d), 0.45],
		[clamp(50 + metrics.average_next_premium * 8, 0, 100), 0.3],
		[clamp(50 + metrics.return_3d * 3, 0, 100), 0.25],
	]);
	const amountValue = metrics.avg_amount_5d || stock.amount || 0;
	const hasAmount = amountValue > 0;
	const hasTurnover = metrics.avg_turnover_5d > 0;
	const attention = weightedAverage([
		[hasAmount ? percentileScore(peers.amount, amountValue) : 28, 0.7],
		[hasTurnover ? percentileScore(peers.turnover, metrics.avg_turnover_5d) : 35, 0.3],
	]);
	const influenceProxy = weightedAverage([
		[percentileScore(peers.relative5, metrics.relative_strength_5d), 0.45],
		[percentileScore(peers.occurrence, stock.occurrence), 0.35],
		[percentileScore(peers.return3, metrics.return_3d), 0.2],
	]);
	const resilience = weightedAverage([
		[percentileScore(peers.resilience, metrics.down_day_relative_strength), 0.65],
		[metrics.previous_return < -1 && metrics.latest_return > 2 ? 85 : 50, 0.35],
	]);
	const purity = clamp(45 + stock.occurrence * 15, 45, 90);
	const breakdown: LeadershipBreakdown = {
		height: Math.round(height),
		timing: Math.round(timing),
		persistence: Math.round(persistence),
		attention: Math.round(attention),
		influence_proxy: Math.round(influenceProxy),
		resilience: Math.round(resilience),
		purity: Math.round(purity),
	};
	let computedLeaderScore = height * 0.2
		+ timing * 0.15
		+ persistence * 0.15
		+ attention * 0.15
		+ influenceProxy * 0.2
		+ resilience * 0.1
		+ purity * 0.05;
	if (metrics.history_days < 15) computedLeaderScore -= 8;
	if (stock.change_percent < -3 && metrics.relative_strength_5d < 0) computedLeaderScore -= 6;
	computedLeaderScore = Math.round(clamp(computedLeaderScore, 0, 100));
	const leaderScore = typeof stock.rank_score === 'number' ? stock.rank_score : computedLeaderScore;

	let tradabilityScore = attention * 0.55
		+ (hasTurnover ? percentileScore(peers.turnover, metrics.avg_turnover_5d) : 35) * 0.2
		+ rangeTradabilityScore(item.latestRangePercent) * 0.25;
	if (item.lockedLimit) tradabilityScore -= 35;
	if (metrics.position_20d >= 95) tradabilityScore -= 8;
	tradabilityScore = Math.round(clamp(tradabilityScore, 0, 100));

	const hasExactLimitData = Boolean(stock.limit_up_streak || stock.last_limit_date);
	const confidence = calculateConfidence(stock, metrics, hasAmount, hasTurnover, hasExactLimitData);
	const confirmation = confirmationLevel(leaderScore, confidence);
	const state = leadershipState(stock, metrics);
	const role = isStockRole(stock.rank_role)
		? stock.rank_role
		: classifyLeadershipRole(item, lanePeers, leaderScore, attention, timing);
	const evidence = buildEvidence(stock, metrics, breakdown);
	const risks = buildRisks(item, hasAmount, hasTurnover, confidence, hasExactLimitData);
	const latestHistory = history.at(-1);

	return {
		...stock,
		amount: stock.amount || latestHistory?.amount || metrics.avg_amount_5d,
		volume: stock.volume || latestHistory?.volume || 0,
		role,
		role_reason: evidence[0] || '尚未形成稳定的题材领导证据',
		leader_score: leaderScore,
		tradability_score: tradabilityScore,
		confidence,
		confirmation,
		state,
		evidence,
		risks,
		metrics,
		breakdown,
		limit_regime: regime,
	};
}

function isStockRole(value?: string): value is StockRole {
	return value === '高度龙头候选'
		|| value === '先锋候选'
		|| value === '容量核心候选'
		|| value === '补涨候选'
		|| value === '核心候选'
		|| value === '中位跟随'
		|| value === '低位观察'
		|| value === '掉队';
}

function classifyLeadershipRole(
	item: RawLeadership,
	peers: ReturnType<typeof buildPeerRanks>,
	leaderScore: number,
	attention: number,
	timing: number,
): StockRole {
	const { stock, metrics } = item;
	if (stock.change_percent < -1 && metrics.relative_strength_5d < 0 && leaderScore < 55) {
		return '掉队';
	}
	if (
		metrics.max_limit_streak_20d >= 1
		&& (
			metrics.max_limit_streak_20d >= 2
			|| percentileScore(peers.limitStreak, metrics.max_limit_streak_20d) >= 75
			|| Math.max(
				percentileScore(peers.return5, metrics.return_5d),
				percentileScore(peers.return10, metrics.return_10d),
			) >= 80
		)
		&& leaderScore >= 68
	) {
		return '高度龙头候选';
	}
	if (timing >= 82 && metrics.first_strong_date && leaderScore >= 62) {
		return '先锋候选';
	}
	if (attention >= 78 && leaderScore >= 62) {
		return '容量核心候选';
	}
	if ((metrics.start_lag_days || 0) >= 1 && stock.change_percent >= 3 && metrics.relative_strength_5d >= 0) {
		return '补涨候选';
	}
	if (leaderScore >= 62) {
		return '核心候选';
	}
	if (leaderScore >= 45 || stock.change_percent >= 1) {
		return '中位跟随';
	}
	return '低位观察';
}

function leadershipState(stock: BaseThemeStock, metrics: LeadershipMetrics): LeadershipState {
	const current = stock.change_percent;
	if (current <= -3 && metrics.return_5d < 0) return '退潮';
	if (metrics.previous_return < -1.5 && current >= 2.5) return '修复';
	if (current < 0 && metrics.return_5d >= 5 && metrics.position_20d >= 70) return '分歧';
	if (metrics.return_5d >= 12 && current >= 4) return '加速';
	if ((metrics.start_lag_days ?? 99) <= 1 && current >= 3 && metrics.return_5d < 10) return '启动';
	if (metrics.return_5d >= 4) return '发酵';
	return '待确认';
}

function calculateConfidence(
	stock: BaseThemeStock,
	metrics: LeadershipMetrics,
	hasAmount: boolean,
	hasTurnover: boolean,
	hasExactLimitData: boolean,
): number {
	let confidence = 0.18;
	confidence += Math.min(metrics.history_days / 30, 1) * 0.34;
	confidence += hasAmount ? 0.08 : 0;
	confidence += hasTurnover ? 0.06 : 0;
	confidence += hasExactLimitData ? 0.08 : 0;
	confidence += Math.min(stock.occurrence / 3, 1) * 0.08;
	return Number(Math.min(confidence, 0.82).toFixed(2));
}

function confirmationLevel(score: number, confidence: number): ConfirmationLevel {
	if (score >= 78 && confidence >= 0.85) return '已确认';
	if (score >= 72 && confidence >= 0.68) return '强候选';
	if (score >= 58 && confidence >= 0.48) return '候选';
	return '观察';
}

function buildEvidence(stock: BaseThemeStock, metrics: LeadershipMetrics, breakdown: LeadershipBreakdown): string[] {
	const evidence: string[] = [];
	if (stock.limit_up_streak) {
		evidence.push(`${limitRegimeForStock(stock)}涨停池确认：最高${stock.limit_up_streak}连板，近${stock.limit_up_days || stock.limit_up_count || stock.limit_up_streak}日${stock.limit_up_count || stock.limit_up_streak}板`);
	}
	if (metrics.return_5d >= 3) evidence.push(`近5日累计${signedPercent(metrics.return_5d)}，持续性高于单日涨幅信号`);
	if (metrics.max_limit_streak_20d > 0) evidence.push(`近20日识别到最高${metrics.max_limit_streak_20d}连板、${metrics.limit_up_days_20d}个涨停日`);
	if (metrics.start_lag_days !== undefined) {
		if (metrics.start_lag_days <= 0) evidence.push('强势启动不晚于题材集体启动，具备先锋时序');
		else evidence.push(`较题材启动晚${metrics.start_lag_days}个交易日，需按补涨路径观察`);
	}
	if (metrics.relative_strength_5d >= 1) evidence.push(`近5日相对题材中位数领先${metrics.relative_strength_5d.toFixed(1)}个百分点`);
	if (metrics.down_day_relative_strength >= 1) evidence.push(`题材回落日平均跑赢板块${metrics.down_day_relative_strength.toFixed(1)}个百分点`);
	if (metrics.avg_amount_5d > 0) evidence.push(`近5日平均成交额${formatCompactMoney(metrics.avg_amount_5d)}`);
	if (stock.occurrence > 1) evidence.push(`同时覆盖${stock.occurrence}个题材细分节点，题材中心性较高`);
	if (!evidence.length && breakdown.height >= 60) evidence.push('阶段强度位于题材前列，但持续性证据仍不足');
	return evidence.slice(0, 5);
}

function buildRisks(
	item: RawLeadership,
	hasAmount: boolean,
	hasTurnover: boolean,
	confidence: number,
	hasExactLimitData: boolean,
): string[] {
	const risks = ['尚未接入分钟级领涨—跟随关系，板块带动力使用日线代理'];
	if (item.metrics.limit_up_days_20d > 0 && !hasExactLimitData) risks.push('连板高度由日K涨幅近似识别，待涨停事件流校准');
	if (item.metrics.history_days < 15) risks.push('历史样本不足15个交易日');
	if (!hasAmount || !hasTurnover) risks.push('成交额或换手率数据不完整，可交易性置信度受限');
	if (item.lockedLimit) risks.push('最新涨停接近无换手状态，市场地位与可交易性需分开');
	if (item.metrics.position_20d >= 92) risks.push('价格处于20日区间高位，分歧风险上升');
	if (confidence < 0.5) risks.push('当前只能作为观察标签，不能确认龙头身份');
	return risks.slice(0, 4);
}

function normalizeHistory(lines: KLine[]): KLine[] {
	return [...lines]
		.filter((line) => Number.isFinite(line.close) && line.close > 0)
		.sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime());
}

function calculateDailyReturns(history: KLine[]): Array<{ date: string; value: number }> {
	return history.map((line, index) => {
		const previous = history[index - 1];
		const calculated = previous?.close ? ((line.close / previous.close) - 1) * 100 : 0;
		return {
			date: line.time.slice(0, 10),
			value: typeof line.change_percent === 'number' && line.change_percent !== 0 ? line.change_percent : calculated,
		};
	});
}

function buildThemeDailyReturns(
	returnsBySymbol: Map<string, Array<{ date: string; value: number }>>,
): Map<string, number> {
	const byDate = new Map<string, number[]>();
	for (const returns of returnsBySymbol.values()) {
		for (const item of returns) {
			const values = byDate.get(item.date) || [];
			values.push(item.value);
			byDate.set(item.date, values);
		}
	}
	const result = new Map<string, number>();
	for (const [date, values] of byDate) {
		result.set(date, median(values));
	}
	return result;
}

function detectThemeStartDate(
	returnsBySymbol: Map<string, Array<{ date: string; value: number }>>,
	tradingDates: string[],
	stockCount: number,
): string | undefined {
	const recentDates = tradingDates.slice(-20);
	const minimumStrongStocks = Math.max(2, Math.ceil(stockCount * 0.25));
	const breadthStarts: string[] = [];
	for (const date of recentDates) {
		let strong = 0;
		for (const returns of returnsBySymbol.values()) {
			if ((returns.find((item) => item.date === date)?.value || 0) >= 5) strong++;
		}
		if (strong >= Math.min(minimumStrongStocks, stockCount)) breadthStarts.push(date);
	}
	if (breadthStarts.length) return breadthStarts.at(-1);
	for (const date of recentDates) {
		for (const returns of returnsBySymbol.values()) {
			if ((returns.find((item) => item.date === date)?.value || 0) >= 5) return date;
		}
	}
	return undefined;
}

function calculateStartLag(firstStrong?: string, themeStart?: string, tradingDates: string[] = []): number | undefined {
	if (!firstStrong || !themeStart) return undefined;
	const firstIndex = tradingDates.indexOf(firstStrong);
	const themeIndex = tradingDates.indexOf(themeStart);
	if (firstIndex < 0 || themeIndex < 0) return undefined;
	return firstIndex - themeIndex;
}

function themeReturnForDates(themeReturns: Map<string, number>, dates: string[]): number {
	if (dates.length < 2) return 0;
	return dates.slice(1).reduce((total, date) => total + (themeReturns.get(date) || 0), 0);
}

function cumulativeReturn(history: KLine[], bars: number): number {
	if (history.length < 2) return 0;
	const end = history.at(-1)?.close || 0;
	const startIndex = Math.max(0, history.length - 1 - bars);
	const start = history[startIndex]?.close || 0;
	return start > 0 ? ((end / start) - 1) * 100 : 0;
}

function calculateAverageNextPremium(
	history: KLine[],
	dailyReturns: Array<{ date: string; value: number }>,
	threshold: number,
): number {
	const premiums: number[] = [];
	for (let index = 0; index < history.length - 1; index++) {
		if ((dailyReturns[index]?.value || 0) < threshold || history[index].close <= 0) continue;
		premiums.push(((history[index + 1].open / history[index].close) - 1) * 100);
	}
	return premiums.length ? average(premiums) : 0;
}

function approximateLimitThreshold(symbol: string): number {
	const code = symbol.slice(0, 6);
	if (code.startsWith('30') || code.startsWith('68')) return 19.3;
	if (code.startsWith('8') || code.startsWith('4')) return 29.2;
	return 9.5;
}

function limitRegimeForStock(stock: Pick<BoardStock, 'symbol' | 'limit_regime'>): '10cm' | '20cm' | '30cm' {
	if (stock.limit_regime === '20cm' || stock.limit_regime === '30cm' || stock.limit_regime === '10cm') {
		return stock.limit_regime;
	}
	const code = stock.symbol.slice(0, 6);
	if (code.startsWith('30') || code.startsWith('68')) return '20cm';
	if (code.startsWith('8') || code.startsWith('4')) return '30cm';
	return '10cm';
}

function earliestDate(...values: Array<string | undefined>): string | undefined {
	const dates = values.filter((value): value is string => Boolean(value)).sort();
	return dates[0];
}

function maxBooleanStreak(values: boolean[]): number {
	let current = 0;
	let maximum = 0;
	for (const value of values) {
		current = value ? current + 1 : 0;
		maximum = Math.max(maximum, current);
	}
	return maximum;
}

function percentileScore(values: number[], value: number): number {
	if (values.length <= 1) return 70;
	const below = values.filter((item) => item < value).length;
	const equal = values.filter((item) => item === value).length;
	return clamp(((below + Math.max(equal - 1, 0) / 2) / (values.length - 1)) * 100, 0, 100);
}

function weightedAverage(items: Array<[number, number]>): number {
	return items.reduce((total, [value, weight]) => total + value * weight, 0);
}

function rangeTradabilityScore(rangePercent: number): number {
	if (rangePercent <= 0.2) return 20;
	if (rangePercent <= 2) return 55;
	if (rangePercent <= 8) return 85;
	if (rangePercent <= 14) return 65;
	return 40;
}

function average(values: number[]): number {
	return values.length ? values.reduce((total, value) => total + value, 0) / values.length : 0;
}

function median(values: number[]): number {
	if (!values.length) return 0;
	const sorted = [...values].sort((a, b) => a - b);
	const middle = Math.floor(sorted.length / 2);
	return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
}

function signedPercent(value: number): string {
	return `${value >= 0 ? '+' : ''}${value.toFixed(1)}%`;
}

function formatCompactMoney(value: number): string {
	if (Math.abs(value) >= 100_000_000) return `${(value / 100_000_000).toFixed(1)}亿`;
	if (Math.abs(value) >= 10_000) return `${Math.round(value / 10_000)}万`;
	return value.toFixed(0);
}

function clamp(value: number, min: number, max: number): number {
	return Math.min(max, Math.max(min, value));
}
