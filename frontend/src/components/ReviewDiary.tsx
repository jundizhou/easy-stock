import {
	BookOpen,
	BrainCircuit,
	CalendarDays,
	ExternalLink,
	FileInput,
	GitCompareArrows,
	ListChecks,
	MessageCircleMore,
	Plus,
	RefreshCw,
	Search,
	ShieldAlert,
	Sparkles,
	Target,
	Trash2,
	TrendingUp,
	RadioTower,
	Users,
	Zap,
} from 'lucide-react';
import { FormEvent, ReactNode, useCallback, useEffect, useMemo, useState } from 'react';
import { AppSettings, BackendConfig, ReviewAuthor, ReviewAutomationProfile, ReviewDailyAuthorView, ReviewDailyStockView, ReviewDailySummary, ReviewDailySummaryJob, ReviewPost, ReviewSource, ReviewSubscription, ReviewSyncResult, requestJSON } from '../lib/backend';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
};

type LoadState = 'idle' | 'loading' | 'ready' | 'error';
type ReviewView = 'summary' | 'authors' | 'library';

const sourceTabs: Array<{ id: string; name: string }> = [
	{ id: 'all', name: '全部' },
	{ id: 'official', name: '每日复盘' },
	{ id: 'xueqiu', name: '雪球' },
	{ id: 'taoguba', name: '淘股吧' },
	{ id: 'wechat', name: '微信公众号' },
];

