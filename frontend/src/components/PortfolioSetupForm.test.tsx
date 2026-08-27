import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { PortfolioSetupForm } from './PortfolioSetupForm';

describe('PortfolioSetupForm', () => {
	it('keeps every holding slider on a stable 1-100 scale', () => {
		const markup = renderToStaticMarkup(<PortfolioSetupForm
			draft={{
				profile: 'balanced',
				holdings: [
					{ symbol: '688002.SH', name: '睿创微纳', weight: 31, costPrice: '' },
					{ symbol: '300209.SZ', name: '有棵树', weight: 20, costPrice: '' },
				],
			}}
			directory={[]}
			actionLabel="开始 AI 巡检"
			busyLabel="巡检进行中"
			onChange={() => {}}
			onSubmit={() => {}}
		/>);

		const sliders = markup.match(/<input[^>]+type="range"[^>]*>/g) || [];
		expect(sliders).toHaveLength(2);
		expect(sliders.every((slider) => slider.includes('min="1"') && slider.includes('max="100"'))).toBe(true);
	});
});
