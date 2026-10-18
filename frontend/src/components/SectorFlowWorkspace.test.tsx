import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import type { SectorFlowSeries } from '../lib/backend';
import { SectorFlowChart, SectorFlowWorkspace } from './SectorFlowWorkspace';

function series(code: string, name: string, values: number[]): SectorFlowSeries {
	const times = ['09:30', '10:30', '11:30', '13:30', '15:00'];
	return {
		code,
		name,
		trade_date: '2026-10-16',
		points: values.map((main, index) => ({ time: times[index], main, super_large: main, large: 0, medium: -main, small: 0 })),
		latest_main: values[values.length - 1],
		meta: { source: 'eastmoney:board-intraday-flow', fetched_at: '2026-10-16T15:01:00+08:00', latency_ms: 200, stale: false },
	};
}

describe('SectorFlowWorkspace', () => {
	it('renders the board ranking shell with dimension switch and sortable columns', () => {
		const markup = renderToStaticMarkup(<SectorFlowWorkspace config={null} refreshKey={0} onAskAI={() => {}} />);
		expect(markup).toContain('概念资金流');
		expect(markup).toContain('行业资金流');
		expect(markup).toContain('资金净流入排名');
		expect(markup).toContain('净流入占比');
		expect(markup).toContain('10日');
		expect(markup).toContain('已选 0/12');
		expect(markup).toContain('数据盘中实时更新');
		expect(markup).toContain('等待数据来源');
	});
});

describe('SectorFlowChart', () => {
	const charts = [series('BK0917', '半导体概念', [10, 20, 30, 40, 3_981_310_464]), series('BK0490', '军工', [-10, -20, -30, -40, -2_690_573_976])];

	it('draws one path per board with time ticks and end-point labels', () => {
		const markup = renderToStaticMarkup(<SectorFlowChart series={charts} hoverIndex={null} onHover={() => {}} />);
		expect(markup).toContain('板块分时累计净流入曲线');
		// 两条曲线 + 5 个时间刻度 + 6 条水平网格 + 6 条垂直网格。
		expect(markup.match(/<path /g) || []).toHaveLength(2);
		expect(markup).toContain('stroke="#e0533d"');
		expect(markup).toContain('stroke="#2f7fe0"');
		for (const time of ['09:30', '10:30', '11:30', '13:30', '15:00']) {
			expect(markup).toContain(`>${time}</text>`);
		}
		// 末端标签带板块名与当日累计金额，标签按最小间距错开。
		expect(markup).toContain('半导体概念 +39.81亿');
		expect(markup).toContain('军工 -26.91亿');
	});

	it('snaps the crosshair to the hovered minute and back to the close', () => {
		const axisLabel = (markup: string) => markup.match(/class="axis strong"[^>]*>([^<]*)</)?.[1];
		const hoverX = (markup: string) => markup.match(/class="hover-line" x1="([\d.]+)"/)?.[1];
		const close = renderToStaticMarkup(<SectorFlowChart series={charts} hoverIndex={null} onHover={() => {}} />);
		const hovered = renderToStaticMarkup(<SectorFlowChart series={charts} hoverIndex={1} onHover={() => {}} />);
		expect(axisLabel(close)).toBe('15:00');
		expect(axisLabel(hovered)).toBe('10:30');
		expect(hoverX(hovered)).not.toBe(hoverX(close));
		expect(close.match(/<circle /g) || []).toHaveLength(2);
		expect(hovered.match(/<circle /g) || []).toHaveLength(2);
	});
});
