import {
	AlertTriangle,
	BookMarked,
	Bot,
	BrainCircuit,
	CheckCircle2,
	Clock3,
	ExternalLink,
	FileText,
	LoaderCircle,
	RefreshCw,
	Search,
	Sparkles,
} from 'lucide-react';
import { Fragment, ReactNode, useCallback, useEffect, useMemo, useState } from 'react';
import {
	BackendConfig,
	MasteryDocument,
	MasterySnapshot,
	MasteryTraderDetail,
	requestJSON,
} from '../lib/backend';

type Props = {
	config: BackendConfig | null;
	refreshKey: number;
	onAskAI: (traderName: string) => void;
};

type LoadState = 'idle' | 'loading' | 'ready' | 'error';

export function TradingMastery({ config, refreshKey, onAskAI }: Props) {
	const [snapshot, setSnapshot] = useState<MasterySnapshot | null>(null);
	const [detail, setDetail] = useState<MasteryTraderDetail | null>(null);
	const [activeTraderID, setActiveTraderID] = useState('');
	const [activeDocumentID, setActiveDocumentID] = useState('');
	const [query, setQuery] = useState('');
	const [indexState, setIndexState] = useState<LoadState>('idle');
	const [detailState, setDetailState] = useState<LoadState>('idle');
	const [error, setError] = useState('');
	const [refreshing, setRefreshing] = useState(false);

	const loadIndex = useCallback(async (force = false) => {
		if (!config) return;
		setIndexState('loading');
		setError('');
		try {
			const path = force ? '/api/v1/short-term/mastery/refresh' : '/api/v1/short-term/mastery';
			const payload = await requestJSON<{ data: MasterySnapshot }>(config, path, force ? { method: 'POST' } : undefined);
			setSnapshot(payload.data);
			setIndexState('ready');
			setActiveTraderID((current) => payload.data.traders.some((item) => item.id === current) ? current : payload.data.traders[0]?.id || '');
		} catch (loadError) {
			setIndexState('error');
			setError(loadError instanceof Error ? loadError.message : '游资心法加载失败');
		}
	}, [config]);

	useEffect(() => {
		void loadIndex();
	}, [loadIndex, refreshKey]);

	useEffect(() => {
		if (!config || !activeTraderID) {
			setDetail(null);
			return;
		}
		let cancelled = false;
		setDetailState('loading');
		requestJSON<{ data: MasteryTraderDetail }>(config, `/api/v1/short-term/mastery/trader?id=${encodeURIComponent(activeTraderID)}`)
			.then((payload) => {
				if (cancelled) return;
				setDetail(payload.data);
				setActiveDocumentID(payload.data.documents[0]?.id || '');
				setDetailState('ready');
			})
			.catch((loadError) => {
				if (cancelled) return;
				setDetail(null);
				setDetailState('error');
				setError(loadError instanceof Error ? loadError.message : '心法正文加载失败');
			});
		return () => { cancelled = true; };
	}, [activeTraderID, config]);

	const visibleTraders = useMemo(() => {
		const keyword = query.trim().toLowerCase();
		if (!keyword) return snapshot?.traders || [];
		return (snapshot?.traders || []).filter((trader) => [trader.name, ...(trader.tags || []), trader.quote || ''].join(' ').toLowerCase().includes(keyword));
	}, [query, snapshot?.traders]);
	const activeDocument = detail?.documents.find((item) => item.id === activeDocumentID) || detail?.documents[0] || null;

	const refresh = async () => {
		setRefreshing(true);
		try {
			await loadIndex(true);
		} finally {
			setRefreshing(false);
		}
	};

	return (
		<section className="mastery-workspace">
			<header className="mastery-hero">
				<div className="mastery-hero-copy">
					<span><BookMarked size={15} />TRADING MASTERY</span>
					<h2>游资心法库</h2>
					<p>按人物阅读交易理念、情绪周期、龙头战法、仓位与执行经验；原始资料每日缓存一次。</p>
				</div>
				<div className="mastery-hero-status">
					<div className={snapshot?.knowledge_status === 'ready' ? 'ready' : 'limited'}>
						{snapshot?.knowledge_status === 'ready' ? <CheckCircle2 size={18} /> : <AlertTriangle size={18} />}
						<span><strong>{snapshot?.knowledge_status === 'ready' ? 'Hermes 知识库已同步' : 'Hermes 知识库受限'}</strong><small>{snapshot ? `${snapshot.traders.length} 位游资 · ${formatDateTime(snapshot.fetched_at)}` : '正在建立本地缓存'}</small></span>
					</div>
					<button type="button" onClick={() => void refresh()} disabled={refreshing || indexState === 'loading'}><RefreshCw className={refreshing ? 'spin' : ''} size={15} />更新资料</button>
				</div>
			</header>

			{snapshot?.knowledge_message && <div className="mastery-warning"><AlertTriangle size={15} /><span>{snapshot.knowledge_message}</span></div>}
			{error && indexState === 'error' && <div className="mastery-error"><AlertTriangle size={19} /><strong>资料库暂时不可用</strong><span>{error}</span><button type="button" onClick={() => void loadIndex()}>重新加载</button></div>}

			<div className="mastery-layout">
				<aside className="mastery-trader-rail">
					<header><div><span>游资人物</span><strong>{snapshot?.traders.length || 0} 位</strong></div><BrainCircuit size={18} /></header>
					<label className="mastery-search"><Search size={15} /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索人物或关键词" /></label>
					<div className="mastery-trader-list">
						{indexState === 'loading' && !snapshot && <div className="mastery-loading"><LoaderCircle className="spin" size={19} /><span>首次读取 GitHub 并建立缓存…</span></div>}
						{visibleTraders.map((trader, index) => (
							<button type="button" className={trader.id === activeTraderID ? 'active' : ''} onClick={() => setActiveTraderID(trader.id)} key={trader.id}>
								<em>{String(index + 1).padStart(2, '0')}</em>
								<span><strong>{trader.name}</strong><small>{trader.tags?.slice(0, 2).join(' · ') || `${trader.document_count} 篇资料`}</small></span>
								<i>{trader.reading_minutes}m</i>
							</button>
						))}
						{indexState === 'ready' && !visibleTraders.length && <div className="mastery-loading"><Search size={18} /><span>没有匹配人物</span></div>}
					</div>
				</aside>

				<article className="mastery-reader">
					{detailState === 'loading' && <div className="mastery-reader-loading"><LoaderCircle className="spin" size={24} /><strong>读取心法正文</strong></div>}
					{detailState === 'error' && <div className="mastery-reader-loading error"><AlertTriangle size={22} /><strong>{error || '正文加载失败'}</strong></div>}
					{detail && detailState === 'ready' && (
						<>
							<header className="mastery-reader-header">
								<div>
									<span>游资心法 / {detail.documents.length} 篇资料</span>
									<h2>{detail.name}</h2>
									{detail.quote && <blockquote>“{detail.quote}”</blockquote>}
									<div className="mastery-tags">{detail.tags?.map((tag) => <span key={tag}>{tag}</span>)}</div>
								</div>
								<div className="mastery-reader-actions">
									<button type="button" className="ask-ai" onClick={() => onAskAI(detail.name)}><Bot size={15} />让 Hermes 研读</button>
									<a href={detail.source_url} target="_blank" rel="noreferrer"><ExternalLink size={14} />查看上游</a>
								</div>
							</header>

							<div className="mastery-metrics">
								<div><FileText size={15} /><span>正文规模</span><strong>{formatChars(detail.character_count)}</strong></div>
								<div><Clock3 size={15} /><span>预计阅读</span><strong>{detail.reading_minutes} 分钟</strong></div>
								<div><Sparkles size={15} /><span>资料状态</span><strong>{detail.placeholder_count ? `${detail.placeholder_count} 处待补充` : '相对完整'}</strong></div>
							</div>

							<nav className="mastery-document-tabs" aria-label="心法文档">
								{detail.documents.map((document) => <button type="button" className={document.id === activeDocument?.id ? 'active' : ''} onClick={() => setActiveDocumentID(document.id)} key={document.id}>{documentLabel(document)}</button>)}
							</nav>
							{activeDocument && (
								<div className="mastery-document">
									<div className="mastery-document-note"><span>{activeDocument.kind === 'deep_report' ? '优先阅读：深度研读报告通常比学习笔记完整' : '原始学习笔记，可能包含待补充占位'}</span><a href={activeDocument.source_url} target="_blank" rel="noreferrer">原文 <ExternalLink size={12} /></a></div>
									<MarkdownDocument content={activeDocument.content} />
								</div>
							)}
						</>
					)}
				</article>
			</div>
		</section>
	);
}

