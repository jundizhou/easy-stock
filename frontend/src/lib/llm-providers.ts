export type LLMAPIMode = 'chat_completions' | 'codex_responses' | 'anthropic_messages';

export type LLMProviderDefinition = {
	id: string;
	label: string;
	baseURL: string;
	defaultModel: string;
	apiMode: LLMAPIMode;
};

export const llmProviders: LLMProviderDefinition[] = [
	{ id: 'openai', label: 'OpenAI', baseURL: 'https://api.openai.com/v1', defaultModel: 'gpt-4o-mini', apiMode: 'chat_completions' },
	{ id: 'deepseek', label: 'DeepSeek', baseURL: 'https://api.deepseek.com', defaultModel: 'deepseek-chat', apiMode: 'chat_completions' },
	{ id: 'moonshot', label: 'Kimi（月之暗面）', baseURL: 'https://api.moonshot.cn/v1', defaultModel: 'moonshot-v1-8k', apiMode: 'chat_completions' },
	{ id: 'minimax', label: 'MiniMax', baseURL: 'https://api.minimaxi.com/v1', defaultModel: 'MiniMax-Text-01', apiMode: 'chat_completions' },
	{ id: 'zhipu', label: '智谱 GLM', baseURL: 'https://open.bigmodel.cn/api/paas/v4', defaultModel: 'glm-4-plus', apiMode: 'chat_completions' },
	{ id: 'qwen', label: '通义千问（百炼）', baseURL: 'https://dashscope.aliyuncs.com/compatible-mode/v1', defaultModel: 'qwen-plus', apiMode: 'chat_completions' },
	{ id: 'siliconflow', label: '硅基流动', baseURL: 'https://api.siliconflow.cn/v1', defaultModel: '', apiMode: 'chat_completions' },
	{ id: 'anthropic', label: 'Anthropic', baseURL: 'https://api.anthropic.com', defaultModel: 'claude-3-5-haiku-latest', apiMode: 'anthropic_messages' },
	{ id: 'custom', label: 'OpenAI 兼容接口', baseURL: '', defaultModel: '', apiMode: 'chat_completions' },
];

const providersByID = new Map(llmProviders.map((provider) => [provider.id, provider]));

export function llmProviderDefinition(provider: string): LLMProviderDefinition {
	return providersByID.get(provider) || providersByID.get('custom')!;
}

export function llmProviderName(provider: string): string {
	return providersByID.get(provider)?.label || provider;
}

export function llmProviderDefaultModel(provider: string): string {
	return providersByID.get(provider)?.defaultModel || '';
}
