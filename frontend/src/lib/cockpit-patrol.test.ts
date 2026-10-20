import { describe, expect, it } from 'vitest';
import { type PatrolBrief, intradaySummary, latestBriefFor, narrativeNotice, sessionIntradayBriefs, stripLeadingTitle } from './cockpit-patrol';

const facts = { sources: [] };
const brief = (overrides: Partial<PatrolBrief>): PatrolBrief => ({
	id: 'b', key: 'k', phase: 'intraday', session_date: '2026-10-20', label: '盘中巡查 10:10', trigger: 'scheduled', status: 'ready', narrative_status: 'ready', facts, started_at: '2026-10-20T02:10:00Z', ...overrides,
});

describe('patrol narrative notice', () => {
	it('says plainly when AI is not configured instead of implying a conclusion', () => {
		const notice = narrativeNotice(brief({ narrative_status: 'unavailable' }));
		expect(notice.tone).toBe('muted');
		expect(notice.text).toContain('没有生成结论');
	});

	it('flags numbers that could not be verified against collected data', () => {
		const notice = narrativeNotice(brief({ unverified_numbers: ['9.9%', '88.8'] }));
		expect(notice.tone).toBe('warn');
		expect(notice.text).toContain('2 个数值');
	});

	it('reports verified narratives and failures explicitly', () => {
		expect(narrativeNotice(brief({})).tone).toBe('ok');
		expect(narrativeNotice(brief({ status: 'failed', error: '采集超时' })).text).toBe('采集超时');
		expect(narrativeNotice(brief({ narrative_status: 'failed', error: 'AI 叙述生成失败：timeout' })).tone).toBe('error');
	});
});

describe('intraday summary', () => {
	it('drops the status prefix the backend already prepends', () => {
		expect(intradaySummary('高位延续 · 低位一般', '高位延续')).toBe('低位一般');
		expect(intradaySummary('低位一般', '高位延续')).toBe('低位一般');
		expect(intradaySummary('退潮风险', '')).toBe('退潮风险');
	});
});

describe('narrative title', () => {
	it('drops a leading heading that repeats the panel title', () => {
		expect(stripLeadingTitle('# 盘中巡查 09:40（2026-10-20）\n\n一、正在发生什么')).toBe('一、正在发生什么');
		expect(stripLeadingTitle('没有标题的正文')).toBe('没有标题的正文');
	});

	it('keeps the original text when the heading is the whole narrative', () => {
		expect(stripLeadingTitle('# 只有标题')).toBe('# 只有标题');
	});
});

describe('patrol brief selection', () => {
	it('prefers a finished brief over a running one for the same phase', () => {
		const briefs = [brief({ id: 'running', status: 'running' }), brief({ id: 'done', status: 'ready' })];
		expect(latestBriefFor(briefs, 'intraday')?.id).toBe('done');
		expect(latestBriefFor(briefs, 'post_close')).toBeNull();
	});

	it('orders the intraday timeline of one session by start time', () => {
		const briefs = [
			brief({ id: 'late', started_at: '2026-10-20T05:00:00Z' }),
			brief({ id: 'early', started_at: '2026-10-20T02:10:00Z' }),
			brief({ id: 'other-day', session_date: '2026-10-19', started_at: '2026-10-19T03:00:00Z' }),
			brief({ id: 'post', phase: 'post_close', started_at: '2026-10-20T07:35:00Z' }),
		];
		expect(sessionIntradayBriefs(briefs, '2026-10-20').map((item) => item.id)).toEqual(['early', 'late']);
	});
});
