import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { MessageContent } from './MarkdownContent';

describe('MessageContent', () => {
	it('renders assistant GFM content as semantic HTML', () => {
		const output = renderToStaticMarkup(
			<MessageContent
				markdown
				content={'## 行情结论\n\n- **趋势向上**\n- `注意风险`\n\n| 项目 | 状态 |\n| --- | --- |\n| 行情 | 正常 |'}
			/>,
		);

		expect(output).toContain('<h2>行情结论</h2>');
		expect(output).toContain('<ul>');
		expect(output).toContain('<strong>趋势向上</strong>');
		expect(output).toContain('<code>注意风险</code>');
		expect(output).toContain('<table>');
	});

	it('keeps user content as plain text', () => {
		const output = renderToStaticMarkup(<MessageContent content={'**不要加粗**\n- 不要转列表'} />);

		expect(output).toContain('**不要加粗**');
		expect(output).toContain('- 不要转列表');
		expect(output).not.toContain('<strong>');
		expect(output).not.toContain('<ul>');
	});

	it('preserves single line breaks in streamed assistant replies', () => {
		const output = renderToStaticMarkup(<MessageContent markdown content={'第一行\n第二行'} />);

		expect(output).toContain('第一行<br/>\n第二行');
	});

	it('drops raw HTML and protects external links', () => {
		const output = renderToStaticMarkup(
			<MessageContent markdown content={'<script>alert(1)</script>\n\n[查看详情](https://example.com)'} />,
		);

		expect(output).not.toContain('<script>');
		expect(output).not.toContain('alert(1)');
		expect(output).toContain('href="https://example.com"');
		expect(output).toContain('target="_blank"');
		expect(output).toContain('rel="noreferrer"');
	});
});
