import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { expect, it } from 'vitest';
import { PortfolioValidationIssues } from './PortfolioValidationIssues';

it('shows independent evidence and action failures together', () => {
 const html = renderToStaticMarkup(<PortfolioValidationIssues issues={[
  { code: 'evidence', path: 'dimensions.holding_logic', message: '引用不属于当前证据目录' },
  { code: 'action', path: 'scenarios[2].action_ids', message: '动作不属于该标的' },
 ]} />);
 expect(html).toContain('2 项');
 expect(html).toContain('来源引用');
 expect(html).toContain('动作与执行条件');
 expect(html).toContain('scenarios[2].action_ids');
 expect(html).toContain('有效标的研究已保留');
});

it('keeps historical records without diagnostics readable', () => {
 expect(renderToStaticMarkup(<PortfolioValidationIssues issues={undefined} />)).toBe('');
 expect(renderToStaticMarkup(<PortfolioValidationIssues issues={[]} />)).toBe('');
});
