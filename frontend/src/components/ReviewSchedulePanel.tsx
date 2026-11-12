import { CalendarClock, ChevronDown, LoaderCircle, Save } from 'lucide-react';
import { useEffect, useState, type ReactNode } from 'react';
import { requestJSON, type BackendConfig, type NotificationSettings } from '../lib/backend';
import { notificationSettingsChangedEvent } from '../lib/notifications';
import { scheduleTimeLabel } from '../lib/portfolio-schedule';

type Channel = 'dingtalk' | 'feishu';
type Schedule = { enabled: boolean; time: string; channels: Channel[]; next_run_at?: string; target_trade_date?: string; last_target_date?: string; last_run_at?: string; last_window_start?: string; last_window_end?: string; last_error?: string };
const path = '/api/v1/reviews/daily-summary/schedule';
export function ReviewSchedulePanel({ config, onOpenSettings, onView, subscriptionCount = 0, children }: { config: BackendConfig | null; onOpenSettings: () => void; onView: (start: string, end: string) => void; subscriptionCount?: number; children?: ReactNode }) {
 const [form, setForm] = useState<Schedule>({ enabled: false, time: '22:00', channels: [] });
 const [saved, setSaved] = useState<Schedule | null>(null);
 const [robots, setRobots] = useState<NotificationSettings | null>(null);
 const [loaded, setLoaded] = useState(false);
 const [saving, setSaving] = useState(false);
 const [error, setError] = useState('');
 const [message, setMessage] = useState('');
 const [reload, setReload] = useState(0);
 useEffect(() => {
  if (!config) return;
  const controller = new AbortController();
  setLoaded(false); setError('');
  Promise.all([
   requestJSON<{ data: Schedule }>(config, path, { signal: controller.signal }),
   requestJSON<{ data: NotificationSettings }>(config, '/api/v1/settings/notifications', { signal: controller.signal }),
  ]).then(([schedule, settings]) => { if (!controller.signal.aborted) { setForm(schedule.data); setSaved(schedule.data); setRobots(settings.data); setLoaded(true); } })
   .catch((cause) => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : '读取定时复盘失败'); });
  const refreshRobots = () => { requestJSON<{ data: NotificationSettings }>(config, '/api/v1/settings/notifications', { signal: controller.signal }).then(({ data }) => { if (!controller.signal.aborted) setRobots(data); }).catch(() => {}); };
  window.addEventListener(notificationSettingsChangedEvent, refreshRobots);
  const timer = window.setInterval(() => { requestJSON<{ data: Schedule }>(config, path, { signal: controller.signal }).then(({ data }) => { if (!controller.signal.aborted) setSaved(data); }).catch(() => {}); }, 30000);
  return () => { controller.abort(); window.clearInterval(timer); window.removeEventListener(notificationSettingsChangedEvent, refreshRobots); };
 }, [config, reload]);
 const ready = (ch: Channel) => Boolean(robots?.[ch].enabled && robots[ch].webhook.configured);
 const unavailable = form.enabled && form.channels.some((ch) => !ready(ch));
 const update = (patch: Partial<Schedule>) => { setForm((value) => ({ ...value, ...patch })); setMessage('修改尚未保存'); };
 const save = async () => {
  if (!config || !loaded || saving) return;
  setSaving(true); setError(''); setMessage('');
  try {
   const { data } = await requestJSON<{ data: Schedule }>(config, path, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled: form.enabled, time: form.time, channels: form.channels }) });
   setSaved(data); setForm(data); setMessage(data.enabled ? '定时复盘已保存' : '定时复盘已停用');
  } catch (cause) { setError(cause instanceof Error ? cause.message : '保存定时复盘失败'); }
  finally { setSaving(false); }
 };
 return <details className="review-schedule review-automation-settings">
  <summary><CalendarClock size={18} /><strong>复盘自动化</strong><span>{saved?.enabled ? `下次 ${scheduleTimeLabel(saved.next_run_at)} · 北京时间` : '定时复盘未启用'} · {subscriptionCount} 个订阅</span><ChevronDown size={15} className="review-automation-chevron" /></summary>
  <div className="review-automation-settings-body">
   {!loaded && !error && <p role="status">正在读取定时设置…</p>}
   <form className="review-automation-schedule-form" onSubmit={(event) => { event.preventDefault(); void save(); }}><fieldset disabled={!loaded || saving}>
    <div className="review-automation-row">
     <strong>执行计划</strong>
     <div className="review-automation-controls">
     <label className="review-automation-check review-automation-enable"><input type="checkbox" checked={form.enabled} onChange={(e) => update({ enabled: e.target.checked })} />每日定时复盘</label>
     <label className="review-automation-time">北京时间<input type="time" required value={form.time} onChange={(e) => update({ time: e.target.value })} /></label>
     <span className="review-automation-hint" title="按深交所交易日历，在每个交易日的前一个自然日执行，默认 22:00。例如周日晚上为周一复盘；节假日休市自动跳过。">交易日前一晚执行，休市自动跳过</span>
     </div>
     <div className="review-automation-actions">
     <button type="submit" className="review-automation-save" disabled={unavailable}>{saving ? <LoaderCircle size={15} className="spin" /> : <Save size={15} />}保存设置</button>
     {message && <span role="status" className="review-automation-hint">{message}</span>}
     </div>
    </div>
    <div className="review-automation-row">
     <strong title="完整 Markdown 结果通知：包含综合判断、方向、情景、个股关注、观察计划与逐位作者观点，省略原文和具体证据；长报告自动分段。">结果通知</strong>
     <div className="review-automation-controls">
     {(['dingtalk', 'feishu'] as const).map((ch) => <label className="review-automation-check review-automation-channel" key={ch}><input type="checkbox" checked={form.channels.includes(ch)} disabled={!ready(ch) && !form.channels.includes(ch)} onChange={(e) => update({ channels: e.target.checked ? [...form.channels, ch] : form.channels.filter((v) => v !== ch) })} />{ch === 'dingtalk' ? '钉钉' : '飞书'}{!ready(ch) && <small>未配置</small>}</label>)}
     <button type="button" onClick={onOpenSettings}>配置通知机器人</button>
     <span className="review-automation-hint" title="失败或部分完成提醒沿用系统设置。">不勾选则仅保存结果</span>
     </div>
    </div>
    {unavailable && <p role="alert">请配置并启用所选机器人，或取消勾选。</p>}
   </fieldset></form>
   {error && <p role="alert" className="review-automation-warning">{error}{!loaded && <button type="button" onClick={() => setReload((v) => v + 1)}>重新加载</button>}</p>}
   {saved?.last_error && <p role="alert" className="review-automation-warning">{saved.last_error}</p>}
   {children}
   <footer className="review-automation-footer">
    <span title="文章窗口为最近已收盘交易日至设定时间，包含期间周末和假日。错过计划仅在目标交易日 09:30 前补跑；已有复盘运行时等待。交易日历获取失败时使用 7 天内缓存，无可用日历则暂停并重试。">定时任务需保持应用运行、电脑唤醒，并提前同步文章。</span>
    {saved?.last_window_start && !saved.last_window_start.startsWith('0001-') && saved.last_window_end && <span>最近任务：为 {saved.last_target_date} 复盘 · <button type="button" onClick={() => onView(saved.last_window_start!, saved.last_window_end!)}>查看结果</button></span>}
   </footer>
  </div>
 </details>;
}
