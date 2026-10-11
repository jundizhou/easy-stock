import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { WORKSPACE_MODES } from './lib/workspace-modes';

// 这些断言直接读源码，而不是渲染组件：本项目前端测试运行在 node 环境、未安装 jsdom /
// @testing-library，为了渲染整个 App 引入新依赖会改动 lockfile（进而影响 CI 的 npm ci），
// 而「切板块会卸载组件」这一回归完全可以在源码层面精确锁定 —— 它本来就是结构问题。
const appSource = readFileSync(new URL('./App.tsx', import.meta.url), 'utf8');
const stylesSource = readFileSync(new URL('./styles.css', import.meta.url), 'utf8');

describe('App 板块保活（keep-alive）回归护栏', () => {
	it('每个板块都有侧栏入口', () => {
		for (const mode of WORKSPACE_MODES) {
			expect(appSource, `缺少侧栏入口: ${mode}`).toContain(`switchWorkspace('${mode}')`);
		}
	});

	it('每个板块都通过 workspace-pane 常驻挂载，而不是条件渲染', () => {
		for (const mode of WORKSPACE_MODES) {
			expect(appSource, `缺少保活挂载: ${mode}`).toContain(`visitedModes.has('${mode}')`);
			expect(appSource, `缺少 hidden 控制: ${mode}`).toContain(`hidden={workspaceMode !== '${mode}'}`);
		}
	});

	it('不再使用条件渲染（切换即卸载会导致 AI 流式中断、板块数据回退）', () => {
		// 注意模式名可能含数字（core2），字符类必须覆盖 [a-z0-9-]。
		expect(appSource).not.toMatch(/workspaceMode === '[a-z0-9-]+' *\? *</);
	});

	it('保活容器在样式上必须 display:contents，隐藏时必须 display:none', () => {
		expect(stylesSource).toMatch(/\.workspace-pane\s*\{[^}]*display:\s*contents/);
		expect(stylesSource).toMatch(/\.workspace-pane\[hidden\]\s*\{[^}]*display:\s*none/);
	});

	it('不给工作区模式维护第二份定义，hash 统一走共享映射', () => {
		expect(appSource).not.toMatch(/^type WorkspaceMode =/m);
		expect(appSource).toContain("from './lib/workspace-modes'");
		expect(appSource).toContain('workspaceHash(mode)');
		expect(appSource).not.toMatch(/replaceState\(\s*null\s*,\s*''\s*,\s*mode === /);
	});

	it('初始板块由共享 hash 解析决定', () => {
		expect(appSource).toContain('workspaceModeFromHash(window.location.hash)');
	});

	it('点开板块（首次与切回）都会触发刷新，走统一 refreshKey 路径', () => {
		// 改为：switchWorkspace 内对目标 mode 调用 refreshWorkspace，不再用 visitedModes 门控，
		// 首次点开与切回已访问板块都显式刷新（组件用 AbortController 管理 load，旧请求会被 abort，无重复请求）。
		expect(appSource).toMatch(/refreshWorkspace\(mode\);/);
		expect(appSource).not.toMatch(/if \(visitedModes\.has\(mode\)\) refreshWorkspace\(mode\)/);
	});

	it('存在自动刷新定时器，且跳过 AI 对话、个股 AI 分析与拐点引擎的手动评估', () => {
		expect(appSource).toMatch(/window\.setInterval\(\(\) => \{[\s\S]*?\}, 30_000\)/);
		// 个股 AI 分析是 POST 型重操作：自动重跑会清空结果面板并重新采集，
		// 每 30 秒闪一次（还会向后端刷研究任务），必须与 AI 对话、拐点引擎一样跳过。
		expect(appSource).toMatch(/if \(mode === 'ai' \|\| mode === 'stock-ai' \|\| mode === 'inflection'\) return;/);
	});
});
