import { Activity, ArrowUpRight, Bot, CheckCheck, Database, Download, Pause, Play, Search } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { type BackendConfig, type SourceMeta } from '../../lib/backend';
import { etfMoney, etfTime } from '../../lib/national-team';
import { subscriptionChartPoints, subscriptionPrompt, subscriptionWindowNet, subscriptionTrialStatus, subscriptionRangeBounds, subscriptionPresetRange, subscriptionRangeError, subscriptionWindow, type ETFSubscriptionPeriod, type ETFSubscriptionRange, type ETFSubscriptionTrial } from '../../lib/etf-subscription';
import { ModuleState } from './MarketDataViews';
import { ETFSubscriptionChart } from './ETFSubscriptionChart';
import { loadETFSubscriptionSnapshot, readETFSubscriptionSnapshot, type ETFSubscriptionPayload } from '../../lib/etf-subscription-cache';
import './national-team-etf.css';

type Props = { config: BackendConfig | null; refreshKey: number; onAskAI: (prompt: string) => void };
export function ETFSubscriptionView({ config, refreshKey, onAskAI }: Props) {
 const [payload, setPayload] = useState<ETFSubscriptionPayload | null>(() => readETFSubscriptionSnapshot(config));
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState('');
 const [auto, setAuto] = useState(true);
 const autoRef = useRef(auto); autoRef.current = auto;
 const [interval, setIntervalSeconds] = useState(60);
 const [code, setCode] = useState('pool');
 const [cumulative, setCumulative] = useState(true);
 const [period, setPeriod] = useState<ETFSubscriptionPeriod>('1m');
 const [customRange, setCustomRange] = useState<ETFSubscriptionRange | null>(null);
 const [rangeDraft, setRangeDraft] = useState<ETFSubscriptionRange | null>(null);
 const [selectedDate, setSelectedDate] = useState('');
 const [query, setQuery] = useState('');
 useEffect(() => {
  if (!config) { setError('后端尚未连接'); return; }
  let disposed = false; let loading = false;
  setPayload(readETFSubscriptionSnapshot(config));
  async function load() {
   if (disposed || loading) return;
   loading = true; setBusy(true);
   try {
    const result = await loadETFSubscriptionSnapshot(config!);
    if (!disposed) { setPayload(result); setError(''); }
   } catch (reason) {
    if (!disposed) {
     setError(reason instanceof Error ? reason.message : '净申购数据暂不可用');
     setPayload((current) => current ? { ...current, meta: { ...current.meta, stale: true, fallback_reason: '本次刷新失败，显示上次成功的净申购快照' } } : null);
    }
   } finally { loading = false; if (!disposed) setBusy(false); }
  }
  void load();
  const timer = window.setInterval(() => { if (autoRef.current && document.visibilityState !== 'hidden') void load(); }, interval * 1000);
  const onVisible = () => { if (autoRef.current && document.visibilityState === 'visible') void load(); };
  document.addEventListener('visibilitychange', onVisible);
  return () => { disposed = true; window.clearInterval(timer); document.removeEventListener('visibilitychange', onVisible); };
 }, [config, refreshKey, interval]);
 const sourceData = payload?.data;
 const bounds = useMemo(() => sourceData ? subscriptionRangeBounds(sourceData) : null, [sourceData]);
 const range = useMemo(() => bounds ? period === 'custom' && customRange ? customRange : subscriptionPresetRange(bounds, period === 'custom' ? '1m' : period) : null, [bounds, period, customRange]);
 const rangeError = bounds && rangeDraft ? subscriptionRangeError(rangeDraft, bounds) : '';
 const data = useMemo(() => sourceData && range ? subscriptionWindow(sourceData, range) : undefined, [sourceData, range]);
 const changePeriod = (value: ETFSubscriptionPeriod) => {
  if (value === 'custom') { setCustomRange(range); setRangeDraft(range); }
  setPeriod(value);
 };
 const date = data?.days.some((day) => day.date === selectedDate) ? selectedDate : data?.latest_complete_date || data?.latest_date || '';
 const day = data?.days.find((item) => item.date === date);
 const windowNet = data ? subscriptionWindowNet(data) : null;
 const points = useMemo(() => data ? subscriptionChartPoints(data, code, cumulative) : [], [data, code, cumulative]);
 const checks = data?.checks.filter((item) => item.date === date) || [];
 const rows = useMemo(() => (data?.series || []).map((item) => ({ item, point: item.points.find((p) => p.date === date) })).filter(({ item }) => `${item.name}${item.code}${item.index}`.includes(query.trim())).sort((a, b) => Math.abs(b.point?.estimated_net ?? 0) - Math.abs(a.point?.estimated_net ?? 0)), [data, date, query]);
 const exportCSV = () => {
  if (!data) return;
  const records: unknown[][] = [['ETF代码', 'ETF', '份额所属日期', '比较基准日期', '份额（份）', '份额差（份）', '测算收盘价', '价格日期', '估算净申购（元）', '核验', '最新价格', '最新行情时间', '是否缓存']];
  for (const { item, point } of rows) records.push([item.code, item.name, date, point?.previous_date, point?.shares, point?.share_change, point?.close, point?.price_date, point?.estimated_net, point?.verification, item.latest_price, item.quote_time, payload?.meta.stale ? '是' : '否']);
  const quote = (value: unknown) => `"${String(value ?? '').replaceAll('"', '""')}"`;
  const url = URL.createObjectURL(new Blob(['\uFEFF', records.map((row) => row.map(quote).join(',')).join('\r\n')], { type: 'text/csv;charset=utf-8' }));
  const a = document.createElement('a'); a.href = url; a.download = `etf-subscriptions-${date}.csv`; a.click(); URL.revokeObjectURL(url);
 };
 return <div className="market-data-view national-team-view etf-subscription-view">
  <div className="national-team-toolbar"><div className="national-team-controls"><button type="button" aria-pressed={auto} onClick={() => setAuto((value) => !value)}>{auto ? <Pause size={13} /> : <Play size={13} />}{auto ? '自动同步' : '已暂停同步'}</button><select aria-label="净申购同步间隔" value={interval} onChange={(event) => setIntervalSeconds(Number(event.target.value))}><option value={30}>30 秒</option><option value={60}>60 秒</option><option value={120}>120 秒</option></select>{busy && payload && <span>同步中…</span>}</div></div>
  {error && payload && <div className="market-source-notice stale" role="status">刷新失败，显示缓存净申购快照 · {error}</div>}
  <ModuleState state={!payload && busy ? 'loading' : !payload && error ? 'error' : 'ready'} error={error}>
   {data && <>
    {data.today_trial && payload && <TodayTrial trial={data.today_trial} meta={payload.meta} />}
    <div className="etf-subscription-dates"><span><strong>份额最新齐备日 {sourceData?.latest_complete_date || '尚未齐备'}</strong> · 公开快照生成 {etfTime(data.source_generated_at, true)}{sourceData?.latest_date !== sourceData?.latest_complete_date ? ` · ${sourceData?.latest_date} 尚有份额或同日价格待补齐` : ''}</span><label>查看披露日期<select aria-label="查看份额披露日期" value={date} onChange={(event) => setSelectedDate(event.target.value)}>{[...data.days].reverse().map((item) => <option key={item.date} value={item.date}>{item.date}（{item.coverage}/{data.expected}）</option>)}</select></label></div>
    <section className="market-flow-summary market-margin-summary">
     <SubscriptionMetric icon={<Activity size={17} />} label="全池净申购（估算）" value={etfMoney(day?.estimated_net)} amount={day?.estimated_net} detail={`${date || '--'} · 对比 ${day?.previous_date || '--'}`} />
     <SubscriptionMetric icon={<ArrowUpRight size={17} />} label="所选区间累计（估算）" value={etfMoney(windowNet)} amount={windowNet} detail={`${range?.start} 至 ${data.latest_complete_date || '--'}`} />
     <SubscriptionMetric icon={<Database size={17} />} label="同步净申购只数" value={`${day?.subscribed ?? '--'} / ${data.expected}`} detail={`净赎回 ${day?.redeemed ?? '--'} 只 · 金额有效 ${day?.coverage ?? 0}/${data.expected}`} />
     <SubscriptionMetric icon={<CheckCheck size={17} />} label="当日交易所直接核验" value={`${checks.length} / ${data.expected}`} detail="份额已与交易所原始数据对照" />
    </section>
    <section className="national-team-chart national-team-history">
     <header><div><h3>净申购趋势</h3><span>份额所属日期与收盘价格日期对齐 · {cumulative ? '区间累计' : '每日净申购'} · 估算</span></div><div className="national-team-tabs" role="group" aria-label="净申购统计方式"><button type="button" aria-pressed={!cumulative} className={!cumulative ? 'active' : ''} onClick={() => setCumulative(false)}>每日净申购</button><button type="button" aria-pressed={cumulative} className={cumulative ? 'active' : ''} onClick={() => setCumulative(true)}>区间累计</button></div></header>
     <div className="national-team-history-toolbar">
      <div className="etf-subscription-range-controls"><label>时间范围<select aria-label="净申购时间范围" value={period} onChange={(event) => changePeriod(event.target.value as ETFSubscriptionPeriod)}><option value="1m">近 1 个月</option><option value="3m">近 3 个月</option><option value="6m">近 6 个月</option><option value="1y">近 1 年</option><option value="custom">自定义</option></select></label><label>观察范围<select aria-label="净申购 ETF 范围" value={code} onChange={(event) => setCode(event.target.value)}><option value="pool">监测池合计（22 只）</option>{data.series.map((item) => <option key={item.code} value={item.code}>{item.name}（{item.code}）</option>)}</select></label></div>
      <span className="national-team-times">{points.length} 个披露交易日 · 单位：亿元</span>
      {period === 'custom' && rangeDraft && bounds && <form className="etf-subscription-custom-range" onSubmit={(event) => { event.preventDefault(); if (!rangeError) setCustomRange({ ...rangeDraft }); }}><label>开始日期<input type="date" aria-label="净申购开始日期" min={bounds.start} max={bounds.end} value={rangeDraft.start} onChange={(event) => setRangeDraft({ ...rangeDraft, start: event.target.value })} required /></label><span>至</span><label>结束日期<input type="date" aria-label="净申购结束日期" min={bounds.start} max={bounds.end} value={rangeDraft.end} onChange={(event) => setRangeDraft({ ...rangeDraft, end: event.target.value })} required /></label><button type="submit" disabled={!!rangeError}>应用</button><span className="national-team-times">最长 1 年</span>{rangeError && <span role="alert">{rangeError}</span>}</form>}
      <span className="etf-subscription-selected-range">统计区间：{range?.start} 至 {range?.end}</span>
     </div>
     <ETFSubscriptionChart key={`${code}-${cumulative}-${range?.start}-${range?.end}`} points={points} />
     <footer>{cumulative ? '累计从本窗口首个披露交易日起算，不是持仓金额。' : '每个点为该交易日估算净申购。'} 同一日期必须有全部 22 只 ETF 的份额基准及对应日收盘价才计算池合计；缺失保留断点，累计在缺失后停止。池曲线截至最新齐备日，未齐备的新披露日期在明细中单独查看；最新价不用于重算历史申赎。</footer>
    </section>
    <div className="national-team-table-toolbar"><label><Search size={14} /><input aria-label="搜索净申购 ETF" placeholder="搜索 ETF / 代码 / 指数" value={query} onChange={(event) => setQuery(event.target.value)} /></label><span>{date} · 正数为净申购，负数为净赎回</span><button type="button" onClick={exportCSV}><Download size={14} />导出明细</button><button type="button" onClick={() => data && payload && onAskAI(subscriptionPrompt(data, day, payload.meta))}><Bot size={14} />交给 AI 解读</button></div>
    <div className="market-data-table etf-subscription-table" data-tt-grid="etf-subscriptions"><header><span>ETF / 跟踪指数</span><span>份额（万份）</span><span>份额变化（万份）</span><span>测算收盘价</span><span>估算净申购</span><span>份额核验</span><span>最新价 / 行情时间</span></header>{rows.map(({ item, point }) => <article key={item.code}>
     <span><strong title={item.name}>{item.name}</strong><small>{item.code} · {item.index} · {item.exchange === 'SSE' ? '沪市' : '深市'}</small></span>
     <span>{wan(point?.shares)}</span><span className={tone(point?.share_change)}>{wan(point?.share_change, true)}</span>
     <span>{point?.close?.toFixed(3) ?? '--'}<small>{point?.price_date || '同日收盘价缺失'}</small></span>
     <strong className={tone(point?.estimated_net)}>{etfMoney(point?.estimated_net)}</strong>
     <span><small>{point?.verification || '份额待披露'}</small></span>
     <span>{item.latest_price?.toFixed(3) ?? '--'} <em className={tone(item.change_percent)}>{item.change_percent == null ? '' : `${item.change_percent > 0 ? '+' : ''}${item.change_percent.toFixed(2)}%`}</em><small>{etfTime(item.quote_time, true)}</small></span>
    </article>)}</div>

   </>}
  </ModuleState>
 </div>;
}
function tone(value: number | null | undefined) { return value == null || value === 0 ? 'flat' : value > 0 ? 'up' : 'down'; }
function wan(value: number | null | undefined, signed = false) { return value == null ? '--' : `${signed && value > 0 ? '+' : ''}${(value / 10_000).toLocaleString('zh-CN', { maximumFractionDigits: 2 })}`; }
function SubscriptionMetric({ icon, label, value, amount, detail }: { icon: React.ReactNode; label: string; value: string; amount?: number | null; detail: string }) { return <article><i>{icon}</i><span><small>{label}</small><strong className={tone(amount)}>{value}</strong><em>{detail}</em></span></article>; }

