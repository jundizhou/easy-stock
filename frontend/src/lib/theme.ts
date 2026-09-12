/**
 * 外观主题（浅色 / 深色 / 跟随系统）。
 *
 * 设计取舍：主样式表 styles.css 里有 2000+ 处硬编码色值，逐条改写既不可靠也
 * 无法维护。因此主题实现为「深色覆盖层」：
 *   - 这里只负责持久化偏好、把主题写到 <html data-theme="dark">；
 *   - 真正的深色规则集中在 theme-dark.css（自动生成），整表映射后包进
 *     [data-theme='dark'] 作用域，因此每条浅色规则都有对应的深色覆盖。
 * 这样新增页面只要用既有类名，就自动获得深色支持；重新生成见
 * K:/easy-stock-env/docs/theme/gen_dark.js。
 */

import { useCallback, useEffect, useState } from 'react';

export type ThemePreference = 'light' | 'dark' | 'system';
export type ResolvedTheme = 'light' | 'dark';

export const THEME_STORAGE_KEY = 'easy-stock.theme';

const THEME_CYCLE: ThemePreference[] = ['light', 'dark', 'system'];

export const THEME_LABELS: Record<ThemePreference, string> = {
	light: '浅色',
	dark: '深色',
	system: '跟随系统',
};

export function isThemePreference(value: unknown): value is ThemePreference {
	return value === 'light' || value === 'dark' || value === 'system';
}

export function readStoredTheme(): ThemePreference {
	if (typeof window === 'undefined') return 'system';
	try {
		const raw = window.localStorage.getItem(THEME_STORAGE_KEY);
		return isThemePreference(raw) ? raw : 'system';
	} catch {
		return 'system';
	}
}

export function writeStoredTheme(preference: ThemePreference): void {
	if (typeof window === 'undefined') return;
	try {
		window.localStorage.setItem(THEME_STORAGE_KEY, preference);
	} catch { /* 隐私模式等场景静默降级，仅本次会话生效 */ }
}

/**
 * 把偏好解析成实际生效的主题。'system' 走 prefers-color-scheme，
 * 且在没有 matchMedia 的环境（老 WebView）回退浅色。
 */
export function resolveTheme(preference: ThemePreference, prefersDark?: boolean): ResolvedTheme {
	if (preference === 'dark') return 'dark';
	if (preference === 'light') return 'light';
	return prefersDark ? 'dark' : 'light';
}

export function systemPrefersDark(): boolean {
	if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false;
	try {
		return window.matchMedia('(prefers-color-scheme: dark)').matches;
	} catch {
		return false;
	}
}

/**
 * 把主题写到 <html>：data-theme 供 CSS 选择，color-scheme 让原生控件
 * （滚动条、下拉、日期选择器）也跟着变深，避免出现刺眼的白块。
 */
export function applyTheme(resolved: ResolvedTheme): void {
	if (typeof document === 'undefined') return;
	const root = document.documentElement;
	root.dataset.theme = resolved;
	root.style.colorScheme = resolved;
}

export function nextTheme(preference: ThemePreference): ThemePreference {
	const index = THEME_CYCLE.indexOf(preference);
	return THEME_CYCLE[(index + 1) % THEME_CYCLE.length];
}

/** 供 UI 显示用的图标语义（避免在组件里散落判断逻辑）。 */
export function themeIconName(preference: ThemePreference): 'sun' | 'moon' | 'monitor' {
	if (preference === 'light') return 'sun';
	if (preference === 'dark') return 'moon';
	return 'monitor';
}

/**
 * 用一段内联脚本在首屏渲染前把主题打到 <html> 上，避免刷新时先闪一下浅色。
 * 由 index.html 的 <head> 直接执行（此时 React 还没挂载）。
 */
export const THEME_BOOTSTRAP_SCRIPT = `(function(){try{var p=localStorage.getItem(${JSON.stringify(
	THEME_STORAGE_KEY,
)});if(p!=='light'&&p!=='dark'){p='system';}var d=p==='dark'||(p==='system'&&window.matchMedia&&window.matchMedia('(prefers-color-scheme: dark)').matches);var e=document.documentElement;e.dataset.theme=d?'dark':'light';e.style.colorScheme=d?'dark':'light';}catch(_){}})();`;

/**
 * 主题状态。偏好变化时立刻落盘并应用到 <html>；选「跟随系统」时
 * 监听系统配色变化实时跟随，无需刷新。
 */
export function useTheme(): {
	preference: ThemePreference;
	resolved: ResolvedTheme;
	setPreference: (next: ThemePreference) => void;
	toggle: () => void;
} {
	const [preference, setPreferenceState] = useState<ThemePreference>(() => readStoredTheme());
	const [prefersDark, setPrefersDark] = useState<boolean>(() => systemPrefersDark());

	// 跟随系统配色变化
	useEffect(() => {
		if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return undefined;
		const query = window.matchMedia('(prefers-color-scheme: dark)');
		const onChange = (event: MediaQueryListEvent) => setPrefersDark(event.matches);
		query.addEventListener('change', onChange);
		return () => query.removeEventListener('change', onChange);
	}, []);

	const resolved = resolveTheme(preference, prefersDark);

	useEffect(() => {
		applyTheme(resolved);
	}, [resolved]);

	const setPreference = useCallback((next: ThemePreference) => {
		setPreferenceState(next);
		writeStoredTheme(next);
	}, []);

	const toggle = useCallback(() => {
		setPreferenceState((current) => {
			const next = nextTheme(current);
			writeStoredTheme(next);
			return next;
		});
	}, []);

	return { preference, resolved, setPreference, toggle };
}
