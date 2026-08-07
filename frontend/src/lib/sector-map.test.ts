import { describe, expect, it } from 'vitest';
import {
  buildSectorMapPath,
  filterAndSortSectorGroups,
  nodeSubtitle,
  sectorMapStats,
  toneForChange,
  warningLabel,
} from './sector-map';
import { SectorMap } from './backend';

describe('sector map helpers', () => {
  it('builds the sector map endpoint with an encoded theme', () => {
    expect(buildSectorMapPath('semiconductor_materials')).toBe('/api/v1/sector-map?theme=semiconductor_materials');
  });

  it('counts nodes, matched nodes, and hydrated stocks', () => {
    const map: SectorMap = {
      theme: 'semiconductor_materials',
      name: '半导体材料',
      tabs: ['半导体材料'],
      groups: [
        {
          id: 'materials_core',
          name: '半导体材料',
          nodes: [
            { id: 'photoresist', name: '光刻胶', board_code: 'BK0891', board_name: '光刻胶', change_percent: -1, main_net_inflow: -200, stocks: [{ symbol: '300576.SZ', name: '容大感光', price: 48, change: 0, change_percent: -1, volume: 0, amount: 0, total_market_cap: 0, float_market_cap: 0, main_net_inflow: 0, meta: { source: 'test', fetched_at: '', latency_ms: 0, stale: false } }], match_status: 'matched' },
            { id: 'cmp', name: 'CMP', change_percent: 0, main_net_inflow: 0, stocks: [], match_status: 'unmatched' },
          ],
        },
      ],
      meta: { source: 'test', fetched_at: '', latency_ms: 0, stale: false },
    };

    expect(sectorMapStats(map)).toEqual({ groups: 1, nodes: 2, matchedNodes: 1, stocks: 1 });
  });

  it('uses A-share red for gains and green for losses', () => {
    expect(toneForChange(1.2)).toBe('up');
    expect(toneForChange(-0.5)).toBe('down');
    expect(toneForChange(0)).toBe('flat');
  });

  it('filters sector groups and sorts nodes by main inflow', () => {
    const map: SectorMap = {
      theme: 'semiconductor_materials',
      name: '半导体材料',
      tabs: ['半导体材料'],
      groups: [
        {
          id: 'materials_core',
          name: '半导体材料',
          nodes: [
            { id: 'a', name: '上涨节点', board_code: 'BK1', board_name: 'A', change_percent: 3, main_net_inflow: 10, stocks: [], match_status: 'matched' },
            { id: 'b', name: '下跌节点', board_code: 'BK2', board_name: 'B', change_percent: -2, main_net_inflow: 90, stocks: [], match_status: 'matched' },
            { id: 'c', name: '未匹配', change_percent: 0, main_net_inflow: 0, stocks: [], match_status: 'unmatched' },
          ],
        },
      ],
      meta: { source: 'test', fetched_at: '', latency_ms: 0, stale: false },
    };

    const filtered = filterAndSortSectorGroups(map, { filter: 'matched', sort: 'main_net_inflow' });
    expect(filtered).toHaveLength(1);
    expect(filtered[0].nodes.map((node) => node.id)).toEqual(['b', 'a']);

    const rising = filterAndSortSectorGroups(map, { filter: 'up', sort: 'original' });
    expect(rising[0].nodes.map((node) => node.id)).toEqual(['a']);
  });

  it('formats node subtitles and warning labels for the UI', () => {
    expect(nodeSubtitle({ id: 'a', name: 'A', board_name: '光刻胶', change_percent: 0, main_net_inflow: 0, stocks: [], match_status: 'matched' })).toBe('光刻胶');
		expect(nodeSubtitle({ id: 'b', name: 'B', change_percent: 1, main_net_inflow: 0, stocks: [], stock_source: 'eastmoney:stock-selection', match_status: 'matched' })).toBe('东财行业/概念成分');
    expect(warningLabel('no matched EastMoney board')).toBe('未匹配到行情板块');
    expect(warningLabel('EastMoney board constituents unavailable')).toBe('板块成分股暂不可用');
  });
});
