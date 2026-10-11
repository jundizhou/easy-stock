// 工作区（左侧板块）注册表：模式清单、hash 映射、hash 解析。
//
// 这里是「板块」相关事实的唯一来源，App.tsx 只消费它。抽出来的两个理由：
//   1. 18 个板块的入口 / 渲染 / hash / 顶栏状态链过去分散在多处巨型三元链里，漏一处就
//      会静默回退到题材雷达（历史上 `#shannon/#core2/#inflection/#resonance/#regime`
//      五个板块的 hash 直达就漏过）；
//   2. 纯函数可以在 node 环境直接测（本项目前端测试不依赖 jsdom），从而给「板块保活」
//      改造加上回归护栏。

export type WorkspaceMode =
	| 'themes'
	| 'limit-up'
	| 'mastery'
	| 'reviews'
	| 'stock-ai'
	| 'ai'
	| 'market'
	| 'token-usage'
	| 'quant'
	| 'signals'
	| 'shannon'
	| 'core2'
	| 'inflection'
	| 'resonance'
	| 'execution'
	| 'news-verdict';

/** 全部工作区模式。每新增一个板块必须同时登记 hash 映射与渲染面板。 */
export const WORKSPACE_MODES: readonly WorkspaceMode[] = [
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
	'news-verdict',
];

/** 无有效 hash（或无法识别）时的落点。 */
export const DEFAULT_WORKSPACE_MODE: WorkspaceMode = 'themes';

/** 每个模式写回地址栏的 hash。 */
export const WORKSPACE_HASHES: Record<WorkspaceMode, string> = {
	themes: '#themes',
	'limit-up': '#limit-up',
	mastery: '#mastery',
	reviews: '#reviews',
	'stock-ai': '#stock-ai',
	ai: '#ai',
	market: '#market/pulse',
	'token-usage': '#token-usage',
	quant: '#quant',
	signals: '#signals',
	shannon: '#shannon',
	core2: '#core2',
	inflection: '#inflection',
	resonance: '#resonance',
	execution: '#execution',
	'news-verdict': '#news-verdict',
};

/**
 * hash → 模式。
 *
 * `market` 允许带子路径（如 `#market/pulse`），因此用前缀匹配；其余模式要求精确匹配。
 * 无法识别时回到默认板块，避免刷新页面掉进空白状态。
 */
export function workspaceModeFromHash(hash: string): WorkspaceMode {
	if (typeof hash !== 'string') return DEFAULT_WORKSPACE_MODE;
	for (const mode of WORKSPACE_MODES) {
		if (mode === 'market') {
			if (hash.startsWith('#market')) return 'market';
			continue;
		}
		if (hash === WORKSPACE_HASHES[mode]) return mode;
	}
	return DEFAULT_WORKSPACE_MODE;
}

/** 模式 → 写回地址栏的 hash。 */
export function workspaceHash(mode: WorkspaceMode): string {
	return WORKSPACE_HASHES[mode] ?? WORKSPACE_HASHES[DEFAULT_WORKSPACE_MODE];
}
