import { Bold, BookOpen, Check, Eye, Heading2, Italic, List, ListChecks, PenLine, Plus, Quote, RefreshCw, Save, Search } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { BackendConfig, requestJSON } from '../lib/backend';
import { confirmDialog } from './ConfirmDialog';
import { MessageContent } from './MarkdownContent';

type Journal = {
	id: string;
	title: string;
	entry_date: string;
	content: string;
	revision: number;
	created_at: string;
	updated_at: string;
};
type Draft = Pick<Journal, 'id' | 'title' | 'entry_date' | 'content' | 'revision'>;
type EditorState = { draft: Draft; saved: Draft | null };
const draftKey = 'easy-stock:personal-review-draft:v1';
const pageSize = 30;
const blankDraft = (): Draft => ({ id: '', title: '', entry_date: new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' }).format(new Date()), content: '', revision: 0 });
const toDraft = ({ id, title, entry_date, content, revision }: Journal): Draft => ({ id, title, entry_date, content, revision });
function isDirty({ draft, saved }: EditorState) {
	return saved ? JSON.stringify(draft) !== JSON.stringify(saved) : !!(draft.title || draft.content);
}
function restoreEditor(): EditorState {
	try {
		const stored = JSON.parse(localStorage.getItem(draftKey) || 'null') as EditorState | null;
		if (stored && validDraft(stored.draft) && (stored.saved === null || validDraft(stored.saved))) return stored;
	} catch { /* A fresh editor remains available if browser storage is unavailable. */ }
	return { draft: blankDraft(), saved: null };
}
function validDraft(value: unknown): value is Draft {
	if (!value || typeof value !== 'object') return false;
	const draft = value as Draft;
	return typeof draft.id === 'string' && typeof draft.title === 'string' && typeof draft.content === 'string'
		&& typeof draft.entry_date === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(draft.entry_date) && Number.isInteger(draft.revision);
}

export function PersonalReviewJournal({ config, refreshKey }: { config: BackendConfig | null; refreshKey: number }) {
	const [editor, setEditor] = useState<EditorState>(restoreEditor);
	const { draft } = editor;
	const dirty = isDirty(editor);
	const [entries, setEntries] = useState<Journal[]>([]);
	const [total, setTotal] = useState(0);
	const [search, setSearch] = useState('');
	const [query, setQuery] = useState('');
	const [offset, setOffset] = useState(0);
	const [reload, setReload] = useState(0);
	const [loading, setLoading] = useState(false);
	const [opening, setOpening] = useState(false);
	const [saving, setSaving] = useState(false);
	const [listError, setListError] = useState('');
	const [error, setError] = useState('');
	const [draftError, setDraftError] = useState('');
	const [notice, setNotice] = useState('');
	const [preview, setPreview] = useState(false);
	const [updatedAt, setUpdatedAt] = useState('');
	const bodyRef = useRef<HTMLTextAreaElement>(null);
	const busyRef = useRef(false);
	const busy = saving || opening;

	useEffect(() => {
		try {
			if (dirty) localStorage.setItem(draftKey, JSON.stringify(editor));
			else localStorage.removeItem(draftKey);
			setDraftError('');
		} catch {
			setDraftError('暂时无法保留草稿，请及时点击“保存日记”，离开前请复制未保存的内容。');
		}
	}, [editor, dirty]);

	useEffect(() => {
		if (!dirty) return;
		const preventLoss = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
		window.addEventListener('beforeunload', preventLoss);
		return () => window.removeEventListener('beforeunload', preventLoss);
	}, [dirty]);

	useEffect(() => {
		if (!config) return;
		const controller = new AbortController();
		setLoading(true);
		setListError('');
		const params = new URLSearchParams({ q: query, limit: String(pageSize), offset: String(offset) });
		requestJSON<{ data: Journal[]; total: number }>(config, `/api/v1/reviews/journals?${params}`, { signal: controller.signal })
			.then((payload) => { if (!controller.signal.aborted) { setEntries(payload.data || []); setTotal(payload.total); } })
			.catch((cause) => { if (!controller.signal.aborted) setListError(cause instanceof Error ? cause.message : '日记加载失败'); })
			.finally(() => { if (!controller.signal.aborted) setLoading(false); });
		return () => controller.abort();
	}, [config, query, offset, reload, refreshKey]);

	const change = (patch: Partial<Draft>) => {
		setEditor((current) => ({ ...current, draft: { ...current.draft, ...patch } }));
		setNotice('');
	};
	const canSwitch = () => !dirty || confirmDialog({ title: '放弃当前未保存的编辑？', message: '当前草稿将被替换。若要保留，请取消并先保存日记。', confirmText: '放弃编辑' });
	const newEntry = async () => {
		if (busyRef.current) return;
		busyRef.current = true;
		try {
			if (!await canSwitch()) return;
			setEditor({ draft: blankDraft(), saved: null });
			setPreview(false); setError(''); setNotice(''); setUpdatedAt('');
		} finally { busyRef.current = false; }
	};
	const openEntry = async (id: string) => {
		if (!config || busyRef.current) return;
		busyRef.current = true;
		try {
			if (!await canSwitch()) return;
			setOpening(true); setError('');
			const { data } = await requestJSON<{ data: Journal }>(config, `/api/v1/reviews/journals/${encodeURIComponent(id)}`);
			const value = toDraft(data);
			setEditor({ draft: value, saved: value });
			setUpdatedAt(data.updated_at); setPreview(false); setNotice('');
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : '日记读取失败');
		} finally { busyRef.current = false; setOpening(false); }
	};
	const save = async () => {
		if (!config || busyRef.current || !dirty || !draft.title.trim() || !draft.content.trim() || !draft.entry_date) return;
		busyRef.current = true; setSaving(true); setError(''); setNotice('');
		try {
			const { data } = await requestJSON<{ data: Journal }>(config, `/api/v1/reviews/journals${draft.id ? `/${encodeURIComponent(draft.id)}` : ''}`, {
				method: draft.id ? 'PUT' : 'POST', headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ title: draft.title, entry_date: draft.entry_date, content: draft.content, revision: draft.revision }),
			});
			const value = toDraft(data);
			// The request can finish after navigation unmounts this editor. Clear
			// only its own draft, preserving any newer edits made elsewhere.
			try { if (localStorage.getItem(draftKey) === JSON.stringify(editor)) localStorage.removeItem(draftKey); } catch { /* The entry itself is already saved. */ }
			setEditor({ draft: value, saved: value });
			setUpdatedAt(data.updated_at); setNotice('日记已保存到本机');
			setSearch(''); setQuery(''); setOffset(0); setReload((value) => value + 1);
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : '保存失败，请重试，当前编辑仍保留');
		} finally { busyRef.current = false; setSaving(false); }
	};
	const format = (prefix: string, suffix = '', placeholder = '文字') => {
		const input = bodyRef.current;
		if (!input) return;
		const { selectionStart: start, selectionEnd: end } = input;
		const selection = draft.content.slice(start, end) || placeholder;
		change({ content: draft.content.slice(0, start) + prefix + selection + suffix + draft.content.slice(end) });
		requestAnimationFrame(() => { input.focus(); input.setSelectionRange(start + prefix.length, start + prefix.length + selection.length); });
	};

	return <section className="personal-journal" aria-label="我的复盘日记">
		<header className="personal-journal-heading">
			<div><PenLine size={20} /><span><h2>我的复盘日记</h2><p>记录盘面观察、交易得失与明日计划，只保存在本机。</p></span></div>
			<button type="button" className="journal-primary" onClick={() => void newEntry()} disabled={busy}><Plus size={16} />新建日记</button>
		</header>
		<div className="personal-journal-layout">
			<aside className="journal-history" aria-label="已保存的日记">
				<form className="journal-search" onSubmit={(event) => { event.preventDefault(); setQuery(search.trim()); setOffset(0); }}><Search size={16} /><input aria-label="搜索我的日记" placeholder="搜索标题、正文或日期" value={search} onChange={(event) => setSearch(event.target.value)} /><button type="submit">搜索</button></form>
				<div className="journal-history-count">{query ? '搜索结果' : '已保存的日记'}<span>{total} 篇</span></div>
				{listError && <div className="journal-feedback error" role="alert">{listError}<button type="button" onClick={() => setReload((value) => value + 1)}>重试</button></div>}
				<div className="journal-history-list" aria-busy={loading}>
					{entries.map((entry) => <button type="button" className={`journal-entry${draft.id === entry.id ? ' active' : ''}`} key={entry.id} onClick={() => void openEntry(entry.id)} disabled={busy}><time>{entry.entry_date}</time><strong>{entry.title}</strong><p>{entry.content.replace(/[#*>`_\[\]]/g, '').slice(0, 100)}</p></button>)}
					{loading && <p className="journal-empty" role="status">正在读取日记…</p>}
					{!loading && !listError && !entries.length && <div className="journal-empty"><BookOpen size={24} /><strong>{query ? '没有找到相关日记' : '从今天的复盘开始'}</strong><span>{query ? '换个关键词，或清空搜索查看全部日记。' : '在右侧写下第一篇日记，保存后可以随时回来编辑。'}</span></div>}
				</div>
				{total > pageSize && <nav className="journal-pagination" aria-label="日记分页"><button type="button" disabled={!offset || loading} onClick={() => setOffset((value) => Math.max(0, value - pageSize))}>上一页</button><span>{Math.floor(offset / pageSize) + 1} / {Math.ceil(total / pageSize)}</span><button type="button" disabled={offset + pageSize >= total || loading} onClick={() => setOffset((value) => value + pageSize)}>下一页</button></nav>}
			</aside>
			<form className="journal-editor" onSubmit={(event) => { event.preventDefault(); void save(); }} onKeyDown={(event) => { if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') { event.preventDefault(); void save(); } }}>
				<div className="journal-editor-heading"><span><PenLine size={16} />{draft.id ? '编辑日记' : '新日记'}</span><div className="journal-editor-controls"><div className="journal-mode" role="group" aria-label="编辑器模式"><button type="button" aria-pressed={!preview} onClick={() => setPreview(false)}><PenLine size={14} />编辑</button><button type="button" aria-pressed={preview} onClick={() => setPreview(true)}><Eye size={14} />预览</button></div><button type="submit" className="journal-primary" disabled={!config || busy || !dirty || !draft.title.trim() || !draft.content.trim() || !draft.entry_date}>{saving ? <RefreshCw className="spin" size={16} /> : <Save size={16} />}保存日记</button></div></div>
				<fieldset disabled={busy}>
					<div className="journal-fields"><label>复盘日期<input type="date" required value={draft.entry_date} onChange={(event) => change({ entry_date: event.target.value })} /></label><label className="journal-title">日记标题<input required maxLength={200} value={draft.title} onChange={(event) => change({ title: event.target.value })} placeholder="给今天的复盘起个标题" /></label></div>
					{!preview && <div className="journal-format" role="group" aria-label="正文格式工具">
						<button type="button" title="二级标题" aria-label="插入标题" onClick={() => format('\n## ', '\n', '盘面观察')}><Heading2 size={17} /></button>
						<button type="button" title="粗体" aria-label="插入粗体" onClick={() => format('**', '**')}><Bold size={16} /></button>
						<button type="button" title="斜体" aria-label="插入斜体" onClick={() => format('*', '*')}><Italic size={16} /></button>
						<button type="button" title="列表" aria-label="插入列表" onClick={() => format('\n- ', '\n', '观察要点')}><List size={17} /></button>
						<button type="button" title="待办" aria-label="插入待办" onClick={() => format('\n- [ ] ', '\n', '明日计划')}><ListChecks size={17} /></button>
						<button type="button" title="引用" aria-label="插入引用" onClick={() => format('\n> ', '\n', '值得记住的一句话')}><Quote size={16} /></button>
						<span>支持 Markdown · 可切换预览</span>
					</div>}
					{preview ? <div className="journal-preview" aria-label="日记预览"><h2>{draft.title || '未命名日记'}</h2>{draft.content ? <MessageContent content={draft.content} markdown /> : <p className="journal-empty">写下正文后，即可在这里预览。</p>}</div>
						: <textarea ref={bodyRef} className="journal-body" aria-label="日记正文" maxLength={200000} value={draft.content} onChange={(event) => change({ content: event.target.value })} placeholder={'今天的盘面有什么变化？\n\n哪些判断得到了验证，哪些需要修正？\n\n记录交易得失，写下明日的观察与计划…'} />}
				</fieldset>
				{(error || draftError) && <div className="journal-feedback error" role="alert">{error || draftError}</div>}
				{notice && <div className="journal-feedback success" role="status"><Check size={15} />{notice}</div>}
				<footer className="journal-editor-footer"><div><span>{draft.content.length.toLocaleString()} 字</span><small>{saving ? '正在保存…' : dirty ? draftError ? '有未保存的修改' : '有未保存的修改 · 草稿已保留' : updatedAt ? `已保存 · ${new Date(updatedAt).toLocaleString('zh-CN', { hour12: false })}` : '填写标题和正文后保存'}</small></div><small>⌘ / Ctrl + S 保存</small></footer>
			</form>
		</div>
	</section>;
}
