import { describe, expect, it } from 'vitest';
import { runtimeErrorDetails, runtimeFeatureForPath } from './runtime-log';

describe('runtime logging helpers', () => {
	it('maps API routes to stable feature names without query data', () => {
		expect(runtimeFeatureForPath('/api/v1/themes/screen')).toBe('theme-radar');
		expect(runtimeFeatureForPath('/api/v1/stocks/ai-analysis')).toBe('stock-analysis');
		expect(runtimeFeatureForPath('/api/v1/reviews/posts')).toBe('reviews');
	});

	it('keeps error diagnostics bounded to the useful fields', () => {
		const details = runtimeErrorDetails(new TypeError('network failed'));
		expect(details).toMatchObject({ name: 'TypeError', message: 'network failed' });
	});
});
