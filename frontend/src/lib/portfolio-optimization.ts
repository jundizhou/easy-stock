import type { PortfolioInspectionReport } from './backend';

export const portfolioScoringVersion = 'portfolio-ai-score-v4';
export const portfolioOptimizationVersion = 'portfolio-optimization-v23';
export const portfolioOptimizationPromptVersion = 'portfolio-optimization-prompts-v31';
export const targetPortfolioOptimizationScore = 70;
export const minimumPortfolioOptimizationScore = 65;

function verifiedOptimizationScore(conclusion?: PortfolioInspectionReport['conclusion']): number | undefined {
 if (!conclusion?.score_available || typeof conclusion.total_score !== 'number' || conclusion.dimensions?.length!==4) return undefined;
 const weights: Record<string,number>={holding_logic:35,portfolio_structure:25,risk_capacity:25,strategy_fit:15};
 const seen=new Set<string>();let total=0;
 for (const d of conclusion.dimensions) {
  if (!weights[d.key] || seen.has(d.key) || !Number.isInteger(d.score) || d.score===null || d.score===undefined || d.score<0 || d.score>100) return undefined;
  seen.add(d.key);total+=d.score*weights[d.key]/100;
 }
 const score=Math.round(total);
 return score===conclusion.total_score ? score : undefined;
}

export function qualifiedOptimizationScore(conclusion: PortfolioInspectionReport['conclusion'], original?: PortfolioInspectionReport['conclusion']) {
 const score=verifiedOptimizationScore(conclusion);
 if(score===undefined || score<minimumPortfolioOptimizationScore) return false;
 if(score>=targetPortfolioOptimizationScore) return true;
 const before=verifiedOptimizationScore(original);
 return before!==undefined && score-before>=5 && conclusion.dimensions!.every((d) => d.score!==null && d.score!==undefined && d.score>=50);
}

export function optimizationCandidateSource(source: string) {
 if (source.startsWith('industry_startup:')) return `行业初启 · ${source.slice('industry_startup:'.length)}`;
 if (source === 'user_industry_startup') return '用户候选 · 行业初启筛选';
 if (source.startsWith('recent_research:') || source === 'recent_research') return '24小时内个股研究';
 if (source.startsWith('industry_momentum:')) return `行业动量 · ${source.slice('industry_momentum:'.length)}`;
 if (source === 'catalog_liquidity_diversity') return '市场目录 · 流动性与行业覆盖';
 if (source === 'catalog_liquidity') return '市场目录 · 流动性筛选';
 if (source === 'user_catalog') return '用户指定候选';
 if (source === 'user_input_unverified') return '用户指定候选 · 目录待确认';
 return '候选目录';
}
export function optimizationLiquiditySource(source?: string) {
 if (!source) return '成交数据尚未核验';
 const kind = source.endsWith(':realtime') ? '实时行情' : '日线数据';
 const provider = source.split(':')[0];
 const label = ({sina:'新浪',eastmoney:'东方财富'} as Record<string,string>)[provider];
 return `${label ? `${label} · ` : ''}${kind}`;
}

// Only same-rubric, independently recomputed scores describe this improvement.
export function portfolioScoreImprovement(original: PortfolioInspectionReport, target: PortfolioInspectionReport): number | undefined {
 if (original.algorithm_version !== portfolioScoringVersion || target.algorithm_version !== portfolioScoringVersion) return undefined;
 const before = verifiedOptimizationScore(original.conclusion), after = verifiedOptimizationScore(target.conclusion);
 return before === undefined || after === undefined ? undefined : after - before;
}
