import { useEffect, useState } from 'react';

export type Theme = 'light' | 'dark';
export const themeStorageKey = 'easy-stock.theme.v1';

export function normalizeTheme(value: string | null | undefined): Theme {
	return value === 'dark' ? 'dark' : 'light';
}

export function readTheme(): Theme {
	try {
		return normalizeTheme(window.localStorage.getItem(themeStorageKey));
	} catch {
		return normalizeTheme(document.documentElement.dataset.theme);
	}
}

export function applyTheme(theme: Theme, target: Document = document) {
	target.documentElement.dataset.theme = theme;
	target.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme === 'dark' ? '#151719' : '#ffffff');
}

export function saveTheme(theme: Theme) {
	applyTheme(theme);
	try {
		window.localStorage.setItem(themeStorageKey, theme);
	} catch {
		// Theme switching remains available when storage is disabled or full.
	}
}

export function useTheme() {
	const [theme, setTheme] = useState<Theme>(readTheme);
	useEffect(() => {
		applyTheme(theme);
	}, [theme]);
	useEffect(() => {
		const syncTheme = (event: StorageEvent) => {
			if (event.key === themeStorageKey || event.key === null) setTheme(readTheme());
		};
		window.addEventListener('storage', syncTheme);
		return () => window.removeEventListener('storage', syncTheme);
	}, []);
	const toggleTheme = () => {
		const next = theme === 'light' ? 'dark' : 'light';
		saveTheme(next);
		setTheme(next);
	};
	return { theme, toggleTheme };
}