export function ReviewDiary({ config, refreshKey }: Props) {
	const [sources, setSources] = useState<ReviewSource[]>([]);
	const [authors, setAuthors] = useState<ReviewAuthor[]>([]);
	const [posts, setPosts] = useState<ReviewPost[]>([]);
	const [subscriptions, setSubscriptions] = useState<ReviewSubscription[]>([]);
	const [reviewProfiles, setReviewProfiles] = useState<ReviewAutomationProfile[]>([]);
	const [total, setTotal] = useState(0);
	const [activeSource, setActiveSource] = useState('all');
	const [activeAuthor, setActiveAuthor] = useState('all');
	const [selectedID, setSelectedID] = useState('');
	const [query, setQuery] = useState('');
	const [appliedQuery, setAppliedQuery] = useState('');
	const [importURL, setImportURL] = useState('');
	const [state, setState] = useState<LoadState>('idle');
	const [importing, setImporting] = useState(false);
	const [subscriptionSource, setSubscriptionSource] = useState('xueqiu');
	const [subscriptionConfigID, setSubscriptionConfigID] = useState('');
	const [subscriptionName, setSubscriptionName] = useState('');
	const [homepageURL, setHomepageURL] = useState('');
	const [savingSubscription, setSavingSubscription] = useState(false);
	const [syncing, setSyncing] = useState('');
	const [analyzing, setAnalyzing] = useState('');
	const [dailySummary, setDailySummary] = useState<ReviewDailySummary | null>(null);
	const [dailySummaryJob, setDailySummaryJob] = useState<ReviewDailySummaryJob | null>(null);
	const [reviewView, setReviewView] = useState<ReviewView>('library');
	const [summarizingDaily, setSummarizingDaily] = useState(false);
	const [notice, setNotice] = useState('');
	const [error, setError] = useState('');

	const loadDiary = useCallback(async () => {
		if (!config) return;
		setState('loading');
		setError('');
		const params = new URLSearchParams({ limit: '80' });
		if (activeSource !== 'all') params.set('source', activeSource);
		if (activeAuthor !== 'all') params.set('author_id', activeAuthor);
		if (appliedQuery) params.set('q', appliedQuery);
		try {
			const [sourcePayload, authorPayload, postPayload, subscriptionPayload, settingsPayload, dailySummaryPayload, dailySummaryJobPayload] = await Promise.all([
				requestJSON<{ data: ReviewSource[] }>(config, '/api/v1/reviews/sources'),
				requestJSON<{ data: ReviewAuthor[] }>(config, `/api/v1/reviews/authors?source=${encodeURIComponent(activeSource)}`),
				requestJSON<{ data: ReviewPost[]; total: number }>(config, `/api/v1/reviews/posts?${params.toString()}`),
				requestJSON<{ data: ReviewSubscription[] }>(config, '/api/v1/reviews/subscriptions'),
				requestJSON<{ data: AppSettings }>(config, '/api/v1/settings'),
				requestJSON<{ data: ReviewDailySummary | null }>(config, '/api/v1/reviews/daily-summary'),
				requestJSON<{ data: ReviewDailySummaryJob }>(config, '/api/v1/reviews/daily-summary/status'),
			]);
			setSources(sourcePayload.data || []);
			setAuthors(authorPayload.data || []);
			setPosts(postPayload.data || []);
			setTotal(postPayload.total || 0);
			setSubscriptions(subscriptionPayload.data || []);
			setReviewProfiles((settingsPayload.data.review_automation?.profiles || []).filter((profile) => profile.enabled));
			setDailySummary(dailySummaryPayload.data || null);
			setDailySummaryJob(dailySummaryJobPayload.data || null);
			setSelectedID((current) => postPayload.data.some((post) => post.id === current) ? current : postPayload.data[0]?.id || '');
			setState('ready');
		} catch (loadError) {
			setState('error');
			setError(loadError instanceof Error ? loadError.message : '复盘日记加载失败');
		}
	}, [activeAuthor, activeSource, appliedQuery, config]);

	useEffect(() => {
		void loadDiary();
	}, [loadDiary, refreshKey]);

	useEffect(() => {
		const available = reviewProfiles.filter((profile) => profile.source === subscriptionSource);
		if (!available.some((profile) => profile.id === subscriptionConfigID)) setSubscriptionConfigID(available[0]?.id || '');
	}, [reviewProfiles, subscriptionConfigID, subscriptionSource]);

	const dailySummaryRunning = dailySummaryJob?.status === 'running';

	useEffect(() => {
		if (!config || !dailySummaryRunning) return;
		let active = true;
		const poll = async () => {
			try {
				const payload = await requestJSON<{ data: ReviewDailySummaryJob }>(config, '/api/v1/reviews/daily-summary/status');
				if (!active) return;
				if (payload.data.status === 'succeeded' && payload.data.summary_available) {
					const summaryPayload = await requestJSON<{ data: ReviewDailySummary | null }>(config, '/api/v1/reviews/daily-summary');
					if (!active) return;
					setDailySummary(summaryPayload.data || null);
					setNotice('今日大V观点总结已完成并缓存，可随时查看结果');
				}
				setDailySummaryJob(payload.data);
			} catch {
				// A later poll or returning to this page will recover persisted status.
			}
		};
		void poll();
		const timer = window.setInterval(() => void poll(), 3000);
		return () => { active = false; window.clearInterval(timer); };
	}, [config, dailySummaryRunning]);

	const selectedPost = useMemo(
		() => posts.find((post) => post.id === selectedID) || posts[0] || null,
		[posts, selectedID],
	);

	const sourceMap = useMemo(() => new Map(sources.map((source) => [source.id, source])), [sources]);

	const submitSearch = (event: FormEvent) => {
		event.preventDefault();
		setAppliedQuery(query.trim());
	};

	const addSubscription = async (event: FormEvent) => {
		event.preventDefault();
		if (!config || !homepageURL.trim()) return;
		setSavingSubscription(true); setError(''); setNotice('');
		try {
			await requestJSON(config, '/api/v1/reviews/subscriptions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ source: subscriptionSource, homepage_url: homepageURL.trim(), name: subscriptionName.trim(), config_id: subscriptionConfigID }) });
			setHomepageURL(''); setSubscriptionName(''); setNotice('订阅已添加，点击“立即同步”可马上抓取'); await loadDiary();
		} catch (cause) { setError(cause instanceof Error ? readableAPIError(cause.message) : '添加订阅失败'); } finally { setSavingSubscription(false); }
	};

	const syncSubscriptions = async (id = '') => {
		if (!config) return; setSyncing(id || 'all'); setError(''); setNotice('');
		try {
			const path = id ? `/api/v1/reviews/subscriptions/${id}/sync` : '/api/v1/reviews/sync';
			const payload = await requestJSON<{ data: ReviewSyncResult | ReviewSyncResult[] }>(config, path, { method: 'POST' });
			const results = Array.isArray(payload.data) ? payload.data : [payload.data];
			const imported = results.reduce((sum, item) => sum + (item.imported || 0), 0); const analyzed = results.reduce((sum, item) => sum + (item.analyzed || 0), 0);
			const syncErrors = results.map((item) => item.error).filter(Boolean);
			if (syncErrors.length) setError(`同步未完成：${syncErrors.join('；')}${imported ? `；已新增 ${imported} 篇` : ''}`);
			else setNotice(`同步完成：新增 ${imported} 篇，AI 提炼 ${analyzed} 篇`);
			await loadDiary();
		} catch (cause) { setError(cause instanceof Error ? readableAPIError(cause.message) : '同步失败'); } finally { setSyncing(''); }
	};

	const deleteSubscription = async (id: string) => {
		if (!config) return; setError('');
		try { await requestJSON(config, `/api/v1/reviews/subscriptions/${id}`, { method: 'DELETE' }); await loadDiary(); } catch (cause) { setError(cause instanceof Error ? readableAPIError(cause.message) : '删除订阅失败'); }
	};

	const analyzePost = async (id: string) => {
		if (!config) return; setAnalyzing(id); setError('');
		try { const payload = await requestJSON<{ data: ReviewPost }>(config, `/api/v1/reviews/posts/${id}/analyze`, { method: 'POST' }); setPosts((current) => current.map((post) => post.id === id ? payload.data : post)); } catch (cause) { setError(cause instanceof Error ? readableAPIError(cause.message) : 'AI 提炼失败'); } finally { setAnalyzing(''); }
	};

	const summarizeToday = async (regenerate = false) => {
		if (!config) return;
		if (dailySummaryRunning) {
			window.setTimeout(() => document.getElementById('daily-summary-job')?.scrollIntoView({ behavior: 'smooth', block: 'center' }), 30);
			return;
		}
		if (dailySummary && !regenerate) {
			setReviewView('summary');
			window.setTimeout(() => document.getElementById('daily-viewpoint-summary')?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 30);
			return;
		}
		setSummarizingDaily(true); setError(''); setNotice('');
		try {
			const payload = await requestJSON<{ data: ReviewDailySummaryJob }>(config, '/api/v1/reviews/daily-summary', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ force: regenerate }) });
			setDailySummaryJob(payload.data);
			if (payload.data.status === 'succeeded' && payload.data.summary_available) {
				setReviewView('summary');
				setNotice('今日观点总结已从本机缓存加载');
				window.setTimeout(() => document.getElementById('daily-viewpoint-summary')?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 30);
			} else {
				setNotice('AI总结任务已提交，预计需要几分钟；你可以先浏览其他页面，稍后回来查看结果');
				window.setTimeout(() => document.getElementById('daily-summary-job')?.scrollIntoView({ behavior: 'smooth', block: 'center' }), 30);
			}
		} catch (cause) {
			setError(cause instanceof Error ? readableAPIError(cause.message) : '今日大V观点总结失败');
		} finally {
			setSummarizingDaily(false);
		}
	};

	const submitImport = async (event: FormEvent) => {
		event.preventDefault();
		if (!config || !importURL.trim()) return;
		setImporting(true);
		setError('');
		try {
			const payload = await requestJSON<{ data: ReviewPost }>(config, '/api/v1/reviews/import', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ url: importURL.trim() }),
			});
			setImportURL('');
			setActiveSource('all');
			setActiveAuthor('all');
			setAppliedQuery('');
			setQuery('');
			await loadDiary();
			setSelectedID(payload.data.id);
		} catch (importError) {
			setError(importError instanceof Error ? readableAPIError(importError.message) : '文章导入失败');
		} finally {
			setImporting(false);
		}
	};

	return (
		<section className="review-diary">
			<header className="review-hero">
				<div>
					<div className="eyebrow"><BookOpen size={15} aria-hidden="true" />TRADER REVIEW JOURNAL</div>
					<h2>大V复盘日记</h2>
					<p>先看跨作者结论，再下钻到单个作者与原文证据。</p>
				</div>
				<div className="review-hero-actions">
					<button type="button" className="daily-summary-cta" disabled={summarizingDaily} onClick={() => void summarizeToday()}>
						<span className="daily-summary-cta-icon">{summarizingDaily || dailySummaryRunning ? <RefreshCw className="spin" size={21} /> : <BrainCircuit size={21} />}</span>
						<span><strong>{summarizingDaily ? '正在提交后台任务…' : dailySummaryRunning ? `AI复盘生成中 ${dailySummaryJob?.completed_authors || 0}/${dailySummaryJob?.total_authors || '—'}` : dailySummary ? '查看今日综合复盘' : '生成今日综合复盘'}</strong><small>{dailySummaryRunning ? dailySummaryJob?.message || '预计需要几分钟，可稍后回来查看' : '作者观点、共识分歧、三情景与盘中验证一次生成'}</small></span>
						<Sparkles size={17} />
					</button>
					{reviewView === 'library' && <form className="review-import" onSubmit={submitImport}>
						<FileInput size={16} aria-hidden="true" />
						<input value={importURL} onChange={(event) => setImportURL(event.target.value)} placeholder="粘贴具体的微信文章链接，或雪球/淘股吧文章链接" aria-label="文章链接" />
						<button type="submit" disabled={importing || !importURL.trim()}>{importing ? <RefreshCw className="spin" size={15} /> : <span>导入文章</span>}</button>
					</form>}
				</div>
			</header>

			<nav className="review-view-tabs" aria-label="复盘日记视图">
				<button type="button" className={reviewView === 'library' ? 'active' : ''} onClick={() => setReviewView('library')}><BookOpen size={16} /><span><strong>原文资料</strong><small>{total} 篇本地文章</small></span></button>
				<button type="button" className={reviewView === 'summary' ? 'active' : ''} onClick={() => setReviewView('summary')}><BrainCircuit size={16} /><span><strong>AI总结</strong><small>结论、情景与验证</small></span></button>
				<button type="button" className={reviewView === 'authors' ? 'active' : ''} onClick={() => setReviewView('authors')}><Users size={16} /><span><strong>作者观点</strong><small>{dailySummary?.author_views?.length || 0} 位作者观点卡</small></span></button>
			</nav>

			{dailySummaryJob && dailySummaryJob.status !== 'idle' && <DailySummaryJobPanel job={dailySummaryJob} onView={() => { setReviewView('summary'); window.setTimeout(() => document.getElementById('daily-viewpoint-summary')?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 30); }} onRetry={() => void summarizeToday(true)} />}

			{error && <div className="review-error"><ShieldAlert size={16} /><span>{error}</span>{!isWechatListUnavailableMessage(error) && <button type="button" onClick={() => void loadDiary()}>刷新页面</button>}</div>}
			{notice && <div className="review-notice"><Sparkles size={15} /><span>{notice}</span></div>}

			{reviewView === 'summary' && (dailySummary
				? <DailyViewpointSummary summary={dailySummary} regenerating={summarizingDaily || dailySummaryRunning} onRegenerate={() => void summarizeToday(true)} onShowAuthors={() => setReviewView('authors')} />
				: <ReviewSummaryEmpty running={!!dailySummaryRunning} onGenerate={() => void summarizeToday()} onOpenLibrary={() => setReviewView('library')} />)}

			{reviewView === 'authors' && (dailySummary?.author_views?.length
				? <AuthorViewpointLibrary views={dailySummary.author_views} tradeDate={dailySummary.trade_date} />
				: <ReviewSummaryEmpty running={!!dailySummaryRunning} onGenerate={() => void summarizeToday()} onOpenLibrary={() => setReviewView('library')} authors />)}

			{reviewView === 'library' && <>
				<nav className="review-source-tabs" aria-label="复盘内容平台">
					{sourceTabs.map((tab) => {
						const status = sourceMap.get(tab.id);
						return <button type="button" className={activeSource === tab.id ? 'active' : ''} onClick={() => { setActiveSource(tab.id); setActiveAuthor('all'); }} key={tab.id}><span>{tab.name}</span>{status && <i className={`source-dot ${status.status}`} title={status.message} />}</button>;
					})}
				</nav>

				<details className="review-automation" open={!subscriptions.length}>
					<summary><span><RadioTower size={16} /><strong>自动订阅与每日同步</strong><small>{subscriptions.length ? `${subscriptions.length} 个主页 · 后台定时检查` : '添加大V主页后，系统会每天自动获取新文章'}</small></span><button type="button" disabled={!subscriptions.length || !!syncing} onClick={(event) => { event.preventDefault(); void syncSubscriptions(); }}>{syncing === 'all' ? <RefreshCw className="spin" size={14} /> : <RefreshCw size={14} />}立即同步</button></summary>
					<div className="review-automation-body">
						<div className="review-wechat-import-hint"><FileInput size={16} /><span><strong>微信公众号自动订阅暂不可用</strong><small>微信已停用历史文章列表接口；已知文章仍可将具体链接粘贴到上方“导入文章”。</small></span></div>
						<form className="review-subscription-form" onSubmit={addSubscription}>
							<select value={subscriptionSource} onChange={(event) => setSubscriptionSource(event.target.value)}><option value="xueqiu">雪球</option><option value="taoguba">淘股吧</option><option value="wechat" disabled>微信公众号（仅链接导入）</option></select>
							<select value={subscriptionConfigID} onChange={(event) => setSubscriptionConfigID(event.target.value)} aria-label="采集配置">{reviewProfiles.filter((profile) => profile.source === subscriptionSource).map((profile) => <option value={profile.id} key={profile.id}>{profile.name}</option>)}</select>
							<input value={subscriptionName} onChange={(event) => setSubscriptionName(event.target.value)} placeholder={subscriptionSource === 'wechat' ? '公众号名称（推荐填写）' : '作者名称（可选）'} />
							<input value={homepageURL} onChange={(event) => setHomepageURL(event.target.value)} placeholder={subscriptionSource === 'wechat' ? '公众号名称或 FakeID' : '粘贴大V主页地址'} />
							<button type="submit" disabled={!homepageURL.trim() || !subscriptionConfigID || savingSubscription}>{savingSubscription ? <RefreshCw className="spin" size={14} /> : <Plus size={14} />}添加订阅</button>
						</form>
						<div className="review-subscription-list">{subscriptions.map((sub) => <div key={sub.id}><span className={`author-avatar ${sub.source}`}>{sub.name.slice(0, 1)}</span><span><strong>{sub.name}</strong><small>{sourceLabel(sub.source)} · {reviewProfiles.find((profile) => profile.id === sub.config_id)?.name || '默认配置'} · {sub.last_sync_at ? `上次 ${formatDateTime(sub.last_sync_at)}` : '等待首次同步'}{sub.last_error ? ` · ${sub.last_error}` : ''}</small></span><i className={`source-dot ${sub.last_status === 'ok' ? 'ready' : sub.last_status === 'error' ? 'limited' : 'experimental'}`} /><button type="button" title="立即同步该主页" disabled={!!syncing} onClick={() => void syncSubscriptions(sub.id)}>{syncing === sub.id ? <RefreshCw className="spin" size={14} /> : <RefreshCw size={14} />}</button><button type="button" title="删除订阅" onClick={() => void deleteSubscription(sub.id)}><Trash2 size={14} /></button></div>)}</div>
					</div>
				</details>

				<div className="review-layout">
				<aside className="review-author-rail">
					<div className="review-section-heading"><div><span>关注列表</span><h3>复盘作者</h3></div><Users size={17} /></div>
					<div className="review-author-list">
						<button type="button" className={activeAuthor === 'all' ? 'active' : ''} onClick={() => setActiveAuthor('all')}><span className="author-avatar all"><Users size={15} /></span><span><strong>全部作者</strong><small>{total} 篇复盘</small></span></button>
						{authors.map((author) => <button type="button" className={activeAuthor === author.id ? 'active' : ''} onClick={() => setActiveAuthor(author.id)} key={`${author.source}-${author.id}`}><span className={`author-avatar ${author.source}`}>{author.name.slice(0, 1)}</span><span><strong>{author.name}</strong><small>{sourceLabel(author.source)} · {author.post_count}篇</small></span></button>)}
					</div>
					<div className="review-source-health">
						<strong>数据源状态</strong>
						{sources.map((source) => <div key={source.id}><i className={`source-dot ${source.status}`} /><span>{source.name}</span><em>{sourceStatusLabel(source.status)}</em></div>)}
					</div>
				</aside>

				<section className="review-feed-panel">
					<div className="review-feed-toolbar">
						<div><span>复盘时间流</span><strong>{total} 篇内容</strong></div>
						<form onSubmit={submitSearch}><Search size={15} /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索作者、标题或正文" /><button type="submit">搜索</button></form>
					</div>
					<div className="review-feed-list">
						{posts.map((post, index) => {
							const day = formatDay(post.published_at);
							const previousDay = index > 0 ? formatDay(posts[index - 1].published_at) : '';
							return <div className="review-feed-entry" key={post.id}>{day !== previousDay && <div className="review-date-divider"><CalendarDays size={14} /><span>{day}</span></div>}<button type="button" className={`review-post-card ${selectedPost?.id === post.id ? 'active' : ''}`} onClick={() => setSelectedID(post.id)}><div className="review-post-meta"><span className={`source-badge ${post.source}`}>{sourceLabel(post.source)}</span><strong>{post.author_name}</strong><time>{formatClock(post.published_at)}</time></div><h3>{post.title}</h3><p>{post.digest || '暂无摘要'}</p><div className="review-post-tags">{post.related_themes.slice(0, 2).map((tag) => <span key={tag}>{tag}</span>)}{post.related_stocks.slice(0, 2).map((tag) => <span key={tag}>{tag}</span>)}</div></button></div>;
						})}
						{state === 'loading' && <div className="review-empty"><RefreshCw className="spin" size={22} /><strong>正在整理复盘内容</strong></div>}
						{state === 'ready' && !posts.length && <div className="review-empty"><MessageCircleMore size={24} /><strong>还没有复盘文章</strong><span>从上方粘贴文章链接开始建立自己的复盘资料库。</span></div>}
					</div>
				</section>

				<article className="review-reader">
					{selectedPost ? <>
						<div className="review-reader-heading"><div><span className={`source-badge ${selectedPost.source}`}>{sourceLabel(selectedPost.source)}</span><small>{selectedPost.author_name} · {formatDateTime(selectedPost.published_at)}</small></div><a href={selectedPost.original_url} target="_blank" rel="noreferrer">查看原文 <ExternalLink size={14} /></a></div>
						<h2>{selectedPost.title}</h2>
						<div className={`review-ai-placeholder ${selectedPost.ai_summary ? 'ready' : ''}`}><Sparkles size={15} /><div><strong>Hermes AI 复盘提炼</strong>{selectedPost.ai_summary ? <><p>{selectedPost.ai_summary}</p>{selectedPost.ai_key_points?.length > 0 && <ul>{selectedPost.ai_key_points.map((point) => <li key={point}>{point}</li>)}</ul>}{selectedPost.ai_outlook && <span><b>后市预期：</b>{selectedPost.ai_outlook}</span>}</> : <span>{selectedPost.ai_error || '在系统设置中配置 Hermes 模型后，可自动提炼核心观点和后市预期。'}</span>}</div><button type="button" disabled={analyzing === selectedPost.id} onClick={() => void analyzePost(selectedPost.id)}>{analyzing === selectedPost.id ? <RefreshCw className="spin" size={14} /> : <Sparkles size={14} />}{selectedPost.ai_summary ? '重新提炼' : '立即提炼'}</button></div>
						<div className="review-article-text">{selectedPost.content_text || selectedPost.digest || '正文暂未获取，请查看原文。'}</div>
						<footer><span>抓取时间 {formatDateTime(selectedPost.fetched_at)}</span><span>观点来源于原作者，不代表系统结论</span></footer>
					</> : <div className="review-reader-empty"><BookOpen size={28} /><strong>选择一篇复盘开始阅读</strong><span>文章正文只展示经过清洗的纯文本，原始页面可通过“查看原文”打开。</span></div>}
				</article>
				</div>
			</>}
		</section>
	);
}

