import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { ETFAnalysisReport, etfSnapshotText } from './ETFAnalysisReport';
import { StockResearchOptions } from './StockResearchReport';
import { etfFixture } from '../test-utils/etf-fixture';
import { researchPlanText } from '../lib/stock-research';
import { searchStockDirectory } from '../lib/stock-analysis';

const props = {view:'research' as const,copied:false,exporting:false,onCopy:()=>{},onExport:()=>{},onRefresh:()=>{},onAskAI:()=>{}};
describe('ETF reports and shared research',()=>{
 it('renders persisted ETF reports without stock fields and preserves precision in copy',()=>{
  const a = JSON.parse(JSON.stringify(etfFixture()));
  const markup=renderToStaticMarkup(<ETFAnalysisReport {...props} analysis={a}/>);
  for(const text of ['底层资产','基金产品','当前介入条件','1.237','1.2345','数据不足','暂不形成交易计划']) expect(markup).toContain(text);
  for(const text of ['量化基线 0','综合评分','主营背景']) expect(markup).not.toContain(text);
  const copy=researchPlanText(a);expect(copy).toContain('底层资产方向：');expect(copy).toContain('1.237');expect(copy).not.toContain('量化基线');
 });
 it('renders quantitative and money reports without fabricated price momentum',()=>{
  const a=etfFixture(false);a.etf.profile.asset_class='money';a.etf.metrics=[];a.etf.net_values.points=[];a.etf.net_values.income_per_ten_thousand=[{date:'2026-10-09',value:.3456}];
  const markup=renderToStaticMarkup(<ETFAnalysisReport {...props} analysis={a}/>);
  expect(markup).toContain('万份收益');expect(markup).toContain('0.3456');expect(markup).toContain('尚无AI研究结论');expect(markup).not.toContain('核心判断');
  expect(etfSnapshotText(a)).toContain('货币型');
 });
 it('renders dated holdings, notice bodies and incomparable IOPV honestly',()=>{
  const a=etfFixture(false);
  a.etf.holdings=[{symbol:a.symbol,asset_type:'equity',report_date:'2026-06-30',scope:'前十大披露持仓，非实时',disclosed_weight_percent:23.25,items:[{code:'600519',name:'样本持仓',weight_percent:2.52}],meta:a.etf.profile.meta}];
  a.etf.announcements=[{id:'AN1',symbol:a.symbol,title:'申赎公告样本',published_at:'2026-10-09T00:00:00+08:00',url:'https://fund.eastmoney.com/',content:'实际公告正文',content_status:'fulltext',meta:a.etf.profile.meta}];
  a.etf.execution={snapshot:{symbol:a.symbol,price:1.237,bid:1.236,ask:1.237,bid_units:100,ask_units:100,quote_at:'2026-10-09T15:34:59+08:00',iopv:1.2345,iopv_at:'2026-10-09T15:34:59+08:00',quote_status:'normal_quote',raw_status:'00',meta:a.etf.profile.meta},spread_percent:.08,premium_percent:null,comparable:false,limitations:[],trading_rule:'逐产品规则未核实',subscription_status:'未知'};
  const markup=renderToStaticMarkup(<ETFAnalysisReport {...props} analysis={a}/>);
  for(const text of ['2026-06-30','23.25%','样本持仓','申赎公告样本','实际公告正文','当前时点不可比','逐产品规则未核实'])expect(markup).toContain(text);
  const copy = etfSnapshotText(a);
  for (const text of ['可比折溢价：未计算', '实际公告正文', '逐产品规则未核实', ...a.etf.execution_blockers]) expect(copy).toContain(text);
 });
 it('separates research coverage from execution and shows enriched facts',()=>{
  const a=etfFixture();
  a.research_report!.etf_judgments!.product.evidence_level='sufficient';
  a.etf.actions={symbol:a.symbol,items:[{kind:'split',date:'2026-07-20',value:3,price_effective_date:'2026-07-21'}],meta:a.etf.profile.meta};
  a.etf.underlying=[{holding:{code:'688001',symbol:'688001.SH',name:'芯片样本',weight_percent:8.51},holding_report_date:'2026-06-30',financials:[{symbol:'688001.SH',report_date:'2026-06-30',fields:{revenue_yoy:20.2,net_profit_yoy:-3.5},meta:a.etf.profile.meta}]}];
  a.etf.execution={snapshot:{symbol:a.symbol,price:.996,bid:.995,ask:.996,bid_units:100,ask_units:100,quote_at:'2026-10-12T15:34:58+08:00',iopv:.9964,iopv_at:'2026-10-12T15:34:58+08:00',quote_status:'normal_quote',raw_status:'00',meta:a.etf.profile.meta},spread_percent:.1,premium_percent:null,comparable:false,session_status:'outside_session',observation_premium_percent:-.0401,observation_basis:'盘后研究参考，不是实时溢价',limitations:[],trading_rule:'未核实',subscription_status:'未核实'};
  const html=renderToStaticMarkup(<ETFAnalysisReport {...props} analysis={a}/>);
  for(const text of ['底层依据：有限','产品依据：较充分','执行：待核验','仍需补证的研究问题','1:3','2026-07-21','20.20%','-3.50%','盘后参考偏离','-0.040%'])expect(html).toContain(text);
  const copy=etfSnapshotText(a);expect(copy).toContain('芯片样本');expect(copy).toContain('盘后参考偏离');expect(copy).toContain('不是实时溢价');
 });
 it('uses fund units in setup and searches typed ETF candidates',()=>{
  const markup=renderToStaticMarkup(<StockResearchOptions isETF purpose="holding" horizon="short" cost="1.237" onPurpose={()=>{}} onHorizon={()=>{}} onCost={()=>{}}/>);
  expect(markup).toContain('每份成本');expect(markup).toContain('短线');expect(markup).not.toContain('每股成本');
  const candidates=[{symbol:'510300.SH',code:'510300',name:'沪深300ETF',instrument_type:'etf' as const},{symbol:'600519.SH',code:'600519',name:'贵州茅台',instrument_type:'stock' as const}];
  expect(searchStockDirectory(candidates,'300ETF')[0].instrument_type).toBe('etf');
 });
 it.each([{label:'null', financials:null}, {label:'omitted', financials:undefined}, {label:'empty', financials:[]}])('renders and copies persisted valuation-only samples with $label financials', ({financials}) => {
  for (const withReport of [true, false]) {
   const a = JSON.parse(JSON.stringify(etfFixture(withReport)));
   a.etf.underlying = [{
    holding: {code:'688001',symbol:'688001.SH',name:'财务缺失样本',weight_percent:8.51},
    holding_report_date:'2026-06-30', financials,
    valuation:{pe_ttm:30,pb:3,trade_time:'2026-10-19T15:00:00+08:00'},
   }];
   for (const view of ['research', 'expectation', 'risk'] as const) {
    const html = renderToStaticMarkup(<ETFAnalysisReport {...props} analysis={a} view={view}/>);
    expect(html).toContain('财务缺失样本');
    expect(html).toContain('<td>未取得</td>');
    expect(html).toContain('30.00');
    expect(html).toContain('3.00');
    expect(html).not.toContain('NaN');
   }
   const copy = etfSnapshotText(a);
   expect(copy).toContain('财务缺失样本');
   expect(copy).toContain('重仓股财务：未取得');
   expect(copy).not.toContain('营收同比：0');
  }
 });
});