function TodayTrial({ trial, meta }: { trial: ETFSubscriptionTrial; meta: SourceMeta }) {
 const [clock, setClock] = useState(Date.now());
 useEffect(() => { const timer = window.setInterval(() => setClock(Date.now()), 15_000); return () => window.clearInterval(timer); }, []);
 const status = subscriptionTrialStatus(trial, meta, clock);
 const usable = status === trial.status;
 return <section className="etf-today-trial">
  <header><h3>当日净申购试算</h3><span className={usable && trial.coverage === trial.expected ? 'ready' : ''}>{status}</span></header>
  <div className="market-flow-summary market-margin-summary">
   <SubscriptionMetric icon={<Activity size={17} />} label="今日全池试算" value={etfMoney(usable ? trial.estimated_net : null)} amount={usable ? trial.estimated_net : null} detail="同日份额、基准和有效价格齐备后计算" />
   <SubscriptionMetric icon={<Database size={17} />} label="今日份额已披露" value={`${trial.share_coverage} / ${trial.expected}`} detail={`基准份额齐备 ${trial.baseline_coverage}/${trial.expected}`} />
   <SubscriptionMetric icon={<ArrowUpRight size={17} />} label="本次取得有效今日价格" value={`${trial.price_coverage} / ${trial.expected}`} detail="价格日期不等于份额所属日期" />
   <SubscriptionMetric icon={<CheckCheck size={17} />} label="本次满足试算条件" value={`${trial.coverage} / ${trial.expected}`} detail="不将缺失份额当作零" />
  </div>
  <details><summary>逐只查看当日试算条件</summary><div className="etf-today-trial-items">{trial.items.map((item) => <article key={item.code}><span><strong>{item.name}</strong><small>{item.code} · 份额日期 {item.share_date || '待披露'}</small></span><span>最新价 {item.price?.toFixed(3) ?? '--'}<small>{etfTime(item.quote_time, true)}</small></span><strong className={tone(item.estimated_net)}>{etfMoney(usable ? item.estimated_net : null)}</strong><span>{usable ? item.status : status}</span></article>)}</div></details>
 </section>;
}
