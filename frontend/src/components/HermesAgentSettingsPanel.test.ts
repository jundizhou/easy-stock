import { describe, expect, it } from 'vitest';
import { splitMCPArgs } from './HermesAgentSettingsPanel';

describe('Hermes MCP settings', () => {
	it('splits one command argument per line and removes blank lines', () => {
		expect(splitMCPArgs(' -y\n\n @modelcontextprotocol/server-filesystem \n/path ')).toEqual(['-y', '@modelcontextprotocol/server-filesystem', '/path']);
	});
});
