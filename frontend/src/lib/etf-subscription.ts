import type { SourceMeta } from './backend';
import { etfMoney, type ETFChartPoint } from './national-team';
export type ETFSubscriptionPoint = {
 date: string; previous_date: string; shares: number | null; previous_shares: number | null;
 share_change: number | null; close: number | null; price_date: string; estimated_net: number | null; verification: string;
};
export type ETFSubscriptionSeries = {
 code: string; name: string; index: string; exchange: string; points: ETFSubscriptionPoint[];
 latest_price: number | null; change_percent: number | null; quote_time: string | null;
};
export type ETFSubscriptionDay = { date: string; previous_date: string; estimated_net: number | null; inflow: number | null; outflow: number | null; coverage: number; subscribed: number; redeemed: number };
export type ETFSubscriptionDashboard = {
 today_trial?: ETFSubscriptionTrial | null;
 start_date: string; source_date: string; source_generated_at: string | null; report_date: string;
 latest_date: string; latest_complete_date: string; expected: number;
 days: ETFSubscriptionDay[]; series: ETFSubscriptionSeries[];
 checks: Array<{ code: string; date: string; snapshot_shares: number | null; official_shares: number | null; status: string }>;
 warnings: string[];
};
export function subscriptionChartPoints(data: ETFSubscriptionDashboard, code: string, cumulative: boolean): ETFChartPoint[] {
 const individual = new Map(data.series.find((item) => item.code === code)?.points.map((item) => [item.date, item.estimated_net]) || []);
 let total: number | null = 0;
 return data.days.filter((day) => code !== 'pool' || !data.latest_complete_date || day.date <= data.latest_complete_date).map((day) => {
  const value = code === 'pool' ? day.estimated_net : individual.get(day.date) ?? null;
  if (value == null || !Number.isFinite(value)) total = null;
  else if (total != null) total += value;
  return { time: day.date, net: cumulative ? total : value, coverage: code === 'pool' ? `${day.coverage}/${data.expected}` : undefined };
 });
}
export function subscriptionWindowNet(data: ETFSubscriptionDashboard): number | null {
 const days = data.days.filter((day) => data.latest_complete_date && day.date <= data.latest_complete_date);
 if (!days.length || days.some((day) => day.estimated_net == null)) return null;
 return days.reduce((sum, day) => sum + day.estimated_net!, 0);
}
export function subscriptionPrompt(data: ETFSubscriptionDashboard, day: ETFSubscriptionDay | undefined, meta: SourceMeta): string {
 return [
  '请解读以下宽基 ETF 全市场净申赎估算，严格区分匿名申赎与国家队实名持仓。',
  '金额逐只按（当日份额−上一披露交易日份额）×同一份额所属日期的收盘价计算，再求和。收盘价替代净值有误差。不得用盘中大单净额推断申赎或国家队买卖。',
  `来源 ${meta.source}；快照生成 ${data.source_generated_at || '未知'}；抓取 ${meta.fetched_at}；缓存 ${meta.stale ? '是' : '否'}。`,
  `所选披露日期 ${day?.date || '未知'}，比较基准 ${day?.previous_date || '未知'}；覆盖 ${day?.coverage ?? 0}/${data.expected}；全池估算净申购 ${etfMoney(day?.estimated_net)}。`,
  `所示窗口 ${data.start_date} 至 ${data.latest_date}，区间估算净申购 ${etfMoney(subscriptionWindowNet(data))}。份额每日披露，不能把抓取时间称为实时资金发生时间。`,
  ...(data.today_trial ? [`当日试算 ${data.today_trial.date}，状态 ${data.today_trial.status}；今日份额 ${data.today_trial.share_coverage}/${data.today_trial.expected}，可算 ${data.today_trial.coverage}/${data.today_trial.expected}，全池试算 ${etfMoney(data.today_trial.estimated_net)}。当日份额或价格未齐备时，全池未知，不能写成零；不把上一披露日份额变化改称今日净申购。`] : []),
  ...data.series.map((item) => { const point = item.points.find((p) => p.date === day?.date); return `${item.name}（${item.code}）：份额差 ${point?.share_change ?? '缺失'} 份，测算收盘价 ${point?.close ?? '缺失'}，价格日期 ${point?.price_date || '缺失'}，估算净申购 ${etfMoney(point?.estimated_net)}，核验 ${point?.verification || '未提供'}。`; }),
  `数据限制 ${data.warnings.join('；') || '见各日覆盖率与逐项核验'}。实名报告参考日期 ${data.report_date || '未知'}，报告持有人不是盘中账户识别证据。`,
  '只分析 ETF 申赎方向、持续性、覆盖与数据限制，不能把全池净申购额称为国家队实际买入额；不能为了符合预期而改动金额方向。',
 ].join('\n');
}

export type ETFSubscriptionTrialItem = {
 code: string; name: string; share_date: string; shares: number | null; previous_shares: number | null;
 share_change: number | null; price: number | null; quote_time: string | null; estimated_net: number | null; status: string;
};
export type ETFSubscriptionTrial = {
 date: string; previous_date: string; expected: number; share_coverage: number; baseline_coverage: number;
 price_coverage: number; coverage: number; estimated_net: number | null; covered_net: number | null; status: string;
 items: ETFSubscriptionTrialItem[];
};
export function subscriptionTrialStatus(trial: ETFSubscriptionTrial, meta: SourceMeta, now = Date.now()): string {
 const local = new Date(now + 8 * 60 * 60 * 1000);
 if (trial.date !== local.toISOString().slice(0, 10)) return '历史试算，非今日';
 if (meta.stale) return '缓存试算，等待刷新';
 const minute = local.getUTCHours() * 60 + local.getUTCMinutes();
 const weekday = local.getUTCDay();
 const trading = weekday > 0 && weekday < 6 && ((minute >= 570 && minute <= 690) || (minute >= 780 && minute <= 900));
 const expired = trial.items.some((item) => {
  if (item.estimated_net == null) return false;
  const stamp = item.quote_time ? Date.parse(item.quote_time) : NaN;
  if (!Number.isFinite(stamp) || stamp > now + 60_000) return true;
  const quoteLocal = new Date(stamp + 8 * 60 * 60 * 1000);
  if (quoteLocal.toISOString().slice(0, 10) !== trial.date) return true;
  const sourceMinute = quoteLocal.getUTCHours() * 60 + quoteLocal.getUTCMinutes();
  if (trading) return now - stamp > 180_000;
  if (minute > 900) return sourceMinute < 900;
  if (minute > 690 && minute < 780) return sourceMinute < 685;
  return true;
 });
 if (expired) return '试算价格已过期，等待刷新';
 return trial.status;
}