function DailySummaryJobPanel({ job, onView, onRetry }: { job: ReviewDailySummaryJob; onView: () => void; onRetry: () => void }) {
	const running = job.status === 'running';
	const succeeded = job.status === 'succeeded';
	const progress = succeeded ? 100 : job.stage === 'finalizing' ? 92 : job.stage === 'authors' && job.total_authors > 0 ? Math.min(85, 10 + Math.round((job.completed_authors / job.total_authors) * 75)) : job.stage === 'preparing' ? 6 : 0;
	const stageLabel = job.stage === 'preparing' ? '筛选有效文章' : job.stage === 'authors' ? '归纳作者观点' : job.stage === 'finalizing' ? '生成跨作者共识' : succeeded ? '总结已完成' : '任务未完成';
	return <section className={`daily-summary-job ${job.status}`} id="daily-summary-job">
		<div className="daily-summary-job-icon">{running ? <RefreshCw className="spin" size={22} /> : succeeded ? <Sparkles size={22} /> : <ShieldAlert size={22} />}</div>
		<div className="daily-summary-job-copy"><span>AI 总结任务 · {stageLabel}</span><strong>{running ? '总结耗时较长，可以先去浏览其他内容' : succeeded ? '结果已生成并缓存在本机' : '本次总结已停止'}</strong><p>{job.error || job.message}</p>{running && <div className="daily-summary-job-progress"><i style={{ width: `${progress}%` }} /><small>{job.total_authors > 0 ? `${job.completed_authors}/${job.total_authors} 位作者` : '正在准备文章'} · {progress}%</small></div>}</div>
		<div className="daily-summary-job-actions">{job.summary_available && <button type="button" onClick={onView}>查看缓存结果</button>}{!running && !succeeded && <button type="button" onClick={onRetry}>重新生成</button>}{running && <small>离开此页面不会中断</small>}</div>
	</section>;
}

