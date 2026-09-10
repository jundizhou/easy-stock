import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { applyTheme, normalizeTheme, readTheme, saveTheme, themeStorageKey } from './theme';

function environment(value: string | null = null, blocked = false) {
	const dataset: { theme?: string } = {};
	const meta = { setAttribute: vi.fn() };
	const document = { documentElement: { dataset }, querySelector: vi.fn(() => meta) };
	const storage = {
		getItem: vi.fn(() => { if (blocked) throw new Error('blocked'); return value; }),
		setItem: vi.fn((key: string, next: string) => { if (blocked) throw new Error('full'); value = next; }),
	};
	vi.stubGlobal('document', document);
	vi.stubGlobal('window', { localStorage: storage });
	return { document, dataset, storage, meta };
}

afterEach(() => vi.unstubAllGlobals());

describe('appearance preference', () => {
	it.each([null, undefined, '', 'system', 'invalid', 'light'])('keeps the existing light default for %s', value => {
		expect(normalizeTheme(value)).toBe('light');
	});
	it('loads the saved dark theme and persists both directions', () => {
		const env = environment('dark');
		expect(readTheme()).toBe('dark');
		expect(env.storage.getItem).toHaveBeenCalledWith(themeStorageKey);
		saveTheme('light');
		expect(readTheme()).toBe('light');
		expect(env.dataset.theme).toBe('light');
		saveTheme('dark');
		expect(readTheme()).toBe('dark');
		expect(env.meta.setAttribute).toHaveBeenLastCalledWith('content', '#151719');
	});
	it('switches in memory even when storage is inaccessible', () => {
		const env = environment(null, true);
		expect(readTheme()).toBe('light');
		expect(() => saveTheme('dark')).not.toThrow();
		expect(env.dataset.theme).toBe('dark');
		expect(readTheme()).toBe('dark');
	});
	it('forces only the export clone to light without changing the live preference', () => {
		const env = environment('dark');
		saveTheme('dark');
		const clone = { documentElement: { dataset: { theme: 'dark' } }, querySelector: () => null };
		applyTheme('light', clone as unknown as Document);
		expect(clone.documentElement.dataset.theme).toBe('light');
		expect(env.dataset.theme).toBe('dark');
		expect(readTheme()).toBe('dark');
	});
	it.each(['light', 'dark', 'invalid', null])('bootstraps %s before React loads', value => {
		const env = environment(value);
		const html = readFileSync(new URL('../../index.html', import.meta.url), 'utf8');
		const script = html.match(/<script>([\s\S]*?)<\/script>/)?.[1];
		expect(script).toBeTruthy();
		runInNewContext(script!, { document: env.document, localStorage: env.storage });
		expect(env.dataset.theme).toBe(normalizeTheme(value));
	});
	it('bootstraps safely with disabled storage', () => {
		const env = environment(null, true);
		const html = readFileSync(new URL('../../index.html', import.meta.url), 'utf8');
		runInNewContext(html.match(/<script>([\s\S]*?)<\/script>/)![1], { document: env.document, localStorage: env.storage });
		expect(env.dataset.theme).toBe('light');
	});
	it('defines every migrated color token and keeps explicit light fallbacks', () => {
		const dark = readFileSync(new URL('../theme.css', import.meta.url), 'utf8');
		const definitions = new Set([...dark.matchAll(/(--theme-[\w-]+):/g)].map(match => match[1]));
		for (const path of ['../styles.css', '../components/stock-research.css']) {
			const css = readFileSync(new URL(path, import.meta.url), 'utf8');
			for (const match of css.matchAll(/var\((--theme-[\w-]+)([^)]*)\)/g)) {
				expect(definitions.has(match[1]), match[1]).toBe(true);
				expect(match[2]).toMatch(/^,\s*(#|rgb|white|black)/);
			}
		}
	});
});
