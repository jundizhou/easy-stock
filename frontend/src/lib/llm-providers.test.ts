import { describe, expect, it } from 'vitest';
import { llmProviderDefaultModel, llmProviderDefinition, llmProviderName, llmProviders } from './llm-providers';

describe('LLM provider definitions', () => {
	it('includes the supported Chinese model providers', () => {
		expect(llmProviders.map((provider) => provider.id)).toEqual(expect.arrayContaining(['moonshot', 'minimax', 'zhipu', 'qwen', 'siliconflow']));
		expect(llmProviderName('moonshot')).toContain('Kimi');
		expect(llmProviderName('zhipu')).toContain('GLM');
	});

	it('provides model-discovery defaults for provider switching', () => {
		expect(llmProviderDefinition('minimax')).toMatchObject({ baseURL: 'https://api.minimaxi.com/v1', apiMode: 'chat_completions' });
		expect(llmProviderDefinition('zhipu')).toMatchObject({ baseURL: 'https://open.bigmodel.cn/api/paas/v4', apiMode: 'chat_completions' });
		expect(llmProviderDefaultModel('deepseek')).toBe('deepseek-chat');
	});

	it('falls back to custom settings for unknown providers', () => {
		expect(llmProviderDefinition('unknown')).toMatchObject({ id: 'custom', baseURL: '', defaultModel: '' });
	});
});
