import type { MarketBillboardSeat } from './backend';
import { BILLBOARD_SEAT_MAPPINGS } from '../data/billboard-seat-mappings';

export type BillboardSeatKind = 'institution' | 'trader' | 'quant' | 'retail' | 'unknown';

export type BillboardSeatLabel = {
	kind: BillboardSeatKind;
	label: string;
	note: string;
};

export function classifyBillboardSeat(seat: MarketBillboardSeat): BillboardSeatLabel | null {
	const name = (seat.name || '').replace(/\s+/g, '');
	if (seat.institution) return { kind: 'institution', label: '机构', note: '数据源明确标记为机构席位' };
	const mapping = BILLBOARD_SEAT_MAPPINGS.find((rule) => rule.keywords.some((keyword) => name.includes(keyword.replace(/\s+/g, ''))));
	if (mapping) return { kind: mapping.kind, label: mapping.label, note: `${mapping.note}（置信度：${mapping.confidence}）` };
	return null;
}
