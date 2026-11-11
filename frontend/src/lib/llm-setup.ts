import { useEffect, useState } from 'react';
import { requestJSON, type AppSettings, type BackendConfig } from './backend';

export function hasConfiguredLLM(settings: AppSettings): boolean {
	if (settings.agent.configured || Object.values(settings.runtimes || {}).some((runtime) => runtime.configured)) return true;
	return [...(settings.llm_profiles || []), settings.llm].some((profile) => (
		Boolean(profile.model.trim()) && (profile.api_key.configured || (profile.provider === 'custom' && Boolean(profile.base_url.trim())))
	));
}

export function useLLMSetupRequired(config: BackendConfig | null): boolean {
	const [required, setRequired] = useState(false);

	useEffect(() => {
		setRequired(false);
		if (!config) return;
		const abort = new AbortController();
		void requestJSON<{ data: AppSettings }>(config, '/api/v1/settings', { signal: abort.signal })
			.then(({ data }) => {
				if (!abort.signal.aborted) setRequired(!hasConfiguredLLM(data));
			})
			.catch(() => {
				// A failed settings request does not mean the user has no model.
			});
		return () => abort.abort();
	}, [config]);

	return required;
}
