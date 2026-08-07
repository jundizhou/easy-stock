import { SectorMap, SectorMapGroup, SectorMapNode } from './backend';

export type SectorFilter = 'all' | 'matched' | 'up' | 'down' | 'warnings';
export type SectorSort = 'original' | 'main_net_inflow' | 'change_percent';

export type SectorViewOptions = {
	filter: SectorFilter;
	sort: SectorSort;
};

export type SectorMapStats = {
	groups: number;
	nodes: number;
	matchedNodes: number;
	stocks: number;
};

export function buildSectorMapPath(theme: string): string {
	const params = new URLSearchParams({ theme });
	return `/api/v1/sector-map?${params.toString()}`;
}

export function sectorMapStats(map: SectorMap | null): SectorMapStats {
	if (!map) {
		return { groups: 0, nodes: 0, matchedNodes: 0, stocks: 0 };
	}
	return map.groups.reduce<SectorMapStats>(
		(stats, group) => {
			stats.groups += 1;
			stats.nodes += group.nodes.length;
			for (const node of group.nodes) {
				if (node.board_code) {
					stats.matchedNodes += 1;
				}
				stats.stocks += node.stocks.length;
			}
			return stats;
		},
		{ groups: 0, nodes: 0, matchedNodes: 0, stocks: 0 },
	);
}

export function toneForChange(value?: number): 'up' | 'down' | 'flat' {
	if (typeof value !== 'number' || value === 0) {
		return 'flat';
	}
	return value > 0 ? 'up' : 'down';
}

export function filterAndSortSectorGroups(map: SectorMap | null, options: SectorViewOptions): SectorMapGroup[] {
	if (!map) {
		return [];
	}
	return map.groups
		.map((group) => ({
			...group,
			nodes: sortNodes(group.nodes.filter((node) => matchesFilter(node, options.filter)), options.sort),
		}))
		.filter((group) => group.nodes.length > 0);
}

export function nodeSubtitle(node: SectorMapNode): string {
	if (node.board_name) {
		return node.board_name;
	}
	if (node.stock_source?.includes('eastmoney:stock-selection')) {
		return '东财行业/概念成分';
	}
	return '待匹配题材成分';
}

export function warningLabel(warning: string): string {
	const labels: Record<string, string> = {
		'EastMoney board constituents unavailable': '板块成分股暂不可用',
		'no matched EastMoney board': '未匹配到行情板块',
		'no stocks hydrated': '未获取到股票行情',
	};
	return labels[warning] || warning;
}

function matchesFilter(node: SectorMapNode, filter: SectorFilter): boolean {
	switch (filter) {
		case 'matched':
			return node.match_status === 'matched';
		case 'up':
			return node.change_percent > 0;
		case 'down':
			return node.change_percent < 0;
		case 'warnings':
			return Boolean(node.warnings?.length);
		case 'all':
		default:
			return true;
	}
}

function sortNodes(nodes: SectorMapNode[], sort: SectorSort): SectorMapNode[] {
	const copied = [...nodes];
	switch (sort) {
		case 'main_net_inflow':
			return copied.sort((a, b) => b.main_net_inflow - a.main_net_inflow);
		case 'change_percent':
			return copied.sort((a, b) => b.change_percent - a.change_percent);
		case 'original':
		default:
			return copied;
	}
}
