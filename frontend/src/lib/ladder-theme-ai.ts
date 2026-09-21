import { useEffect, useRef, useState } from 'react';
import { BackendConfig, LimitUpLadderStock, requestJSON } from './backend';

export type LadderThemeEntry = {
	symbol: string; name: string; status: 'running' | 'success' | 'failed';
	trade_date?: string; last_attempt: string; identified_at?: string; error?: string;
	result?: { themes: string[]; reason: string; basis: 'web'; caveat: string; sources: { title: string; url: string; date?: string; snippet?: string }[]; confidence: 'high' | 'medium' };
};
export type LadderThemeStock = LimitUpLadderStock & { trade_date: string };
export type LadderThemeEntries = Record<string, LadderThemeEntry>;
const path = '/api/v1/short-term/ladder-theme-ai';
const enabledStorageKey = 'easy-stock.ladder-theme-ai.enabled';

function pause(signal: AbortSignal) {
	return new Promise<void>((resolve, reject) => {
		if (signal.aborted) { reject(new Error('aborted')); return; }
		const abort = () => { clearTimeout(timer); reject(new Error('aborted')); };
		const timer = setTimeout(() => { signal.removeEventListener('abort', abort); resolve(); }, 2000);
		signal.addEventListener('abort', abort, {once:true});
	});
}

const cooldownMS = 7 * 24 * 60 * 60 * 1000;
export type LadderThemeProgress = { total: number; completed: number; active: string[]; remaining: number };
const emptyProgress: LadderThemeProgress = { total: 0, completed: 0, active: [], remaining: 0 };

export function ladderThemeNeedsWork(entry: LadderThemeEntry | undefined, force: boolean, now = Date.now()) {
 if (entry?.status === 'running') return true;
 if (force) return true;
 return !entry?.result && (!entry || now - Date.parse(entry.last_attempt) >= cooldownMS);
}

// Process the full selection; only a stalled queue has a waiting limit.
export async function identifyLadderThemes(config: BackendConfig, stocks: LadderThemeStock[], force: boolean, signal: AbortSignal, update: (entries: LadderThemeEntries) => void, progress: (value: LadderThemeProgress) => void = () => {}) {
 const controller = new AbortController();
 const abort = () => controller.abort();
 signal.addEventListener('abort', abort, { once: true });
 if (signal.aborted) controller.abort();
 let timedOut = false;
 const onStall = () => { timedOut = true; controller.abort(); };
 let timer = setTimeout(onStall, 120_000);
 let lastCompleted = 0;
 try {
  await runLadderThemeBatch(config, stocks, force, controller.signal, update, value => {
   if (value.completed > lastCompleted) {
    lastCompleted = value.completed;
    clearTimeout(timer);
    timer = setTimeout(onStall, 120_000);
   }
   progress(value);
  });
 } catch (error) {
  if (timedOut) throw new Error('连续两分钟没有股票完成识别，已暂停后续任务。已启动的请求可能仍在收尾，稍后可继续。');
  throw error;
 } finally {
  clearTimeout(timer);
  signal.removeEventListener('abort', abort);
  controller.abort();
 }
}

