import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import type { SectorFlowSeries, SectorPriceSeries } from '../lib/backend';
import { SECTOR_FLOW_AXIS_GUTTER, SECTOR_PRICE_EMPTY_AXIS_EXTENT, formatSectorFlowPercent, formatSectorFlowPriceAxis, sectorFlowFundCurves, sectorFlowPriceCurves } from '../lib/sector-flow';
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

function priceSeries(code: string, name: string, changes: number[]): SectorPriceSeries {
	const times = ['09:30', '10:30', '11:30', '13:30', '15:00'];
	return {
		code,
		name,
		trade_date: '2026-10-16',
		pre_close: 2419.01,
		points: changes.map((change_percent, index) => ({ time: times[index], price: 2419.01 * (1 + change_percent / 100), average: 2419.01, change_percent })),
		latest_change_percent: changes[changes.length - 1],
		meta: { source: 'eastmoney:board-intraday-trend', fetched_at: '2026-10-16T15:01:00+08:00', latency_ms: 120, stale: false },
	};
}

/** 下横轴（第二条 axis-rail）的像素宽度，用来验证左右留白。 */
function railSpan(markup: string) {
	const rails = [...markup.matchAll(/class="axis-rail" x1="([\d.]+)" x2="([\d.]+)"/g)];
	const last = rails[rails.length - 1];
	return Number(last[2]) - Number(last[1]);
}

describe('SectorFlowWorkspace', () => {
	it('renders the board ranking shell with dimension switch and sortable columns', () => {
		const markup = renderToStaticMarkup(<SectorFlowWorkspace config={null} refreshKey={0} onAskAI={() => {}} />);
		expect(markup).toContain('概念资金流');
		expect(markup).toContain('行业资金流');
		expect(markup).toContain('板块资金流');
		expect(markup).toContain('资金曲线');
		// 曲线区是一块满高图 + 口径切换：默认画资金曲线，价格分时用同一套图例与状态切换过去。
		expect(markup).toContain('aria-label="曲线口径"');
		expect(markup).toContain('aria-pressed="true">资金曲线');
		expect(markup).toContain('价格分时');
		expect(markup).toContain('aria-label="资金曲线"');
		expect(markup).toContain('INTRADAY NET INFLOW');
		// 单位随口径变化：默认是资金曲线的亿元。
		expect(markup).toContain('单位：亿元');
		expect(markup).toContain('净流入占比');
		expect(markup).toContain('10日');
		// 概览卡片一行四个：榜首板块 / 净流入板块 / 净流出板块 / 合计净流入。
		expect(markup).toContain('榜首板块');
		expect(markup).toContain('净流出板块');
		expect(markup).toContain('已选 0/12');
		expect(markup).toContain('等待数据来源');
		// 资金口径滑动按钮：当日/5日/10日三档，默认选中当日。
		expect(markup).toContain('aria-label="资金口径"');
		expect(markup).toContain('sector-flow-period-thumb');
		expect(markup).toContain('aria-pressed="true">当日');
		// 板块维度同样是滑动按钮：选中档带白色滑块，两个分段控件外观一致。
		expect(markup).toContain('aria-label="板块维度"');
		expect(markup).toContain('sector-flow-tabs-thumb');
		// 口径切换排在搜索框前面，搜索框之后才是「已选中」与右侧操作。
		expect(markup.indexOf('aria-label="资金口径"')).toBeLessThan(markup.indexOf('sector-flow-search'));
		expect(markup.indexOf('sector-flow-search')).toBeLessThan(markup.indexOf('已选中'));
		// 表头点击可在净流入榜/净流出榜之间切换方向，当前排序列带方向箭头。
		expect(markup).toContain('aria-sort="descending"');
		expect(markup).toContain('sector-flow-sort-arrow');
		expect(markup).toContain('再点一次切换为升序（净流出榜）');
		// 净流入占比一列同样可排序，标题跟随资金口径。
		expect(markup).toContain('按当日主力净占比排队');
		expect(markup).toContain('按当日涨跌幅排序');
		// 曲线区先画坐标轴框架，没选板块时也是「等数据的图表」而不是空白。
		expect(markup).toContain('sector-flow-chart-stage');
		expect(markup).toContain('>09:31</text>');
	});
});

