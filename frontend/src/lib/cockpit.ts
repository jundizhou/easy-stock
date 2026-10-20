import type { MarketEmotionHistory, MarketEmotionPoint, MarketIndexSnapshot, PortfolioInspectionJob, SourceHealth } from './backend';

export type CockpitTarget = 'market' | 'limit-up' | 'themes' | 'portfolio-inspection' | 'stock-ai' | 'reviews' | 'mastery' | 'ai' | 'token-usage';

export type AttentionLevel = 'danger' | 'warn' | 'info';

export type AttentionItem = {
	id: string;
	level: AttentionLevel;
	title: string;
	detail: string;
	target: CockpitTarget | null;
};

export type CockpitInput = {
	indexes?: MarketIndexSnapshot[] | null;
	emotion?: MarketEmotionHistory | null;
	inspections?: PortfolioInspectionJob[] | null;
	sources?: SourceHealth[] | null;
};

export const cockpitTargetLabels: Record<CockpitTarget, string> = {
	market: '行情总览',
	'limit-up': '短线连板',
	themes: '趋势题材',
	'portfolio-inspection': '持仓巡检',
	'stock-ai': '个股分析',
	reviews: '大V复盘',
	mastery: '游资心法',
	ai: 'AI 对话',
	'token-usage': 'Token 统计',
};

const INDEX_DROP_THRESHOLD = -1;
const HEALTH_WARN_SCORE = 60;
const LIMIT_DOWN_WARN_COUNT = 5;

export function formatPercent(value: number | undefined | null) {
	if (typeof value !== 'number' || !Number.isFinite(value)) return '--';
	return `${value > 0 ? '+' : ''}${value.toFixed(2)}%`;
}

export function emotionPhaseTone(phase?: string): 'up' | 'down' | 'neutral' {
	if (!phase) return 'neutral';
	if (phase === '退潮' || phase === '冰点') return 'down';
	if (phase === '发酵/主升' || phase === '高潮' || phase === '启动/修复') return 'up';
	return 'neutral';
}

export function latestInspection(jobs: PortfolioInspectionJob[] | null | undefined): PortfolioInspectionJob | null {
	if (!jobs?.length) return null;
	return [...jobs].sort((a, b) => (b.started_at || '').localeCompare(a.started_at || ''))[0];
}

export function emotionTrend(points: MarketEmotionPoint[] | undefined, count = 12): number[] {
	if (!points?.length) return [];
	return points.slice(-count).map((point) => point.emotion_score);
}

