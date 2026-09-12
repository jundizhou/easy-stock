import { describe, expect, it } from 'vitest';
import {
	brokenRate,
	catalystEmptyReason,
	catalystHorizonLabel,
	catalystImpactLabel,
	catalystStrengthLabel,
	catalystTone,
	elapsedTradingRatio,
	formatAmount,
	formatAmountShort,
	formatClock,
	hotRankStatus,
	projectVolume,
	summarizeTelegraph,
	toneForValue,
	volumeBarHeights,
} from './workbench';

describe('formatAmount', () => {
	it('按中文口径换算万亿/亿/万', () => {
		expect(formatAmount(1_267_400_000_000)).toBe('1.27万亿');
		expect(formatAmount(234_500_000_000)).toBe('2345亿');
		expect(formatAmount(56_000_000)).toBe('5600万');
	});

	it('无效值返回占位符', () => {
		expect(formatAmount(0)).toBe('--');
		expect(formatAmount(Number.NaN)).toBe('--');
		expect(formatAmount(-5)).toBe('--');
	});
});

describe('formatAmountShort', () => {
	it('保留两位小数便于并排比较', () => {
		expect(formatAmountShort(1_267_400_000_000)).toBe('1.27万亿');
		expect(formatAmountShort(234_500_000_000)).toBe('2345.00亿');
	});
});

describe('elapsedTradingRatio', () => {
	const at = (hour: number, minute: number) => new Date(2026, 8, 10, hour, minute, 0);

	it('开盘前为 0，收盘后为 1', () => {
		expect(elapsedTradingRatio(at(9, 0))).toBe(0);
		expect(elapsedTradingRatio(at(15, 1))).toBe(1);
	});

	it('上午 10:30 恰好走完上午一半', () => {
		expect(elapsedTradingRatio(at(10, 30))).toBeCloseTo(0.25, 5);
	});

	it('午间休市期间固定为 0.5', () => {
		expect(elapsedTradingRatio(at(11, 30))).toBe(0.5);
		expect(elapsedTradingRatio(at(12, 30))).toBe(0.5);
	});

	it('下午 14:00 为 0.75', () => {
		expect(elapsedTradingRatio(at(14, 0))).toBeCloseTo(0.75, 5);
	});

	it('11:31（刚过上午收盘）仍是 0.5 而非越过', () => {
		expect(elapsedTradingRatio(at(11, 31))).toBe(0.5);
	});
});

describe('projectVolume', () => {
	const at = (hour: number, minute: number) => new Date(2026, 8, 10, hour, minute, 0);

	it('盘中按已流逝比例线性外推', () => {
		// 13:42 → 比例 = 0.5 + 42/240 = 0.675；1.27万亿 / 0.675 ≈ 1.88万亿
		const result = projectVolume(1_270_000_000_000, 1_860_000_000_000, at(13, 42));
		expect(result.closed).toBe(false);
		expect(result.current).toBe(1_270_000_000_000);
		expect(result.projected / 1e12).toBeCloseTo(1.88, 1);
		// 1.88万亿 > 1.86万亿（上一交易日）→ 小幅放量
		expect(result.delta).toBeGreaterThan(0);
		expect(result.deltaRatio).toBeGreaterThan(0);
	});

	it('缩量时 delta 为负', () => {
		// 比例 0.675、上一交易日 2.4万亿 → 预估 1.88万亿，明显缩量
		const result = projectVolume(1_270_000_000_000, 2_400_000_000_000, at(13, 42));
		expect(result.delta).toBeLessThan(0);
		expect(result.deltaRatio).toBeLessThan(0);
	});

	it('放量时 delta 为正', () => {
		const result = projectVolume(2_000_000_000_000, 1_860_000_000_000, at(14, 30));
		expect(result.delta).toBeGreaterThan(0);
		expect(result.deltaRatio).toBeGreaterThan(0);
	});

	it('收盘后不再外推，直接用实际值', () => {
		const result = projectVolume(1_500_000_000_000, 1_400_000_000_000, at(15, 30));
		expect(result.closed).toBe(true);
		expect(result.projected).toBe(1_500_000_000_000);
	});

	it('开盘瞬间不外推（避免除以极小比例导致天文数字）', () => {
		const result = projectVolume(10_000_000, 1_860_000_000_000, at(9, 30));
		expect(result.projected).toBe(10_000_000);
	});

	it('缺少量能基准时不给出比例', () => {
		const result = projectVolume(1_000_000_000, undefined, at(10, 30));
		expect(result.deltaRatio).toBeUndefined();
	});
});