describe('SectorFlowChart', () => {
	const charts = [series('BK0917', '半导体概念', [10, 20, 30, 40, 3_981_310_464]), series('BK0490', '军工', [-10, -20, -30, -40, -2_690_573_976])];
	const curves = sectorFlowFundCurves(charts);

	it('draws one path per board with time ticks and end-point labels', () => {
		const markup = renderToStaticMarkup(<SectorFlowChart curves={curves} hoverIndex={null} onHover={() => {}} />);
		expect(markup).toContain('板块分时累计净流入曲线');
		// 两条曲线 + 5 个时间刻度 + 6 条水平网格 + 5 条垂直网格。
		expect(markup.match(/<path /g) || []).toHaveLength(2);
		expect(markup).toContain('stroke="#e0533d"');
		expect(markup).toContain('stroke="#2f7fe0"');
		// 时间轴按整个交易日铺开：数据最早的一分钟与收盘两端都在轴上（中间档位随宽度自适应，10:00 / 14:00 落在各档步长上）。
		for (const time of ['09:30', '10:00', '14:00', '15:00']) {
			expect(markup).toContain(`>${time}</text>`);
		}
		// 末端标签带板块名与当日累计金额，标签按最小间距错开。
		expect(markup).toContain('半导体概念 +39.81亿');
		expect(markup).toContain('军工 -26.91亿');
		// 有曲线时才在右侧留出末端标签区，绘图区相应变窄（空态是左右等宽）。
		expect(railSpan(markup)).toBeLessThan(980 - SECTOR_FLOW_AXIS_GUTTER * 2);
	});

	it('keeps the whole session on the axis and pins the end labels to the data end', () => {
		// 数据只画到 09:35：坐标轴照样到收盘，十字线与末端标签挂在数据末端，不是钉在绘图区右缘。
		const early: Array<{ code: string; name: string; trade_date: string; points: Array<{ time: string; value: number }> }> = [
			{ code: 'BK0917', name: '半导体概念', trade_date: '2026-10-16', points: [{ time: '09:31', value: 1_000_000_000 }, { time: '09:35', value: 2_000_000_000 }] },
			{ code: 'BK0490', name: '军工', trade_date: '2026-10-16', points: [{ time: '09:31', value: -1_000_000_000 }, { time: '09:35', value: -3_000_000_000 }] },
		];
		const markup = renderToStaticMarkup(<SectorFlowChart curves={early} hoverIndex={null} onHover={() => {}} />);
		expect(markup).toContain('>15:00</text>');
		expect(markup).toContain('>14:00</text>');
		expect(markup).toMatch(/class="axis strong"[^>]*>09:35</);
		// 标签紧贴数据末端（十字线右侧十几像素），而绘图区右缘在 700px 之外。
		const labelX = Number(markup.match(/class="series-label" x="([\d.]+)"/)?.[1]);
		const hoverX = Number(markup.match(/class="hover-line" x1="([\d.]+)"/)?.[1]);
		expect(labelX).toBeGreaterThan(hoverX);
		expect(labelX - hoverX).toBeLessThan(14);
		expect(labelX).toBeLessThan(400);
	});

	it('draws the axis frame on its own so the block is never a blank box', () => {
		const markup = renderToStaticMarkup(<SectorFlowChart curves={[]} hoverIndex={null} onHover={() => {}} />);
		// 左竖轴 + 下横轴 + 时间刻度：09:31 起，宽度不足时步长自动放宽。
		expect(markup).toContain('class="axis-rail"');
		expect(markup).toContain('class="tick"');
		for (const time of ['09:31', '10:00', '15:00']) {
			expect(markup).toContain(`>${time}</text>`);
		}
		// 空框架也带完整的纵轴刻度数字（±40亿 占位轴），不会只剩一条没有数字的竖轴。
		for (const label of ['-40亿', '-20亿', '0亿', '20亿', '40亿']) {
			expect(markup).toContain(`>${label}</text>`);
		}
		// 没有曲线时右侧不留标签区，左右留白等宽，坐标轴在面板里居中。
		expect(railSpan(markup)).toBe(980 - SECTOR_FLOW_AXIS_GUTTER * 2);
		// 没有数据不画静态十字线，也不出末端标签。
		expect(markup).not.toContain('hover-line');
		expect(markup).not.toContain('series-label');
	});

	it('follows the mouse with the crosshair even before any curve is loaded', () => {
		// 120 号分钟点是 13:01：空框架下鼠标移上来也要有跟随的竖线与时点标签。
		const markup = renderToStaticMarkup(<SectorFlowChart curves={[]} hoverIndex={120} onHover={() => {}} />);
		expect(markup).toContain('hover-line');
		expect(markup).toContain('>13:01</text>');
	});

	it('snaps the crosshair to the hovered minute and back to the close', () => {
		const axisLabel = (markup: string) => markup.match(/class="axis strong"[^>]*>([^<]*)</)?.[1];
		const hoverX = (markup: string) => markup.match(/class="hover-line" x1="([\d.]+)"/)?.[1];
		const close = renderToStaticMarkup(<SectorFlowChart curves={curves} hoverIndex={null} onHover={() => {}} />);
		const hovered = renderToStaticMarkup(<SectorFlowChart curves={curves} hoverIndex={1} onHover={() => {}} />);
		expect(axisLabel(close)).toBe('15:00');
		expect(axisLabel(hovered)).toBe('09:31');
		expect(hoverX(hovered)).not.toBe(hoverX(close));
		expect(close.match(/<circle /g) || []).toHaveLength(2);
		expect(hovered.match(/<circle /g) || []).toHaveLength(2);
		// 悬停浮层只在鼠标停在图上时出现，列出该时点各板块数值。
		expect(hovered).toContain('sector-flow-chart-tip');
		expect(hovered).toContain('半导体概念');
		expect(close).not.toContain('sector-flow-chart-tip');
	});

	// 价格分时复用同一个曲线组件，只是纵轴与数值换成涨跌幅：末端标签、悬停浮层、占位刻度都要跟着换口径。
	it('renders price curves with a percent axis and percent labels', () => {
		const priceCurves = sectorFlowPriceCurves([priceSeries('BK0917', '半导体概念', [0.5, 1, -0.18]), priceSeries('BK0490', '军工', [0.2, -0.6, -1.4])]);
		const markup = renderToStaticMarkup(<SectorFlowChart
			curves={priceCurves}
			formatAxis={formatSectorFlowPriceAxis}
			formatValue={formatSectorFlowPercent}
			emptyExtent={SECTOR_PRICE_EMPTY_AXIS_EXTENT}
			ariaLabel="板块价格分时曲线"
			hoverIndex={null}
			onHover={() => {}}
		/>);
		expect(markup).toContain('板块价格分时曲线');
		expect(markup.match(/<path /g) || []).toHaveLength(2);
		// 末端标签是板块名 + 最新涨跌幅，不再出现金额。
		expect(markup).toContain('半导体概念 -0.18%');
		expect(markup).toContain('军工 -1.40%');
		expect(markup).not.toContain('亿');
		// 纵轴刻度也是涨跌幅：范围 -1.4 ~ 1，刻度落在 -2% / 0% / 1% 上。
		for (const label of ['-2%', '0%', '1%']) {
			expect(markup).toContain(`>${label}</text>`);
		}
	});

	it('gives the price chart its own percent placeholder axis when nothing is selected', () => {
		const markup = renderToStaticMarkup(<SectorFlowChart
			curves={[]}
			formatAxis={formatSectorFlowPriceAxis}
			formatValue={formatSectorFlowPercent}
			emptyExtent={SECTOR_PRICE_EMPTY_AXIS_EXTENT}
			ariaLabel="板块价格分时曲线"
			hoverIndex={null}
			onHover={() => {}}
		/>);
		// 占位轴是 ±2%：空框架也有自己的刻度口径，不会沿用资金曲线的「亿」。
		for (const label of ['-2%', '-1%', '0%', '1%', '2%']) {
			expect(markup).toContain(`>${label}</text>`);
		}
		expect(markup).not.toContain('亿');
	});
});
