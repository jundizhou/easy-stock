import { useCallback, useEffect, useState } from 'react';
import { requestJSON, type BackendConfig, type ReviewDailySummary, type ReviewDailySummaryHistoryItem } from './backend';

const pageSize = 30;

export function useReviewSummaryHistory(config: BackendConfig | null, currentSummary: ReviewDailySummary | null, refreshKey: number) {
	const [items, setItems] = useState<ReviewDailySummaryHistoryItem[]>([]);
	const [total, setTotal] = useState(0);
	const [offset, setOffset] = useState(0);
	const [latestDate, setLatestDate] = useState('');
	const [selectedDate, setSelectedDate] = useState('');
	const [savedSummary, setSavedSummary] = useState<ReviewDailySummary | null>(null);
	const [loading, setLoading] = useState(!!config);
	const [listError, setListError] = useState('');
	const [summaryError, setSummaryError] = useState('');
	const [revision, setRevision] = useState(0);
	const displayDate = selectedDate || currentSummary?.trade_date || latestDate;
	const summary = displayDate === currentSummary?.trade_date ? currentSummary
		: savedSummary?.trade_date === displayDate ? savedSummary : null;
	const savedGeneration = items.find((item) => item.trade_date === displayDate)?.generated_at;

	useEffect(() => {
		if (!config) return;
		const controller = new AbortController();
		setLoading(true);
		setListError('');
		requestJSON<{ data: ReviewDailySummaryHistoryItem[]; total: number }>(config, `/api/v1/reviews/daily-summary/history?limit=${pageSize}&offset=${offset}`, { signal: controller.signal })
			.then((payload) => {
				if (controller.signal.aborted) return;
				setItems(payload.data);
				setTotal(payload.total);
				if (offset === 0) setLatestDate(payload.data[0]?.trade_date || '');
			})
			.catch((cause) => {
				if (!controller.signal.aborted) setListError(cause instanceof Error ? cause.message : '读取历史复盘失败');
			})
			.finally(() => { if (!controller.signal.aborted) setLoading(false); });
		return () => controller.abort();
	}, [config, currentSummary?.generated_at, offset, refreshKey, revision]);

	useEffect(() => {
		if (!config || !displayDate || displayDate === currentSummary?.trade_date) return;
		const controller = new AbortController();
		setSummaryError('');
		requestJSON<{ data: ReviewDailySummary }>(config, `/api/v1/reviews/daily-summary?trade_date=${encodeURIComponent(displayDate)}`, { signal: controller.signal })
			.then((payload) => { if (!controller.signal.aborted) setSavedSummary(payload.data); })
			.catch((cause) => {
				if (!controller.signal.aborted) setSummaryError(cause instanceof Error ? cause.message : '读取已保存的复盘失败');
			});
		return () => controller.abort();
	}, [config, currentSummary?.trade_date, displayDate, refreshKey, revision, savedGeneration]);

	const selectDate = useCallback((date: string) => {
		setSelectedDate(date);
		setSummaryError('');
	}, []);
	const refresh = useCallback(() => {
		setOffset(0);
		setRevision((value) => value + 1);
	}, []);
	const reset = useCallback(() => {
		setSelectedDate('');
		setSavedSummary(null);
		setLatestDate('');
		setItems([]);
		setTotal(0);
		setSummaryError('');
		refresh();
	}, [refresh]);

	return {
		summary, items, total, offset, pageSize, selectedDate, loading,
		loadingSummary: !summary && !summaryError && (loading || !!displayDate),
		error: summaryError || listError,
		selectDate, setOffset, refresh, reset,
	};
}