function DailyViewpointSummary({ summary, regenerating, onRegenerate, onShowAuthors }: { summary: ReviewDailySummary; regenerating: boolean; onRegenerate: () => void; onShowAuthors: () => void }) {
	const playbook = summary.tomorrow_playbook || { pre_open: [], opening: [], intraday: [], close: [] };
	const framework = summary.market_framework || { cycle: '', capital_pricing: '', direction_competition: '', trading_method: '' };
	const authorViews = summary.author_views || [];
	return <section className="daily-viewpoint-summary" id="daily-viewpoint-summary">
		<header className="daily-summary-heading">
			<div><span><BrainCircuit size={15} />AI DAILY REVIEW</span><h2>{summary.trade_date} 市场复盘与次日剧本</h2><small>{summary.author_count} 位作者 / {summary.article_count} 篇有效文章 · {formatDateTime(summary.generated_at)} 生成</small>{summary.window_start && <small className="daily-summary-window">有效窗口 {formatSummaryWindow(summary.window_start, summary.window_end)} · {summary.freshness_rule}</small>}</div>
			<div><button type="button" onClick={onShowAuthors}><Users size={14} />查看作者观点</button><button type="button" onClick={onRegenerate} disabled={regenerating}>{regenerating ? <RefreshCw className="spin" size={14} /> : <RefreshCw size={14} />}重新生成</button></div>
		</header>
		<div className="daily-summary-lead"><span>{summary.market_regime || '样本不足'}</span><div><small>一句话市场结论</small><p>{summary.executive_summary}</p></div></div>
		<section className="daily-summary-block market-framework-block"><header><TrendingUp size={16} /><div><strong>市场四层框架</strong><small>周期位置、资金定价、方向竞争与执行方法分别判断</small></div></header><div className="market-framework-grid"><FrameworkItem label="周期位置" content={framework.cycle} /><FrameworkItem label="资金定价" content={framework.capital_pricing} /><FrameworkItem label="方向竞争" content={framework.direction_competition} /><FrameworkItem label="交易方法" content={framework.trading_method} /></div></section>
		<div className="daily-summary-two-column">
			<SummaryTextCard icon={<TrendingUp size={16} />} title="今日盘面分析" content={summary.market_analysis} />
			<SummaryTextCard icon={<Target size={16} />} title="明日预期" content={summary.tomorrow_outlook} />
		</div>
		{authorViews.length > 0 && <section className="daily-summary-block author-preview-block"><header><Users size={16} /><div><strong>主要作者观点</strong><small>先看各自最终判断，避免用跨作者结论抹平差异</small></div><button type="button" onClick={onShowAuthors}>查看全部 {authorViews.length} 位</button></header><div className="author-preview-grid">{authorViews.slice(0, 6).map((view) => <article key={`${view.source}-${view.author}`}><div><span className={`author-avatar ${sourceClass(view.source)}`}>{view.author.slice(0, 1)}</span><div><strong>{view.author}</strong><small>{view.source} · {view.confidence || '未评级'}置信度</small></div></div><p>{view.core_view}</p><div>{(view.themes || []).slice(0, 3).map((theme) => <span key={theme}>{theme}</span>)}</div></article>)}</div></section>}
		<section className="daily-summary-block consensus-block">
			<header><Zap size={16} /><div><strong>跨作者高频共识</strong><small>至少两位独立作者共同支持，按作者数而非文章数计票</small></div></header>
			<div className="daily-consensus-grid">{summary.consensus?.length ? summary.consensus.map((item) => <article key={`${item.topic}-${item.conclusion}`}><div><strong>{item.topic}</strong><em>{item.support_count} 位作者</em></div><p>{item.conclusion}</p><small>{item.authors.join(' · ')}</small>{item.evidence?.length > 0 && <ul>{item.evidence.slice(0, 3).map((evidence) => <li key={evidence}>{evidence}</li>)}</ul>}</article>) : <SummaryEmpty text="今日文章尚未形成两位以上作者共同支持的明确共识。" />}</div>
		</section>
		<section className="daily-summary-block disagreement-table-block"><header><GitCompareArrows size={16} /><div><strong>共识之外的关键分歧</strong><small>按作者保留原立场，不把分歧平均成一句空泛结论</small></div></header><div className="daily-disagreement-table">{summary.disagreements?.length ? summary.disagreements.map((item) => <article key={item.topic}><h3>{item.topic}</h3>{item.positions?.length ? item.positions.map((position) => <div key={`${position.author}-${position.view}`}><strong>{position.author}</strong><em>{position.stance || '观点'}</em><p>{position.view}</p>{position.evidence && <small>{position.evidence}</small>}</div>) : item.views.map((view, index) => <div key={view}><strong>{item.authors[index] || '作者观点'}</strong><p>{view}</p></div>)}</article>) : <SummaryEmpty text="今日样本未发现清晰的跨作者分歧。" />}</div></section>
		<section className="daily-summary-block scenario-block"><header><Target size={16} /><div><strong>次日三情景推演</strong><small>预期不是一个点位，而是三条可以被盘面证伪的路径</small></div></header><div className="daily-scenario-grid">{summary.scenarios?.length ? summary.scenarios.map((scenario) => <article className={scenario.key} key={scenario.key}><span>{scenario.name}</span><p>{scenario.summary}</p><dl><div><dt>触发</dt><dd>{scenario.trigger || '未明确'}</dd></div><div><dt>确认</dt><dd>{scenario.confirmation || '未明确'}</dd></div><div><dt>失效</dt><dd>{scenario.invalidation || '未明确'}</dd></div></dl>{scenario.focus?.length > 0 && <footer>{scenario.focus.map((item) => <em key={item}>{item}</em>)}</footer>}</article>) : <SummaryEmpty text="当前样本未形成完整的基础、偏强、偏弱三情景。" />}</div></section>
		<section className="daily-summary-block direction-block"><header><GitCompareArrows size={16} /><div><strong>题材与方向优先级</strong><small>同时展示支持者、反对者、确认与失效条件</small></div></header><div className="daily-direction-list">{summary.directions?.length ? summary.directions.map((direction) => <article key={direction.name}><div><strong>{direction.name}</strong><em>{direction.stance || '等待验证'}</em></div><p>{direction.summary}</p>{direction.stocks?.length > 0 && <div className="direction-stock-tags">{direction.stocks.map((stock) => <span key={stock}>{stock}</span>)}</div>}<dl><div><dt>支持</dt><dd>{direction.supporting_authors?.join(' · ') || '无'}</dd></div><div><dt>保留/反对</dt><dd>{direction.opposing_authors?.join(' · ') || '无'}</dd></div><div><dt>确认</dt><dd>{direction.trigger || '未明确'}</dd></div><div><dt>失效</dt><dd>{direction.invalidation || '未明确'}</dd></div></dl>{direction.risks?.length > 0 && <small><b>风险：</b>{direction.risks.join('；')}</small>}</article>) : <SummaryEmpty text="作者观点尚不足以形成有来源的方向优先级。" />}</div></section>
		<div className="daily-summary-two-column stock-view-columns">
			<StockViewSection title="今日超预期个股" subtitle="文章明确提到主动走强、逆势承接或超出盘前预期" items={summary.today_surprises || []} tone="surprise" />
			<StockViewSection title="明日预期个股" subtitle="只保留有逻辑、触发条件和失效条件的观察对象" items={summary.tomorrow_focus || []} tone="focus" />
		</div>
		<section className="daily-summary-block playbook-block">
			<header><ListChecks size={16} /><div><strong>明日观察剧本</strong><small>把预测拆成盘前到收盘的可验证条件</small></div></header>
			<div className="daily-playbook-grid"><PlaybookStage label="竞价 / 盘前" items={playbook.pre_open} /><PlaybookStage label="开盘前30分钟" items={playbook.opening} /><PlaybookStage label="盘中确认" items={playbook.intraday} /><PlaybookStage label="收盘验证" items={playbook.close} /></div>
		</section>
		<section className="daily-summary-block"><header><ShieldAlert size={16} /><div><strong>催化与风险</strong><small>重点防范共识拥挤和盘后叙事偏差</small></div></header><div className="daily-risk-grid"><SummaryBulletList title="潜在催化" items={summary.catalysts || []} /><SummaryBulletList title="主要风险" items={summary.risks || []} /></div></section>
		<section className="daily-summary-block verification-block"><header><ListChecks size={16} /><div><strong>明日验证清单</strong><small>逐条核对，而不是把预期当成结论</small></div></header><ol>{(summary.verification_checklist || []).map((item) => <li key={item}>{item}</li>)}</ol></section>
		{summary.sources?.length > 0 && <section className="daily-summary-block daily-sources-block"><header><BookOpen size={16} /><div><strong>来源与证据样本</strong><small>链接回到原作者文章，便于复核 AI 归纳是否准确</small></div></header><div>{summary.sources.slice(0, 12).map((source, index) => source.url ? <a href={source.url} target="_blank" rel="noreferrer" key={`${source.post_id || source.url}-${index}`}><strong>{source.author}</strong><span>{source.title}</span><ExternalLink size={13} /></a> : <span key={`${source.author}-${source.title}-${index}`}><strong>{source.author}</strong>{source.title}</span>)}</div></section>}
		{summary.limitations?.length > 0 && <footer><strong>样本局限</strong><span>{summary.limitations.join('；')}</span></footer>}
	</section>;
}

