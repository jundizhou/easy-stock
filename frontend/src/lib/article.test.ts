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
