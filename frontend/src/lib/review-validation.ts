import type { ReviewDailyValidationJob } from './backend';

export function validationResultMessage(job?: ReviewDailyValidationJob | null): string {
	if (!job) return '该验证尚未运行，点击开始验证';
	if (job.status === 'failed') return job.error || job.message || '验证失败，请重试';
	if (job.status === 'succeeded') return '验证已完成，但本机缓存读取失败，请重新运行验证';
	return job.message || (job.status === 'running' ? '正在核验，请稍候' : '该验证尚未运行，点击开始验证');
}