function FrameworkItem({ label, content }: { label: string; content: string }) {
	return <article><span>{label}</span><p>{content || '样本不足，尚不能形成明确判断。'}</p></article>;
}

function ReviewSummaryEmpty({ running, onGenerate, onOpenLibrary, authors = false }: { running: boolean; onGenerate: () => void; onOpenLibrary: () => void; authors?: boolean }) {
	return <section className="review-summary-empty"><div><BrainCircuit size={34} /><span>{running ? 'Hermes 正在处理作者观点' : authors ? '还没有可展示的作者观点卡' : '还没有今天的结构化复盘'}</span><strong>{running ? '任务完成后会自动出现在这里' : '先从本地原文资料生成一份可验证的综合复盘'}</strong><p>结果将包含作者独立观点、跨作者共识与分歧、三种次日情景、题材优先级和盘中验证清单。</p><div>{!running && <button type="button" onClick={onGenerate}><Sparkles size={15} />生成今日复盘</button>}<button type="button" className="secondary" onClick={onOpenLibrary}><BookOpen size={15} />查看原文资料</button></div></div></section>;
}

function AuthorViewpointLibrary({ views, tradeDate }: { views: ReviewDailyAuthorView[]; tradeDate: string }) {
	return <section className="author-viewpoint-library"><header><div><span>AUTHOR VIEWPOINTS</span><h2>{tradeDate} 作者独立观点</h2><p>每位作者最多一票；多篇文章先在作者内部去重与修正，再参与跨作者统计。</p></div><em>{views.length} 位作者</em></header><div>{views.map((view) => <AuthorViewpointCard key={`${view.source}-${view.author}`} view={view} />)}</div></section>;
}

