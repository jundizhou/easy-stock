import { useEffect, useRef, useState } from 'react';
import { requestJSON, type AgentSettings, type AgentSkillSetting, type AgentMCPServerSetting, type BackendConfig, type SecretSettingStatus } from './backend';

export type SecretEntryDraft = { id: string; key: string; value: string; configured: boolean; masked?: string; remove: boolean };
export type MCPDraft = Omit<AgentMCPServerSetting, 'env' | 'headers' | 'args'> & { id: string; originalName: string; argsText: string; env: SecretEntryDraft[]; headers: SecretEntryDraft[] };
type AgentDraft = { skills: AgentSkillSetting[]; servers: MCPDraft[] };
let draftSequence = 0;
export const nextID = (prefix: string) => `${prefix}-${Date.now()}-${++draftSequence}`;

function toDraft(data: AgentSettings): AgentDraft {
  return { skills: data.skills || [], servers: (data.mcp_servers || []).map(toMCPDraft) };
}
function toUpdate(draft: AgentDraft) {
  return { skills: draft.skills.map(({ name, enabled }) => ({ name, enabled })), mcp_servers: draft.servers.map(toMCPUpdate) };
}

// Refresh discovery metadata without discarding locally edited switches.
function mergeSkills(current: AgentSkillSetting[], baseline: AgentSkillSetting[], refreshed: AgentSkillSetting[]) {
  const previous = new Map(baseline.map((skill) => [skill.name, skill.enabled]));
  const edits = new Map(current.filter((skill) => previous.has(skill.name) && skill.enabled !== previous.get(skill.name)).map((skill) => [skill.name, skill.enabled]));
  return refreshed.map((skill) => ({ ...skill, enabled: edits.get(skill.name) ?? skill.enabled }));
}

export function useAgentSettings(config: BackendConfig | null, open: boolean) {
  const [draft, setDraft] = useState<AgentDraft | null>(null);
  const [baseline, setBaseline] = useState<AgentDraft | null>(null);
  const [ready, setReady] = useState(false);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [loadError, setLoadError] = useState('');
  const lock = useRef(false);
  const generation = useRef(0);

  const acceptSaved = (data: AgentSettings) => {
    const next = toDraft(data);
    setDraft(next); setBaseline(next); setReady(true); setLoadError('');
  };

  useEffect(() => {
    const current = ++generation.current;
    setDraft(null); setBaseline(null); setReady(false); setLoadError('');
    if (!open || !config) { setLoading(false); return; }
    setLoading(true);
    requestJSON<{ data: AgentSettings }>(config, '/api/v1/settings/agent')
      .then(({ data }) => { if (current === generation.current) acceptSaved(data); })
      .catch((error) => { if (current === generation.current) setLoadError(error instanceof Error ? error.message : '读取 Skill/MCP 设置失败'); })
      .finally(() => { if (current === generation.current) setLoading(false); });
    return () => { generation.current++; };
  }, [config, open]);

  const refreshSkills = async () => {
    if (!config) throw new Error('后端尚未连接');
    const currentGeneration = generation.current;
    try {
      const { data } = await requestJSON<{ data: AgentSettings }>(config, '/api/v1/settings/agent');
      if (currentGeneration !== generation.current) return;
      if (!draft || !baseline) { acceptSaved(data); return; }
      const skills = data.skills || [];
      setDraft((current) => current && ({ ...current, skills: mergeSkills(current.skills, baseline.skills, skills) }));
      setBaseline((current) => current && ({ ...current, skills }));
      setReady(true); setLoadError('');
    } catch (error) {
      if (currentGeneration !== generation.current) return;
      setReady(false);
      setLoadError('读取 Skill/MCP 设置失败，请重试；未保存的修改已保留。');
      throw error;
    }
  };

  const retry = async () => {
    if (lock.current || loading || !config) return;
    lock.current = true; setLoading(true);
    try { await refreshSkills(); } catch { /* The read error is displayed in the panel. */ }
    finally { lock.current = false; setLoading(false); }
  };

  // All saves and Skill mutations share one lock, including the global Save.
  const run = async (operation: () => Promise<void>) => {
    if (!open || !config || !ready || !draft || loading) throw new Error('Skill/MCP 配置尚未读取成功，请重试读取后保存');
    if (lock.current) throw new Error('Skill/MCP 操作正在进行，请稍后保存');
    lock.current = true; setBusy(true);
    try { await operation(); }
    finally { lock.current = false; setBusy(false); }
  };

  const update = draft ? toUpdate(draft) : undefined;
  const dirty = !!draft && !!baseline && JSON.stringify(update) !== JSON.stringify(toUpdate(baseline));
  return { draft, setDraft, ready, loading, busy, loadError, retry, run, refreshSkills, acceptSaved, update, dirty };
}
export type AgentSettingsController = ReturnType<typeof useAgentSettings>;

function toMCPDraft(server: AgentMCPServerSetting): MCPDraft {
  return { ...server, id: nextID('mcp'), originalName: server.name, transport: server.transport || 'stdio', argsText: (server.args || []).join('\n'), env: toSecretDrafts(server.env, 'env'), headers: toSecretDrafts(server.headers, 'header') };
}

function toSecretDrafts(values: Record<string, SecretSettingStatus> | undefined, prefix: string): SecretEntryDraft[] {
	return Object.entries(values || {}).map(([key, status]) => ({ id: nextID(prefix), key, value: '', configured: status.configured, masked: status.masked, remove: false }));
}

export function splitMCPArgs(value: string): string[] {
	return value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean);
}

function protectedMapUpdate(entries: SecretEntryDraft[]) {
	const updates: Record<string, string> = {};
	const clear: string[] = [];
	for (const entry of entries) {
		const key = entry.key.trim();
		if (!key) continue;
		if (entry.remove) clear.push(key);
		else if (entry.value.trim()) updates[key] = entry.value.trim();
	}
	return { updates, clear };
}

function toMCPUpdate(server: MCPDraft) {
	const env = protectedMapUpdate(server.env);
	const headers = protectedMapUpdate(server.headers);
	return { name: server.name.trim(), original_name: server.originalName, enabled: server.enabled, transport: server.transport, command: (server.command || '').trim(), args: splitMCPArgs(server.argsText), env: env.updates, clear_env: env.clear, url: (server.url || '').trim(), headers: headers.updates, clear_headers: headers.clear, timeout: server.timeout || 0, connect_timeout: server.connect_timeout || 0, supports_parallel_tool_calls: Boolean(server.supports_parallel_tool_calls) };
}
