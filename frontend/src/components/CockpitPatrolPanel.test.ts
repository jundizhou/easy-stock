import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import type { PatrolBrief, PatrolTemperature } from '../lib/cockpit-patrol';
import { Narrative, NarrativeReport, NarrativeSnapshot, TemperatureGauge, intradayNextRunTime, narrativeInitiallyExpanded, parseNarrativeReport, parseNarrativeSnapshot, patrolNarrativeText, previewNarrativeBlocks } from './CockpitPatrolPanel';

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
		expect(output).not.toContain('不构成投资建议');
		expect(output).not.toContain('nv-snapshot-footer');
	});

	it('collapses to market state while keeping data gaps and extra warnings visible', () => {
		const snapshot = parseNarrativeSnapshot('市场状态：--（数据不足）\n主要风险：等待风险数据\n数据缺口：行情采集未就绪\n数据时效说明：仅为盘中快照\n本简报仅用于信息整理与研究辅助，不构成投资建议。');
		const output = renderToStaticMarkup(createElement(NarrativeSnapshot, { snapshot: snapshot!, expanded: false }));
		expect(output.match(/<dt(?:\s|>)/g)).toHaveLength(1);
		expect(output).toContain('--（数据不足）');
		expect(output).not.toContain('等待风险数据');
		expect(output).toContain('行情采集未就绪');
		expect(output).not.toContain('数据时效说明：仅为盘中快照');
		expect(output).not.toContain('不构成投资建议');
		expect(output).not.toContain('nv-snapshot-footer');
	});

	it('keeps a legacy market judgment in one title without dropping its evidence', () => {
		const snapshot = parseNarrativeSnapshot('市场状态：待确认；数据尚未采集（数据源）');
		const output = renderToStaticMarkup(createElement(NarrativeSnapshot, { snapshot: snapshot! }));
		expect(output).toContain('<strong class="nv-market-state" title="待确认；数据尚未采集（数据源）">待确认；数据尚未采集');
		expect(output).not.toContain('nv-market-evidence');
		expect(snapshot?.fields[0].text).toBe('待确认；数据尚未采集（数据源）');
	});

	it('preserves concise titles, long original text and missing-data warnings', () => {
		const text = '测试概述：指数改善但高位分歧仍待验证';
		const snapshot = parseNarrativeSnapshot(`市场状态：${text}\n数据缺口：市场温度刷新失败，当前使用缓存`)!;
		const output = renderToStaticMarkup(createElement(NarrativeSnapshot, { snapshot }));
		expect(output).toContain(`title="${text}">${text}</strong>`);
		expect(output).toContain('市场温度刷新失败，当前使用缓存');
		const longText = '测试历史原文：' + '保持完整证据，不改写存储记录。'.repeat(10);
		const legacy = renderToStaticMarkup(createElement(NarrativeSnapshot, { snapshot: parseNarrativeSnapshot(`市场状态：${longText}`)! }));
		expect(legacy).toContain(`title="${longText}"`);
		expect(legacy).toContain(longText);
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
		expect(snapshot?.remainder).toBe('');
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

	const historicalBrief: PatrolBrief = {
		id: 'historical-test', key: 'test', phase: 'intraday', session_date: '2026-10-28', label: '盘中巡查 11:10',
		trigger: 'scheduled', status: 'ready', narrative_status: 'ready', started_at: '2026-10-28T11:10:00+08:00',
		facts: { sources: [{ name: '市场温度', ok: false }] },
	};
	const oldGap = '市场温度数据刷新失败，目前沿用 2026-10-28T10:39:50 抓取的缓存快照（71.654 度、上涨占比 70.4747%），涨跌分布结论以该缓存为准；该时间只是抓取时间，并非成交时间。';

	it('attributes a historical gap to its patrol and rejects unsupported cache prose without rewriting history', () => {
		const snapshot = parseNarrativeSnapshot(`市场状态：待确认\n数据缺口：${oldGap}`)!;
		const before = JSON.stringify({ snapshot, brief: historicalBrief });
		const output = renderToStaticMarkup(createElement(NarrativeSnapshot, { snapshot, brief: historicalBrief, expanded: false }));
		expect(output).toContain('<strong>巡查缺口</strong>');
		expect(output).toMatch(/2026-10-28 \d{2}:\d{2} 巡查：/);
		expect(output).toContain('该次采集无可用读数');
		expect(output).toContain('原文引用的旧缓存没有该次事实支持');
		expect(output).toContain('实时采集状态见下方对应模块');
		expect(output).toContain(`title="${oldGap}"`);
		const visible = output.replace(/ title="[^"]*"/g, '');
		expect(visible).not.toContain('71.654');
		expect(visible).not.toContain('以该缓存为准');
		expect(visible).not.toContain('该时间只是');
		expect(JSON.stringify({ snapshot, brief: historicalBrief })).toBe(before);
	});

	it('preserves other source gaps alongside the corrected historical temperature claim', () => {
		const snapshot = parseNarrativeSnapshot(`市场状态：待确认\n数据缺口：行业资金未接入；${oldGap}快讯采集失败。`)!;
		const output = renderToStaticMarkup(createElement(NarrativeSnapshot, { snapshot, brief: historicalBrief }));
		expect(output).toContain('行业资金未接入；');
		expect(output).toContain('快讯采集失败。');
		expect(output).toContain('该次采集无可用读数');
	});

	it('keeps a genuine cache available at the patrol time and preserves ordinary source failures', () => {
		const temperature: PatrolTemperature = {
			value: 71.65396185034605, model: 'test-fixture', sample_count: 5077, up: 3578, down: 1309, flat: 190,
			up_percent: 70.47468977742761, mean_change_percent: 1.25, median_change_percent: 0.91, trade_date: '2026-10-28',
			fetched_at: '2026-10-28T10:39:50+08:00', source: 'synthetic-test', session_status: '测试样本', stale: true,
		};
		const snapshot = parseNarrativeSnapshot(`市场状态：待确认\n数据缺口：${oldGap}`)!;
		const output = renderToStaticMarkup(createElement(NarrativeSnapshot, { snapshot, brief: { ...historicalBrief, started_at: '2026-10-28T10:40:00+08:00', facts: { ...historicalBrief.facts, temperature } } }));
		expect(output.replace(/<[^>]*>/g, '')).toContain(oldGap.replace('71.654', '71.65').replace('70.4747', '70.47'));
		expect(output).not.toContain('没有该次事实支持');
		const missing = parseNarrativeSnapshot('市场状态：数据不足\n数据缺口：市场温度来源失败，本次无可用温度读数；行业资金未接入。')!;
		const failure = renderToStaticMarkup(createElement(NarrativeSnapshot, { snapshot: missing, brief: historicalBrief }));
		expect(failure).toContain('本次无可用温度读数；行业资金未接入。');
		expect(failure).not.toContain('没有该次事实支持');
	});
});

