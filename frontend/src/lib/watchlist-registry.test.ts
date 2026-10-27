import { beforeEach, describe, expect, it } from 'vitest';
import {
	isWatched,
	markWatched,
	setWatchSymbols,
	subscribeWatchlist,
	unmarkWatched,
	watchCodeOf,
	watchedName,
} from './watchlist-registry';

beforeEach(() => {
	setWatchSymbols([]);
});

describe('watchCodeOf', () => {
	it('把带后缀、带前缀、纯 6 位三种写法归一成同一个代码', () => {
		expect(watchCodeOf('002354.SZ')).toBe('002354');
		expect(watchCodeOf('sz002354')).toBe('002354');
		expect(watchCodeOf('002354')).toBe('002354');
	});

	it('非股票输入归零，不产生虚假命中', () => {
		expect(watchCodeOf('')).toBe('');
		expect(watchCodeOf('—')).toBe('');
	});

	it('只取末尾 6 位，避免把交易所前缀当成代码', () => {
		expect(watchCodeOf('SH600588')).toBe('600588');
	});
});

describe('关注仓镜像', () => {
	it('整份清单替换后，带后缀与不带后缀都能命中', () => {
		setWatchSymbols([{ symbol: '002354.SZ', name: '天娱数科' }, { symbol: '688277.SH', name: '天智航' }]);
		// 雷达页拿到的带后缀、批量粘贴拿到的纯 6 位，都应判定为已关注。
		expect(isWatched('002354.SZ')).toBe(true);
		expect(isWatched('002354')).toBe(true);
		expect(watchedName('002354')).toBe('天娱数科');
		expect(isWatched('300680.SZ')).toBe(false);
	});

	it('整份替换会清掉旧集合，不会残留已移除的票', () => {
		setWatchSymbols([{ symbol: '002354.SZ' }]);
		setWatchSymbols([{ symbol: '600588.SH' }]);
		expect(isWatched('002354')).toBe(false);
		expect(isWatched('600588')).toBe(true);
	});

	it('单只增删即时生效，便于写入成功后立刻切换菜单首项', () => {
		markWatched('301101.SZ', '明月镜片');
		expect(isWatched('301101')).toBe(true);
		expect(watchedName('301101')).toBe('明月镜片');
		unmarkWatched('301101.SZ');
		expect(isWatched('301101')).toBe(false);
		// 名称随代码一起清掉，避免残留脏名字。
		expect(watchedName('301101')).toBe('');
	});

	it('重复写入不重复触发订阅，避免无意义的菜单重绘', () => {
		markWatched('002354.SZ');
		let hits = 0;
		const off = subscribeWatchlist(() => { hits += 1; });
		markWatched('002354.SZ');
		expect(hits).toBe(0);
		markWatched('600588.SH');
		expect(hits).toBe(1);
		off();
	});

	it('空代码不写入镜像，防止把空行当成有效标的', () => {
		markWatched('');
		expect(isWatched('')).toBe(false);
	});
});
