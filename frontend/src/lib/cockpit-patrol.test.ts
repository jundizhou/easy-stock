import { describe, expect, it } from 'vitest';
import { type PatrolBrief, TIMELINE_FOLD_LIMIT, foldTimeline, intradaySummary, latestBriefFor, latestIntradayFallback, narrativeNotice, sessionIntradayBriefs, stripLeadingTitle } from './cockpit-patrol';

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

describe('intraday timeline folding', () => {
	const many = Array.from({ length: 15 }, (_, index) => brief({ id: `b${index}`, started_at: `2026-10-20T${String(index).padStart(2, '0')}:00:00Z` }));

	it('keeps the timeline untouched while it is short enough', () => {
		const short = many.slice(0, TIMELINE_FOLD_LIMIT);
		expect(foldTimeline(short)).toEqual({ visible: short, hidden: 0 });
	});

	it('keeps only the latest runs and reports how many are hidden', () => {
		const folded = foldTimeline(many);
		expect(folded.visible.map((item) => item.id)).toEqual(many.slice(-TIMELINE_FOLD_LIMIT).map((item) => item.id));
		expect(folded.hidden).toBe(15 - TIMELINE_FOLD_LIMIT);
	});

	it('shows everything again once expanded', () => {
		expect(foldTimeline(many, TIMELINE_FOLD_LIMIT, true).visible).toHaveLength(15);
	});

	it('keeps an older selected run visible so the panel and the list agree', () => {
		const folded = foldTimeline(many, TIMELINE_FOLD_LIMIT, false, 'b1');
		expect(folded.visible[0].id).toBe('b1');
		expect(folded.visible).toHaveLength(TIMELINE_FOLD_LIMIT + 1);
	});
});

describe('latest intraday fallback', () => {
	const base = { key: 'k', label: '', trigger: 'scheduled', narrative_status: '', facts: { sources: [] }, started_at: '' } as const;
	const brief = (overrides: Partial<PatrolBrief>) => ({ ...base, ...overrides }) as PatrolBrief;

	it('falls back to the most recent ready brief that carries an intraday snapshot', () => {
		const briefs = [
			brief({ id: 'a', phase: 'pre_open', session_date: '2026-10-21', status: 'ready', facts: { sources: [] } }),
			brief({ id: 'b', phase: 'post_close', session_date: '2026-10-20', status: 'ready', started_at: '2026-10-20T15:10:00Z', facts: { sources: [], intraday: { status: '低位活跃', breadth: '', summary: '', risk_score: 40, limit_up: 54, max_streak: 6, previous_max_streak: 5, high_down_rate: 0.2, high_weak_rate: 0.3 } } }),
		];
		const fallback = latestIntradayFallback(briefs);
		expect(fallback?.intraday.limit_up).toBe(54);
		expect(fallback?.label).toBe('2026-10-20 盘后');
	});

	it('skips running or failed briefs and returns null when nothing carries intraday data', () => {
		expect(latestIntradayFallback([])).toBeNull();
		expect(latestIntradayFallback([brief({ id: 'a', phase: 'intraday', session_date: '2026-10-20', status: 'running', facts: { sources: [] } })])).toBeNull();
	});
});
