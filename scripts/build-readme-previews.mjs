// Render current React components with public, fictional documentation fixtures.
// Run `npm run build:frontend && node scripts/build-readme-previews.mjs`.
// Serve .runtime/readme-previews on localhost, then capture with browser tooling.
import { mkdir, readFile, readdir, writeFile, copyFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createElement as h } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { createServer } from 'vite';

const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const output=path.join(root,'.runtime/readme-previews');
await mkdir(output,{recursive:true});
const assets=path.join(root,'frontend/dist/assets');
const styles=(await readdir(assets)).filter(name=>name.endsWith('.css'));
if (!styles.length) throw new Error('Build the frontend first.');
await writeFile(path.join(output,'app.css'),(await Promise.all(styles.map(name=>readFile(path.join(assets,name),'utf8')))).join('\n'));
await copyFile(path.join(root,'frontend/public/easy-stock-mark-180.png'),path.join(output,'logo.png'));
const server=await createServer({root:path.join(root,'frontend'),server:{middlewareMode:true},appType:'custom'});
try {
 const {PortfolioAIReportView}=await server.ssrLoadModule('/src/components/PortfolioAIReport.tsx');
 const {OptimizationPlan}=await server.ssrLoadModule('/src/components/PortfolioOptimizationReport.tsx');
 const {PortfolioSetupForm}=await server.ssrLoadModule('/src/components/PortfolioSetupForm.tsx');
 const {StockResearchReportView,StockResearchOptions}=await server.ssrLoadModule('/src/components/StockResearchReport.tsx');
 const {portfolioScoringVersion,portfolioOptimizationVersion,portfolioOptimizationPromptVersion}=await server.ssrLoadModule('/src/lib/portfolio-optimization.ts');
 const noop=()=>{};
 const now='2026-10-09T08:00:00Z';
 const holdings=[['DEMO.A','示例科技',40],['DEMO.B','示例设备',20],['DEMO.C','示例材料',20],['DEMO.D','示例消费',10]].map(([symbol,name,weight_percent])=>({symbol,name,weight_percent}));
 const target=[...holdings.map((item,i)=>({...item,weight_percent:[25,25,20,10][i]})),{symbol:'DEMO.E',name:'示例医药',weight_percent:10}];
 const claim=(text,kind='inference')=>({text,kind,source_ids:['demo-announcement']});
 const research={
  headline:'盈利改善提供支撑，新增仓位仍需等待量价确认',
  thesis:claim('示例公司主营订单与盈利同步改善。继续观察需求兑现，结合估值、成交量和退出条件判断配置时机。'),
  main_conflict:'基本面改善与短期涨幅之间存在分歧，需要区分已披露事实和未来增长预期。',
  support:[claim('示例公告披露：已结束报告期的营收与扣非利润同比增长。','fact')],
  counter:[claim('短期成交量尚未持续放大，主题内个股走势仍有分化。','fact')],
  alternatives:[],evidence_level:'sufficient',limitations:['全部公司、证据、评分和配置均为界面演示数据。'],
  conditions:[{id:'confirm',text:'后续订单与经营数据继续支持盈利改善',metric:'disclosure',operator:'gte',window:'next_disclosure',source_ids:['demo-announcement'],status:'pending'},{id:'invalid',text:'若核心订单收缩或经营逻辑破坏，重新评估持有依据',metric:'manual',operator:'lte',window:'next_disclosure',source_ids:['demo-announcement'],status:'pending'}],
  invalidation_ids:['invalid'],scenarios:[],decision:{status:'observe',mode:'conditional',horizon:'swing',new_position:'等待量价确认后，再核对入场条件。',existing_position:'结合持仓比例持续核验主营逻辑与退出条件。',reason:'已取得的支持证据与短期盘面分歧需要共同评估。'},
  baseline_relation:'disagree',baseline_reason:'经营改善与盘面强度并非完全同步，继续核验成交与相对强度。',
  snapshot_id:'demo-snapshot',snapshot_version:1,prompt_version:'演示数据',request:{symbol:'DEMO.A',purpose:'observe',horizon:'swing',analysis_level:'standard'},model:'已配置模型',generated_at:now,cutoff_at:now,
  sources:[{id:'demo-announcement',kind:'announcement',title:'示例公司经营公告（虚构）',content:'用于展示来源引用的演示材料。',provider:'演示来源',captured_at:now,time_status:'known'}],
  anchors:[],questions:[],attempts:[],validation:'passed',validation_notes:[],
 };
 const results=holdings.map((holding,i)=>({holding,status:'succeeded',research_origin:i===3?'new':'reused',analysis_id:`demo-${i}`,analysis:{symbol:holding.symbol,name:holding.name,ai:{status:'ready'},scorecard:{overall:68},research_report:research},report_completed_at:now,research_cutoff_at:now}));
 const dimensions=(scores)=>['holding_logic','portfolio_structure','risk_capacity','strategy_fit'].map((key,i)=>({key,label:['持仓逻辑质量','组合结构合理性','风险管理质量','策略匹配度'][i],score:scores[i],weight:[35,25,25,15][i],reason:['结合主营、盈利质量与可核验的催化评估持有依据。','前三大股票占比较高，需要检查共同驱动与历史相关性。','结合波动、退出条件和潜在回撤评估组合承受能力。','按照均衡风格与波段周期评估配置是否匹配。'][i],evidence_refs:[],adjustments:[],limitations:[]}));
 const report={id:'readme-demo',algorithm_version:portfolioScoringVersion,prompt_version:'演示数据',generated_at:now,request:{trader_profile:'balanced',horizon:'swing',research_level:'standard',holdings},profile:{label:'均衡'},holdings:results,facts:{},metrics:{total_position_percent:90,cash_percent:10,ai_research_coverage_percent:100,max_single_percent:40,top_three_percent:80,stop_loss_coverage_percent:0},conclusion:{score_available:true,total_score:66,risk_level:'中',risk_reason:'主要持仓存在共同驱动，配置比例与退出纪律需要一起评估。',executive_summary:'组合的主要持有逻辑有研究支持，但前三大持仓合计占总资产 80%。优先复核共同驱动与波动风险，再比较增持原股和引入不同盈利来源的候选。',dimensions:dimensions([72,58,62,72]),holdings:[],primary_risks:['前三大持仓集中，需要检查行业与共同驱动。'],adjustment_order:['先核验持有逻辑，再比较减持与新增资金的用途。'],next_checklist:[],concentration_findings:[],scenarios:[],data_limitations:['本图全部数据为虚构演示。']}};
 const comparison=(scores,total,hs)=>({...report,request:{...report.request,holdings:hs},conclusion:{...report.conclusion,total_score:total,dimensions:dimensions(scores)}});
 const plan={name:'分散盈利来源',status:'conditional',target,checks:{valid:true,total_position_percent:90,cash_percent:10,sold_percent:15,bought_percent:15,retained_percent:75,replacement_ratio_percent:16.7,maximum_replacement_ratio_percent:70,change_budget_mode:'replacement_share',errors:[]},allocations:[],improvements:[{issue:'最大单票仓位',metric:'max_single_percent',before:40,after:25}],original_comparison:comparison([70,50,60,66],62,holdings),target_comparison:comparison([78,75,73,78],76,target),assessment_order:'original_first',assessment:{accepted:true,reason:'示例方案降低单票集中，并加入不同盈利来源。执行前仍需核对实际买点、流动性与退出条件。',original_issue:'集中与共同驱动',tradeoffs:['新配置需要跟踪其独立经营逻辑。'],residual_risks:['行业分散不能消除市场整体回撤。'],evidence_refs:[]}};
 const job={id:'readme-optimization',version:portfolioOptimizationVersion,model_prompt_version:portfolioOptimizationPromptVersion,source_report:report,results,plans:[plan]};
 const pages=[
  ['portfolio-setup','持仓 AI 巡检','选择风格、周期与仓位，复用已有研究',h(PortfolioSetupForm,{draft:{profile:'balanced',horizon:'swing',researchLevel:'standard',holdings:holdings.map(item=>({symbol:item.symbol,name:item.name,weight:item.weight_percent,costPrice:''}))},directory:target.map(item=>({...item,code:item.symbol})),actionLabel:'开始持仓分析',busyLabel:'分析中',showResearchOptions:true,onChange:noop,onSubmit:noop})],
  ['portfolio-report','持仓 AI 巡检','巡检综合评分 · 四维依据 · 逐股判断',h(PortfolioAIReportView,{report,onNew:noop,onRefresh:noop,onOpenStockAnalysis:noop,onOptimize:noop})],
  ['portfolio-optimization','AI 优化持仓','原持仓与建议持仓 · 同次独立复评',h('div',{className:'portfolio-optimization stock-research-report'},h(OptimizationPlan,{job,plan,selected:true,applyBusy:false,onApply:noop,onOpenStockAnalysis:noop}))],
  ['stock-research','个股 AI 研究','主营与证据 · 核心分歧 · 条件化决策',h('div',{className:'stock-ai-report'},h(StockResearchOptions,{purpose:'observe',horizon:'swing',cost:'',onPurpose:noop,onHorizon:noop,onCost:noop}),h(StockResearchReportView,{analysis:results[0].analysis}))],
 ];
 const previewCSS=`html,body{margin:0;min-width:0;background:#f3f6fa;color:#172033}body{padding:24px;font-family:Inter,-apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif}.readme-canvas{max-width:1240px;margin:auto}.readme-heading{display:flex;align-items:center;gap:14px;margin-bottom:20px}.readme-heading img{width:48px;height:48px}.readme-heading h1{margin:0 0 5px;font-size:24px}.readme-heading p{margin:0;color:#627084;font-size:14px}.readme-demo{margin-left:auto;align-self:center;border:1px solid #d3e1f2;background:#edf4ff;color:#32699c;padding:9px 12px;border-radius:9px;font-size:12px}.readme-body{container:portfolio-inspection / inline-size}.readme-footer{margin:18px 0 0;color:#758194;font-size:12px}.readme-body>.stock-ai-report{display:grid;gap:16px}.portfolio-builder{padding:22px;background:white;border:1px solid #d9e1eb;border-radius:12px}`;
 for(const [slug,title,subtitle,component] of pages) {
  const html=`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>${title} · easy-stock 文档预览</title><link rel="stylesheet" href="./app.css"><style>${previewCSS}</style><body><main class="readme-canvas"><header class="readme-heading"><img src="./logo.png" alt="easy-stock"><div><h1>${title}</h1><p>${subtitle}</p></div><span class="readme-demo">界面示例 · 虚构数据</span></header><div class="readme-body">${renderToStaticMarkup(component)}</div><footer class="readme-footer">easy-stock · 当前主分支界面 · 所有公司、仓位、证据与评分均为演示数据</footer></main></body></html>`;
  await writeFile(path.join(output,`${slug}.html`),html);
 }
 await writeFile(path.join(output,'index.html'),`<!doctype html><meta charset="utf-8"><title>easy-stock README 配图</title><h1>当前组件 · 虚构演示数据</h1>${pages.map(([slug,title])=>`<p><a href="./${slug}.html">${title}</a></p>`).join('')}`);
 console.log(`Rendered ${pages.length} component previews to ${output}`);
} finally {await server.close();}
