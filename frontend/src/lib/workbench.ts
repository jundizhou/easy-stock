/**
 * 工作台（Workbench）派生计算。
 *
 * 这里只做纯函数：把后端返回的原始数据换算成界面要展示的口径。
 * 单独抽出来的原因是这些换算最容易出错（时分秒口径、成交额预估、量能对比），
 * 放成纯函数才能被单元测试盯住。
 */

/** 沪深两市成交额 → 「1.27万亿」这种中文口径。 */
export function formatAmount(value: number): string {
	if (!Number.isFinite(value) || value <= 0) return '--';
	if (value >= 1e12) return `${(value / 1e12).toFixed(2)}万亿`;
	if (value >= 1e8) return `${(value / 1e8).toFixed(0)}亿`;
	if (value >= 1e4) return `${(value / 1e4).toFixed(0)}万`;
	return value.toFixed(0);
}

/** 缩略金额：用于量能柱、预计值等空间紧张处。 */
export function formatAmountShort(value: number): string {
	if (!Number.isFinite(value) || value <= 0) return '--';
	if (value >= 1e12) return `${(value / 1e12).toFixed(2)}万亿`;
	if (value >= 1e8) return `${(value / 1e8).toFixed(2)}亿`;
	return `${(value / 1e4).toFixed(0)}万`;
}

/**
 * A 股交易时段的「已流逝比例」。
 *
 * 上午 09:30–11:30、下午 13:00–15:00，各 120 分钟，全天 240 分钟。
 * 返回值范围 [0, 1]：未开盘返回 0，收盘后返回 1。
 * 注意：这里只按时钟计算，不含集合竞价与停牌，够用于成交额线性预估。
 */
export function elapsedTradingRatio(now: Date): number {
	const minutes = now.getHours() * 60 + now.getMinutes() + now.getSeconds() / 60;
	const morningStart = 9 * 60 + 30;
	const morningEnd = 11 * 60 + 30;
	const afternoonStart = 13 * 60;
	const afternoonEnd = 15 * 60;
	if (minutes <= morningStart) return 0;
	if (minutes >= afternoonEnd) return 1;
	if (minutes <= morningEnd) return (minutes - morningStart) / 240;
	if (minutes < afternoonStart) return 0.5;
	return 0.5 + (minutes - afternoonStart) / 240;
}

export type VolumeProjection = {
	/** 截至当前的成交额 */
	current: number;
	/** 按时间线性外推的全天成交额 */
	projected: number;
	/** 相对上一交易日的增减额（正=放量） */
	delta: number;
	/** 相对上一交易日的增减比例（正=放量），无量能基准时为 undefined */
	deltaRatio?: number;
	/** 是否已过收盘（此时 projected 即真实值） */
	closed: boolean;
};

/**
 * 成交额预估。
 *
 * 方法：按交易时段已流逝比例做线性外推。这是盘中最常用的粗估口径，
 * 早盘会偏乐观（A 股成交额分布前高后低），所以界面上应标注「线性外推」。
 * 收盘后（比例=1）直接用当日实际值，不再外推。
 */
export function projectVolume(current: number, previousClose: number | undefined, now: Date): VolumeProjection {
	const safeCurrent = Number.isFinite(current) && current > 0 ? current : 0;
	const ratio = elapsedTradingRatio(now);
	const closed = ratio >= 1;
	let projected = safeCurrent;
	if (!closed && ratio > 0.02) {
		projected = safeCurrent / ratio;
	}
	const hasBase = typeof previousClose === 'number' && Number.isFinite(previousClose) && previousClose > 0;
	const delta = hasBase ? projected - (previousClose as number) : 0;
	const deltaRatio = hasBase ? delta / (previousClose as number) : undefined;
	return { current: safeCurrent, projected, delta, deltaRatio, closed };
}

/** 两融/涨跌家数的中性色：0 视为中性。 */
export function toneForValue(value?: number): 'up' | 'down' | undefined {
	if (typeof value !== 'number' || !Number.isFinite(value) || value === 0) return undefined;
	return value > 0 ? 'up' : 'down';
}

