import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Check, ChevronDown } from 'lucide-react';

export type MonitorSelectOption = { value: string; label: string };

type Props = {
	value: string;
	onChange: (next: string) => void;
	options: MonitorSelectOption[];
	ariaLabel?: string;
	disabled?: boolean;
	placeholder?: string;
	/** 撑满父容器（表单项用），默认按内容宽度。 */
	block?: boolean;
	className?: string;
};

/**
 * 系统风格下拉：原生 <select> 的弹出层由系统绘制，改不了圆角/选中态/字号，
 * 在监控页里看起来像"从浏览器漏出来的控件"。这里用按钮 + portal 菜单自绘，
 * 菜单挂在 body 上（面板有 overflow:hidden，内联菜单会被裁掉），
 * 位置按触发按钮实时计算，空间不够自动向上翻。
 */
export function MonitorSelect({
	value, onChange, options, ariaLabel, disabled, placeholder, block, className,
}: Props) {
	const triggerRef = useRef<HTMLButtonElement | null>(null);
	const menuRef = useRef<HTMLDivElement | null>(null);
	const [open, setOpen] = useState(false);
	const [active, setActive] = useState(-1);
	const [box, setBox] = useState({ top: 0, left: 0, width: 0, up: false });
	const listId = useId();

	const current = options.find((option) => option.value === value);
	const selectedIndex = options.findIndex((option) => option.value === value);

	const place = useCallback(() => {
		const trigger = triggerRef.current;
		if (!trigger) return;
		const rect = trigger.getBoundingClientRect();
		const menuHeight = Math.min(options.length * 32 + 8, 288);
		// 弹窗里的下拉不要把菜单甩到对话框外面去：可用下边界收到对话框底边。
		const dialog = trigger.closest('.monitor-editor') as HTMLElement | null;
		const limitBottom = Math.min(
			window.innerHeight - 8,
			dialog ? dialog.getBoundingClientRect().bottom - 8 : Number.POSITIVE_INFINITY,
		);
		const below = limitBottom - rect.bottom;
		const up = below < menuHeight + 6 && rect.top > menuHeight + 12;
		setBox({
			top: up ? Math.max(8, rect.top - menuHeight - 6) : rect.bottom + 6,
			left: Math.max(8, Math.min(rect.left, window.innerWidth - rect.width - 8)),
			width: rect.width,
			up,
		});
	}, [options.length]);

	const toggle = () => {
		if (disabled) return;
		if (!open) {
			place();
			setActive(selectedIndex >= 0 ? selectedIndex : 0);
		}
		setOpen((prev) => !prev);
	};

	const pick = (next: string) => {
		onChange(next);
		setOpen(false);
		triggerRef.current?.focus();
	};

	// 打开期间：点击外部、Esc、滚动、窗口尺寸变化都收起；键盘上下键走选项。
	useEffect(() => {
		if (!open) return;
		const onPointerDown = (event: PointerEvent) => {
			const target = event.target as Node;
			if (menuRef.current?.contains(target) || triggerRef.current?.contains(target)) return;
			setOpen(false);
		};
		const onKeyDown = (event: KeyboardEvent) => {
			if (event.key === 'Escape') { setOpen(false); return; }
			if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
				event.preventDefault();
				const step = event.key === 'ArrowDown' ? 1 : -1;
				setActive((prev) => (prev + step + options.length) % options.length);
				return;
			}
			if (event.key === 'Enter' || event.key === ' ') {
				const option = options[active];
				if (option) { event.preventDefault(); pick(option.value); }
			}
		};
		const close = () => setOpen(false);
		document.addEventListener('pointerdown', onPointerDown, true);
		document.addEventListener('keydown', onKeyDown, true);
		window.addEventListener('resize', close);
		window.addEventListener('scroll', close, true);
		return () => {
			document.removeEventListener('pointerdown', onPointerDown, true);
			document.removeEventListener('keydown', onKeyDown, true);
			window.removeEventListener('resize', close);
			window.removeEventListener('scroll', close, true);
		};
	}, [open, active, options]);

	// 菜单渲染后若超出视口底部，再按实际高度修正一次。
	useLayoutEffect(() => {
		if (!open || !menuRef.current) return;
		const menu = menuRef.current.getBoundingClientRect();
		if (menu.bottom > window.innerHeight - 8 && !box.up) {
			place();
		}
	}, [open, box.up, place]);

	return (
		<span className={`monitor-selectbox${block ? ' is-block' : ''}${className ? ` ${className}` : ''}`}>
			<button
				type="button"
				ref={triggerRef}
				className="monitor-select-trigger"
				aria-haspopup="listbox"
				aria-expanded={open}
				aria-controls={open ? listId : undefined}
				aria-label={ariaLabel}
				disabled={disabled}
				onClick={toggle}
			>
				<span className={current ? '' : 'is-placeholder'}>{current?.label ?? placeholder ?? '请选择'}</span>
				<ChevronDown size={13} aria-hidden="true" />
			</button>
			{open && createPortal(
				<div
					ref={menuRef}
					id={listId}
					role="listbox"
					aria-label={ariaLabel}
					className="monitor-select-menu"
					style={{ top: box.top, left: box.left, minWidth: box.width }}
				>
					{options.map((option, index) => (
						<button
							type="button"
							key={option.value}
							role="option"
							aria-selected={option.value === value}
							className={`monitor-select-option${option.value === value ? ' is-selected' : ''}${index === active ? ' is-active' : ''}`}
							onMouseEnter={() => setActive(index)}
							onClick={() => pick(option.value)}
						>
							<span>{option.label}</span>
							{option.value === value && <Check size={13} aria-hidden="true" />}
						</button>
					))}
				</div>,
				document.body,
			)}
		</span>
	);
}
