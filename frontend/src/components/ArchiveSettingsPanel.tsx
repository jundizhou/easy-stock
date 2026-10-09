import { CheckCircle2, CircleAlert, HardDriveDownload, LoaderCircle, RefreshCw, Save, Zap } from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
	ArchiveConfig,
	ArchiveConfigUpdate,
	ArchiveSymbolsSource,
	BackendConfig,
	fetchArchiveConfig,
	updateArchiveConfig,
} from '../lib/backend';
import { SettingsSection } from './SettingsSection';

type Props = {
	config: BackendConfig | null;
	open: boolean;
	onSaved?: () => void;
};

type State = 'idle' | 'loading' | 'saving' | 'saved' | 'error';

const minIntervalSeconds = 5;
const maxIntervalSeconds = 3600;
const minNewsLimit = 1;
const maxNewsLimit = 100;

// 把逗号/空格/换行分隔的清单文本解析成标的数组。
function parseSymbolText(raw: string): string[] {
	return raw
		.split(/[\s,，;；]+/)
		.map((item) => item.trim().toUpperCase())
		.filter(Boolean);
}

function symbolText(list: string[] | undefined): string {
	return (list || []).join(' ');
}

export function ArchiveSettingsPanel({ config, open, onSaved }: Props) {
	const [state, setState] = useState<State>('idle');
	const [message, setMessage] = useState('');
	const [saved, setSaved] = useState<ArchiveConfig | null>(null);

	const [enabled, setEnabled] = useState(false);
	const [intervalSeconds, setIntervalSeconds] = useState(60);
	const [dir, setDir] = useState('');
	const [symbolsSource, setSymbolsSource] = useState<ArchiveSymbolsSource>('manual');
	const [symbolsText, setSymbolsText] = useState('');
	const [tradingOnly, setTradingOnly] = useState(true);
	const [newsLimit, setNewsLimit] = useState(20);
	const loadSequence = useRef(0);

	const applyConfig = useCallback((next: ArchiveConfig) => {
		setSaved(next);
		setEnabled(next.enabled);
		setIntervalSeconds(next.interval_seconds || 60);
		setDir(next.dir || '');
		setSymbolsSource((next.symbols_source as ArchiveSymbolsSource) || 'manual');
		setSymbolsText(symbolText(next.symbols));
		setTradingOnly(next.trading_only);
		setNewsLimit(next.news_limit || 20);
	}, []);

	const load = useCallback(async () => {
		if (!config) return;
		const sequence = ++loadSequence.current;
		setState('loading');
		setMessage('');
		try {
			const next = await fetchArchiveConfig(config);
			if (sequence !== loadSequence.current) return;
			applyConfig(next);
			setState('idle');
		} catch (error) {
			if (sequence !== loadSequence.current) return;
			setState('error');
			setMessage(error instanceof Error ? error.message : '读取归档配置失败');
		}
	}, [applyConfig, config]);

	useEffect(() => {
		if (!open) return;
		void load();
		return () => {
			loadSequence.current += 1;
		};
	}, [load, open]);

	// 本地解析预览：未保存时也能看到实际会采集的清单。
	const draftSymbols = useMemo(() => parseSymbolText(symbolsText), [symbolsText]);
	const previewSymbols = useMemo(() => {
		if (symbolsSource === 'holdings' && (saved?.holdings_count || 0) > 0) {
			return saved?.resolved_symbols?.length ? saved.resolved_symbols : draftSymbols;
		}
		return draftSymbols.length ? draftSymbols : saved?.resolved_symbols || [];
	}, [draftSymbols, saved, symbolsSource]);

	const dirty = useMemo(() => {
		if (!saved) return false;
		return enabled !== saved.enabled
			|| intervalSeconds !== saved.interval_seconds
			|| dir.trim() !== (saved.dir || '')
			|| symbolsSource !== (saved.symbols_source as ArchiveSymbolsSource)
			|| symbolText(draftSymbols) !== symbolText(saved.symbols)
			|| tradingOnly !== saved.trading_only
			|| newsLimit !== saved.news_limit;
	}, [dir, draftSymbols, enabled, intervalSeconds, newsLimit, saved, symbolsSource, tradingOnly]);

	const save = async () => {
		if (!config || state === 'saving') return;
		const patch: ArchiveConfigUpdate = {
			enabled,
			interval_seconds: clamp(intervalSeconds, minIntervalSeconds, maxIntervalSeconds),
			dir: dir.trim(),
			symbols_source: symbolsSource,
			symbols: draftSymbols,
			trading_only: tradingOnly,
			news_limit: clamp(newsLimit, minNewsLimit, maxNewsLimit),
		};
		setState('saving');
		setMessage('');
		try {
			const next = await updateArchiveConfig(config, patch);
			applyConfig(next);
			setState('saved');
			setMessage('归档采集配置已保存到本机 settings.json，正在运行的采集器会在下一轮自动生效。');
			onSaved?.();
		} catch (error) {
			setState('error');
			setMessage(error instanceof Error ? error.message : '保存归档配置失败');
		}
	};

	const report = saved?.archive_status;
	const effectiveDir = saved?.dir_effective || '';
	const lastWrite = report?.last_write ? formatTimestamp(report.last_write) : '';

	return (
		<SettingsSection
			title="数据归档采集"
			description="把实时报价与资讯定时落盘到本机单一专用文件夹，形成可回溯的时间序列，避免上游限流。"
			icon={<HardDriveDownload size={18} />}
		>
			<div className="archive-toggle-row">
				<label className="archive-switch">
					<input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
					<span>开启常驻定时采集</span>
				</label>
				<button type="button" className="archive-refresh" onClick={() => void load()} disabled={!config || state === 'loading' || state === 'saving'}>
					{state === 'loading' ? <LoaderCircle className="spin" size={14} /> : <RefreshCw size={14} />}刷新状态
				</button>
			</div>

			<div className="settings-grid two-columns">
				<label>
					<span>采集周期（秒）</span>
					<input
						type="number"
						min={minIntervalSeconds}
						max={maxIntervalSeconds}
						step={5}
						value={intervalSeconds}
						onChange={(event) => setIntervalSeconds(Number(event.target.value) || minIntervalSeconds)}
						disabled={!enabled}
					/>
					<small>范围 {minIntervalSeconds}–{maxIntervalSeconds} 秒。周期内单次串行采集，源内部已限速，不会突发高频请求。</small>
				</label>
				<label>
					<span>资讯条数上限</span>
					<input
						type="number"
						min={minNewsLimit}
						max={maxNewsLimit}
						step={5}
						value={newsLimit}
						onChange={(event) => setNewsLimit(Number(event.target.value) || minNewsLimit)}
						disabled={!enabled}
					/>
					<small>每轮抓取的最新资讯条数，范围 {minNewsLimit}–{maxNewsLimit}。</small>
				</label>
			</div>

			<label>
				<span>归档目录</span>
				<input
					value={dir}
					onChange={(event) => setDir(event.target.value)}
					placeholder="例如 D:/quant_archive_easy_stock（留空使用默认目录）"
				/>
				<small>
					留空则使用默认目录。当前生效：
					<code className="archive-path">{effectiveDir || '未配置'}</code>
					{' '}目录下按日期生成 <code className="archive-path">realtime/YYYY-MM-DD.jsonl</code>、<code className="archive-path">news/YYYY-MM-DD.jsonl</code> 与 <code className="archive-path">latest.json</code>。
				</small>
			</label>

			<label>
				<span>关注清单来源</span>
				<div className="archive-source-buttons" role="group" aria-label="关注清单来源">
					<button type="button" aria-pressed={symbolsSource === 'manual'} onClick={() => setSymbolsSource('manual')}>手动填写</button>
					<button type="button" aria-pressed={symbolsSource === 'holdings'} onClick={() => setSymbolsSource('holdings')}>
						联动自选股/持仓{saved?.holdings_count ? `（${saved.holdings_count}）` : ''}
					</button>
				</div>
				<small>
					{symbolsSource === 'holdings'
						? '从最近一次持仓检查任务中提取标的；没有持仓记录时回退到手动填写，再回退到默认清单。'
						: '按下面填写的清单采集；留空时使用默认大盘蓝筹清单。'}
				</small>
			</label>

			<label>
				<span>关注清单</span>
				<textarea
					value={symbolsText}
					onChange={(event) => setSymbolsText(event.target.value)}
					placeholder="600519.SH 000001.SZ 300750.SZ（可用空格、逗号或换行分隔）"
					disabled={symbolsSource === 'holdings'}
				/>
				<small>支持空格、逗号、换行分隔；自动转大写并去重。{symbolsSource === 'holdings' && '当前来源为持仓联动，此项作为兜底清单。'}</small>
			</label>

			<div className="archive-preview">
				<div className="archive-preview-heading">
					<Zap size={15} />
					<strong>实际采集清单预览（{previewSymbols.length}）</strong>
				</div>
				{previewSymbols.length ? (
					<div className="archive-preview-chips">{previewSymbols.map((symbol) => <span key={symbol}>{symbol}</span>)}</div>
				) : (
					<span className="archive-preview-empty">暂无可采集标的，请填写清单或先完成一次持仓检查。</span>
				)}
			</div>

			<label className="archive-switch">
				<input type="checkbox" checked={tradingOnly} onChange={(event) => setTradingOnly(event.target.checked)} disabled={!enabled} />
				<span>仅交易时段采集（周一至周五 09:15–11:35 / 12:55–15:05），非交易时段自动跳过</span>
			</label>

			{report && (
				<div className={`archive-status-strip ${report.last_error ? 'error' : 'success'}`}>
					{report.last_error ? <CircleAlert size={15} /> : <CheckCircle2 size={15} />}
					<span>
						<strong>{report.last_error ? '最近一次落盘失败' : '归档写入正常'}</strong>
						<small>
							{report.last_error
								? report.last_error
								: `目录 ${report.dir || effectiveDir || '—'} · 文件 ${report.files ?? 0} 个 · 累计 ${report.lines ?? 0} 行${lastWrite ? ` · 最近写入 ${lastWrite}` : ''}`}
						</small>
					</span>
				</div>
			)}

			<div className="archive-actions">
				<div className={`settings-message ${state}`} role={state === 'error' ? 'alert' : 'status'}>
					{state === 'saved' && <CheckCircle2 size={15} />}
					{state === 'error' && <CircleAlert size={15} />}
					<span>{message || '配置保存在本机，采集器按周期热加载，无需重启应用。'}</span>
				</div>
				<button type="button" className="archive-save" onClick={() => void save()} disabled={!config || state === 'saving' || state === 'loading' || !dirty}>
					{state === 'saving' ? <LoaderCircle className="spin" size={15} /> : <Save size={15} />}{dirty ? '保存归档配置' : '已保存'}
				</button>
			</div>
		</SettingsSection>
	);
}

function clamp(value: number, min: number, max: number): number {
	if (!Number.isFinite(value)) return min;
	return Math.min(max, Math.max(min, Math.round(value)));
}

function formatTimestamp(raw: string): string {
	const parsed = new Date(raw);
	if (Number.isNaN(parsed.getTime())) return raw;
	return parsed.toLocaleString();
}
