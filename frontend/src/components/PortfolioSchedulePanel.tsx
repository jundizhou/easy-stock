import { CalendarClock, ChevronDown, LoaderCircle, Save } from 'lucide-react';
import { useEffect, useState } from 'react';
import { requestJSON, type BackendConfig, type NotificationSettings } from '../lib/backend';
import { notificationSettingsChangedEvent } from '../lib/notifications';
import type { PortfolioPlan } from '../lib/portfolio-draft';
import { defaultSchedule, scheduleRequest, scheduleSnapshotMatches, scheduleTimeLabel, type PortfolioSchedule, type PortfolioScheduleConfig, type ScheduleChannel } from '../lib/portfolio-schedule';
import './portfolio-schedule.css';

type Props = { config: BackendConfig | null; plan: PortfolioPlan; disabled: boolean; onOpenSettings: () => void; onBusyChange: (busy: boolean) => void };

export function PortfolioSchedulePanel({ config, plan, disabled, onOpenSettings, onBusyChange }: Props) {
 const [form, setForm] = useState(() => defaultSchedule(plan));
 const [saved, setSaved] = useState<PortfolioSchedule | null>(null);
 const [notifications, setNotifications] = useState<NotificationSettings | null>(null);
 const [loading, setLoading] = useState(true);
 const [loaded, setLoaded] = useState(false);
 const [saving, setSaving] = useState(false);
 const [error, setError] = useState('');
 const [message, setMessage] = useState('');
 const [reload, setReload] = useState(0);
 const path = `/api/v1/portfolio-schedules/${encodeURIComponent(plan.id)}`;
 useEffect(() => {
  if (!config) { setLoading(false); return; }
  const controller = new AbortController();
  setLoading(true); setLoaded(false); setError('');
  Promise.all([
   requestJSON<{ data: PortfolioSchedule | null }>(config, path, { signal: controller.signal }),
   requestJSON<{ data: NotificationSettings }>(config, '/api/v1/settings/notifications', { signal: controller.signal }),
  ]).then(([schedule, settings]) => {
   if (controller.signal.aborted) return;
   setSaved(schedule.data);
   if (schedule.data) setForm(schedule.data);
   setNotifications(settings.data); setLoaded(true);
  }).catch((cause) => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : '读取定时巡检失败'); })
   .finally(() => { if (!controller.signal.aborted) setLoading(false); });
  return () => controller.abort();
 }, [config, path, reload]);
 useEffect(() => {
  if (!config || !loaded) return;
  const controller = new AbortController();
  const timer = window.setInterval(() => {
   requestJSON<{ data: PortfolioSchedule | null }>(config, path, { signal: controller.signal })
    .then(({ data }) => { if (!controller.signal.aborted) setSaved(data); }).catch(() => {});
  }, 30000);
  return () => { controller.abort(); window.clearInterval(timer); };
 }, [config, loaded, path]);
 useEffect(() => {
  if (!config) return;
  const controller = new AbortController();
  const refreshNotifications = () => {
   requestJSON<{ data: NotificationSettings }>(config, '/api/v1/settings/notifications', { signal: controller.signal })
    .then(({ data }) => { if (!controller.signal.aborted) setNotifications(data); })
    .catch(() => { if (!controller.signal.aborted) setError('刷新机器人配置失败，请重新打开巡检页面'); });
  };
  window.addEventListener(notificationSettingsChangedEvent, refreshNotifications);
  return () => { controller.abort(); window.removeEventListener(notificationSettingsChangedEvent, refreshNotifications); };
 }, [config]);
 const update = (patch: Partial<PortfolioScheduleConfig>) => { setForm((value) => ({ ...value, ...patch })); setMessage('修改尚未保存，请保存定时设置'); };
 const save = async () => {
  if (!config || !loaded || saving) return;
  setSaving(true); onBusyChange(true); setError(''); setMessage('');
  try {
   const { data } = await requestJSON<{ data: PortfolioSchedule }>(config, path, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({
    enabled: form.enabled, interval: form.interval, unit: form.unit, start_date: form.start_date, time: form.time,
    channels: form.channels, request: scheduleRequest(plan),
   }) });
   setSaved(data); setForm(data);
   setMessage(data.enabled ? '定时巡检已保存，已同步当前方案的持仓与研究设置' : '定时巡检已停用');
  } catch (cause) { setError(cause instanceof Error ? cause.message : '保存定时巡检失败'); }
  finally { setSaving(false); onBusyChange(false); }
 };
 const snapshotChanged = saved?.enabled && !scheduleSnapshotMatches(saved.request, scheduleRequest(plan));
 const channelReady = (channel: ScheduleChannel) => Boolean(notifications?.[channel]?.enabled && notifications[channel].webhook.configured);
 const unavailable = form.enabled && form.channels.some((channel) => !channelReady(channel));
 return <details className="portfolio-schedule stock-ai-panel">
  <summary><CalendarClock size={18} /><strong>定时巡检</strong><span>{saved?.enabled ? `下次 ${scheduleTimeLabel(saved.next_run_at)} · 北京时间` : '未启用'}{snapshotChanged ? ' · 持仓配置待同步' : ''}</span><ChevronDown size={15} className="portfolio-schedule-chevron" /></summary>
  {loading && <p role="status"><LoaderCircle size={15} className="spin" />正在读取定时设置</p>}
  <form onSubmit={(event) => { event.preventDefault(); void save(); }}>
   <fieldset disabled={!loaded || disabled || saving}>
    <label className="portfolio-schedule-check"><input type="checkbox" checked={form.enabled} onChange={(event) => update({ enabled: event.target.checked })} />启用「{plan.name}」定时巡检</label>
    <div className="portfolio-schedule-presets" role="group" aria-label="快捷巡检周期">
     {([{ label: '每 3 天', interval: 3, unit: 'days' }, { label: '每周', interval: 1, unit: 'weeks' }, { label: '每月', interval: 1, unit: 'months' }] as const).map((item) => <button key={item.unit} type="button" aria-pressed={form.interval === item.interval && form.unit === item.unit} onClick={() => update({ interval: item.interval, unit: item.unit })}>{item.label}</button>)}
    </div>
    <div className="portfolio-schedule-grid">
     <label>巡检间隔<input aria-label="巡检间隔" type="number" min={1} max={365} step={1} required value={form.interval || ''} onChange={(event) => update({ interval: Number(event.target.value) })} /></label>
     <label>间隔单位<select value={form.unit} onChange={(event) => update({ unit: event.target.value as PortfolioScheduleConfig['unit'] })}><option value="days">天</option><option value="weeks">周</option><option value="months">月</option></select></label>
     <label>起始日期<input type="date" required min="2000-01-01" max="9998-12-31" value={form.start_date} onChange={(event) => update({ start_date: event.target.value })} /></label>
     <label>执行时间（北京时间）<input type="time" required value={form.time} onChange={(event) => update({ time: event.target.value })} /></label>
    </div>
    <p>按自然日重复，每周沿用起始日期的星期，每月沿用起始日；当月没有该日期时在月末执行。</p>
    <div className="portfolio-schedule-channels"><strong>巡检结果通知</strong>{(['dingtalk', 'feishu'] as const).map((channel) => <label key={channel} className="portfolio-schedule-check"><input type="checkbox" checked={form.channels.includes(channel)} disabled={!channelReady(channel) && !form.channels.includes(channel)} onChange={(event) => update({ channels: event.target.checked ? [...form.channels, channel] : form.channels.filter((item) => item !== channel) })} />{channel === 'dingtalk' ? '钉钉' : '飞书'}{!channelReady(channel) && <small>（需配置并启用）</small>}</label>)}<button type="button" onClick={onOpenSettings}>配置通知机器人</button></div>
    <p>不勾选则只保存报告；已勾选渠道接收本定时巡检结果，失败提醒沿用系统设置。关闭系统中的渠道开关可停止发送。</p>
    {unavailable && <p role="alert">所选通知渠道尚未配置或已关闭，请前往系统设置启用，或取消勾选。</p>}
    {snapshotChanged && <p className="portfolio-schedule-warning" role="status">当前方案已修改。请保存定时设置以同步持仓、成本、风格和研究设置；保存前仍按上次配置巡检。</p>}
    <footer><button type="submit" disabled={!config || unavailable}>{saving ? <LoaderCircle size={15} className="spin" /> : <Save size={15} />}保存定时设置</button><span role="status">{message}</span></footer>
   </fieldset>
  </form>
  {error && <p className="portfolio-schedule-warning" role="alert">{error}{!loaded && <button type="button" onClick={() => setReload((value) => value+1)}>重新加载</button>}</p>}
  {saved?.last_error && <p className="portfolio-schedule-warning" role="alert">{saved.last_error}</p>}
  {saved?.last_job_id && <p>最近启动：{scheduleTimeLabel(saved.last_run_at)} · 报告见当前方案的巡检记录</p>}
  <p className="portfolio-schedule-note">需保持应用运行且电脑唤醒。错过多次计划时，恢复运行后补跑一次；已有巡检运行时顺延等待。定时任务按保存时的持仓配置执行。</p>
 </details>;
}
