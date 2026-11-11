import { ArrowRight, ChevronLeft, LoaderCircle, Sparkles } from 'lucide-react';
import { useLayoutEffect, useRef, useState, type RefObject } from 'react';
import './LLMSetupTour.css';

const steps = [
	{ target: 'provider', title: '选择模型服务商', description: '连接一个大模型，即可开启 AI 对话、个股研究和复盘。先在亮起的区域选择服务商，确认接口地址与协议。', hint: '使用代理或本地模型时，选择“OpenAI 兼容接口”并填写地址。' },
	{ target: 'key', title: '填写模型 API Key', description: '粘贴从模型服务商获取的 API Key。这是应用调用模型所需的密钥。', hint: '密钥只保存在本机；无需密钥的本地兼容接口可直接进入下一步。' },
	{ target: 'model', title: '选择要使用的模型', description: '点击“获取模型”后选择一个模型，也可以直接填写服务商提供的模型名称。', hint: '模型名称需与服务商提供的名称一致。' },
	{ target: 'save', title: '保存，开启 AI 功能', description: '配置已填写完成。点击亮起的“保存设置”，或点击下方按钮，保存这套模型连接。', hint: '保存后即可使用 AI 功能，之后可在系统设置中测试连接或切换模型。' },
] as const;

type Props = {
	formRef: RefObject<HTMLFormElement | null>;
	canContinue: boolean[];
	busy: boolean;
	error?: string;
	onDismiss: () => void;
	onSave: () => void;
};

type Layout = {
	top: number; left: number; width: number; height: number;
	cardTop: number; cardLeft: number; cardWidth: number;
	viewportWidth: number; viewportHeight: number;
	placement: 'left' | 'right' | 'above' | 'below';
};

