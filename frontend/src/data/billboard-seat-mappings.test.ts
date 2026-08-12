import { describe, expect, it } from 'vitest';
import { BILLBOARD_SEAT_MAPPINGS, BILLBOARD_SOURCE_NOTES } from './billboard-seat-mappings';
import { classifyBillboardSeat } from '../lib/billboard';

describe('billboard seat mappings', () => {
	it('keeps source notes for Eastmoney, THS and Kaipanla', () => {
		expect(BILLBOARD_SOURCE_NOTES.eastmoney.status).toContain('已接入');
		expect(BILLBOARD_SOURCE_NOTES.ths.confirmed).toContain('游资上榜');
		expect(BILLBOARD_SOURCE_NOTES.kaipanla.status).toContain('待确认');
	});

	it('classifies explicit institutions before heuristic mappings', () => {
		const result = classifyBillboardSeat({ direction: 'buy', rank: 1, name: '机构专用', buy_amount: 1, buy_ratio: 0, sell_amount: 0, sell_ratio: 0, net_amount: 1, institution: true });
		expect(result).not.toBeNull();
		if (!result) return;
		expect(result.kind).toBe('institution');
		expect(result.label).toBe('机构');
	});

	it('matches common public seat aliases with cautious labels', () => {
		const result = classifyBillboardSeat({ direction: 'buy', rank: 1, name: '中国银河证券股份有限公司绍兴证券营业部', buy_amount: 1, buy_ratio: 0, sell_amount: 0, sell_ratio: 0, net_amount: 1, institution: false });
		expect(result).not.toBeNull();
		if (!result) return;
		expect(result.label).toContain('赵老哥');
		expect(result.note).toContain('置信度');
		expect(BILLBOARD_SEAT_MAPPINGS.length).toBeGreaterThan(5);
	});
});