export function buildAttentionItems(input: CockpitInput): AttentionItem[] {
	const items: AttentionItem[] = [];

	for (const index of input.indexes || []) {
		if (typeof index.change_percent === 'number' && index.change_percent <= INDEX_DROP_THRESHOLD) {
			items.push({
				id: `index-${index.secid}`,
				level: 'warn',
				title: `${index.name} 下跌 ${Math.abs(index.change_percent).toFixed(2)}%`,
				detail: '大盘走弱，检查持仓与情绪是否同步承压',
				target: 'market',
			});
		}
	}

	const latest = input.emotion?.latest;
	if (latest) {
		if (emotionPhaseTone(latest.phase) === 'down') {
			items.push({
				id: 'emotion-phase',
				level: 'warn',
				title: `短线情绪处于「${latest.phase}」`,
				detail: `情绪分 ${Math.round(latest.emotion_score)}，追高与接力风险上升`,
				target: 'limit-up',
			});
		}
		if (latest.raw && latest.raw.limit_down_count >= LIMIT_DOWN_WARN_COUNT && latest.raw.limit_down_count > latest.raw.limit_up_count) {
			items.push({
				id: 'limit-down-heavy',
				level: 'warn',
				title: '跌停家数多于涨停',
				detail: `跌停 ${latest.raw.limit_down_count} 家，涨停 ${latest.raw.limit_up_count} 家`,
				target: 'limit-up',
			});
		}
	}

	const inspection = latestInspection(input.inspections);
	if (inspection) {
		if (inspection.status === 'failed' || inspection.status === 'interrupted') {
			items.push({
				id: 'inspection-incomplete',
				level: 'warn',
				title: '最近一次持仓巡检未完成',
				detail: inspection.resume_available ? '可在巡检页恢复继续，已完成的研究会复用' : inspection.error || inspection.message || '请进入巡检页查看原因',
				target: 'portfolio-inspection',
			});
		}
		const conclusion = inspection.report?.conclusion;
		if (conclusion) {
			if (conclusion.risk_level?.includes('高')) {
				items.push({
					id: 'portfolio-risk-high',
					level: 'danger',
					title: `持仓风险等级：${conclusion.risk_level}`,
					detail: conclusion.primary_risks?.[0] || conclusion.executive_summary || '进入巡检报告查看主要风险',
					target: 'portfolio-inspection',
				});
			} else if (typeof conclusion.health_score === 'number' && conclusion.health_score < HEALTH_WARN_SCORE) {
				items.push({
					id: 'portfolio-health-low',
					level: 'warn',
					title: `持仓健康分 ${conclusion.health_score}，低于 ${HEALTH_WARN_SCORE} 分`,
					detail: conclusion.primary_risks?.[0] || '进入巡检报告查看逐股调整建议',
					target: 'portfolio-inspection',
				});
			}
		}
	}

	const failedSources = (input.sources || []).filter((source) => !source.ok);
	if (failedSources.length) {
		items.push({
			id: 'sources-failed',
			level: 'warn',
			title: `${failedSources.length} 个数据源异常`,
			detail: failedSources.slice(0, 3).map((source) => source.name).join('、'),
			target: 'themes',
		});
	}

	const rank: Record<AttentionLevel, number> = { danger: 0, warn: 1, info: 2 };
	items.sort((a, b) => rank[a.level] - rank[b.level]);
	if (!items.length && (input.indexes || input.emotion || input.inspections || input.sources)) {
		return [{ id: 'all-clear', level: 'info', title: '暂无需要处理的事项', detail: '已加载的模块均未触发提醒规则', target: null }];
	}
	return items;
}

export function buildCockpitBriefPrompt(input: CockpitInput, attention: AttentionItem[]): string {
	const lines: string[] = [];
	const indexes = (input.indexes || []).slice(0, 6).map((index) => `${index.name} ${formatPercent(index.change_percent)}`);
	if (indexes.length) lines.push(`指数：${indexes.join('；')}`);
	const latest = input.emotion?.latest;
	if (latest) {
		lines.push(`情绪：阶段「${latest.phase}」，情绪分 ${Math.round(latest.emotion_score)}，涨停 ${latest.raw?.limit_up_count ?? '--'} 家，跌停 ${latest.raw?.limit_down_count ?? '--'} 家，最高连板 ${latest.raw?.max_streak ?? '--'} 板`);
	}
	const inspection = latestInspection(input.inspections);
	const conclusion = inspection?.report?.conclusion;
	if (conclusion) {
		lines.push(`持仓：健康分 ${conclusion.health_score}，风险等级 ${conclusion.risk_level}，主要风险：${(conclusion.primary_risks || []).slice(0, 3).join('；') || '无'}`);
	}
	const failed = (input.sources || []).filter((source) => !source.ok).map((source) => source.name);
	lines.push(`数据源：${(input.sources || []).length - failed.length}/${(input.sources || []).length || '--'} 正常${failed.length ? `，异常：${failed.join('、')}` : ''}`);
	const alerts = attention.filter((item) => item.level !== 'info').map((item) => `[${item.level === 'danger' ? '高' : '中'}] ${item.title}`);
	lines.push(`需要处理：${alerts.length ? alerts.join('；') : '无'}`);

	return [
		'请基于下面 AI 总控台页面已加载的汇总数据，生成一份 AI 总控简报，按以下结构输出：',
		'1. 市场状态：一句话概括指数与情绪的关系；',
		'2. 需要你处理的事项：按优先级列出，并说明依据来自哪个模块；',
		'3. 持仓风险：如有巡检数据，指出最需要关注的一条；',
		'4. 数据可靠性：标出异常或缺失的数据源，说明哪些结论因此不可靠。',
		'',
		'约束：只使用下列数据，不要编造实时行情或未提供的指标；每个结论标注数据来源模块；结尾注明本简报不构成投资建议。',
		'',
		...lines,
	].join('\n');
}
