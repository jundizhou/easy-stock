import { CheckCircle2, CircleAlert, LoaderCircle, Network, Plus, Puzzle, Save, Search, ShieldCheck, Trash2 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import type { BackendConfig, HermesAgentSettings, HermesMCPServerSetting, HermesSkillSetting, SecretSettingStatus } from '../lib/backend';
import { requestJSON } from '../lib/backend';

type Props = { config: BackendConfig | null; open: boolean };
type SecretEntryDraft = { id: string; key: string; value: string; configured: boolean; masked?: string; remove: boolean };
type MCPDraft = Omit<HermesMCPServerSetting, 'env' | 'headers' | 'args'> & { id: string; originalName: string; argsText: string; env: SecretEntryDraft[]; headers: SecretEntryDraft[] };

let draftSequence = 0;
const nextID = (prefix: string) => `${prefix}-${Date.now()}-${++draftSequence}`;

export function HermesAgentSettingsPanel({ config, open }: Props) {
	const [skills, setSkills] = useState<HermesSkillSetting[]>([]);
	const [servers, setServers] = useState<MCPDraft[]>([]);
	const [search, setSearch] = useState('');
	const [state, setState] = useState<'idle' | 'loading' | 'saving' | 'saved' | 'error'>('idle');
	const [message, setMessage] = useState('');

	useEffect(() => {
		if (!open || !config) return;
		let cancelled = false;
		setState('loading');
		setMessage('');
		requestJSON<{ data: HermesAgentSettings }>(config, '/api/v1/settings/agent')
			.then(({ data }) => {
				if (cancelled) return;
				setSkills(data.skills || []);
				setServers((data.mcp_servers || []).map(toMCPDraft));
				setState('idle');
			})
			.catch((error) => {
				if (cancelled) return;
				setState('error');
				setMessage(error instanceof Error ? error.message : '读取 Skill/MCP 设置失败');
			});
		return () => { cancelled = true; };
	}, [config, open]);

	const filteredSkills = useMemo(() => {
		const query = search.trim().toLowerCase();
		if (!query) return skills;
		return skills.filter((skill) => `${skill.name} ${skill.description} ${skill.category}`.toLowerCase().includes(query));
	}, [search, skills]);
	const enabledSkillCount = skills.filter((skill) => skill.enabled).length;

	const updateServer = (id: string, patch: Partial<MCPDraft>) => setServers((current) => current.map((server) => server.id === id ? { ...server, ...patch } : server));
	const updateSecretEntry = (serverID: string, field: 'env' | 'headers', entryID: string, patch: Partial<SecretEntryDraft>) => setServers((current) => current.map((server) => server.id === serverID ? { ...server, [field]: server[field].map((entry) => entry.id === entryID ? { ...entry, ...patch } : entry) } : server));
	const addSecretEntry = (serverID: string, field: 'env' | 'headers') => setServers((current) => current.map((server) => server.id === serverID ? { ...server, [field]: [...server[field], { id: nextID(field), key: '', value: '', configured: false, remove: false }] } : server));
	const removeSecretEntry = (serverID: string, field: 'env' | 'headers', entryID: string) => setServers((current) => current.map((server) => {
		if (server.id !== serverID) return server;
		return { ...server, [field]: server[field].flatMap((entry) => entry.id !== entryID ? [entry] : entry.configured ? [{ ...entry, remove: !entry.remove, value: '' }] : []) };
	}));

	const addServer = () => setServers((current) => [...current, {
		id: nextID('mcp'), name: '', originalName: '', enabled: true, transport: 'stdio', command: '', argsText: '', env: [], url: '', headers: [], timeout: 300, connect_timeout: 60, supports_parallel_tool_calls: false,
	}]);

	const save = async () => {
		if (!config) return;
		setState('saving');
		setMessage('');
		try {
			const payload = await requestJSON<{ data: HermesAgentSettings }>(config, '/api/v1/settings/agent', {
				method: 'PUT',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					skills: skills.map(({ name, enabled }) => ({ name, enabled })),
					mcp_servers: servers.map(toMCPUpdate),
				}),
			});
			setSkills(payload.data.skills || []);
			setServers((payload.data.mcp_servers || []).map(toMCPDraft));
			setState('saved');
			setMessage('Skill 与 MCP 设置已同步到本机 Hermes；MCP 会自动重新加载。');
		} catch (error) {
			setState('error');
			setMessage(error instanceof Error ? error.message : '保存 Skill/MCP 设置失败');
		}
	};

	return (
		<section className="settings-section hermes-agent-settings" onKeyDown={(event) => { if (event.key === 'Enter' && event.target instanceof HTMLInputElement) event.preventDefault(); }}>
			<div className="settings-section-title"><Puzzle size={18} /><div><h3>Skill 与 MCP</h3><p>控制 Hermes 可加载的本机技能，并连接 stdio、Streamable HTTP 或 SSE MCP Server。</p></div></div>
			{state === 'loading' ? <div className="agent-settings-loading"><LoaderCircle className="spin" size={18} />读取 Hermes 能力配置</div> : <>
				<div className="agent-settings-block">
					<div className="agent-settings-heading"><div><strong>Skills</strong><span>{enabledSkillCount}/{skills.length} 个已启用</span></div><label><Search size={14} /><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索 Skill" /></label></div>
					<div className="skill-settings-list">
						{filteredSkills.map((skill) => <label className="skill-setting-item" key={skill.name}><span><strong>{skill.name}</strong><small>{skill.category} · {skill.description || '暂无描述'}</small></span><input type="checkbox" checked={skill.enabled} onChange={(event) => setSkills((current) => current.map((item) => item.name === skill.name ? { ...item, enabled: event.target.checked } : item))} /></label>)}
						{!filteredSkills.length && <div className="agent-settings-empty">没有匹配的本机 Skill</div>}
					</div>
				</div>

				<div className="agent-settings-block">
					<div className="agent-settings-heading"><div><strong>MCP Servers</strong><span>{servers.filter((server) => server.enabled).length}/{servers.length} 个已启用</span></div><button type="button" onClick={addServer}><Plus size={14} />添加 MCP</button></div>
					<div className="mcp-server-list">
						{servers.map((server) => <MCPServerCard server={server} onChange={(patch) => updateServer(server.id, patch)} onRemove={() => setServers((current) => current.filter((item) => item.id !== server.id))} onEntryChange={updateSecretEntry} onEntryAdd={addSecretEntry} onEntryRemove={removeSecretEntry} key={server.id} />)}
						{!servers.length && <div className="agent-settings-empty"><Network size={20} /><strong>尚未配置 MCP Server</strong><span>可以添加本机命令或远程 HTTP/SSE 服务。</span></div>}
					</div>
				</div>
			</>}
			<div className={`agent-settings-footer ${state}`}><span>{state === 'saved' ? <CheckCircle2 size={14} /> : state === 'error' ? <CircleAlert size={14} /> : <ShieldCheck size={14} />}{message || 'MCP 环境变量和请求头按密钥处理，页面不会取回已保存的原文。'}</span><button type="button" onClick={() => void save()} disabled={!config || state === 'loading' || state === 'saving'}>{state === 'saving' ? <LoaderCircle className="spin" size={15} /> : <Save size={15} />}保存 Skill/MCP</button></div>
		</section>
	);
}

