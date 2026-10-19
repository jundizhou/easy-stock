import { describe, expect, it } from 'vitest';
import { formatArticleParagraphs, isIndentedParagraph } from './article';

describe('formatArticleParagraphs', () => {
	it('把没有换行的一整段公告按小标题切段', () => {
		const text = '证券代码：688757 证券简称：胜科纳米 1、本次募集资金投向 答：用于半导体检测。2、必要性分析 答：行业需求增长。';
		expect(formatArticleParagraphs(text)).toEqual([
			'证券代码：688757 证券简称：胜科纳米',
			'1、本次募集资金投向 答：用于半导体检测。',
			'2、必要性分析 答：行业需求增长。',
		]);
	});

	it('去掉中文字与字之间由原页面排版带入的空格，但保留数字前后的空格', () => {
		const [paragraph] = formatArticleParagraphs('投资者关系活 动类别 2026 年上半年 增长');
		expect(paragraph).toBe('投资者关系活动类别 2026 年上半年增长');
	});

	it('保留研报正文自带的换行，并支持（一）这类编号', () => {
		const text = '投资要点\n高毛利原料药放量。\n（一）风险提示：研发不及预期。';
		expect(formatArticleParagraphs(text)).toEqual([
			'投资要点',
			'高毛利原料药放量。',
			'（一）风险提示：研发不及预期。',
		]);
	});

	it('数字里的顿号不会被当成编号：约3.54、9.34、8.32—9.32亿元', () => {
		const text = '单季度看，2026Q1—Q3公司分别实现归母净利润约3.54、9.34、8.32—9.32亿元，第三季度单季利润继续处于高位。';
		expect(formatArticleParagraphs(text)).toEqual([text]);
	});

	it('句中的数字顿号串不切段，句末的编号小标题仍然切段', () => {
		const text = '分别较上年同期下降2.41、1.48、3.09个百分点。2、盈利能力改善';
		expect(formatArticleParagraphs(text)).toEqual([
			'分别较上年同期下降2.41、1.48、3.09个百分点。',
			'2、盈利能力改善',
		]);
	});

	it('紧跟汉字的数字顿号串不切段：增长1、2个百分点', () => {
		expect(formatArticleParagraphs('订单同比增长1、2个百分点后企稳。')).toEqual(['订单同比增长1、2个百分点后企稳。']);
	});

	it('字间空格清理不会吃掉小标题的分段依据', () => {
		const text = '投资者关系总监 蔡清源先生 一、公司经营情况说明 公司前三季度实现营业收入 740.44 亿元。';
		expect(formatArticleParagraphs(text)).toEqual([
			'投资者关系总监蔡清源先生',
			'一、公司经营情况说明公司前三季度实现营业收入 740.44 亿元。',
		]);
	});

	it('超过 400 字的长段按句读折行，避免公告名单成一整块', () => {
		const text = '某某证券资管 张三；'.repeat(80);
		const paragraphs = formatArticleParagraphs(text);
		expect(paragraphs.length).toBeGreaterThan(1);
		expect(paragraphs.every((paragraph) => paragraph.length <= 400)).toBe(true);
		expect(paragraphs.join('').length).toBe(720);
	});

	it('未超过 400 字的段落保持原样', () => {
		const text = '营收保持较快增长，毛利率同比提升。'.repeat(15);
		expect(text.length).toBeLessThanOrEqual(400);
		expect(formatArticleParagraphs(text)).toEqual([text]);
	});

	it('空正文返回空数组', () => {
		expect(formatArticleParagraphs('   ')).toEqual([]);
	});
});

describe('isIndentedParagraph', () => {
	it('首段顶格，长段落首行缩进，短标题不缩进', () => {
		expect(isIndentedParagraph('投资要点', 0)).toBe(false);
		expect(isIndentedParagraph('答：2026 年上半年，公司新增订单 9,607.06MW，累计在手订单 47,503.74MW。', 1)).toBe(true);
		expect(isIndentedParagraph('答：2026 年上半年，公司新增订单 9,607.06MW，累计在手订单 47,503.74MW。', 0)).toBe(false);
		expect(isIndentedParagraph('风险提示', 2)).toBe(false);
	});
});
