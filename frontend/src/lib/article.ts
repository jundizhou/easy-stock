const HAN = '\u4e00-\u9fff';

// 中文正文里由原页面排版带进来的“字间空格”，例如「投资者关系活 动类别」。
// 「答：」这类问答标签前保留一个空格，避免把标签和上文粘在一起。
const HAN_SPACE = new RegExp(`(?<=[${HAN}]) (?=(?!答[:：])[${HAN}])`, 'g');

// 公告/研报常见的小标题起点：一、／（二）／1、。
const SECTION_START = /(?=(?:[一二三四五六七八九十]{1,3}、|（[一二三四五六七八九十\d]{1,3}）|\d{1,2}、))/;

export function formatArticleParagraphs(text: string): string[] {
	const normalized = text
		.replace(/\r\n?/g, '\n')
		.replace(/[ \t\u3000]+/g, ' ')
		.replace(HAN_SPACE, '')
		.trim();
	if (!normalized) return [];
	return normalized
		.split('\n')
		.flatMap((line) => line.split(SECTION_START))
		.map((paragraph) => paragraph.trim())
		.filter(Boolean);
}

// 判断某段该不该首行缩进：首段与小标题、编号行保持顶格。
export function isIndentedParagraph(paragraph: string, index = 0): boolean {
	return index > 0 && paragraph.length >= 24;
}