function MarkdownDocument({ content }: { content: string }) {
	const lines = content.replace(/\r\n/g, '\n').split('\n');
	const blocks: ReactNode[] = [];
	let index = 0;
	while (index < lines.length) {
		const line = lines[index].trim();
		if (!line) { index++; continue; }
		if (/^```/.test(line)) {
			const language = line.slice(3).trim();
			const code: string[] = [];
			index++;
			while (index < lines.length && !/^```/.test(lines[index].trim())) code.push(lines[index++]);
			index++;
			blocks.push(<div className="mastery-code" key={`code-${index}`}>{language && <span>{language}</span>}<pre><code>{code.join('\n')}</code></pre></div>);
			continue;
		}
		const heading = line.match(/^(#{1,4})\s+(.+)$/);
		if (heading) {
			const level = heading[1].length;
			blocks.push(markdownHeading(level, heading[2], `heading-${index}`));
			index++;
			continue;
		}
		if (/^[-*_]{3,}$/.test(line)) {
			blocks.push(<hr key={`hr-${index}`} />);
			index++;
			continue;
		}
		if (line.startsWith('>')) {
			const quotes: string[] = [];
			while (index < lines.length && lines[index].trim().startsWith('>')) quotes.push(lines[index++].trim().replace(/^>\s?/, ''));
			blocks.push(<blockquote key={`quote-${index}`}>{quotes.map((item, quoteIndex) => <Fragment key={quoteIndex}>{inlineMarkdown(item)}{quoteIndex < quotes.length - 1 && <br />}</Fragment>)}</blockquote>);
			continue;
		}
		if (isTableRow(line) && index + 1 < lines.length && /^\s*\|?\s*:?-+/.test(lines[index + 1])) {
			const headers = tableCells(lines[index]);
			index += 2;
			const rows: string[][] = [];
			while (index < lines.length && isTableRow(lines[index].trim())) rows.push(tableCells(lines[index++]));
			blocks.push(<div className="mastery-table-wrap" key={`table-${index}`}><table><thead><tr>{headers.map((cell, cellIndex) => <th key={cellIndex}>{inlineMarkdown(cell)}</th>)}</tr></thead><tbody>{rows.map((row, rowIndex) => <tr key={rowIndex}>{row.map((cell, cellIndex) => <td key={cellIndex}>{inlineMarkdown(cell)}</td>)}</tr>)}</tbody></table></div>);
			continue;
		}
		if (/^[-*+]\s+/.test(line)) {
			const items: string[] = [];
			while (index < lines.length && /^\s*[-*+]\s+/.test(lines[index])) items.push(lines[index++].trim().replace(/^[-*+]\s+/, ''));
			blocks.push(<ul key={`ul-${index}`}>{items.map((item, itemIndex) => <li key={itemIndex}>{inlineMarkdown(item)}</li>)}</ul>);
			continue;
		}
		if (/^\d+[.)]\s+/.test(line)) {
			const items: string[] = [];
			while (index < lines.length && /^\s*\d+[.)]\s+/.test(lines[index])) items.push(lines[index++].trim().replace(/^\d+[.)]\s+/, ''));
			blocks.push(<ol key={`ol-${index}`}>{items.map((item, itemIndex) => <li key={itemIndex}>{inlineMarkdown(item)}</li>)}</ol>);
			continue;
		}
		const paragraph: string[] = [line];
		index++;
		while (index < lines.length && lines[index].trim() && !isBlockStart(lines[index].trim(), lines[index + 1]?.trim())) paragraph.push(lines[index++].trim());
		blocks.push(<p key={`p-${index}`}>{inlineMarkdown(paragraph.join(' '))}</p>);
	}
	return <div className="mastery-markdown">{blocks}</div>;
}

