import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
	THEME_STORAGE_KEY,
	applyTheme,
	isThemePreference,
	nextTheme,
	readStoredTheme,
	resolveTheme,
	systemPrefersDark,
	themeIconName,
	writeStoredTheme,
} from './theme';

/**
 * 项目测试环境不含 jsdom（见 package.json devDependencies），
 * 这里手写最小 window/document 桩，用完即还原，避免污染其他用例。
 */
const globals = globalThis as { window?: unknown; document?: unknown };

function installBrowserStub(options: { storageThrows?: boolean; prefersDark?: boolean } = {}) {
	const store = new Map<string, string>();
	const localStorageStub = {
		getItem: (key: string) => {
			if (options.storageThrows) throw new Error('SecurityError');
			return store.has(key) ? store.get(key)! : null;
		},
		setItem: (key: string, value: string) => {
			if (options.storageThrows) throw new Error('SecurityError');
			store.set(key, value);
		},
		removeItem: (key: string) => store.delete(key),
		clear: () => store.clear(),
	};
	const matchMediaStub = vi.fn((query: string) => ({
		matches: query.includes('dark') ? Boolean(options.prefersDark) : false,
		media: query,
		addEventListener: vi.fn(),
		removeEventListener: vi.fn(),
	}));
	const documentElementStub: Record<string, unknown> = { dataset: {}, style: {} };
	globals.window = { localStorage: localStorageStub, matchMedia: matchMediaStub };
	globals.document = { documentElement: documentElementStub };
	return { store, documentElementStub, matchMediaStub };
}

function restoreBrowserStub() {
	delete globals.window;
	delete globals.document;
}

describe('isThemePreference', () => {
	it('只认三种合法偏好', () => {
		expect(isThemePreference('light')).toBe(true);
		expect(isThemePreference('dark')).toBe(true);
		expect(isThemePreference('system')).toBe(true);
	});

	it('拒绝脏数据（防止手工改 localStorage 后界面错乱）', () => {
		expect(isThemePreference('Dark')).toBe(false);
		expect(isThemePreference('')).toBe(false);
		expect(isThemePreference(null)).toBe(false);
		expect(isThemePreference(undefined)).toBe(false);
		expect(isThemePreference(1)).toBe(false);
		expect(isThemePreference({ theme: 'dark' })).toBe(false);
	});
});

describe('readStoredTheme / writeStoredTheme', () => {
	afterEach(restoreBrowserStub);

	it('未设置时回退 system', () => {
		installBrowserStub();
		expect(readStoredTheme()).toBe('system');
	});

	it('写入后能读回', () => {
		const { store } = installBrowserStub();
		writeStoredTheme('dark');
		expect(store.get(THEME_STORAGE_KEY)).toBe('dark');
		expect(readStoredTheme()).toBe('dark');
	});

	it('存储里是脏值时静默回退 system，不抛错', () => {
		const { store } = installBrowserStub();
		store.set(THEME_STORAGE_KEY, 'neon');
		expect(readStoredTheme()).toBe('system');
	});

	it('localStorage 抛错（隐私模式）时不崩溃', () => {
		installBrowserStub({ storageThrows: true });
		expect(readStoredTheme()).toBe('system');
		expect(() => writeStoredTheme('dark')).not.toThrow();
	});

	it('无 window 环境（SSR / 测试）时安全回退', () => {
		expect(readStoredTheme()).toBe('system');
		expect(() => writeStoredTheme('dark')).not.toThrow();
	});
});

describe('resolveTheme', () => {
	it('显式偏好优先于系统', () => {
		expect(resolveTheme('dark', false)).toBe('dark');
		expect(resolveTheme('light', true)).toBe('light');
	});

	it('system 跟随 prefers-color-scheme', () => {
		expect(resolveTheme('system', true)).toBe('dark');
		expect(resolveTheme('system', false)).toBe('light');
	});

	it('system 拿不到系统信号时回退浅色', () => {
		expect(resolveTheme('system', undefined)).toBe('light');
	});
});

describe('systemPrefersDark', () => {
	afterEach(restoreBrowserStub);

	it('读取 matchMedia 结果', () => {
		installBrowserStub({ prefersDark: true });
		expect(systemPrefersDark()).toBe(true);
	});

	it('无 matchMedia 时回退 false', () => {
		globals.window = { localStorage: { getItem: () => null, setItem: () => {} } };
		expect(systemPrefersDark()).toBe(false);
	});

	it('无 window 时回退 false', () => {
		expect(systemPrefersDark()).toBe(false);
	});
});

describe('applyTheme', () => {
	afterEach(restoreBrowserStub);

	it('把主题写到 <html data-theme> 与 color-scheme', () => {
		const { documentElementStub } = installBrowserStub();
		applyTheme('dark');
		expect((documentElementStub.dataset as Record<string, string>).theme).toBe('dark');
		expect((documentElementStub.style as Record<string, string>).colorScheme).toBe('dark');
	});

	it('切回浅色时同步还原', () => {
		const { documentElementStub } = installBrowserStub();
		applyTheme('dark');
		applyTheme('light');
		expect((documentElementStub.dataset as Record<string, string>).theme).toBe('light');
		expect((documentElementStub.style as Record<string, string>).colorScheme).toBe('light');
	});

	it('无 document 时不抛错', () => {
		expect(() => applyTheme('dark')).not.toThrow();
	});
});

describe('nextTheme', () => {
	it('按浅色 → 深色 → 跟随系统循环', () => {
		expect(nextTheme('light')).toBe('dark');
		expect(nextTheme('dark')).toBe('system');
		expect(nextTheme('system')).toBe('light');
	});

	it('三轮后回到原点', () => {
		expect(nextTheme(nextTheme(nextTheme('dark')))).toBe('dark');
	});
});

describe('themeIconName', () => {
	it('映射到 lucide 图标语义', () => {
		expect(themeIconName('light')).toBe('sun');
		expect(themeIconName('dark')).toBe('moon');
		expect(themeIconName('system')).toBe('monitor');
	});
});
