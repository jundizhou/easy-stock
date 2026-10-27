import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { NarrativeSnapshot, parseNarrativeSnapshot, previewNarrativeBlocks } from './CockpitPatrolPanel';

describe('patrol narrative snapshot', () => {
	it('shows all five fields in market-first order, including risks and watch conditions', () => {
		const snapshot = parseNarrativeSnapshot([
			'- **资金方向**：--（数据不足）',
			'- **市场状态**：待确认（数据不足）',
			'- **接下来盯什么**：等待盘中快照',
			'- **主要风险**：数据不足，无法判断',
			'- **较上次变化**：--（无同口径对比）',
			'- **数据缺口**：行业资金未接入',
			'',
			'本简报仅用于信息整理与研究辅助，不构成投资建议。',
		].join('\n'));
		expect(snapshot?.fields.map((field) => field.label)).toEqual(['市场状态', '较上次变化', '资金方向', '主要风险', '接下来盯什么']);
		const output = renderToStaticMarkup(createElement(NarrativeSnapshot, { snapshot: snapshot! }));
		expect(output.match(/<dt(?:\s|>)/g)).toHaveLength(5);
		expect(output).toContain('<div class="is-market">');
		expect(output).toContain('class="nv-market-state"');
		expect(output.match(/class="nv-snapshot-item/g)).toHaveLength(4);
		expect(output).toContain('等待盘中快照');
		expect(output).toContain('数据不足，无法判断');
		expect(output).toContain('行业资金未接入');
		expect(output).toContain('不构成投资建议');
		expect(output).toContain('<footer class="nv-snapshot-footer">');
	});

	it('collapses to market state while keeping data gaps and extra warnings visible', () => {
		const snapshot = parseNarrativeSnapshot('市场状态：--（数据不足）\n主要风险：等待风险数据\n数据缺口：行情采集未就绪\n数据时效说明：仅为盘中快照\n本简报仅用于信息整理与研究辅助，不构成投资建议。');
		const output = renderToStaticMarkup(createElement(NarrativeSnapshot, { snapshot: snapshot!, expanded: false }));
		expect(output.match(/<dt(?:\s|>)/g)).toHaveLength(1);
		expect(output).toContain('--（数据不足）');
		expect(output).not.toContain('等待风险数据');
		expect(output).toContain('行情采集未就绪');
		expect(output).toContain('数据时效说明：仅为盘中快照');
		expect(output).not.toContain('不构成投资建议');
		expect(output).not.toContain('nv-snapshot-footer');
	});

	it('separates the explicitly stated market status and supporting text', () => {
		const snapshot = parseNarrativeSnapshot('市场状态：待确认；数据尚未采集（数据源）');
		const output = renderToStaticMarkup(createElement(NarrativeSnapshot, { snapshot: snapshot! }));
		expect(output).toContain('<strong class="nv-market-state">待确认</strong>');
		expect(output).toContain('<span class="nv-market-evidence">数据尚未采集');
	});

	it('uses placeholders for missing fields without inventing a market judgment', () => {
		const snapshot = parseNarrativeSnapshot('市场状态：--（数据不足）');
		expect(snapshot?.fields.slice(1).map((field) => field.text)).toEqual(['--', '--', '--', '--']);
		expect(snapshot?.gap).toBeNull();
	});

	it('accepts both bold label styles and preserves extra warnings', () => {
		const snapshot = parseNarrativeSnapshot('数据时效说明：仅为盘中快照\n**市场状态：** --（数据不足）\n**主要风险**: **等待核验**');
		expect(snapshot?.fields[0].text).toBe('--（数据不足）');
		expect(snapshot?.fields[3].text).toBe('**等待核验**');
		expect(snapshot?.remainder).toBe('数据时效说明：仅为盘中快照');
	});

	it('keeps older prose and ambiguous repeated fields on the original renderer', () => {
		expect(parseNarrativeSnapshot('## 一、外围市场\n历史叙述正文')).toBeNull();
		expect(parseNarrativeSnapshot('市场状态：待确认\n市场状态：数据不足')).toBeNull();
	});

	it('omits the historical emotion score comparison while retaining actual data gaps', () => {
		const markdown = '市场状态：数据不足\n数据缺口：市场温度来源失败，本次无可用温度读数；情绪周期为 2026-10-26 收盘口径 67.12 分，与本次盘中情绪 63.57 分不同口径，不作直接比较；行业资金未接入。';
		const snapshot = parseNarrativeSnapshot(markdown)!;
		expect(snapshot.gap).toBe('市场温度来源失败，本次无可用温度读数；行业资金未接入。');
		const output = renderToStaticMarkup(createElement(NarrativeSnapshot, { snapshot, expanded: false }));
		expect(output).toContain('市场温度来源失败');
		expect(output).toContain('行业资金未接入');
		expect(output).not.toContain('67.12');
		expect(output).not.toContain('63.57');
		expect(markdown).toContain('67.12');
	});

	it('keeps real emotion source failures and removes an empty comparison-only gap', () => {
		const missing = parseNarrativeSnapshot('市场状态：数据不足\n数据缺口：情绪周期来源失败，历史 67.12 分不可用。')!;
		expect(missing.gap).toBe('情绪周期来源失败，历史 67.12 分不可用。');
		const comparison = parseNarrativeSnapshot('市场状态：待确认\n数据缺口：情绪周期为 2026-10-26 收盘口径 67.12 分，不与盘中比较。')!;
		expect(comparison.gap).toBeNull();
		expect(renderToStaticMarkup(createElement(NarrativeSnapshot, { snapshot: comparison }))).not.toContain('nv-snapshot-gap');
	});
});

// 「展开全文」失效的根因是预览返回了整篇：播报整篇只有小标题和列表时，
// 找不到自然段(paragraph)就走了返回全部的兜底分支。这里锁住预览必须短于全文。
describe('patrol narrative preview', () => {
	it('列表型播报只保留首个小标题与前三条', () => {
		const list = Array.from({ length: 8 }, (_, index) => `${index + 1}. 第 ${index + 1} 条事实`).join('\n');
		const markdown = `## 一、外围市场与隔夜变化\n${list}\n\n## 二、A 股开盘可能的信号\n1. 另一段事实`;
		const blocks = previewNarrativeBlocks(markdown);
		const rendered = blocks.map((block) => (block.kind === 'list' ? block.items : [])).flat();
		expect(blocks.length).toBeLessThan(3);
		expect(rendered.length).toBe(3);
		expect(blocks.some((block) => block.kind === 'list')).toBe(true);
		expect(rendered).toEqual(['第 1 条事实', '第 2 条事实', '第 3 条事实']);
	});

	it('有自然段时只截到第一段', () => {
		const markdown = '## 一、外围市场\n美股三大指数分化。\n\n## 二、A 股\n更多内容';
		const blocks = previewNarrativeBlocks(markdown);
		expect(blocks).toHaveLength(2);
		expect(blocks[1]).toEqual({ kind: 'paragraph', text: '美股三大指数分化。' });
	});
});