function AuthorViewpointCard({ view }: { view: ReviewDailyAuthorView }) {
	return <article className="author-viewpoint-card">
		<header><span className={`author-avatar ${sourceClass(view.source)}`}>{view.author.slice(0, 1)}</span><div><h3>{view.author}</h3><small>{view.source} · 采用 {view.article_count}/{view.available_article_count} 篇 · {view.time_range}</small></div><em className={`confidence-${view.confidence || 'unknown'}`}>{view.confidence || '未评级'}置信度</em></header>
		<section><div className="author-core-view"><span>最终核心观点</span><p>{view.core_view}</p></div><div className="author-market-view"><span>盘面解释</span><p>{view.market_interpretation || '未明确'}</p></div></section>
		{view.themes?.length > 0 && <div className="author-theme-tags">{view.themes.map((theme) => <span key={theme}>{theme}</span>)}</div>}
		{view.view_evolution?.length > 0 && <div className="author-evolution"><strong>观点演变</strong><ol>{view.view_evolution.map((item) => <li key={item}>{item}</li>)}</ol></div>}
		<div className="author-card-columns"><div><strong>明日预期</strong><p>{view.tomorrow_outlook || '未明确'}</p><SummaryBulletList title="催化" items={view.catalysts || []} /><SummaryBulletList title="风险" items={view.risks || []} /></div><StockViewSection title="明日关注" subtitle="该作者明确给出的观察对象" items={view.tomorrow_focus || []} tone="focus" /></div>
		{view.evidence?.length > 0 && <details><summary>查看观点证据（{view.evidence.length}）</summary><ul>{view.evidence.map((item) => <li key={item}>{item}</li>)}</ul></details>}
		{view.sources?.length > 0 && <footer>{view.sources.map((source, index) => source.url ? <a href={source.url} target="_blank" rel="noreferrer" key={`${source.post_id || source.url}-${index}`}><span>{source.title}</span><small>{source.published_at ? formatDateTime(source.published_at) : source.source}</small><ExternalLink size={13} /></a> : <span key={`${source.title}-${index}`}>{source.title}</span>)}</footer>}
	</article>;
}

