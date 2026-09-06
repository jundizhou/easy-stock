import { CheckCircle2, CircleAlert, ExternalLink, LoaderCircle, Network, Plus, Puzzle, Save, Search, ShieldCheck, Trash2 } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import type { BackendConfig, HermesAgentSettings, HermesInstalledSkill, HermesMCPServerSetting, HermesSkillMarketEntry, HermesSkillMarketSource, HermesSkillSetting, SecretSettingStatus } from '../lib/backend';
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
	const [gitURL, setGitURL] = useState('');
	const [marketSkills, setMarketSkills] = useState<HermesSkillMarketEntry[]>([]);
	const [marketSources, setMarketSources] = useState<HermesSkillMarketSource[]>([]);
	const [downloadProgress, setDownloadProgress] = useState<{ downloaded: number; total: number; bytesPerSecond: number; url: string } | null>(null);
	const directoryInput = useRef<HTMLInputElement>(null);
	const zipInput = useRef<HTMLInputElement>(null);

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
		requestJSON<{ data: HermesSkillMarketEntry[] }>(config, '/api/v1/settings/agent/skills/market')
			.then(({ data }) => { if (!cancelled) setMarketSkills(data || []); })
			.catch(() => { if (!cancelled) setMarketSkills([]); });
		requestJSON<{ data: HermesSkillMarketSource[] }>(config, '/api/v1/settings/agent/skills/market/sources')
			.then(({ data }) => { if (!cancelled) setMarketSources(data || []); })
			.catch(() => { if (!cancelled) setMarketSources([]); });
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

	const importSkills = async (files: FileList | null) => {
		if (!config || !files?.length) return;
		setState('saving');
		setMessage('正在导入 Skill…');
		try {
			const form = new FormData();
			Array.from(files).forEach((file) => {
				const relativePath = (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name;
				form.append('files', file, file.name);
				form.append('paths', relativePath);
			});
			const payload = await requestJSON<{ data: HermesInstalledSkill[] }>(config, '/api/v1/settings/agent/skills/import', { method: 'POST', body: form });
			const imported = payload.data || [];
			const refreshed = await requestJSON<{ data: HermesAgentSettings }>(config, '/api/v1/settings/agent');
			setSkills(refreshed.data.skills || []);
			setState('saved');
			setMessage(`已导入 ${imported.length} 个 Skill，请在列表中确认启用状态。`);
		} catch (error) {
			setState('error');
			setMessage(error instanceof Error ? error.message : '导入 Skill 失败');
		} finally {
			if (directoryInput.current) directoryInput.current.value = '';
			if (zipInput.current) zipInput.current.value = '';
		}
	};

	const installGitSkill = async () => {
		if (!config || !gitURL.trim()) return;
		await installGitSkillURL(gitURL.trim());
	};

	const installGitSkillFromMarket = async (entry: HermesSkillMarketEntry) => {
		await installGitSkillURL(entry.path);
	};

	const installGitSkillURL = async (url: string) => {
		if (!config || !url.trim()) return;
		setState('saving');
		setDownloadProgress({ downloaded: 0, total: 0, bytesPerSecond: 0, url: '' });
		setMessage('正在连接 GitHub…');
		try {
			const response = await fetch(new URL('/api/v1/settings/agent/skills/install-git?progress=1', config.backendUrl), { method: 'POST', headers: { 'Content-Type': 'application/json', ...(config.token ? { Authorization: `Bearer ${config.token}` } : {}) }, body: JSON.stringify({ url: url.trim() }) });
			if (!response.ok) throw new Error((await response.text()) || `HTTP ${response.status}`);
			const reader = response.body?.getReader();
			if (!reader) throw new Error('浏览器不支持读取下载进度');
			const decoder = new TextDecoder();
			let pending = '';
			let installed: HermesInstalledSkill[] = [];
			while (true) {
				const { value, done } = await reader.read();
				pending += decoder.decode(value || new Uint8Array(), { stream: !done });
				const lines = pending.split('\n');
				pending = lines.pop() || '';
				for (const line of lines) {
					if (!line.trim()) continue;
					const event = JSON.parse(line) as { type: string; url?: string; downloaded?: number; total?: number; bytes_per_second?: number; data?: HermesInstalledSkill[]; error?: string };
					if (event.type === 'started') setDownloadProgress((current) => ({ downloaded: current?.downloaded || 0, total: current?.total || 0, bytesPerSecond: current?.bytesPerSecond || 0, url: event.url || current?.url || '' }));
					if (event.type === 'progress') {
						setDownloadProgress((current) => ({ downloaded: event.downloaded || 0, total: event.total || 0, bytesPerSecond: event.bytes_per_second || 0, url: current?.url || '' }));
						const percent = event.total && event.total > 0 ? ` ${Math.min(100, Math.round((event.downloaded || 0) * 100 / event.total))}%` : '';
						setMessage(`正在下载 GitHub Skill…${percent} · ${formatTransferRate(event.bytes_per_second || 0)}`);
					}
					if (event.type === 'error') throw new Error(event.error || '安装 GitHub Skill 失败');
					if (event.type === 'complete') installed = event.data || [];
				}
				if (done) break;
			}
			const refreshed = await requestJSON<{ data: HermesAgentSettings }>(config, '/api/v1/settings/agent');
			setSkills(refreshed.data.skills || []);
			setGitURL('');
			setState('saved');
			setMessage(`已从 GitHub 安装 ${installed.length} 个 Skill。`);
		} catch (error) {
			setState('error');
			setMessage(error instanceof Error ? error.message : '安装 GitHub Skill 失败');
		} finally {
			setDownloadProgress(null);
		}
	};

	const marketInstalled = (entry: HermesSkillMarketEntry) => skills.some((skill) => skill.name === entry.path.split('/').pop());

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
					<div className="agent-settings-heading"><div><strong>Skills</strong><span>{enabledSkillCount}/{skills.length} 个已启用</span></div><div className="agent-settings-heading-actions"><label><Search size={14} /><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索 Skill" /></label><button type="button" onClick={() => directoryInput.current?.click()}><Plus size={14} />导入目录</button><button type="button" onClick={() => zipInput.current?.click()}><Plus size={14} />导入 ZIP</button><input ref={directoryInput} type="file" hidden multiple {...({ webkitdirectory: '', directory: '' } as Record<string, string>)} onChange={(event) => void importSkills(event.target.files)} /><input ref={zipInput} type="file" hidden accept=".zip,application/zip" onChange={(event) => void importSkills(event.target.files)} /></div></div>
					<div className="skill-git-import"><input value={gitURL} onChange={(event) => setGitURL(event.target.value)} placeholder="GitHub 仓库地址，例如 https://github.com/openai/skills" /><button type="button" disabled={!gitURL.trim() || state === 'saving'} onClick={() => void installGitSkill()}><Plus size={14} />从 GitHub 安装</button>{gitURL.trim() && <a className="skill-git-link" href={gitURL.trim()} target="_blank" rel="noreferrer" title="查看下载链接"><ExternalLink size={14} />查看链接</a>}</div>
					{downloadProgress && <div className="skill-download-progress"><div className="skill-download-progress-bar"><i style={{ width: `${downloadProgress.total > 0 ? Math.min(100, downloadProgress.downloaded * 100 / downloadProgress.total) : 8}%` }} /></div><span>{downloadProgress.total > 0 ? `${Math.round(downloadProgress.downloaded * 100 / downloadProgress.total)}%` : '准备中'} · {formatBytes(downloadProgress.downloaded)}{downloadProgress.total > 0 ? ` / ${formatBytes(downloadProgress.total)}` : ''} · {formatTransferRate(downloadProgress.bytesPerSecond)}{downloadProgress.url && <a href={downloadProgress.url} target="_blank" rel="noreferrer">查看下载链接</a>}</span></div>}
					{marketSkills.length > 0 && <div className="skill-market"><div className="skill-market-title"><strong>A 股精选 Skill</strong><span>easy-stock 投研目录</span></div><div className="skill-market-grid">{marketSkills.map((entry) => <article className="skill-market-card" key={entry.id}><div><strong>{entry.name}</strong><small>{entry.category}</small></div><p>{entry.description}</p><button type="button" disabled={marketInstalled(entry) || state === 'saving'} onClick={() => void installGitSkillFromMarket(entry)}>{marketInstalled(entry) ? '已安装' : '安装'}</button></article>)}</div></div>}
					{marketSources.length > 0 && <div className="skill-market-sources"><div className="skill-market-title"><strong>更多市场</strong><span>打开目录浏览后，可复制仓库地址或下载 ZIP 导入</span></div><div className="skill-market-source-list">{marketSources.map((source) => <a href={source.url} target="_blank" rel="noreferrer" key={source.id}><span><strong>{source.name}</strong><small>{source.region}</small></span><em>{source.description}</em></a>)}</div></div>}
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

function formatBytes(value: number): string {
	if (!value || value < 1024) return `${Math.max(0, Math.round(value || 0))} B`;
	const units = ['KB', 'MB', 'GB'];
	let amount = value / 1024;
	let unit = 0;
	while (amount >= 1024 && unit < units.length - 1) { amount /= 1024; unit += 1; }
	return `${amount.toFixed(amount >= 10 ? 0 : 1)} ${units[unit]}`;
}

function formatTransferRate(value: number): string {
	return value > 0 ? `${formatBytes(value)}/s` : '速度计算中';
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