describe('brokenRate', () => {
	it('炸板 / (涨停 + 炸板)', () => {
		expect(brokenRate(32, 21)).toBeCloseTo(39.62, 1);
	});

	it('无数据返回 undefined 而不是 0', () => {
		expect(brokenRate(0, 0)).toBeUndefined();
	});
});

describe('volumeBarHeights', () => {
	it('以区间最大值为 100%', () => {
		expect(volumeBarHeights([50, 100, 25])).toEqual([50, 100, 25]);
	});

	it('最低不低于 4%，保证柱子可见', () => {
		expect(volumeBarHeights([100, 1])).toEqual([100, 4]);
	});

	it('全无效时全部返回最小高度', () => {
		expect(volumeBarHeights([Number.NaN, 0])).toEqual([4, 4]);
	});
});

describe('toneForValue', () => {
	it('0 视为中性无色调', () => {
		expect(toneForValue(0)).toBeUndefined();
		expect(toneForValue(1.2)).toBe('up');
		expect(toneForValue(-1.2)).toBe('down');
	});
});

describe('formatClock / summarizeTelegraph', () => {
	it('格式化为 HH:MM', () => {
		expect(formatClock('2026-09-10T11:32:00+08:00')).toBe('11:32');
		expect(formatClock(undefined)).toBe('--:--');
		expect(formatClock('not-a-date')).toBe('--:--');
	});

	it('摘要压缩空白并截断', () => {
		expect(summarizeTelegraph('  A   B  ')).toBe('A B');
		expect(summarizeTelegraph('x'.repeat(200), 10)).toBe('xxxxxxxxxx…');
	});
});

describe('hotRankStatus', () => {
	it('首板涨停', () => {
		expect(hotRankStatus({ limitUpStreak: 1, limitUpDays: 1 })).toBe('首板涨停');
	});

	it('多天多板', () => {
		expect(hotRankStatus({ limitUpStreak: 4, limitUpDays: 4 })).toBe('4天4板');
		expect(hotRankStatus({ limitUpStreak: 2, limitUpDays: 3 })).toBe('3天2板');
	});

	it('无连板信息时退回涨跌停判断', () => {
		expect(hotRankStatus({ changePercent: 10.02 })).toBe('涨停');
		expect(hotRankStatus({ changePercent: -9.9 })).toBe('跌停');
		expect(hotRankStatus({ changePercent: 2.1 })).toBe('');
	});

	it('既无连板也无行情时返回空（不瞎猜状态）', () => {
		expect(hotRankStatus({})).toBe('');
	});
});

describe('catalyst helpers', () => {
	it('影响方向映射到涨红跌绿的语气', () => {
		expect(catalystTone('bullish')).toBe('up');
		expect(catalystTone('bearish')).toBe('down');
		expect(catalystTone('neutral')).toBe('flat');
		expect(catalystTone(undefined)).toBe('flat');
	});

	it('影响方向给出中文短标签', () => {
		expect(catalystImpactLabel('bullish')).toBe('利好');
		expect(catalystImpactLabel('bearish')).toBe('利空');
		expect(catalystImpactLabel('neutral')).toBe('中性');
		expect(catalystImpactLabel(undefined)).toBe('中性');
	});

	it('强度换算成档位而非裸数字', () => {
		expect(catalystStrengthLabel(92)).toBe('强');
		expect(catalystStrengthLabel(80)).toBe('强');
		expect(catalystStrengthLabel(65)).toBe('较强');
		expect(catalystStrengthLabel(45)).toBe('中等');
		expect(catalystStrengthLabel(20)).toBe('偏弱');
		expect(catalystStrengthLabel(undefined)).toBe('未知');
	});

	it('时间尺度标签', () => {
		expect(catalystHorizonLabel('immediate')).toBe('当日');
		expect(catalystHorizonLabel('short')).toBe('数日');
		expect(catalystHorizonLabel('medium')).toBe('数周');
		expect(catalystHorizonLabel(undefined)).toBe('');
	});

	it('空态文案把「没有催化」讲成结论而非故障', () => {
		expect(catalystEmptyReason(null)).toContain('正在扫描');
		expect(catalystEmptyReason({ scanned: 0 })).toContain('稍后重试');
		const empty = catalystEmptyReason({ scanned: 120, candidates: 3, modelUsed: true });
		expect(empty).toContain('120');
		expect(empty).toContain('未发现够格的催化消息');
	});

	it('降级或未启用 AI 时在空态里说明口径', () => {
		expect(catalystEmptyReason({ scanned: 120, modelUsed: false })).toContain('规则筛选');
		expect(catalystEmptyReason({ scanned: 120, modelUsed: true, note: '模型超时' })).toContain('模型超时');
	});
});