function SummaryTextCard({ icon, title, content }: { icon: ReactNode; title: string; content: string }) {
	return <section className="daily-summary-block summary-text-card"><header>{icon}<strong>{title}</strong></header><p>{content || '文章未形成明确结论。'}</p></section>;
}

function StockViewSection({ title, subtitle, items, tone }: { title: string; subtitle: string; items: ReviewDailyStockView[]; tone: string }) {
	return <section className={`daily-summary-block stock-view-section ${tone}`}><header><Target size={16} /><div><strong>{title}</strong><small>{subtitle}</small></div></header><div className="daily-stock-view-list">{items.length ? items.map((item) => <article key={`${item.name}-${item.logic}`}><div><strong>{item.name}</strong>{item.symbol && <span>{item.symbol}</span>}<em>{item.support_count > 1 ? `${item.support_count}位共识` : '单一来源'}</em></div><p>{item.logic}</p>{item.trigger && <small><b>确认：</b>{item.trigger}</small>}{item.invalidation && <small><b>失效：</b>{item.invalidation}</small>}{item.risk && <small><b>风险：</b>{item.risk}</small>}<footer>{item.authors.join(' · ')}</footer></article>) : <SummaryEmpty text="文章没有提供足够证据。" />}</div></section>;
}

function PlaybookStage({ label, items }: { label: string; items: string[] }) {
	return <article><strong>{label}</strong>{items?.length ? <ul>{items.map((item) => <li key={item}>{item}</li>)}</ul> : <span>暂无明确信号</span>}</article>;
}