describe('temperature gauge palette', () => {
	const sample: PatrolTemperature = {
		value: 50, model: 'test-fixture', sample_count: 100, up: 50, down: 50, flat: 0,
		up_percent: 50, mean_change_percent: 0, median_change_percent: 0,
		trade_date: '2026-10-28', fetched_at: '2026-10-28T10:40:00+08:00',
		source: 'synthetic-test', session_status: '测试样本', stale: false,
	};

	it.each([
		[0, 'var(--theme-blue)'],
		[20, 'var(--theme-blue)'],
		[39, 'var(--theme-blue)'],
		[49.99, 'var(--theme-blue)'],
		[50, 'var(--theme-red)'],
		[75, 'var(--theme-red)'],
		[100, 'var(--theme-red)'],
	])('uses only the fixed cold or warm text color at %s degrees', (value, color) => {
		const output = renderToStaticMarkup(createElement(TemperatureGauge, { temperature: { ...sample, value } }));
		expect(output).toContain(`<strong style="color:${color}">${value.toFixed(2)}°</strong>`);
		expect(output).not.toContain('var(--theme-green)');
	});

	it('matches the end cap to the reading and starts the ring at the cold color', () => {
		const output = renderToStaticMarkup(createElement(TemperatureGauge, { temperature: sample }));
		expect(output).toContain('fill:#2d68aa;stroke:none');
		expect(output).toContain('fill:color-mix(in srgb, #fae6e8 50%, #d34c5c);stroke:none');
		expect(output).toContain('class="temperature-gradient"');
	});

	it('rounds the temperature reading to two decimal places without changing the source', () => {
		const temperature = { ...sample, value: 39.005 };
		const before = JSON.stringify(temperature);
		const output = renderToStaticMarkup(createElement(TemperatureGauge, { temperature }));
		expect(output).toContain('aria-label="市场温度 39.01 度"');
		expect(output).toContain('color:var(--theme-blue)">39.01°</strong>');
		expect(JSON.stringify(temperature)).toBe(before);
	});

	it('keeps missing or invalid values as placeholders without drawing a temperature arc', () => {
		for (const temperature of [undefined, { ...sample, value: Number.NaN }, { ...sample, value: 101 }, { ...sample, sample_count: 0 }]) {
			const output = renderToStaticMarkup(createElement(TemperatureGauge, { temperature }));
			expect(output).toContain('市场温度暂无有效数据');
			expect(output).toContain('<strong>--</strong>');
			expect(output).not.toContain('class="temperature-gradient"');
		}
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

describe('phase-specific structured patrol reports', () => {
	const chatFooter = '我是 Hermes，可以帮你做行情复盘、数据核对和资料整理，输入 /help 可查看全部命令。如果愿意，我可以顺手记一份简短的个人偏好（比如你关注的风格、偏好的报告长度），之后输出会更贴合你；不方便的话直接说不，或以后再说都行。';
	const firstUseFooter = '顺带一句：这是你第一次用我，输入 /help 可以看到全部命令。如果你愿意，我也可以花两分钟建一份简短的个人偏好档案（怎么称呼你、主要关注哪些板块、希望我默认多详细），以后就不用反复解释；现在不做、或者以后再说都完全可以。';
	const disclaimer = '本简报仅用于信息整理与研究辅助，不构成投资建议。';

	it.each(['pre_open', 'intraday', 'post_close'] as const)('removes chat and disclaimer footers from %s while retaining the report and real gaps', (phase) => {
		const body = phase === 'intraday'
			? '市场状态：测试样例，待确认。\n主要风险：等待数据核实。\n数据缺口：新闻来源未接入。'
			: `${phase === 'pre_open' ? '盘前定调' : '收盘定调'}：测试样例，待确认。\n\n## 一、风险与数据缺口\n等待数据核实。\n新闻来源未接入。`;
		const original = `${body}\n\n${disclaimer}\n\n---\n\n${chatFooter}`;
		const text = patrolNarrativeText(original);
		const snapshot = parseNarrativeSnapshot(text);
		const report = parseNarrativeReport(text, phase);
		const output = renderToStaticMarkup(snapshot
			? createElement(NarrativeSnapshot, { snapshot, expanded: true })
			: createElement(NarrativeReport, { report: report!, expanded: true }));
		expect(output).toContain('测试样例，待确认。');
		expect(output).toContain('等待数据核实。');
		expect(output).toContain('新闻来源未接入。');
		expect(output).not.toContain(disclaimer);
		expect(output).not.toContain('我是 Hermes');
		expect(output).not.toContain('/help');
		expect(output).not.toContain('个人偏好');
		expect(text).not.toContain('---');
		expect(original).toContain(chatFooter);
	});

	it('retains adjacent report content and ordinary mentions of Hermes', () => {
		const body = '## 一、数据缺口\r\n---\r\nHermes 的新闻来源未接入。';
		const text = patrolNarrativeText(`${body}\r\n${chatFooter}\r\n${disclaimer}`);
		expect(text).toBe(body);
		expect(patrolNarrativeText(body)).toBe(body);
	});

	it.each(['pre_open', 'intraday', 'post_close'] as const)('removes the first-use chat footer from %s without touching adjacent risk text', (phase) => {
		const body = `${phase === 'pre_open' ? '盘前定调' : phase === 'post_close' ? '收盘定调' : '市场状态'}：合成测试，待验证。\n\n主要风险：新闻日期未核实。`;
		const original = `${body}\n\n---\n\n${firstUseFooter}\n\n${disclaimer}`;
		const output = patrolNarrativeText(original);
		expect(output).toContain(body);
		expect(output).not.toContain(disclaimer);
		expect(output).not.toContain('/help');
		expect(output).not.toContain('个人偏好');
		expect(output).not.toContain('---');
		expect(original).toContain(firstUseFooter);
	});

	it('omits the repeated snapshot provenance footer but keeps actual source warnings', () => {
		const note = '市场温度37.912、上涨占比35.8224%，是2026-11-03 13:33:49采集的盘中快照；情绪周期阶段（发酵/主升）取自2026-11-02口径，与盘中情绪分属不同口径。数据为2026-11-03 13:34盘中快照，非收盘结果。本简报仅用于信息整理与研究辅助，不构成投资建议。';
		const body = '市场状态：合成测试，待验证。\n较上次变化：市场温度由43.4977降至37.912，上涨占比由42.3603%降至35.8224%\n数据缺口：行业资金未接入。';
		const original = `${body}\n\n${note}`;
		const text = patrolNarrativeText(original);
		expect(text).toBe(body);
		expect(original).toContain(note);
		const warning = note + '市场温度刷新失败，缓存已过期。';
		expect(patrolNarrativeText(`${body}\n\n${warning}`)).toContain('市场温度刷新失败，缓存已过期。');
		expect(patrolNarrativeText(`${body}\n\n${note.replace('37.912', '**37.912**')}`)).toBe(body);
	});

	it.each(['pre_open', 'intraday', 'post_close'] as const)('hides later snapshot wording in %s without suppressing real warnings', (phase) => {
		const body = `${phase === 'pre_open' ? '盘前定调' : phase === 'post_close' ? '收盘定调' : '市场状态'}：合成样例，等待验证。`;
		const note = '数据为2026-11-03 14:10盘中快照。市场温度采集于14:10:13，非收盘结果；指数接口的状态标记为收盘口径，与本次采集时点不一致，该数以采集时点为准。情绪周期阶段（发酵/主升）取自2026-11-02，仅引用阶段，不展开分数。' + disclaimer;
		const gap = '数据缺口：行业资金未接入，市场温度刷新失败。';
		const original = `${body}\n\n${note}\n\n${gap}`;
		const text = patrolNarrativeText(original);
		expect(text).toContain(body);
		expect(text).toContain(gap);
		expect(text).not.toContain('市场温度采集于');
		expect(text).not.toContain('不展开分数');
		expect(text).not.toContain(disclaimer);
		expect(original).toContain(note);
	});

	it('groups legacy pre-open sections into accessible disclosures and leaves risks open', () => {
		const markdown = '盘前定调：合成测试，等待验证。\n\n## 一、外围市场与隔夜变化\n| 市场/指数 | 涨跌幅 | 行情时间/状态 |\n| --- | --- | --- |\n| 测试市场 | -1.255 | 日期未核实 |\n\n## 二、消息线索\n- 测试消息仍待验证。\n\n## 三、主要风险\n- 行业资金未接入。\n- 测试方向走弱时失效。';
		const report = parseNarrativeReport(markdown, 'pre_open')!;
		const before = JSON.stringify(report);
		const output = renderToStaticMarkup(createElement(NarrativeReport, { report, expanded: true }));
		expect(output.match(/<details /g)).toHaveLength(3);
		expect(output.match(/<summary /g)).toHaveLength(3);
		expect(output.match(/<details [^>]*open=""/g)).toHaveLength(1);
		expect(output).toContain('日期未核实');
		expect(output).toContain('行业资金未接入');
		expect(output).toContain('测试方向走弱时失效');
		expect(output).toContain('nv-preopen-count">2 项');
		expect(JSON.stringify(report)).toBe(before);
		const collapsed = renderToStaticMarkup(createElement(NarrativeReport, { report, expanded: false }));
		expect(collapsed).toContain('合成测试，等待验证');
		expect(collapsed).not.toContain('<details');
		expect(renderToStaticMarkup(createElement(NarrativeReport, { report: parseNarrativeReport(markdown.replace('盘前定调', '收盘定调'), 'post_close')!, expanded: true }))).not.toContain('<details');
	});

	it('retains old pre-open prose without inventing missing section summaries', () => {
		const report = parseNarrativeReport('历史原文，仅有风险说明，数据不足。', 'pre_open')!;
		const output = renderToStaticMarkup(createElement(NarrativeReport, { report, expanded: true }));
		expect(output).toContain('--（未提供一句话总结）');
		expect(output).toContain('历史原文，仅有风险说明，数据不足');
		expect(output).not.toContain('<details');
	});

	it('renders report sections and tables with signed values and complete conditions', () => {
		const markdown = [
			'**盘前定调**：测试样例，等待开盘验证。',
			'## 一、外围市场与隔夜变化',
			'| 市场/指数 | 涨跌幅 | 行情时间/状态 |',
			'| --- | ---: | --- |',
			'| 测试市场甲 | +1.25% | 2026-10-27 已收盘 |',
			'| 测试市场乙 | -0.75% | 日期未核实 |',
			'## 二、今日待验证清单',
			'| 方向 | 支持信号 | 否定信号 |',
			'| --- | --- | --- |',
			'| 测试题材 | 承接增强（推断） | 冲高回落 |',
			'',
			'**数据缺口**：早间新闻未接入。',
		].join('\n');
		expect(parseNarrativeSnapshot(markdown)).toBeNull();
		expect(parseNarrativeSnapshot(`市场状态：待验证\n${markdown}`)).toBeNull();
		const output = renderToStaticMarkup(createElement(Narrative, { markdown }));
		expect(output.match(/<table /g)).toHaveLength(2);
		expect(output.match(/scope="col"/g)).toHaveLength(6);
		expect(output).toContain('nv-section');
		expect(output).toContain('外围市场与隔夜变化');
		expect(output).toContain('class="nv-num is-up">+1.25%');
		expect(output).toContain('class="nv-num is-down">-0.75%');
		expect(output).toContain('日期未核实');
		expect(output).toContain('冲高回落');
		expect(output).toContain('早间新闻未接入');
		expect(output).not.toContain('| ---');
	});

	it('preserves escaped pipes, extra cell text and missing data without interpreting HTML', () => {
		const markdown = '| 事件 | 条件 |\n| --- | --- |\n| 测试甲 \\| 测试乙 | <script>alert(1)</script> | 额外原文 |\n| 日期未知 |';
		const output = renderToStaticMarkup(createElement(Narrative, { markdown }));
		expect(output).toContain('测试甲 | 测试乙');
		expect(output.replace(/<[^>]*>/g, '')).toContain('&lt;script&gt;alert(1)&lt;/script&gt; | 额外原文');
		expect(output).toContain('<td>--</td>');
		expect(output).not.toContain('<script>');
	});

	it('keeps malformed tables as text and does not consume following prose', () => {
		const malformed = renderToStaticMarkup(createElement(Narrative, { markdown: '| 甲 | 乙 |\n| --- |\n| 原始内容 |' }));
		expect(malformed).not.toContain('<table');
		expect(malformed).toContain('原始内容');
		const valid = renderToStaticMarkup(createElement(Narrative, { markdown: '| 甲 | 乙 |\n| --- | --- |\n| 样例 | 条件 |\n后续风险说明' }));
		expect(valid).toContain('</table></div><p class="nv-p">后续风险说明</p>');
	});

	it('shows pre-open reports in full by default and preserves the first section heading', () => {
		expect(narrativeInitiallyExpanded('pre_open', false)).toBe(true);
		expect(narrativeInitiallyExpanded('intraday', false)).toBe(false);
		expect(narrativeInitiallyExpanded('post_close', false)).toBe(false);
		expect(narrativeInitiallyExpanded('intraday', true)).toBe(true);
		const report = '## 一、外围市场\n已提供的事实。';
		expect(patrolNarrativeText(report)).toBe(report);
		expect(patrolNarrativeText('# 盘前巡查\n\n' + report)).toBe(report);
	});

	it('limits table-only previews to three rows and leaves the full report intact', () => {
		const rows = Array.from({ length: 6 }, (_, index) => `| 测试${index} | 待验证 |`).join('\n');
		const markdown = `## 一、今日待验证\n| 方向 | 条件 |\n| --- | --- |\n${rows}`;
		const preview = previewNarrativeBlocks(markdown);
		expect(preview[1]).toMatchObject({ kind: 'table', rows: [['测试0', '待验证'], ['测试1', '待验证'], ['测试2', '待验证']] });
		const full = renderToStaticMarkup(createElement(Narrative, { markdown }));
		expect(full.replace(/<[^>]*>/g, '')).toContain('测试5');
	});
});

describe('patrol narrative display precision', () => {
	it('rounds decimal values, signs and carry boundaries while keeping dates, times and codes intact', () => {
		const markdown = '测试样本：温度由43.4977降至37.912，上涨占比由+42.3603%降至35.8224%；负值-1.005，边界9.999，小数0.6622。时间2026-11-03 13:33:49，代码000001，版本v1.234.5。';
		const output = renderToStaticMarkup(createElement(Narrative, { markdown })).replace(/<[^>]*>/g, '');
		expect(output).toBe('测试样本：温度由43.50降至37.91，上涨占比由+42.36%降至35.82%；负值-1.01，边界10.00，小数0.66。时间2026-11-03 13:33:49，代码000001，版本v1.234.5。');
		expect(markdown).toContain('43.4977');
	});

	it('formats amounts in readable units with two decimals and keeps the raw value in the tooltip', () => {
		const markdown = '合成资金样本：净流入4022000000元，资金13550元，小额净买入0.00008万元；股价1.005元/股。';
		const output = renderToStaticMarkup(createElement(Narrative, { markdown }));
		const visible = output.replace(/<[^>]*>/g, '');
		expect(visible).toBe('合成资金样本：净流入40.22亿元，资金1.36万元，小额净买入0.80元；股价1.01元/股。');
		expect(output).toContain('title="原始金额：4022000000元"');
	});
});

describe('intraday next-run display', () => {
	it('uses the next scheduled time rather than the last completed record', () => {
		expect(intradayNextRunTime(true, '2026-11-03T14:10:00+08:00', '盘中巡查 14:10')).toBe('14:10');
		expect(intradayNextRunTime(true, '2026-11-03T14:20:00+08:00', '盘中巡查 14:20')).toBe('14:20');
	});

	it('does not invent an upcoming intraday run when disabled, unavailable or scheduled for another phase', () => {
		expect(intradayNextRunTime(false, '2026-11-03T14:10:00+08:00', '盘中巡查 14:10')).toBe('');
		expect(intradayNextRunTime(true, undefined, '盘中巡查')).toBe('');
		expect(intradayNextRunTime(true, 'invalid', '盘中巡查')).toBe('');
		expect(intradayNextRunTime(true, '2026-11-03T15:30:00+08:00', '盘后巡查 15:30')).toBe('');
	});
});
