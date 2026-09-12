/**
 * 自选股实时行情排序：按涨跌幅从高到低。
 *
 * 规则：
 * - 有涨跌幅的排前面，降序；
 * - 停牌 / 行情缺失（undefined、NaN）的排最后，按代码字典序保证渲染稳定；
 * - 不修改入参数组。
 */
export function sortSymbolsByChangePercent(symbols: string[], changeBySymbol: Record<string, number | undefined>): string[] {
	return [...symbols].sort((left, right) => {
		const a = changeBySymbol[left];
		const b = changeBySymbol[right];
		const aValid = typeof a === 'number' && Number.isFinite(a);
		const bValid = typeof b === 'number' && Number.isFinite(b);
		if (!aValid && !bValid) return left.localeCompare(right);
		if (!aValid) return 1;
		if (!bValid) return -1;
		return (b as number) - (a as number);
	});
}