/** 炸板率 = 炸板数 / (涨停数 + 炸板数)，返回百分比数值。 */
export function brokenRate(limitUpCount: number, brokenCount: number): number | undefined {
	const total = limitUpCount + brokenCount;
	if (!Number.isFinite(total) || total <= 0) return undefined;
	return (brokenCount / total) * 100;
}

/**
 * 近 20 日量能柱的高度比例（相对区间最大值的百分比，最低 4% 保证可见）。
 * 传入的 amount 数组按「旧 → 新」排列。
 */
export function volumeBarHeights(amounts: number[]): number[] {
	const valid = amounts.filter((value) => Number.isFinite(value) && value > 0);
	if (valid.length === 0) return amounts.map(() => 4);
	const max = Math.max(...valid);
	return amounts.map((value) => {
		if (!Number.isFinite(value) || value <= 0 || max <= 0) return 4;
		return Math.max(4, (value / max) * 100);
	});
}

/** 把 ISO 时间格式化为 HH:MM，用于电报时间轴。 */
export function formatClock(value?: string): string {
	if (!value) return '--:--';
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return '--:--';
	return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
}

/** 电报正文摘要：截断到指定字数，并压掉多余空白。 */
export function summarizeTelegraph(content: string | undefined, limit = 90): string {
	const text = (content ?? '').replace(/\s+/g, ' ').trim();
	if (text.length <= limit) return text;
	return `${text.slice(0, limit)}…`;
}

/**
 * 热榜「状态」标签。
 *
 * 热榜数据源本身只提供「代码/名称/排名」，不含连板与涨幅，
 * 因此状态与涨幅需由调用方用连板梯队 / 实时行情补齐后传入。
 */
export function hotRankStatus(entry: { limitUpStreak?: number; limitUpDays?: number; changePercent?: number }): string {
	const streak = entry.limitUpStreak ?? 0;
	const days = entry.limitUpDays ?? 0;
	if (streak > 0 && days > 0) {
		if (streak === 1) return '首板涨停';
		return days > streak ? `${days}天${streak}板` : `${streak}天${streak}板`;
	}
	const change = entry.changePercent ?? 0;
	if (change >= 9.8) return '涨停';
	if (change <= -9.8) return '跌停';
	return '';
}

// ── 情绪催化（Catalyst） ──────────────────────────────────────────

/** 影响方向 → 界面语气。涨红跌绿，neutral 走中性色。 */
export function catalystTone(impact: string | undefined): 'up' | 'down' | 'flat' {
	if (impact === 'bullish') return 'up';
	if (impact === 'bearish') return 'down';
	return 'flat';
}

/** 影响方向 → 中文短标签。 */
export function catalystImpactLabel(impact: string | undefined): string {
	if (impact === 'bullish') return '利好';
	if (impact === 'bearish') return '利空';
	return '中性';
}

/** 影响强度 → 中文档位，避免让用户面对裸数字。 */
export function catalystStrengthLabel(strength: number | undefined): string {
	if (typeof strength !== 'number' || !Number.isFinite(strength)) return '未知';
	if (strength >= 80) return '强';
	if (strength >= 60) return '较强';
	if (strength >= 40) return '中等';
	return '偏弱';
}

/** 影响时间尺度 → 中文短标签。 */
export function catalystHorizonLabel(horizon: string | undefined): string {
	if (horizon === 'immediate') return '当日';
	if (horizon === 'medium') return '数周';
	if (horizon === 'short') return '数日';
	return '';
}

/**
 * 催化筛选的空态文案。
 *
 * 「一条都没筛出来」是本功能设计上被鼓励的结果，文案必须把它讲成
 * 「今天确实没有够格的催化」，而不是让用户以为加载失败。
 */
export function catalystEmptyReason(meta: { scanned?: number; candidates?: number; modelUsed?: boolean; note?: string } | null): string {
	if (!meta) return '正在扫描电报流…';
	const scanned = meta.scanned ?? 0;
	if (scanned === 0) return '暂时取不到电报流，稍后重试';
	const base = `已扫描 ${scanned} 条电报，未发现够格的催化消息`;
	if (meta.note) return `${base}（${meta.note}）`;
	if (!meta.modelUsed) return `${base}（当前为规则筛选，未接入 AI 精筛）`;
	return `${base}——空仓也是一种信息`;
}
