import type { MarketBillboardSeat } from './backend';
import { BILLBOARD_SEAT_MAPPINGS } from '../data/billboard-seat-mappings';

export type BillboardSeatKind = 'institution' | 'trader' | 'quant' | 'retail' | 'unknown';

export type BillboardSeatLabel = {
	kind: BillboardSeatKind;
	label: string;
	note: string;
};

export function classifyBillboardSeat(seat: MarketBillboardSeat): BillboardSeatLabel | null {
	const name = normalizeSeatName(seat.name || '');
	if (seat.institution) return { kind: 'institution', label: '机构', note: '数据源明确标记为机构席位' };
	if (seat.source_label) {
		const sourceLabel = seat.source_label.trim();
		if (sourceLabel) {
			const lower = sourceLabel.toLowerCase();
			const kind: BillboardSeatKind = /机构/.test(sourceLabel) ? 'institution' : /量化|算法/.test(sourceLabel) || /quant/.test(lower) ? 'quant' : /散户|自然人|跟风/.test(sourceLabel) ? 'retail' : 'trader';
			return { kind, label: sourceLabel, note: `${seat.source === 'ths' ? '同花顺' : seat.source || '数据源'}公开标签（置信度：${seat.label_confidence || 'medium'}）${seat.label_note ? `；${seat.label_note}` : ''}` };
		}
	}
	const mapping = BILLBOARD_SEAT_MAPPINGS.find((rule) => rule.keywords.some((keyword) => name.includes(normalizeSeatName(keyword))));
	if (mapping) return { kind: mapping.kind, label: mapping.label, note: `${mapping.note}（置信度：${mapping.confidence}）` };
	return null;
}

function normalizeSeatName(value: string) {
	return value.replace(/[\s\u3000]/g, '').replace(/[（）]/g, (char) => char === '（' ? '(' : ')').replace(/有限责任公司|股份有限公司|有限公司/g, '');
}
