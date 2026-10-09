import { useEffect, useRef, useState } from 'react';
import { ArrowUpRight, FileText, Target } from 'lucide-react';
import { evidenceLevelLabel, researchMarketStatusLabel, safeResearchURL, type ResearchLogicItem, type ResearchReport, type ResearchSource } from '../lib/stock-research';
import { ResearchClaimView, ResearchPanel } from './StockResearchReport';

export function StockResearchTradingLogic({ report }: { report: ResearchReport }) {
	const [sourceID, setSourceID] = useState('');
	const sourceRef = useRef<HTMLDetailsElement>(null);
	useEffect(() => { if (sourceID) sourceRef.current?.scrollIntoView({ block: 'nearest' }); }, [sourceID]);
	const logic = report.trading_logic;
	const selected = report.sources.find((source) => source.id === sourceID);
	const openSource = (id: string) => {
		setSourceID(id);
		if (sourceRef.current && sourceID === id) {
			sourceRef.current.open = true;
			sourceRef.current.scrollIntoView({ block: 'nearest' });
		}
	};
	const renderItem = (item: ResearchLogicItem) => <article className="stock-research-logic-item" key={item.name}>
		<header><h4>{item.name}</h4><div><span className={`stock-research-evidence-badge ${item.evidence_level}`}>证据{evidenceLevelLabel(item.evidence_level)}</span><span className={`stock-research-market-badge ${item.market_status}`}>{researchMarketStatusLabel(item.market_status)}</span></div></header>
		<ResearchClaimView claim={item.explanation} onSource={openSource} />
		{item.market_evidence && <div className="stock-research-logic-market"><strong>盘面对照</strong><ResearchClaimView claim={item.market_evidence} onSource={openSource} /></div>}
		{item.gaps.length > 0 && <ul className="stock-research-logic-gaps">{item.gaps.map((gap, i) => <li key={i}>{gap}</li>)}</ul>}
	</article>;
	return <ResearchPanel label="AI市场归因" title="近期交易逻辑" icon={Target} className="stock-research-trading-logic" ariaLabel="近期交易逻辑">
		<p className="stock-research-muted">主线与次线均为研究候选；公司证据和盘面状态分别评估，上涨原因仍需验证。</p>
		{logic ? <>
			<div className="stock-research-logic-columns"><div><h3>主线候选</h3>{logic.mainlines.length ? logic.mainlines.map(renderItem) : <p>尚未取得足够依据形成近期主线候选</p>}</div><div><h3>次线候选</h3>{logic.secondary.length ? logic.secondary.map(renderItem) : <p>暂未形成有依据的次线候选</p>}</div></div>
			<div className="stock-research-logic-columns stock-research-logic-context"><div><h3>催化事件</h3>{logic.catalysts.length ? logic.catalysts.map((claim, i) => <div key={i}><ResearchClaimView claim={claim} onSource={openSource} /><small className="stock-research-muted">{claim.source_ids.map((id) => `${id} · ${sourceDate(report.sources.find((entry) => entry.id === id))}`).join('；')}</small></div>) : <p>尚未取得可引用的近期催化事件</p>}</div><div><h3>主营背景</h3>{logic.business ? <ResearchClaimView claim={logic.business} onSource={openSource} /> : <p>主营资料尚待补充</p>}</div></div>
			{logic.gaps.length > 0 && <div className="stock-research-logic-gaps"><strong>待核实事项</strong><ul>{logic.gaps.map((gap, i) => <li key={i}>{gap}</li>)}</ul></div>}
		</> : <p>本报告未保存结构化近期逻辑。重新分析可生成主线、次线和催化事件；原核心判断与证据仍保留。</p>}
		{selected && <details ref={sourceRef} key={sourceID} open className="stock-ai-panel stock-research-source selected"><summary><FileText size={16} /><strong>{selected.title}</strong><span>{selected.id}</span></summary><div><p className="stock-research-source-meta">{selected.provider} · {selected.kind} · {sourceDate(selected)}</p><pre>{selected.content}</pre>{safeResearchURL(selected.url) && <a href={safeResearchURL(selected.url)} target="_blank" rel="noreferrer">查看原始来源<ArrowUpRight size={13} /></a>}</div></details>}
	</ResearchPanel>;
}

function sourceDate(source?: ResearchSource) {
	const value = source?.published_at;
	return source?.report_date || (value && Number.isFinite(Date.parse(value)) && new Date(value).getFullYear() >= 2000 ? value.slice(0, 10) : '发布时间未知');
}
