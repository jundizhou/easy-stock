import { describe, expect, it } from 'vitest';
import {
	DEFAULT_WORKSPACE_MODE,
	WORKSPACE_HASHES,
	WORKSPACE_MODES,
	workspaceHash,
	workspaceModeFromHash,
	type WorkspaceMode,
} from './workspace-modes';

// 与 App.tsx 左侧导航一一对应的期望清单。新增板块时必须同步这里 —— 这正是护栏的意义：
// 任何「加了模式却漏了入口 / 渲染 / hash」的改动都会在这里失败。
const EXPECTED_MODES: readonly WorkspaceMode[] = [
	'themes',
	'limit-up',
	'mastery',
	'reviews',
	'stock-ai',
	'ai',
	'market',
	'token-usage',
	'quant',
	'signals',
	'shannon',
	'core2',
	'inflection',
	'resonance',
	'execution',
];

describe('工作区注册表', () => {
	it('登记全部板块且不重复', () => {
		expect(new Set(WORKSPACE_MODES).size).toBe(WORKSPACE_MODES.length);
		expect([...WORKSPACE_MODES].sort()).toEqual([...EXPECTED_MODES].sort());
	});

	it('每个板块都有唯一且以 # 开头的 hash', () => {
		const hashes = WORKSPACE_MODES.map(mode => workspaceHash(mode));
		for (const [index, mode] of WORKSPACE_MODES.entries()) {
			expect(hashes[index], mode).toBe(WORKSPACE_HASHES[mode]);
			expect(hashes[index].startsWith('#'), mode).toBe(true);
		}
		expect(new Set(hashes).size).toBe(hashes.length);
	});

	it('每个板块都能通过自己的 hash 直达（刷新页面不会掉回默认板块）', () => {
		for (const mode of WORKSPACE_MODES) {
			expect(workspaceModeFromHash(workspaceHash(mode)), mode).toBe(mode);
		}
	});

	it('无法识别的 hash 回落到默认板块', () => {
		expect(DEFAULT_WORKSPACE_MODE).toBe('themes');
		for (const hash of ['', '#', '#unknown', '#themes-extra', '#core22']) {
			expect(workspaceModeFromHash(hash), hash).toBe(DEFAULT_WORKSPACE_MODE);
		}
	});

	it('行情总览接受子路径 hash', () => {
		expect(workspaceModeFromHash('#market')).toBe('market');
		expect(workspaceModeFromHash('#market/pulse')).toBe('market');
		expect(workspaceModeFromHash('#market')).toBe(workspaceModeFromHash(workspaceHash('market')));
	});
});
