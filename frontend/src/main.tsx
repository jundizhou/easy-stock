import React from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import { installRuntimeLogging } from './lib/runtime-log';
import { applyTheme, readStoredTheme, resolveTheme, systemPrefersDark } from './lib/theme';
import './styles.css';
import './theme-dark.css';

installRuntimeLogging();

// 首屏就把主题定下来，避免先渲染浅色再跳深色。
applyTheme(resolveTheme(readStoredTheme(), systemPrefersDark()));

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
