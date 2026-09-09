import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import type { StockAIAnalysis } from '../lib/backend';
import { conditionStatusLabel, isResearchRunning, researchConditionValue, researchPlanText, safeResearchURL, type ResearchReport } from '../lib/stock-research';
import { StockResearchOptions, StockResearchProgress, StockResearchReportView } from './StockResearchReport';

const report: ResearchReport = {
	headline:'证据不足，保留观察', thesis:{text:'主判断只是研究解释',kind:'inference',source_ids:['m-price']}, support:[],counter:[],alternatives:[],main_conflict:'缺少业务兑现证据',evidence_level:'insufficient',limitations:['缺少资料不等于没有风险'],conditions:[{id:'c1',text:'失效时停止沿用判断',metric:'close',operator:'lte',anchor_id:'ma20',threshold:10,window:'next_close',source_ids:['m-price'],status:'pending'}],invalidation_ids:['c1'],scenarios:[],decision:{status:'no_plan',mode:'non_short',horizon:'swing',new_position:'暂不形成新仓计划',existing_position:'重新核实风险',reason:'证据不足',price_plan:null},baseline_relation:'disagree',baseline_reason:'历史强势不能推断未来',snapshot_id:'snapshot-123',snapshot_version:2,prompt_version:'stock-research-v1',request:{symbol:'600519.SH',purpose:'holding',horizon:'swing',cost_price:11},model:'fixture-model',generated_at:'2026-09-08T14:00:00Z',cutoff_at:'2026-09-08T13:00:00Z',sources:[{id:'m-price',kind:'calculation',title:'计算资料',content:'价格统计',provider:'fixture',captured_at:'2026-09-08T13:00:00Z',time_status:'dated'}],anchors:[{id:'ma20',price:10,label:'20日均价',source_id:'m-price',as_of:'2026-09-08'}],questions:[],attempts:[],validation:'references_checked',validation_notes:['不是语义准确性认证'],
};
const analysis = {name:'测试股票',symbol:'600519.SH',analysis_id:'analysis-123',scorecard:{overall:60},research_report:report} as StockAIAnalysis;

describe('Evidence-led stock research',()=>{
	it('keeps missing evidence and no-plan decisions visible without fabricated prices',()=>{
		const markup=renderToStaticMarkup(<StockResearchReportView analysis={analysis}/>);
		expect(markup).toContain('暂不形成交易计划');
		expect(markup).toContain('未取得直接反证，不代表不存在风险');
		expect(markup).toContain('与量化基线存在分歧');
		expect(markup).toContain('成本 11.00 元');
		expect(markup).not.toContain('stock-research-prices');
		expect(markup).not.toContain('胜率');
	});
	it('uses explicit data-insufficient/manual verification labels rather than forecast success',()=>{
		expect(conditionStatusLabel('unavailable')).toBe('数据不足');
		expect(conditionStatusLabel('manual_review')).toBe('需人工核实');
		expect(conditionStatusLabel('not_met')).toBe('窗口结束未满足');
		expect(researchConditionValue(report.conditions[0])).toBe('收盘价 ≤ 10.00 元');
	});
	it('exports the original report identity, evidence references and cutoff',()=>{
		const text=researchPlanText(analysis);
		expect(text).toContain('analysis-123');
		expect(text).toContain('snapshot-123 v2');
		expect(text).toContain(report.cutoff_at);
		expect(text).toContain('[m-price]');
		expect(text).not.toContain('压力参考：0');
	});
	it('limits source links to HTTP/HTTPS',()=>{
		expect(safeResearchURL('javascript:alert(1)')).toBeUndefined();
		expect(safeResearchURL('file:///etc/passwd')).toBeUndefined();
		expect(safeResearchURL('https://example.com/source')).toBe('https://example.com/source');
	});
	it('offers purpose and holding-cost controls with stable accessible labels',()=>{
		const html=renderToStaticMarkup(<StockResearchOptions purpose="holding" horizon="swing" cost="11" onPurpose={()=>{}} onHorizon={()=>{}} onCost={()=>{}}/>);
		expect(html).toContain('aria-label="持仓成本"');
		expect(html).toContain('aria-label="研究周期"');
	});
	it('does not mark a degraded or interrupted job successful',()=>{
		for(const status of ['degraded','interrupted','failed','cancelled']){
			expect(isResearchRunning({status})).toBe(false);
			const markup=renderToStaticMarkup(<StockResearchProgress job={{id:'a',status,stage:status,message:'未完成AI研究',request:report.request,started_at:'',updated_at:''}} onCancel={()=>{}}/>);
			expect(markup).toContain('未完成AI研究');
			expect(markup).not.toContain('停止本次研究');
		}
	});
});