function SummaryBulletList({ title, items }: { title: string; items: string[] }) {
	return <div><strong>{title}</strong>{items.length ? <ul>{items.map((item) => <li key={item}>{item}</li>)}</ul> : <span>文章未明确提及</span>}</div>;
}

function SummaryEmpty({ text }: { text: string }) {
	return <div className="daily-summary-empty">{text}</div>;
}

function sourceLabel(source: string) {
	return source === 'official' ? '每日复盘' : source === 'wechat' ? '微信公众号' : source === 'xueqiu' ? '雪球' : source === 'taoguba' ? '淘股吧' : source;
}

function sourceClass(source: string) {
	if (source.includes('微信')) return 'wechat';
	if (source.includes('雪球')) return 'xueqiu';
	if (source.includes('淘股吧')) return 'taoguba';
	if (source.includes('每日复盘')) return 'official';
	return source.toLowerCase();
}

function sourceStatusLabel(status: string) {
	if (status === 'ready' || status === 'configured') return '可用';
	if (status === 'limited') return '受限';
	if (status === 'experimental') return '实验';
	return status;
}

function formatDay(value: string) {
	return new Date(value).toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit', weekday: 'short' });
}

function formatClock(value: string) {
	return new Date(value).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
}

function formatDateTime(value: string) {
	return new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
}

function formatSummaryWindow(start: string, end: string) {
	return `${formatDateTime(start)} 至 ${formatDateTime(end)}`;
}

function readableAPIError(message: string) {
	try {
		const parsed = JSON.parse(message) as { error?: string; data?: { error?: string } };
		return parsed.error || parsed.data?.error || message;
	} catch {
		return message;
	}
}

function isWechatListUnavailableMessage(message: string) {
	const normalized = message.toLowerCase();
	return normalized.includes('微信已停用公众号历史文章列表接口') || normalized.includes('ret=200013') || normalized.includes('freq control');
}
