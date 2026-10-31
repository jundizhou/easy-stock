import presets from './mcp-presets.json';
import { nextID, type MCPDraft } from './agent-settings';

export function hasParallelSearch(servers: Pick<MCPDraft, 'url'>[]): boolean {
  return servers.some(({ url }) => {
    try {
      const parsed = new URL(url || '');
      return parsed.origin === 'https://search.parallel.ai'
        && ['/mcp', '/mcp-oauth'].includes(parsed.pathname.replace(/\/+$/, ''));
    } catch {
      return false;
    }
  });
}

// Add an editable draft; never overwrite or re-enable a user's existing server.
export function parallelSearchDraft(servers: MCPDraft[]): MCPDraft | null {
  if (hasParallelSearch(servers)) return null;
  const preset = presets['parallel-search'];
  const names = new Set(servers.map((server) => server.name.trim()));
  let name = preset.name;
  for (let suffix = 2; names.has(name); suffix++) name = `${preset.name}-${suffix}`;
  return {
    ...preset, name, transport: 'http', id: nextID('mcp'), originalName: '',
    argsText: '', env: [],
    headers: Object.entries(preset.headers).map(([key, value]) => ({
      id: nextID('header'), key, value, configured: false, remove: false,
    })),
  };
}
