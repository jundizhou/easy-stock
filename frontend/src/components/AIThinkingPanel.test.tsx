import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { AIThinkingPanel } from './AIThinkingPanel';

describe('AIThinkingPanel', () => {
	it('shows received reasoning expanded while the reply is pending', () => {
		const html = renderToStaticMarkup(<AIThinkingPanel content="正在核对数据。" pending />);
		expect(html).toContain('<details class="ai-thinking" open="">');
		expect(html).toContain('思考过程');
		expect(html).toContain('正在核对数据。');
	});

	it('keeps completed reasoning available in a collapsed panel and escapes provider text', () => {
		const html = renderToStaticMarkup(<AIThinkingPanel content={'第一步\n<script>alert(1)</script>'} pending={false} />);
		expect(html).not.toContain('open=""');
		expect(html).toContain('展开');
		expect(html).toContain('&lt;script&gt;');
		expect(html).not.toContain('<script>');
	});

	it('does not invent a reasoning panel when the provider has not returned any', () => {
		expect(renderToStaticMarkup(<AIThinkingPanel pending />)).toBe('');
		expect(renderToStaticMarkup(<AIThinkingPanel content="   " pending={false} />)).toBe('');
	});
});