function MCPServerCard({ server, onChange, onRemove, onEntryChange, onEntryAdd, onEntryRemove }: {
	server: MCPDraft;
	onChange: (patch: Partial<MCPDraft>) => void;
	onRemove: () => void;
	onEntryChange: (serverID: string, field: 'env' | 'headers', entryID: string, patch: Partial<SecretEntryDraft>) => void;
	onEntryAdd: (serverID: string, field: 'env' | 'headers') => void;
	onEntryRemove: (serverID: string, field: 'env' | 'headers', entryID: string) => void;
}) {
	return <article className={`mcp-server-card ${server.enabled ? '' : 'disabled'}`}>
		<header><label><input type="checkbox" checked={server.enabled} onChange={(event) => onChange({ enabled: event.target.checked })} /><span>{server.enabled ? '启用' : '停用'}</span></label><button type="button" onClick={onRemove} aria-label="删除 MCP Server"><Trash2 size={14} /></button></header>
		<div className="settings-grid two-columns"><label><span>名称</span><input value={server.name} onChange={(event) => onChange({ name: event.target.value })} placeholder="例如 filesystem" /></label><label><span>传输方式</span><select value={server.transport} onChange={(event) => onChange({ transport: event.target.value as MCPDraft['transport'] })}><option value="stdio">本机命令（stdio）</option><option value="http">Streamable HTTP</option><option value="sse">SSE</option></select></label></div>
		{server.transport === 'stdio' ? <><label><span>启动命令</span><input value={server.command || ''} onChange={(event) => onChange({ command: event.target.value })} placeholder="npx / uvx / 绝对路径" /></label><label><span>参数（每行一个）</span><textarea value={server.argsText} onChange={(event) => onChange({ argsText: event.target.value })} placeholder={'-y\n@modelcontextprotocol/server-filesystem\n/path'} /></label><SecretMapEditor label="环境变量" entries={server.env} onChange={(entryID, patch) => onEntryChange(server.id, 'env', entryID, patch)} onAdd={() => onEntryAdd(server.id, 'env')} onRemove={(entryID) => onEntryRemove(server.id, 'env', entryID)} /></> : <><label><span>服务 URL</span><input value={server.url || ''} onChange={(event) => onChange({ url: event.target.value })} placeholder="https://example.com/mcp" /></label><SecretMapEditor label="请求头" entries={server.headers} onChange={(entryID, patch) => onEntryChange(server.id, 'headers', entryID, patch)} onAdd={() => onEntryAdd(server.id, 'headers')} onRemove={(entryID) => onEntryRemove(server.id, 'headers', entryID)} /></>}
		<div className="settings-grid two-columns"><label><span>调用超时（秒）</span><input type="number" min="0" max="3600" value={server.timeout || 0} onChange={(event) => onChange({ timeout: Number(event.target.value) })} /></label><label><span>连接超时（秒）</span><input type="number" min="0" max="600" value={server.connect_timeout || 0} onChange={(event) => onChange({ connect_timeout: Number(event.target.value) })} /></label></div>
		<label className="mcp-parallel-toggle"><span><strong>允许并行工具调用</strong><small>仅为确认线程安全的 MCP Server 开启。</small></span><input type="checkbox" checked={Boolean(server.supports_parallel_tool_calls)} onChange={(event) => onChange({ supports_parallel_tool_calls: event.target.checked })} /></label>
	</article>;
}

function SecretMapEditor({ label, entries, onChange, onAdd, onRemove }: { label: string; entries: SecretEntryDraft[]; onChange: (entryID: string, patch: Partial<SecretEntryDraft>) => void; onAdd: () => void; onRemove: (entryID: string) => void }) {
	return <div className="mcp-secret-map"><div><span>{label}</span><button type="button" onClick={onAdd}><Plus size={12} />添加</button></div>{entries.map((entry) => <div className={`mcp-secret-row ${entry.remove ? 'removed' : ''}`} key={entry.id}><input value={entry.key} onChange={(event) => onChange(entry.id, { key: event.target.value })} placeholder="KEY" disabled={entry.configured} /><input type="password" value={entry.value} onChange={(event) => onChange(entry.id, { value: event.target.value, remove: false })} placeholder={entry.configured ? `${entry.masked || '已配置'}（留空保留）` : 'VALUE'} disabled={entry.remove} /><button type="button" onClick={() => onRemove(entry.id)} aria-label={entry.remove ? `撤销删除 ${entry.key}` : `删除 ${entry.key || label}`}>{entry.remove ? '撤销' : <Trash2 size={13} />}</button></div>)}</div>;
}

function toMCPDraft(server: HermesMCPServerSetting): MCPDraft {
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
