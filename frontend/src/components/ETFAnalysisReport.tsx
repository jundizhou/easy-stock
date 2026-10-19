import { useState } from 'react';
import { Clipboard, Download, RefreshCw, MessageCircle, FileText, ChartNoAxesCombined, CircleAlert, Layers3, ChevronDown, Check, Clock3, ArrowUpRight } from 'lucide-react';
import type { ETFAnalysis, ETFNAVPoint } from '../lib/backend';
import type { ResearchVerification } from '../lib/stock-research';
import { safeResearchURL } from '../lib/stock-research';
import { formatCompactAmount } from '../lib/stock-analysis';
import { KLineChart } from './KLineChart';
import { ResearchPanel, StockResearchReportView } from './StockResearchReport';
import { ETFDirectionPanel, etfDirectionText } from './ETFDirectionPanel';
import './etf-analysis.css';

export const etfClassLabel = (value: string) => ({ equity: '股票型', bond: '债券型', commodity: '商品型', money: '货币型', unknown: '分类待核实' }[value] || '分类待核实');
export function etfSnapshotText(analysis: ETFAnalysis) {
 const { profile: p, metrics, holdings = [], announcements = [], execution, limitations, execution_blockers, actions, underlying = [] } = analysis.etf;
 const price = (value?: number | null, precision = 3) => value == null ? '未知' : value.toFixed(precision);
 return [
  `${analysis.name}（${analysis.symbol}）ETF资料快照`,
  `采集时点：${analysis.generated_at}`,
  `${etfClassLabel(p.asset_class)}${p.cross_border ? ' · 跨境' : ''}；跟踪标的：${p.tracking_target || '未取得'}`,
  ...etfDirectionText(analysis.etf.direction),
  `管理费：${p.management_fee || '未取得'}；托管费：${p.custody_fee || '未取得'}`,
  ...metrics.map(m => `${m.label}：${m.value == null ? '数据不足' : m.value.toFixed(m.unit === '元' ? 3 : 2) + m.unit}（${m.as_of || '日期未知'}）`),
  ...(actions?.items || []).map(a => `基金事件：${a.date} ${a.kind === 'split' ? `份额拆分 1:${a.value}${a.price_effective_date ? `，行情调整日 ${a.price_effective_date}（按前后报价核对）` : ''}` : `每份现金分红 ${a.value} 元`}`),
  ...underlying.flatMap(v => [`底层样本：${v.holding.name}（${v.holding.symbol}），${v.holding_report_date} 披露权重 ${v.holding.weight_percent}%`, ...(v.financials?.length ? v.financials.map(f => `${f.report_date} 营收同比：${f.fields.revenue_yoy ?? '未知'}%；归母净利润同比：${f.fields.net_profit_yoy ?? '未知'}%`) : ['重仓股财务：未取得'])]),
  ...holdings.flatMap(h => [
   `持仓披露：${h.report_date} · ${h.scope} · 合计${h.disclosed_weight_percent.toFixed(2)}%`,
   ...h.items.map(v => `${v.symbol || v.code} ${v.name}：${v.weight_percent.toFixed(2)}%`),
   ...(safeResearchURL(h.meta.source_url) ? [`持仓来源：${safeResearchURL(h.meta.source_url)}`] : []),
  ]),
  ...announcements.flatMap(n => [
   `公告：${etfDisclosureDate(n.published_at)} ${n.title}（${n.content_status === 'fulltext' ? '正文' : n.content_status === 'excerpt' ? '正文节选' : '仅标题'}） ${safeResearchURL(n.url) || ''}`,
   n.content || '正文暂未取得，不能仅凭标题推断具体约束。',
  ]),
  ...(execution ? [
   `盘口时间：${etfDate(execution.snapshot.quote_at)}；买一 ${price(execution.snapshot.bid)} / 卖一 ${price(execution.snapshot.ask)}`,
   `IOPV：${price(execution.snapshot.iopv, 4)}（${etfDate(execution.snapshot.iopv_at)}）；可比折溢价：${execution.premium_percent == null ? '未计算' : execution.premium_percent.toFixed(3) + '%'}`,
   ...(execution.observation_premium_percent != null ? [`盘后参考偏离：${execution.observation_premium_percent.toFixed(3)}%；${execution.observation_basis}`] : []),
   `交易规则：${execution.trading_rule}；申赎状态：${execution.subscription_status}`,
  ] : []),
  ...[...new Set([...execution_blockers, ...limitations, ...(execution?.limitations || [])])].map(v => `限制：${v}`),
 ].join('\n');
}

