import React from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import { installRuntimeLogging } from './lib/runtime-log';
import '@fontsource/inter/latin-400.css';
import '@fontsource/inter/latin-600.css';
import '@fontsource/inter/latin-700.css';
import './typography.css';
import './styles.css';
import './theme.css';

if (/Electron\//.test(navigator.userAgent) && /Windows/.test(navigator.userAgent)) {
	document.documentElement.classList.add('desktop-titlebar-overlay');
}

const scrollHideTimers = new WeakMap<Element, number>();
document.addEventListener('scroll', (event) => {
	const target = event.target instanceof Element ? event.target : document.documentElement;
	target.classList.add('is-scrolling');
	window.clearTimeout(scrollHideTimers.get(target));
	scrollHideTimers.set(target, window.setTimeout(() => target.classList.remove('is-scrolling'), 900));
}, true);

installRuntimeLogging();

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
