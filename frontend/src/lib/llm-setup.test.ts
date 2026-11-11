// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { requestJSON, type AppSettings, type BackendConfig } from './backend';
import { hasConfiguredLLM, useLLMSetupRequired } from './llm-setup';

vi.mock('./backend', () => ({ requestJSON: vi.fn() }));

const config: BackendConfig = { backendUrl: 'http://localhost', token: '' };
const emptySettings = (): AppSettings => ({
	agent: { available: true, configured: false, api_key_configured: false },
	llm: { provider: 'openai', base_url: 'https://api.openai.com/v1', model: '', api_mode: 'chat_completions', response_timeout_seconds: 300, api_key: { configured: false } },
	llm_profiles: [], active_llm_profile_id: '',
	credentials: { tushare_token: { configured: false }, ths_cookie: { configured: false }, xueqiu_cookie: { configured: false }, eastmoney_cookie: { configured: false } },
	review_automation: { profiles: [] },
});

beforeEach(() => { vi.mocked(requestJSON).mockReset(); });
afterEach(cleanup);

describe('model setup detection', () => {
	it('requires setup for defaults and incomplete hosted connections', () => {
		const settings = emptySettings();
		expect(hasConfiguredLLM(settings)).toBe(false);
		settings.llm.model = 'gpt-4o-mini';
		expect(hasConfiguredLLM(settings)).toBe(false);
		settings.llm.api_key.configured = true;
		expect(hasConfiguredLLM(settings)).toBe(true);
	});

	it('recognizes an inactive configured profile and keyless custom endpoints', () => {
		const settings = emptySettings();
		settings.llm_profiles = [{ ...settings.llm, id: 'local', name: 'Local', provider: 'custom', base_url: 'http://localhost:11434/v1', model: 'local-model' }];
		expect(hasConfiguredLLM(settings)).toBe(true);
		settings.llm_profiles[0].base_url = ' ';
		expect(hasConfiguredLLM(settings)).toBe(false);
		settings.llm_profiles[0] = { ...settings.llm_profiles[0], provider: 'openai', base_url: 'https://api.openai.com/v1', api_key: { configured: true } };
		expect(hasConfiguredLLM(settings)).toBe(true);
	});

	it('honors runtime configuration even when its executable is unavailable', () => {
		const settings = emptySettings();
		settings.agent = { ...settings.agent, available: false, configured: true };
		expect(hasConfiguredLLM(settings)).toBe(true);
		settings.agent.configured = false;
		settings.runtimes = { hermes: { available: false, configured: true } };
		expect(hasConfiguredLLM(settings)).toBe(true);
	});
});

describe('startup model setup check', () => {
	it('waits for the backend and prompts only after confirming the settings are empty', async () => {
		vi.mocked(requestJSON).mockResolvedValue({ data: emptySettings() });
		const { result, rerender } = renderHook(({ backend }) => useLLMSetupRequired(backend), { initialProps: { backend: null as BackendConfig | null } });
		expect(result.current).toBe(false);
		expect(requestJSON).not.toHaveBeenCalled();
		rerender({ backend: config });
		await waitFor(() => expect(result.current).toBe(true));
		rerender({ backend: config });
		expect(requestJSON).toHaveBeenCalledOnce();
	});

	it.each(['configured', 'error'])('does not prompt for %s settings', async (outcome) => {
		const settings = emptySettings();
		settings.agent.configured = true;
		if (outcome === 'error') vi.mocked(requestJSON).mockRejectedValue(new Error('Backend unavailable'));
		else vi.mocked(requestJSON).mockResolvedValue({ data: settings });
		const { result } = renderHook(() => useLLMSetupRequired(config));
		await act(async () => {});
		expect(result.current).toBe(false);
	});

	it('ignores a cancelled response after the backend changes', async () => {
		let finishFirst!: (value: { data: AppSettings }) => void;
		vi.mocked(requestJSON).mockImplementationOnce(() => new Promise((resolve) => { finishFirst = resolve; }));
		const settings = emptySettings();
		settings.agent.configured = true;
		vi.mocked(requestJSON).mockResolvedValue({ data: settings });
		const { result, rerender } = renderHook(({ backend }) => useLLMSetupRequired(backend), { initialProps: { backend: config } });
		rerender({ backend: { ...config, backendUrl: 'http://localhost:20082' } });
		await act(async () => {});
		await act(async () => { finishFirst({ data: emptySettings() }); });
		expect(result.current).toBe(false);
		expect(vi.mocked(requestJSON).mock.calls[0][2]?.signal?.aborted).toBe(true);
	});
});
