const HAN = '\u4e00-\u9fff';

// 中文正文里由原页面排版带进来的“字间空格”，例如「投资者关系活 动类别」。
// 「答：」这类问答标签前保留一个空格，避免把标签和上文粘在一起。
const HAN_SPACE = new RegExp(`(?<=[${HAN}]) (?=(?!答[:：])[${HAN}])`, 'g');

// 公告/研报常见的小标题起点：一、／（二）／1、。
// 编号必须落在行首、空格或句读标点之后；否则「约3.54、9.34、8.32—9.32亿元」
// 「下降2.41、1.48、3.09个百分点」这类数字顿号串会被当成编号，正文在小数点后被切碎。
const SECTION_START = /(?<=^|[ \u3000。；！？：，])(?=(?:[一二三四五六七八九十]{1,3}、|（[一二三四五六七八九十\d]{1,3}）|\d{1,2}、))/;

// 单段超过这个字数，基本是公告里丢掉换行的参与名单/表格，按句读重新折行。
const LONG_PARAGRAPH_LIMIT = 400;
const SENTENCE_END = /(?<=[。！？；])/;

export function formatArticleParagraphs(text: string): string[] {
	const normalized = text
		.replace(/\r\n?/g, '\n')
		.replace(/[ \t\u3000]+/g, ' ')
		.trim();
	if (!normalized) return [];
	return normalized
		.split('\n')
		// 先按小标题切段：此时原文里的空格还在，正是「…先生 一、公司经营情况」这类编号的分段依据。
		.flatMap((line) => line.split(SECTION_START))
		// 切完再清理原页面排版带进来的字间空格，避免把分段依据一起删掉。
		.map((paragraph) => paragraph.replace(HAN_SPACE, '').trim())
		.filter(Boolean)
		.flatMap(reflowLongParagraph);
}

// 超长段落按句读折行，避免公告名单渲染成一整块。
function reflowLongParagraph(paragraph: string): string[] {
	if (paragraph.length <= LONG_PARAGRAPH_LIMIT) return [paragraph];
	const chunks: string[] = [];
	let current = '';
	for (const sentence of paragraph.split(SENTENCE_END)) {
		if (current && current.length + sentence.length > LONG_PARAGRAPH_LIMIT) {
			chunks.push(current);
			current = '';
		}
		current += sentence;
	}
	if (current) chunks.push(current);
	return chunks.map((chunk) => chunk.trim()).filter(Boolean);
}

// 判断某段该不该首行缩进：首段与小标题、编号行保持顶格。
export function isIndentedParagraph(paragraph: string, index = 0): boolean {
	return index > 0 && paragraph.length >= 24;
}