async function runLadderThemeBatch(config: BackendConfig, stocks: LadderThemeStock[], force: boolean, signal: AbortSignal, update: (entries: LadderThemeEntries) => void, progress: (value: LadderThemeProgress) => void) {
 const seen = new Set<string>();
 const selected = stocks.filter(stock => {
  if (seen.has(stock.symbol)) return false;
  seen.add(stock.symbol);
  return true;
 }).sort((a, b) => b.streak - a.streak);
 const key = selected.map(s => s.symbol).join(',');
 const initial = await requestJSON<{data: LadderThemeEntries}>(config, `${path}?symbols=${encodeURIComponent(key)}`, {signal});
 let current = initial.data;
 if (signal.aborted) return;
 update(current);
 const eligible = selected.filter(stock => ladderThemeNeedsWork(current[stock.symbol], force));
 const batch = eligible;
 let completed = 0;
 const active = new Map<string, string>();
 const report = () => progress({ total: batch.length, completed, active: [...active.values()], remaining: batch.length - completed - active.size });
 report();
 let index = 0;
 let failures = 0;
 const worker = async () => {
  while (!signal.aborted && index < batch.length && failures < 3) {
   const stock = batch[index++];
   active.set(stock.symbol, stock.name); report();
   let entry = current[stock.symbol];
   if (entry?.status !== 'running') {
    const response = await requestJSON<{data: LadderThemeEntry}>(config, path, {
     method:'POST', signal, headers:{'Content-Type':'application/json'},
     body:JSON.stringify({trade_date:stock.trade_date,theme_source:stock.theme_source || stock.source || '',symbol:stock.symbol,name:stock.name,industry:stock.industry || '',concepts:(stock.raw_concepts || []).slice(0,40),existing_theme:stock.primary_theme || '',evidence:(stock.theme_evidence || []).slice(0,20),force}),
    });
    entry = response.data;
   }
   while (!signal.aborted) {
    current = {...current,[stock.symbol]:entry}; update(current);
    if (entry.status !== 'running') break;
    await pause(signal);
    const response = await requestJSON<{data: LadderThemeEntries}>(config, `${path}?symbols=${encodeURIComponent(stock.symbol)}`, {signal});
    if (!response.data[stock.symbol]) throw new Error('识别任务已丢失，请手动刷新');
    entry = response.data[stock.symbol];
   }
   if (signal.aborted) return;
   if (entry.status === 'failed') failures++;
   active.delete(stock.symbol); completed++; report();
  }
 };
 await Promise.all([worker(),worker()]);
 if (failures >= 3) throw new Error('本轮已有三只股票未能识别，已暂停后续任务，请检查模型和网页检索服务后再继续。');
}

export function useLadderThemeAI(config: BackendConfig | null, stocks: LadderThemeStock[]) {
 const [enabled, setEnabledState] = useState(() => {
  try { return window.localStorage.getItem(enabledStorageKey) === 'true'; }
  catch { return false; }
 });
 const setEnabled = (value: boolean) => {
  setEnabledState(value);
  try { window.localStorage.setItem(enabledStorageKey, String(value)); }
  catch { /* Keep the switch usable when local storage is unavailable. */ }
 };
 const [entries, setEntries] = useState<LadderThemeEntries>({});
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState('');
 const [progress, setProgress] = useState<LadderThemeProgress>(emptyProgress);
 const [revision, setRevision] = useState(0);
 const activeController = useRef<AbortController | null>(null);
 const forceNext = useRef(false);
 const stocksRef = useRef(stocks);
 stocksRef.current = stocks;
 const key = [...new Set(stocks.map(s => `${s.symbol}:${s.trade_date}`))].sort().join(',');
 const backendUrl = config?.backendUrl;
 const token = config?.token;
 useEffect(() => {
  if (!enabled || backendUrl == null || token == null || !key) { setBusy(false); return; }
  const controller = new AbortController();
  activeController.current = controller;
  const force = forceNext.current; forceNext.current = false;
  setBusy(true); setError(''); setProgress(emptyProgress);
  void identifyLadderThemes({backendUrl, token}, stocksRef.current, force, controller.signal, data => {
   if (!controller.signal.aborted) setEntries(data);
  }, value => { if (!controller.signal.aborted) setProgress(value); }).then(() => {
   if (!controller.signal.aborted) setBusy(false);
  }).catch(e => {
   if (!controller.signal.aborted) {
    setError(e instanceof Error ? e.message : '题材识别失败'); setBusy(false); controller.abort();
   }
  });
  return () => controller.abort();
 }, [backendUrl, token, enabled, key, revision]);
 const refresh = () => { forceNext.current = true; setRevision(v => v+1); };
 const resume = () => { forceNext.current = false; setRevision(v => v+1); };
 const pauseBatch = () => {
  activeController.current?.abort(); setBusy(false);
  setError('已暂停后续识别，已有结果保留。已启动的请求会在后台收尾，继续时读取结果。');
 };
 return {enabled, setEnabled, entries: enabled ? entries : {}, busy, error, progress, refresh, resume, pauseBatch};
}
