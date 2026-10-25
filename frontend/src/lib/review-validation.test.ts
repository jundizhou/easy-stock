import { describe, expect, it } from 'vitest';
import type { ReviewDailyValidationJob } from './backend';
import { validationResultMessage } from './review-validation';

const job = (status: string, message = ''): ReviewDailyValidationJob => ({ summary_date: '2026-10-23', status, stage: status, message, result_available: false });

describe('validation result states', () => {
	it('does not report idle as completed', () => {
		expect(validationResultMessage(job('idle'))).toContain('尚未运行');
		expect(validationResultMessage(null)).not.toContain('已完成');
	});
	it('preserves the waiting date and failure reason', () => {
		expect(validationResultMessage(job('waiting', '核验日 2026-10-26（周一）15:00 收盘后可开始验证'))).toContain('2026-10-26');
		expect(validationResultMessage({ ...job('failed'), error: '上次验证已中断' })).toBe('上次验证已中断');
	});
	it('only describes missing completed results as a cache error', () => {
		expect(validationResultMessage(job('succeeded'))).toContain('缓存读取失败');
		expect(validationResultMessage(job('running', '正在采集'))).toBe('正在采集');
	});
});