export function LLMSetupTour({ formRef, canContinue, busy, error, onDismiss, onSave }: Props) {
	const [stepIndex, setStepIndex] = useState(0);
	const [layout, setLayout] = useState<Layout | null>(null);
	const cardRef = useRef<HTMLElement>(null);
	const step = steps[stepIndex];
	const latest = useRef({ busy, onDismiss, canContinue });
	latest.current = { busy, onDismiss, canContinue };

	useLayoutEffect(() => {
		const form = formRef.current;
		const target = form?.querySelector<HTMLElement>(`[data-llm-setup-target="${step.target}"]`);
		if (!form || !target) return;
		const details = target.closest('details');
		if (details) details.open = true;
		// Scroll only the drawer. scrollIntoView can also move the document
		// behind the fixed overlay, taking the entire application off screen.
		if (step.target !== 'save') {
			const bounds = form.getBoundingClientRect();
			const targetBounds = target.getBoundingClientRect();
			const footerHeight = form.querySelector('.settings-footer')?.getBoundingClientRect().height || 0;
			const visibleHeight = bounds.height - footerHeight;
			form.scrollTop += targetBounds.top - bounds.top - (visibleHeight - targetBounds.height) / 2;
		}

		const measure = () => {
			const rect = target.getBoundingClientRect();
			const viewportWidth = window.innerWidth;
			const viewportHeight = window.innerHeight;
			const left = Math.max(8, Math.min(rect.left - 8, viewportWidth - 8));
			const top = Math.max(8, Math.min(rect.top - 8, viewportHeight - 8));
			const width = Math.max(0, Math.min(rect.right + 8, viewportWidth - 8) - left);
			const height = Math.max(0, Math.min(rect.bottom + 8, viewportHeight - 8) - top);
			const rightSpace = viewportWidth - left - width;
			const placement = left >= 272 ? 'left' : rightSpace >= 272 ? 'right' : top > viewportHeight - top - height ? 'above' : 'below';
			const cardWidth = Math.min(336, viewportWidth - 32, placement === 'left' ? left - 32 : placement === 'right' ? rightSpace - 32 : 336);
			const cardHeight = cardRef.current?.getBoundingClientRect().height || 270;
			const cardLeft = placement === 'left' ? left - cardWidth - 20 : placement === 'right' ? left + width + 20 : Math.max(16, Math.min(left + width / 2 - cardWidth / 2, viewportWidth - cardWidth - 16));
			const desiredTop = placement === 'above' ? top - cardHeight - 20 : placement === 'below' ? top + height + 20 : top + height / 2 - cardHeight / 2;
			const cardTop = Math.max(16, Math.min(desiredTop, viewportHeight - cardHeight - 16));
			const next: Layout = { top, left, width, height, cardTop, cardLeft, cardWidth, viewportWidth, viewportHeight, placement };
			setLayout((current) => current && Object.keys(next).every((key) => current[key as keyof Layout] === next[key as keyof Layout]) ? current : next);
		};
		measure();
		const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure);
		observer?.observe(target);
		if (cardRef.current) observer?.observe(cardRef.current);
		window.addEventListener('resize', measure);
		document.addEventListener('scroll', measure, true);

		const controls = () => {
			const selector = 'input, select, textarea, button, [tabindex]';
			return [target, ...target.querySelectorAll<HTMLElement>(selector), ...cardRef.current?.querySelectorAll<HTMLElement>(selector) || []]
				.filter((element) => element.matches(selector) && !element.matches(':disabled') && element.tabIndex >= 0);
		};
		controls()[0]?.focus({ preventScroll: true });
		const onKeyDown = (event: KeyboardEvent) => {
			if (event.key === 'Escape') {
				event.preventDefault();
				event.stopImmediatePropagation();
				if (!latest.current.busy) latest.current.onDismiss();
			}
			if (event.key === 'Tab') {
				const elements = controls();
				const index = elements.indexOf(document.activeElement as HTMLElement);
				const next = event.shiftKey ? (index <= 0 ? elements.length - 1 : index - 1) : (index + 1) % elements.length;
				event.preventDefault();
				elements[next]?.focus({ preventScroll: true });
			}
			if (event.key === 'Enter' && event.target instanceof HTMLInputElement && stepIndex < steps.length - 1) {
				event.preventDefault();
				if (!latest.current.busy && latest.current.canContinue[stepIndex]) setStepIndex((current) => current + 1);
			}
		};
		window.addEventListener('keydown', onKeyDown, true);
		return () => {
			observer?.disconnect();
			window.removeEventListener('resize', measure);
			document.removeEventListener('scroll', measure, true);
			window.removeEventListener('keydown', onKeyDown, true);
		};
	}, [formRef, step.target, stepIndex]);

	const lastStep = stepIndex === steps.length - 1;
	return (
		<div className="llm-setup-tour" style={{ visibility: layout ? 'visible' : 'hidden' }}>
			{layout && <>
				<div className="llm-setup-spotlight" data-target={step.target} aria-hidden="true" style={{ top: layout.top, left: layout.left, width: layout.width, height: layout.height }} />
				<div className="llm-setup-blocker" aria-hidden="true" style={{ inset: '0 0 auto 0', height: layout.top }} />
				<div className="llm-setup-blocker" aria-hidden="true" style={{ top: layout.top + layout.height, bottom: 0, left: 0, right: 0 }} />
				<div className="llm-setup-blocker" aria-hidden="true" style={{ top: layout.top, left: 0, width: layout.left, height: layout.height }} />
				<div className="llm-setup-blocker" aria-hidden="true" style={{ top: layout.top, left: layout.left + layout.width, right: 0, height: layout.height }} />
			</>}
			<section ref={cardRef} className={`llm-setup-tour-card placement-${layout?.placement || 'left'}`} role="region" aria-label="大模型配置引导" style={{ top: layout?.cardTop || 16, left: layout?.cardLeft || 16, width: layout?.cardWidth || 336 }}>
				<header><span><Sparkles size={15} aria-hidden="true" />首次设置</span><span>{stepIndex + 1} / {steps.length}</span></header>
				<div className="llm-setup-tour-progress" aria-label={`第 ${stepIndex + 1} 步，共 ${steps.length} 步`}>{steps.map((item, index) => <i key={item.target} className={index <= stepIndex ? 'active' : ''} />)}</div>
				<div aria-live="polite"><h2>{step.title}</h2><p>{step.description}</p></div>
				<p className="llm-setup-tour-hint">{step.hint}</p>
				{error && <p className="llm-setup-tour-error" role="alert">{error}</p>}
				<footer>
					<button type="button" className="llm-setup-tour-skip" onClick={onDismiss} disabled={busy}>稍后配置</button>
					<div>{stepIndex > 0 && <button type="button" className="llm-setup-tour-back" aria-label="上一步" onClick={() => setStepIndex((current) => current - 1)} disabled={busy}><ChevronLeft size={16} /></button>}
					<button type="button" className="llm-setup-tour-next" disabled={busy || !canContinue[stepIndex]} onClick={() => lastStep ? onSave() : setStepIndex((current) => current + 1)}>{busy ? <><LoaderCircle size={15} className="spin" />保存中</> : <>{lastStep ? '保存并开始使用' : '下一步'}<ArrowRight size={15} /></>}</button></div>
				</footer>
			</section>
		</div>
	);
}
