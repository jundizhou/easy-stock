// 关注仓符号注册表：右键菜单要在「任何页面」判断一只票是否已在关注仓，
// 但关注仓清单只有监控中心会请求。早期实现改成看当前行所在 DOM 有没有
// data-stock-context="watchlist" —— 那只在关注仓列表自己里成立，同一只票
// 换到雷达结论/异动/行情总览去右键，就退化成「加入关注」，出现两套互相
// 矛盾的菜单。
//
// 这里做一个进程级只读镜像：谁拉到关注仓清单谁写入，菜单只读。
// 本模块不发起任何请求、不做任何写入，因此不会影响用户真实的关注仓数据。

export type WatchRef = { symbol: string; name?: string };

/** A 股代码规范化：'002354.SZ' / 'sz002354' / '002354' 归一成 '002354'。
 *  后端存的是带后缀形式，而页面各处写法不一（雷达带后缀、批量粘贴只输 6 位），
 *  按 6 位数字对齐是唯一能跨页面命中的口径。 */
export function watchCodeOf(symbol: string): string {
	return (symbol || '').replace(/\D/g, '').slice(-6);
}

const codes = new Set<string>();
const names = new Map<string, string>();
const listeners = new Set<() => void>();

function emit() {
	for (const notify of listeners) notify();
}

/** 用一份完整清单替换镜像（关注仓列表每次加载后调用）。 */
export function setWatchSymbols(list: WatchRef[]) {
	codes.clear();
	names.clear();
	for (const item of list) {
		const code = watchCodeOf(item.symbol);
		if (!code) continue;
		codes.add(code);
		if (item.name) names.set(code, item.name);
	}
	emit();
}

/** 单只加入镜像：本地已完成的写入不必再拉一次清单。 */
export function markWatched(symbol: string, name?: string) {
	const code = watchCodeOf(symbol);
	if (!code || codes.has(code)) return;
	codes.add(code);
	if (name) names.set(code, name);
	emit();
}

/** 单只移出镜像。 */
export function unmarkWatched(symbol: string) {
	const code = watchCodeOf(symbol);
	if (!code || !codes.delete(code)) return;
	names.delete(code);
	emit();
}

export function isWatched(symbol: string): boolean {
	const code = watchCodeOf(symbol);
	return code !== '' && codes.has(code);
}

export function watchedName(symbol: string): string {
	return names.get(watchCodeOf(symbol)) || '';
}

/** 镜像变化订阅；菜单每次打开都会重读，订阅用于需要主动重绘的调用方。 */
export function subscribeWatchlist(listener: () => void): () => void {
	listeners.add(listener);
	return () => { listeners.delete(listener); };
}
