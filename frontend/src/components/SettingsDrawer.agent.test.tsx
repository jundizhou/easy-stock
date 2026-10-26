// @vitest-environment jsdom
// Exercise the real drawer and child panel together; no external services are used.
import { useState } from 'react';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { requestJSON, type AgentSettings, type AppSettings } from '../lib/backend';
import { emptyNotifications } from '../lib/notifications';
import { SettingsDrawer } from './SettingsDrawer';

vi.mock('../lib/backend', async (importOriginal) => ({ ...await importOriginal<typeof import('../lib/backend')>(), requestJSON: vi.fn() }));
vi.mock('./AppUpdatePanel', () => ({ AppUpdatePanel: () => null }));

const config = { backendUrl: 'http://isolated-audit.invalid', token: '' };
let saved: AppSettings;
let agent: AgentSettings;
let failAgentLoad: boolean;
let failSave: boolean;

function Harness() {
  const [open, setOpen] = useState(true);
  return <><button onClick={() => setOpen(true)}>重新打开</button><SettingsDrawer config={config} open={open} onClose={() => setOpen(false)} /></>;
}

beforeEach(() => {
  failAgentLoad = false; failSave = false;
  Element.prototype.scrollIntoView = vi.fn();
  const secret = { configured: false };
  const profile = { id: 'test-model', name: 'audit model', provider: 'openai', base_url: 'https://example.invalid/v1', model: 'old-model', api_mode: 'chat_completions', api_key: secret };
  saved = {
    agent_runtime: 'hermes', agent: { available: false, configured: false, api_key_configured: false },
    llm: { ...profile, response_timeout_seconds: 300 }, llm_profiles: [profile], active_llm_profile_id: profile.id,
    credentials: { tushare_token: secret, ths_cookie: secret, xueqiu_cookie: secret, eastmoney_cookie: secret, wechat_api_token: secret },
    review_automation: { profiles: [{ id: 'audit-review', source: 'xueqiu', name: 'audit review', base_url: 'https://xueqiu.com', credential: secret, enabled: true, auto_analyze: true, sync_hour: 7 }] },
    notifications: emptyNotifications(),
  };
  agent = { reasoning_effort: 'medium', skills: [{ name: 'audit-skill', description: 'test only', category: 'audit', enabled: true }], mcp_servers: [
    { name: 'audit-mcp', enabled: true, transport: 'http', url: 'https://example.invalid/old', timeout: 300, connect_timeout: 60 },
    { name: 'remove-mcp', enabled: true, transport: 'http', url: 'https://example.invalid/remove' },
  ] };
  vi.mocked(requestJSON).mockReset();
  vi.mocked(requestJSON).mockImplementation(async (_config, path, options) => {
    if (path === '/api/v1/settings/agent/skills/market' || path === '/api/v1/settings/agent/skills/market/sources') return { data: [] };
    if (path === '/api/v1/settings/agent/skills/import') {
      agent.skills.push({ name: 'new-skill', description: 'new', category: 'audit', enabled: true });
      return { data: [{ name: 'new-skill' }] };
    }
    if (path === '/api/v1/settings/agent/skills/delete') {
      agent.skills = agent.skills.filter((skill) => skill.name !== JSON.parse(String(options?.body)).name);
      return { data: {} };
    }
    if (path === '/api/v1/settings/agent') {
      if (options?.method === 'PUT') {
        const body = JSON.parse(String(options.body));
        if (failSave) throw new Error('test save failure');
        applyAgent(body);
      } else if (failAgentLoad) throw new Error('Agent settings load failed');
      return { data: structuredClone(agent) };
    }
    if (path === '/api/v1/settings/llm/test') return { data: { model: 'audit', runtime: 'hermes', latency_ms: 1, response: 'ok', api_mode: 'chat_completions' } };
    if (path === '/api/v1/settings') {
      if (options?.method === 'PUT') {
        if (failSave) throw new Error('test save failure');
        const body = JSON.parse(String(options.body));
        if (body.agent_settings) applyAgent(body.agent_settings);
      }
      return { data: structuredClone(saved), agent_settings: structuredClone(agent) };
    }
    throw new Error(`Unexpected request ${path}`);
  });
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

function applyAgent(body: { skills: {name: string; enabled: boolean}[]; mcp_servers: AgentSettings['mcp_servers'] }) {
  agent.skills = agent.skills.map((skill) => ({ ...skill, enabled: body.skills.find((item) => item.name === skill.name)?.enabled ?? skill.enabled }));
  agent.mcp_servers = body.mcp_servers.map((server) => ({ ...server,
    env: Object.fromEntries(Object.entries(server.env || {}).map(([key, value]) => [key, typeof value === 'string' ? { configured: true } : value])),
    headers: Object.fromEntries(Object.entries(server.headers || {}).map(([key, value]) => [key, typeof value === 'string' ? { configured: true } : value])),
  }));
}

async function loaded() {
  await screen.findByLabelText(/^audit-skill/);
  // Open actual details sections so queries and interactions reflect visible controls.
  for (const details of document.querySelectorAll('details')) details.open = true;
  return within(screen.getByRole('dialog', { name: '系统设置' }));
}

function writes(path: string) {
  return vi.mocked(requestJSON).mock.calls.filter(([, url, options]) => url === path && options?.method === 'PUT').map(([, , options]) => JSON.parse(String(options?.body)));
}

async function editSkillsAndMCP() {
  const dialog = await loaded();
  fireEvent.click(dialog.getByLabelText(/^audit-skill/));
  const server = within(dialog.getByDisplayValue('audit-mcp').closest('article')!);
  fireEvent.click(server.getByRole('checkbox', { name: '启用' }));
  fireEvent.change(server.getByLabelText('服务 URL'), { target: { value: 'https://example.invalid/changed' } });
  fireEvent.change(server.getByLabelText('调用超时（秒）'), { target: { value: '90' } });
  const removed = within(dialog.getByDisplayValue('remove-mcp').closest('article')!);
  fireEvent.click(removed.getByRole('button', { name: '删除 MCP Server' }));
  fireEvent.click(dialog.getByRole('button', { name: '添加 MCP' }));
  const created = within(dialog.getByPlaceholderText('npx / uvx / 绝对路径').closest('article')!);
  fireEvent.change(created.getByLabelText('名称'), { target: { value: 'new-mcp' } });
  fireEvent.change(created.getByLabelText('启动命令'), { target: { value: 'audit-never-executed' } });
  return dialog;
}

describe('shared settings save lifecycle', () => {
  it('global Save persists Skill toggles and MCP edit/add/delete before closing', async () => {
    render(<Harness />);
    const dialog = await editSkillsAndMCP();
    fireEvent.click(dialog.getByRole('button', { name: '保存设置' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(writes('/api/v1/settings')).toHaveLength(1);
    expect(writes('/api/v1/settings')[0].agent_settings.skills).toEqual([{ name: 'audit-skill', enabled: false }]);
    expect(writes('/api/v1/settings/agent')).toHaveLength(0);
    fireEvent.click(screen.getByRole('button', { name: '重新打开' }));
    const reopened = await loaded();
    expect((reopened.getByLabelText(/^audit-skill/) as HTMLInputElement).checked).toBe(false);
    expect(reopened.getByDisplayValue('https://example.invalid/changed')).toBeTruthy();
    expect(reopened.queryByDisplayValue('remove-mcp')).toBeNull();
    expect(reopened.getByDisplayValue('new-mcp')).toBeTruthy();
  });

  it('dedicated Skill/MCP Save submits the changes and preserves them after reopening', async () => {
    render(<Harness />);
    const dialog = await editSkillsAndMCP();
    fireEvent.click(dialog.getByRole('button', { name: '仅保存 Skill/MCP' }));
    await dialog.findByText('Skill 与 MCP 设置已同步到本机 Agent；MCP 会自动重新加载。');
    expect(writes('/api/v1/settings/agent')[0]).toMatchObject({ skills: [{ name: 'audit-skill', enabled: false }], mcp_servers: [
      { name: 'audit-mcp', enabled: false, url: 'https://example.invalid/changed', timeout: 90 },
      { name: 'new-mcp', command: 'audit-never-executed' },
    ] });
    fireEvent.click(dialog.getByRole('button', { name: '保存设置' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    fireEvent.click(screen.getByRole('button', { name: '重新打开' }));
    const reopened = await loaded();
    expect((reopened.getByLabelText(/^audit-skill/) as HTMLInputElement).checked).toBe(false);
    expect(reopened.getByDisplayValue('https://example.invalid/changed')).toBeTruthy();
    expect(reopened.getByDisplayValue('new-mcp')).toBeTruthy();
    expect(reopened.queryByDisplayValue('remove-mcp')).toBeNull();
  });

  it('global Save includes model, runtime, timeout, data credentials and disabled review flags', async () => {
    render(<Harness />);
    const dialog = await loaded();
    fireEvent.click(dialog.getByRole('button', { name: /Codex/ }));
    fireEvent.change(dialog.getByLabelText('接口协议'), { target: { value: 'codex_responses' } });
    fireEvent.change(dialog.getByDisplayValue('old-model'), { target: { value: 'changed-model' } });
    fireEvent.change(dialog.getByLabelText(/^模型响应等待时间/), { target: { value: '600' } });
    fireEvent.change(dialog.getByLabelText('模型 API Key', { exact: true }), { target: { value: 'fake-model-key' } });
    fireEvent.change(dialog.getByLabelText('Tushare Pro Token', { exact: true }), { target: { value: 'fake-tushare-token' } });
    fireEvent.change(dialog.getByLabelText('同花顺 Cookie / Token', { exact: true }), { target: { value: 'fake-ths-token' } });
    fireEvent.change(dialog.getByLabelText('东方财富 Cookie', { exact: true }), { target: { value: 'fake-eastmoney-cookie' } });
    fireEvent.click(dialog.getByRole('button', { name: '已启用' }));
    fireEvent.click(dialog.getByRole('button', { name: '抓取后自动 AI 提炼' }));
    fireEvent.change(dialog.getByLabelText('每日同步时间'), { target: { value: '19' } });
    fireEvent.click(dialog.getByRole('button', { name: '保存设置' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(writes('/api/v1/settings')[0]).toMatchObject({
      agent_runtime: 'codex', llm: { response_timeout_seconds: 600 },
      llm_profiles: [{ id: 'test-model', model: 'changed-model', api_mode: 'codex_responses', api_key: 'fake-model-key' }],
      credentials: { tushare_token: 'fake-tushare-token', ths_cookie: 'fake-ths-token', eastmoney_cookie: 'fake-eastmoney-cookie' },
      review_automation: { profiles: expect.arrayContaining([{ id: 'audit-review', source: 'xueqiu', name: 'audit review', base_url: 'https://xueqiu.com', clear_credential: false, enabled: false, auto_analyze: false, sync_hour: 19 }]) },
    });
  });

  it('blocks both saves after a failed Agent load and allows retry without losing existing MCP', async () => {
    failAgentLoad = true;
    render(<Harness />);
    await screen.findByText('Agent settings load failed');
    for (const details of document.querySelectorAll('details')) details.open = true;
    for (const name of ['仅保存 Skill/MCP', '保存设置']) {
      const save = screen.getByRole('button', { name });
      expect((save as HTMLButtonElement).disabled).toBe(true);
      fireEvent.click(save);
    }
    expect(writes('/api/v1/settings/agent')).toHaveLength(0);
    expect(writes('/api/v1/settings')).toHaveLength(0);
    failAgentLoad = false;
    fireEvent.click(screen.getByRole('button', { name: '重试读取 Skill/MCP' }));
    const dialog = await loaded();
    expect(dialog.getByDisplayValue('audit-mcp')).toBeTruthy();
    expect((dialog.getByRole('button', { name: '保存设置' }) as HTMLButtonElement).disabled).toBe(false);
  });

  it('import refresh preserves edited switches and discovers new skills', async () => {
    render(<Harness />);
    const dialog = await loaded();
    fireEvent.click(dialog.getByLabelText(/^audit-skill/));
    expect((dialog.getByLabelText(/^audit-skill/) as HTMLInputElement).checked).toBe(false);
    const zip = document.querySelector('input[accept=".zip,application/zip"]')!;
    fireEvent.change(zip, { target: { files: [new File(['fake'], 'audit.zip', { type: 'application/zip' })] } });
    await screen.findByText('已导入 1 个 Skill，请在列表中确认启用状态。');
    expect((dialog.getByLabelText(/^audit-skill/) as HTMLInputElement).checked).toBe(false);
    expect((dialog.getByLabelText(/^new-skill/) as HTMLInputElement).checked).toBe(true);
    expect(writes('/api/v1/settings/agent')).toHaveLength(0);
  });
  it.each(['仅保存 Skill/MCP', '保存设置'])('preserves drafts after %s fails and retries successfully', async (button) => {
    render(<Harness />);
    const dialog = await editSkillsAndMCP();
    failSave = true;
    fireEvent.click(dialog.getByRole('button', { name: button }));
    await dialog.findByText(/test save failure/);
    expect((dialog.getByLabelText(/^audit-skill/) as HTMLInputElement).checked).toBe(false);
    expect(dialog.getByDisplayValue('https://example.invalid/changed')).toBeTruthy();
    expect(agent.skills[0].enabled).toBe(true);
    failSave = false;
    fireEvent.click(dialog.getByRole('button', { name: button }));
    await waitFor(() => expect(agent.skills[0].enabled).toBe(false));
    expect(agent.mcp_servers.map((server) => server.name)).toEqual(['audit-mcp', 'new-mcp']);
  });

  it('preserves pending switches when deleting a different Skill', async () => {
    agent.skills.push({ name: 'delete-skill', description: 'delete fixture', category: 'audit', enabled: true });
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    render(<Harness />);
    const dialog = await loaded();
    fireEvent.click(dialog.getByLabelText(/^audit-skill/));
    fireEvent.click(dialog.getByRole('button', { name: '删除 delete-skill' }));
    await dialog.findByText('已删除 Skill「delete-skill」。');
    expect((dialog.getByLabelText(/^audit-skill/) as HTMLInputElement).checked).toBe(false);
    expect(dialog.queryByLabelText(/^delete-skill/)).toBeNull();
    fireEvent.click(dialog.getByRole('button', { name: '保存设置' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(agent.skills).toHaveLength(1);
    expect(agent.skills[0].enabled).toBe(false);
  });

  it('preserves pending switches when GitHub installation refreshes the Skill list', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => {
      agent.skills.push({ name: 'github-skill', description: 'installed', category: 'audit', enabled: true });
      return new Response('{"type":"complete","data":[{"name":"github-skill"}]}\n');
    }));
    render(<Harness />);
    const dialog = await loaded();
    fireEvent.click(dialog.getByLabelText(/^audit-skill/));
    fireEvent.change(dialog.getByPlaceholderText(/GitHub 仓库地址/), { target: { value: 'https://github.com/example/test-fixture' } });
    fireEvent.click(dialog.getByRole('button', { name: '从 GitHub 安装' }));
    await dialog.findByText('已从 GitHub 安装 1 个 Skill。');
    expect((dialog.getByLabelText(/^audit-skill/) as HTMLInputElement).checked).toBe(false);
    expect((dialog.getByLabelText(/^github-skill/) as HTMLInputElement).checked).toBe(true);
  });

  it('keeps Skill and MCP drafts when the post-import refresh fails, and recovers on retry', async () => {
    render(<Harness />);
    const dialog = await loaded();
    fireEvent.click(dialog.getByLabelText(/^audit-skill/));
    fireEvent.change(dialog.getByDisplayValue('https://example.invalid/old'), { target: { value: 'https://example.invalid/changed' } });
    failAgentLoad = true;
    fireEvent.change(document.querySelector('input[accept=".zip,application/zip"]')!, { target: { files: [new File(['fake'], 'audit.zip')] } });
    await dialog.findByText('读取 Skill/MCP 设置失败，请重试；未保存的修改已保留。');
    expect((dialog.getByRole('button', { name: '保存设置' }) as HTMLButtonElement).disabled).toBe(true);
    failAgentLoad = false;
    fireEvent.click(dialog.getByRole('button', { name: '重试读取 Skill/MCP' }));
    await waitFor(() => expect(dialog.queryByText('读取 Skill/MCP 设置失败，请重试；未保存的修改已保留。')).toBeNull());
    expect((dialog.getByLabelText(/^audit-skill/) as HTMLInputElement).checked).toBe(false);
    expect(dialog.getByDisplayValue('https://example.invalid/changed')).toBeTruthy();
    expect(dialog.getByLabelText(/^new-skill/)).toBeTruthy();
  });

  it('locks global Save, edits and close while a Skill import is in flight', async () => {
    let finish!: () => void;
    const pending = new Promise<void>((resolve) => { finish = resolve; });
    const original = vi.mocked(requestJSON).getMockImplementation()!;
    vi.mocked(requestJSON).mockImplementation(async (...args) => {
      if (args[1] === '/api/v1/settings/agent/skills/import') await pending;
      return original(...args);
    });
    render(<Harness />);
    const dialog = await loaded();
    fireEvent.change(document.querySelector('input[accept=".zip,application/zip"]')!, { target: { files: [new File(['fake'], 'audit.zip')] } });
    expect(dialog.getByRole('button', { name: '保存设置' }).matches(':disabled')).toBe(true);
    expect(dialog.getByLabelText(/^audit-skill/).matches(':disabled')).toBe(true);
    fireEvent.click(dialog.getByRole('button', { name: '保存设置' }));
    fireEvent.click(dialog.getByRole('button', { name: '关闭设置' }));
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(screen.getByRole('dialog')).toBeTruthy();
    expect(writes('/api/v1/settings')).toHaveLength(0);
    finish();
    await dialog.findByText('已导入 1 个 Skill，请在列表中确认启用状态。');
    expect(dialog.getByRole('button', { name: '保存设置' }).matches(':disabled')).toBe(false);
  });

  it('saves MCP secrets through save-and-test and does not resend them on a subsequent global save', async () => {
    render(<Harness />);
    const dialog = await loaded();
    const server = within(dialog.getByDisplayValue('audit-mcp').closest('article')!);
    fireEvent.click(server.getByRole('button', { name: '添加' }));
    fireEvent.change(server.getByPlaceholderText('KEY'), { target: { value: 'Authorization' } });
    fireEvent.change(server.getByPlaceholderText('VALUE'), { target: { value: 'fake-new-token' } });
    fireEvent.click(dialog.getByRole('button', { name: '保存并测试连接' }));
    await dialog.findByText('Agent 模型连接可用');
    expect(writes('/api/v1/settings')[0].agent_settings.mcp_servers[0].headers).toEqual({ Authorization: 'fake-new-token' });
    expect((dialog.getByPlaceholderText('已配置（留空保留）') as HTMLInputElement).value).toBe('');
    fireEvent.click(dialog.getByRole('button', { name: '保存设置' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(writes('/api/v1/settings')[1]).not.toHaveProperty('agent_settings');
  });

});
