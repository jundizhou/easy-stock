import { CheckCircle2, ChevronLeft, ChevronRight, Sparkles, X } from 'lucide-react';
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import type { ResearchAnalysisLevel, ResearchRequest } from '../lib/stock-research';
import { StockResearchOptions } from './StockResearchReport';

const researchLevelOptions: Array<{ value: ResearchAnalysisLevel; title: string; description: string; coverage: string; tokens: string; time: string }> = [
	{ value: 'quantitative', title: '量化速览', description: '只使用本地行情与规则计算，不调用 AI。', coverage: '无 AI 判断', tokens: '0 Token', time: '10～45 秒' },
	{ value: 'quick', title: 'AI 快速研判', description: '用少量核心数据快速形成初步判断，不生成交易计划。', coverage: '基础覆盖', tokens: '约 2,000～6,000', time: '通常 1～6 分钟' },
	{ value: 'standard', title: 'AI 标准研判', description: '压缩行情与公告，分别生成核心判断和交易条件。', coverage: '中等覆盖', tokens: '约 6,000～16,000', time: '通常 2～12 分钟' },
	{ value: 'deep', title: 'AI 深度研究', description: '完整执行证据核验、核心判断和交易条件。', coverage: '最高覆盖', tokens: '约 20,000～50,000', time: '通常 3～30 分钟' },
];

export type ResearchSetupOptions = {
	purpose: ResearchRequest['purpose'];
	horizon: ResearchRequest['horizon'];
	cost: string;
	level: ResearchAnalysisLevel;
};

type Props = {
	symbol: string;
	initialOptions: ResearchSetupOptions;
	onCancel: () => void;
	onConfirm: (options: ResearchSetupOptions) => void;
};

export function StockResearchSetupDialog({ symbol, initialOptions, onCancel, onConfirm }: Props) {
	const [options, setOptions] = useState(initialOptions);
	const { purpose, horizon, cost, level: value } = options;
	const onChange = (level: ResearchAnalysisLevel) => setOptions((current) => ({ ...current, level }));
	const [step, setStep] = useState<'purpose' | 'level'>('purpose');
	const [costError, setCostError] = useState('');
	const dialogRef = useRef<HTMLElement>(null);
	useEffect(() => {
		const previous = document.activeElement;
		return () => { if (previous instanceof HTMLElement && previous.isConnected) previous.focus(); };
	}, []);
	useEffect(() => {
		dialogRef.current?.querySelector<HTMLButtonElement>(step === 'purpose' ? '[aria-pressed="true"]' : '[aria-checked="true"]')?.focus();
	}, [step]);

	const next = () => {
		const input = dialogRef.current?.querySelector<HTMLInputElement>('input[aria-label="持仓成本"]');
		if (purpose === 'holding' && (input?.validity.badInput || (cost.trim() && (!(Number(cost) > 0) || !Number.isFinite(Number(cost)))))) {
			setCostError('持仓成本须为正数，也可以留空。');
			input?.focus();
			return;
		}
		setCostError('');
		setStep('level');
	};
	const handleKeyDown = (event: KeyboardEvent<HTMLElement>) => {
		if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); onCancel(); return; }
		if (event.key === 'Tab') {
			const controls = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>('button:not([disabled]):not([tabindex="-1"]), input:not([disabled])') || []);
			const first = controls[0], last = controls[controls.length - 1];
			if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
			else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
		}
		if (step === 'purpose' && event.key === 'Enter' && event.target instanceof HTMLInputElement) { event.preventDefault(); next(); }
		if (step !== 'level' || !(event.target instanceof HTMLElement) || event.target.getAttribute('role') !== 'radio' || !['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
		const currentIndex = researchLevelOptions.findIndex((option) => option.value === value);
		const nextIndex = event.key === 'Home' ? 0 : event.key === 'End' ? researchLevelOptions.length - 1 : (currentIndex + (event.key === 'ArrowDown' ? 1 : -1) + researchLevelOptions.length) % researchLevelOptions.length;
		event.preventDefault(); onChange(researchLevelOptions[nextIndex].value);
		dialogRef.current?.querySelectorAll<HTMLButtonElement>('[role="radio"]')[nextIndex]?.focus();
	};
	const purposeLabel = { observe: '观察', new_position: '准备新开仓', holding: '已有持仓' }[purpose];
	const horizonLabel = { short: '超短', swing: '波段', medium: '中期' }[horizon];

	return <div className="stock-ai-level-overlay" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onCancel(); }}>
		<section ref={dialogRef} className="stock-ai-level-dialog" role="dialog" aria-modal="true" aria-labelledby="stock-ai-level-title" onKeyDown={handleKeyDown}>
			<header><div><span><Sparkles size={14} />{symbol}</span><h2 id="stock-ai-level-title">{step === 'purpose' ? '选择用途与周期' : '选择分析深度'}</h2><p>{step === 'purpose' ? '先确定这次分析的用途和关注周期。' : `${purposeLabel} · ${horizonLabel}${purpose === 'holding' && cost.trim() ? ` · 持仓成本 ${Number(cost)} 元` : ''}`}</p></div><button type="button" onClick={onCancel} aria-label="关闭分析设置"><X size={17} /></button></header>
			<ol className="stock-ai-setup-steps" aria-label="分析步骤"><li className={step === 'purpose' ? 'active' : 'complete'} aria-current={step === 'purpose' ? 'step' : undefined}><span>1</span>用途与周期</li><li className={step === 'level' ? 'active' : ''} aria-current={step === 'level' ? 'step' : undefined}><span>2</span>分析级别</li></ol>
			{step === 'purpose' ? <StockResearchOptions purpose={purpose} horizon={horizon} cost={cost} costError={costError} onPurpose={(nextPurpose) => { setCostError(''); setOptions((current) => ({ ...current, purpose: nextPurpose })); }} onHorizon={(nextHorizon) => setOptions((current) => ({ ...current, horizon: nextHorizon }))} onCost={(nextCost) => { setCostError(''); setOptions((current) => ({ ...current, cost: nextCost })); }} /> : <>
				<div className="stock-ai-level-list" role="radiogroup" aria-label="分析级别">
					{researchLevelOptions.map((option) => <button type="button" role="radio" tabIndex={value === option.value ? 0 : -1} aria-checked={value === option.value} className={value === option.value ? 'active' : ''} onClick={() => onChange(option.value)} key={option.value}>
						<span className="stock-ai-level-radio" aria-hidden="true">{value === option.value ? <CheckCircle2 size={18} /> : <span />}</span>
						<span className="stock-ai-level-copy"><strong>{option.title}{option.value === 'deep' && <em>默认</em>}</strong><small>{option.description}</small></span>
						<span className="stock-ai-level-meta"><small>{option.coverage}</small><small>{option.tokens} · {option.time}</small></span>
					</button>)}
				</div>
				<p className="stock-ai-level-note">Token 和耗时为估算值，较高思考档位可能耗时更长。时间上限会适配当前模型设置，并在研究过程中显示；排队不占用执行时间。级别越高表示证据覆盖更广，不代表绝对准确或收益确定。</p>
			</>}
			<footer><button type="button" className="secondary" onClick={step === 'purpose' ? onCancel : () => setStep('purpose')}>{step === 'purpose' ? '取消' : <><ChevronLeft size={14} />上一步</>}</button><button type="button" onClick={step === 'purpose' ? next : () => onConfirm(options)}>{step === 'purpose' ? <>下一步<ChevronRight size={14} /></> : <><Sparkles size={14} />开始分析</>}</button></footer>
		</section>
	</div>;
}