type Props = { analysis: ETFAnalysis; view: 'research' | 'expectation' | 'risk'; copied: boolean; exporting: boolean; onCopy: () => void; onExport: () => void; onRefresh: () => void; onAskAI: () => void; verification?: ResearchVerification; verifying?: boolean; onVerify?: () => void };
export function ETFAnalysisReport({ analysis, view, copied, exporting, onCopy, onExport, onRefresh, onAskAI, verification, verifying, onVerify }: Props) {
 const data = analysis.etf, p = data.profile;
 const [showFacts, setShowFacts] = useState(!analysis.research_report);
 const hasQuote = analysis.quote.price > 0;
 const price = hasQuote ? analysis.quote.price.toFixed(p.price_precision || 3) : '—';
 const change = hasQuote && Number.isFinite(analysis.quote.change_percent) ? analysis.quote.change_percent : null;
 return <div className="etf-analysis">
  <section className="stock-ai-panel etf-summary" aria-label="ETF 研究摘要">
   <div className="etf-summary-top">
    <div className="etf-identity">
     <div className="etf-tags"><span className="etf-tag"><Layers3 size={13} aria-hidden="true"/>场内 ETF</span><span>{etfClassLabel(p.asset_class)}</span>{p.cross_border && <span>跨境</span>}</div>
     <div className="etf-name"><h2>{analysis.name}</h2><span>{analysis.symbol}</span></div>
     <p className="etf-tracking-target">跟踪标的 <strong>{p.tracking_target || '待核实'}</strong></p>
    </div>
    <div className="etf-quote">
     <span>市场报价 <small>元 / 份</small></span>
     <div><strong>{price}</strong>{change != null && <span className={`etf-quote-change ${change > 0 ? 'up' : change < 0 ? 'down' : ''}`}>{change > 0 ? '+' : ''}{change.toFixed(2)}%</span>}</div>
     <small>{analysis.quote.trade_time ? etfDate(analysis.quote.trade_time) : '报价时点未知'}</small>
    </div>
   </div>
   <div className="etf-summary-conclusion"><span>{analysis.research_report ? '研究摘要' : '量化速览'}</span><h3>{analysis.conclusion.headline}</h3><p>{analysis.research_report?.thesis.text || analysis.ai.message}</p></div>
   <div className="etf-summary-footer">
    <span className="etf-report-time"><Clock3 size={13} aria-hidden="true"/>分析于 {etfDate(analysis.generated_at)}</span>
    <div className="etf-actions" data-html2canvas-ignore="true">
     <button type="button" onClick={onRefresh}><RefreshCw size={14} aria-hidden="true"/>重新分析</button>
     <button type="button" onClick={onCopy}>{copied ? <Check size={14} aria-hidden="true"/> : <Clipboard size={14} aria-hidden="true"/>}{copied ? '已复制' : '复制报告'}</button>
     <button type="button" onClick={onExport} disabled={exporting}><Download size={14} aria-hidden="true"/>{exporting ? '生成中…' : '导出长图'}</button>
     <button type="button" className="primary" onClick={onAskAI}><MessageCircle size={14} aria-hidden="true"/>追问 AI</button>
    </div>
   </div>
  </section>
  {data.direction && view === 'research' && <ETFDirectionPanel direction={data.direction} />}
  {analysis.research_report && <StockResearchReportView analysis={analysis} view={view} verification={verification} verifying={verifying} onVerify={onVerify}/>}
  <details className="stock-research-quantitative etf-facts" open={showFacts} onToggle={e => setShowFacts(e.currentTarget.open)}>
   <summary><span className="etf-facts-icon"><FileText size={19} aria-hidden="true"/></span><span><strong>ETF 基金档案与量化事实</strong><small>基金资料、交易行情、披露持仓与原始来源</small></span><ChevronDown size={17} className="etf-facts-chevron" aria-hidden="true"/></summary>
   <ResearchPanel label="产品信息" title="基金档案" icon={FileText}>
    <dl className="etf-profile">{[['跟踪标的',p.tracking_target],['比较基准',p.benchmark],['基金管理人',p.manager],['管理费',p.management_fee],['托管费',p.custody_fee],['净资产规模',p.size],['基金份额',p.shares],['成立日期',p.inception]].map(([label,value]) => <div key={label}><dt>{label}</dt><dd>{value || '未取得'}</dd></div>)}</dl>
    <p className="stock-research-muted">规模、份额及费率按来源披露日期解释；抓取时间不是生效日期。管理费与托管费不包含全部交易成本。</p>
    {[['投资范围',p.investment_scope],['投资策略',p.strategy],['风险收益特征',p.risk_description],['收益分配',p.distribution]].map(([title, text]) => <details key={title}><summary>{title}</summary><p>{text || '来源未提供'}</p></details>)}
    <ETFSourceLink url={p.meta.source_url} label="查看基金档案原文"/>
   </ResearchPanel>
   <ResearchPanel label="市价与量能" title="完整交易日行情" icon={ChartNoAxesCombined}><p>{data.price_basis} · 成交量单位：份</p><KLineChart lines={data.trend_bars || data.bars || []} symbol={analysis.symbol} pricePrecision={3} timeZone="Asia/Shanghai"/>
    <dl className="etf-metrics">{data.metrics.map(m => <div key={m.label} title={m.method}><dt>{m.label}</dt><dd>{m.value == null ? '数据不足' : m.label.includes('成交额') ? formatCompactAmount(m.value) : `${m.value.toFixed(m.unit === '元' ? 3 : 2)}${m.unit}`}</dd><small>{m.as_of || '日期未知'}</small></div>)}</dl>
   </ResearchPanel>
   <ResearchPanel label="净值与分配" title={p.asset_class === 'money' ? '货币收益资料' : '基金单位净值'} icon={FileText}>
    <p>按估值日展示，发布时间未完整取得；不能用旧净值代替实时 IOPV。净值变化未调整分红，不等于总回报。</p>
    {p.asset_class === 'money' ? <><NAVSeries title="万份收益（元）" points={data.net_values.income_per_ten_thousand || []}/><NAVSeries title="七日年化（%）" points={data.net_values.seven_day_annualized || []}/><p>历史年化不代表保证收益；分配规则以基金档案为准。</p></> : <NAVSeries title="单位净值（元）" points={data.net_values.points || []}/>}
   </ResearchPanel>
   <ResearchPanel label="交易条件" title="盘口与参考估值" icon={ChartNoAxesCombined}>
    {data.execution ? <><dl className="etf-metrics">{[['买一', data.execution.snapshot.bid, '元'], ['卖一', data.execution.snapshot.ask, '元'], ['盘口价差', data.execution.spread_percent, '%'], ['参考 IOPV', data.execution.snapshot.iopv, '元'], ['可比折溢价', data.execution.premium_percent, '%']].map(([label, value, unit]) => <div key={String(label)}><dt>{label}</dt><dd>{typeof value === 'number' ? `${value.toFixed(label === '参考 IOPV' ? 4 : 3)}${unit}` : '未取得 / 不可比'}</dd></div>)}</dl>{data.execution.observation_premium_percent != null && <p><strong>盘后参考偏离：{data.execution.observation_premium_percent.toFixed(3)}%</strong> · {data.execution.observation_basis}</p>}<p>盘口时间：{etfDate(data.execution.snapshot.quote_at)} · IOPV 时间：{etfDate(data.execution.snapshot.iopv_at)}</p><p>{data.execution.comparable ? '仅代表冻结时点的参考折溢价，不证明可成交或套利。' : data.execution.session_status === 'outside_session' ? '当前为盘后或非连续交易时段，可研究完整日线和产品事实；实时执行条件待交易时段复核。' : '当前时点不可比，不以旧净值或错位估值计算实时溢价。'}</p><p>{data.execution.trading_rule}</p><p>申赎状态：{data.execution.subscription_status}</p></> : <p>此历史报告尚未采集盘口与 IOPV。</p>}
   </ResearchPanel>
   {data.actions?.items?.length ? <ResearchPanel label="历史口径" title="分红与拆分" icon={FileText}>
    <p className="etf-panel-note">行情图及趋势指标按已披露拆分调整为当前份额；市价锚点保留当日实际价格，现金分红不计入价格收益。</p>
    <ul className="etf-event-list">{data.actions.items.map(a => <li key={a.kind+a.date}>
     <time>{a.date}</time><div><strong>{a.kind === 'split' ? `份额拆分 1:${a.value}` : `每份现金分红 ${a.value} 元`}</strong>{a.kind === 'split' && a.price_effective_date && <span>行情调整日 {a.price_effective_date}（按前后报价核对）</span>}</div>
    </li>)}</ul>
    <ETFSourceLink url={data.actions.meta.source_url} label="查看分红拆分来源"/>
   </ResearchPanel> : null}
   {data.underlying?.length ? <ResearchPanel label="底层样本" title="已披露重仓股的盈利与估值" icon={Layers3}>
    <p className="etf-panel-note">按披露权重选取最多 3 只境内重仓股，财务按报告期展示。样本不代表整个指数或行业，披露持仓不等于实时持仓。</p>
    <div className="etf-table-scroll" role="region" aria-label="重仓股盈利与估值表" tabIndex={0}><table className="etf-underlying-table"><thead><tr><th scope="col">披露持仓</th><th scope="col">财务报告期</th><th scope="col" className="etf-number">营收同比</th><th scope="col" className="etf-number">归母净利润同比</th><th scope="col" className="etf-number">PE / PB（采集时点）</th></tr></thead><tbody>{data.underlying.map(v => <tr key={v.holding.symbol}>
     <td><strong>{v.holding.name}</strong><small>{v.holding_report_date} · {v.holding.weight_percent.toFixed(2)}%</small></td><td>{v.financials?.[0]?.report_date || '未取得'}</td><td className="etf-number">{etfPercent(v.financials?.[0]?.fields.revenue_yoy)}</td><td className="etf-number">{etfPercent(v.financials?.[0]?.fields.net_profit_yoy)}</td><td className="etf-number">{v.valuation?.pe_ttm?.toFixed(2) ?? '—'} / {v.valuation?.pb?.toFixed(2) ?? '—'}<small>{v.valuation ? etfDate(v.valuation.trade_time) : ''}</small></td>
    </tr>)}</tbody></table></div>
   </ResearchPanel> : null}
   <ResearchPanel label="报告期披露" title="基金持仓与覆盖范围" icon={Layers3}>
    {data.holdings?.length ? data.holdings.map(h => <div className="etf-holdings-group" key={h.asset_type}>
     <div className="etf-holdings-heading"><div><h4>{h.asset_type === 'bond' ? '债券' : '股票'}持仓</h4><span>报告期 {h.report_date}</span></div><span className="etf-coverage-badge">披露占净值 <strong>{h.disclosed_weight_percent.toFixed(2)}%</strong></span></div>
     <p className="etf-panel-note">{h.scope}</p>
     <div className="etf-table-scroll" role="region" aria-label="基金披露持仓表" tabIndex={0}><table><thead><tr><th scope="col">代码</th><th scope="col">名称</th><th scope="col" className="etf-number">占净值</th></tr></thead><tbody>{h.items.map(v => <tr key={v.code}><td className="etf-symbol">{v.symbol || v.code}</td><td><strong>{v.name}</strong></td><td className="etf-number"><span className="etf-weight"><span aria-hidden="true"><i style={{width:`${Math.min(100, Math.max(0, v.weight_percent))}%`}}/></span>{v.weight_percent.toFixed(2)}%</span></td></tr>)}</tbody></table></div>
     <ETFSourceLink url={h.meta.source_url} label="查看持仓来源"/>
    </div>) : <p className="etf-empty-note">未取得可核实持仓；投资范围不能替代当前实际持仓。</p>}
   </ResearchPanel>
   <ResearchPanel label="基金披露" title="近期公告" icon={FileText} action={<span className="etf-panel-count">{data.announcements?.length || 0} 条</span>}>
    <p className="etf-panel-note">最近公告样本；正文、节选与仅标题分别标注。未见公告不代表事项不存在。</p>
    {data.announcements?.length ? <div className="etf-announcements">{data.announcements.map(n => <details className="etf-announcement" key={n.id}>
     <summary><FileText size={16} className="etf-announcement-icon" aria-hidden="true"/><span className="etf-announcement-heading"><strong>{n.title}</strong><span><time>{etfDisclosureDate(n.published_at)}</time><span className={`etf-content-badge ${n.content_status}`}>{n.content_status === 'fulltext' ? '正文' : n.content_status === 'excerpt' ? '正文节选' : '仅标题'}</span></span></span><ChevronDown size={15} className="etf-disclosure-chevron" aria-hidden="true"/></summary>
     <div className="etf-announcement-body"><p>{n.content || '正文暂未取得，不能仅凭标题推断具体约束。'}</p><ETFSourceLink url={n.url} label="查看公告原文"/></div>
    </details>)}</div> : <p className="etf-empty-note">公告暂不可用。</p>}
   </ResearchPanel>
   <ResearchPanel label="有限样本" title="同跟踪标的产品对照" icon={Layers3}><p>{data.peer_scope}</p>{data.peers?.length ? <div className="etf-table-scroll"><table><thead><tr><th>产品</th><th>管理费 / 托管费</th><th>规模（含披露日）</th><th className="etf-number">市场报价</th></tr></thead><tbody>{data.peers.map(peer => <tr key={peer.profile.symbol}><td>{peer.profile.name}<small>{peer.profile.symbol}</small></td><td>{peer.profile.management_fee || '未知'} / {peer.profile.custody_fee || '未知'}</td><td>{peer.profile.size || '未知'}</td><td className="etf-number">{peer.quote && peer.quote.price > 0 ? peer.quote.price.toFixed(3) : '未取得'}<small>{peer.quote?.trade_time ? new Date(peer.quote.trade_time).toLocaleString('zh-CN', {timeZone:'Asia/Shanghai'}) : ''}</small></td></tr>)}</tbody></table></div> : <p>未取得可核实的同标的产品样本。</p>}</ResearchPanel>
   <ResearchPanel label="数据边界" title="缺失资料与执行限制" icon={CircleAlert} className="etf-limitations"><ul>{[...new Set([...data.execution_blockers, ...data.limitations])].map(v => <li key={v}>{v}</li>)}</ul></ResearchPanel>
  </details>
 </div>;
}
function ETFSourceLink({url, label}: {url?: string; label: string}) {
 const href = safeResearchURL(url);
 return href ? <a className="etf-source-link" href={href} target="_blank" rel="noreferrer">{label}<ArrowUpRight size={13} aria-hidden="true"/></a> : null;
}

function NAVSeries({title, points}: {title: string; points: ETFNAVPoint[]}) {
 return <div className="etf-nav-series"><strong>{title}</strong>{points.length ? <div className="etf-table-scroll"><table><thead><tr><th>估值日</th><th className="etf-number">数值</th><th>分配备注</th></tr></thead><tbody>{points.slice(-5).reverse().map(p => <tr key={p.date}><td>{p.date}</td><td className="etf-number">{p.value.toFixed(4)}</td><td>{p.distribution || '—'}</td></tr>)}</tbody></table></div> : <p>资料暂不可用</p>}</div>;
}

function etfDate(value?:string) { if (!value || value.startsWith("0001-")) return "未知"; return new Date(value).toLocaleString("zh-CN", {timeZone:"Asia/Shanghai"}); }

function etfDisclosureDate(value: string) { return new Date(value).toLocaleDateString("zh-CN", {timeZone:"Asia/Shanghai"}); }

function etfPercent(value?:number) {return value == null ? "未取得" : `${value.toFixed(2)}%`;}
