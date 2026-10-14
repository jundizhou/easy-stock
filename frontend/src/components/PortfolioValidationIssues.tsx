import type { PortfolioInspectionJob } from '../lib/backend';

const categories: Record<string, string> = { evidence: '来源引用', action: '动作与执行条件', contract: '输出协议', format: '输出格式', score: '评分规则', scenario: '情景内容', holding: '持仓对应', confidence: '证据置信度', research: '标的研究' };

export function PortfolioValidationIssues({ issues }: { issues: PortfolioInspectionJob['validation_issues'] }) {
 if (!issues?.length) return null;
 return <details className="portfolio-validation-issues">
  <summary>查看组合评估校验详情（{issues.length} 项）</summary>
  <p>有效标的研究已保留。重试将根据当前证据与这些问题修正组合评估。</p>
  <ul>{issues.map((issue, i) => <li key={`${issue.path}-${i}`}><strong>{categories[issue.code] || '内容校验'}</strong>：{issue.message}<small>{issue.path}</small></li>)}</ul>
 </details>;
}