function inlineMarkdown(value: string): ReactNode[] {
	const pattern = /(\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\([^)]+\))/g;
	const parts = value.split(pattern).filter(Boolean);
	return parts.map((part, index) => {
		if (part.startsWith('**') && part.endsWith('**')) return <strong key={index}>{part.slice(2, -2)}</strong>;
		if (part.startsWith('`') && part.endsWith('`')) return <code key={index}>{part.slice(1, -1)}</code>;
		const link = part.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
		if (link) return <a href={link[2]} target="_blank" rel="noreferrer" key={index}>{link[1]}</a>;
		return <Fragment key={index}>{part}</Fragment>;
	});
}

function markdownHeading(level: number, content: string, key: string) {
	const children = inlineMarkdown(content);
	if (level === 1) return <h2 key={key}>{children}</h2>;
	if (level === 2) return <h3 key={key}>{children}</h3>;
	if (level === 3) return <h4 key={key}>{children}</h4>;
	return <h5 key={key}>{children}</h5>;
}

function isBlockStart(line: string, nextLine?: string) {
	return /^(#{1,4})\s+/.test(line) || /^```/.test(line) || line.startsWith('>') || /^[-*_]{3,}$/.test(line) || /^[-*+]\s+/.test(line) || /^\d+[.)]\s+/.test(line) || (isTableRow(line) && Boolean(nextLine && /^\s*\|?\s*:?-+/.test(nextLine)));
}

function isTableRow(line: string) { return line.includes('|') && line.replace(/\|/g, '').trim().length > 0; }
function tableCells(line: string) { return line.trim().replace(/^\||\|$/g, '').split('|').map((cell) => cell.trim()); }
function documentLabel(document: MasteryDocument) { return document.kind === 'deep_report' ? '深度研读报告' : document.kind === 'study_notes' ? '学习笔记' : document.title; }
function formatChars(value: number) { return value >= 10_000 ? `${(value / 10_000).toFixed(1)} 万字` : `${value.toLocaleString('zh-CN')} 字`; }
function formatDateTime(value: string) { return new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }); }
