// 系统风格确认弹窗：替代 window.confirm / window.alert。
//
// 为什么不用原生弹窗：Electron 下 window.confirm 的标题会显示成宿主应用的
// 名字（「WorkBuddy」），文案无法换行排版、不能标红危险动作，也完全不跟
// 站内的亮暗主题走。截图里那种「一个系统灰框弹在业务界面中间」尤其突兀。
//
// 用法（命令式，尽量贴近 window.confirm 的写法，迁移成本最低）：
//   if (!(await confirmDialog({ title: '停止监控', message: '...' }))) return;
//
// 实现：一个常驻的宿主节点 + 一个 Promise 队列。同一时刻只展示一个弹窗，
// 后来的请求排队，避免连点叠出多个遮罩。

import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { AlertTriangle, Info } from 'lucide-react';

export type ConfirmOptions = {
	/** 标题：一句话说清要发生什么，动词开头。 */
	title: string;
	/** 正文：后果、影响范围。允许 \n 换行。 */
	message?: string;
	/** 确认按钮文案，默认「确定」。 */
	confirmText?: string;
	/** 取消按钮文案，默认「取消」；传 null 表示只有一个按钮（纯提示）。 */
	cancelText?: string | null;
	/** 危险动作（删除、停止、清空）标红，让用户看清这一下不可逆。 */
	tone?: 'default' | 'danger';
};

type Pending = {
	options: ConfirmOptions;
	resolve: (value: boolean) => void;
};

let host: ((next: ConfirmOptions) => Promise<boolean>) | null = null;

/** 打开一个确认弹窗；返回用户是否确认。 */
export function confirmDialog(options: ConfirmOptions): Promise<boolean> {
	if (!host) {
		// 宿主还没挂载（极早期调用）：退回原生弹窗，保证功能不因样式而中断。
		return Promise.resolve(window.confirm(`${options.title}\n\n${options.message || ''}`));
	}
	return host(options);
}

/** 纯提示（替代 window.alert）。 */
export function alertDialog(options: ConfirmOptions): Promise<boolean> {
	return confirmDialog({ ...options, cancelText: null, confirmText: options.confirmText || '知道了' });
}

/** 弹窗宿主：在 App 根节点挂一次即可。 */
export function ConfirmDialogHost() {
	const queue = useRef<Pending[]>([]);
	/** 「当前有没有弹窗」的 ref 镜像。
	 *  不能直接读 active 这个 state：settle 里那个 setTimeout(pump, 0) 捕获的是
	 *  弹窗还开着那一轮的 pump 闭包，里面 active 恒为真值 → pump 直接 return，
	 *  队列里排着的第二个弹窗永远不会被取出，调用方 await 到天荒地老。
	 *  （触发路径：删除 A 的弹窗开着时又点了删除 B，B 入队后彻底不显示。） */
	const activeRef = useRef<Pending | null>(null);
	const [active, setActive] = useState<Pending | null>(null);
	const [leaving, setLeaving] = useState(false);
	const confirmRef = useRef<HTMLButtonElement | null>(null);

	// 只碰 ref 与 setState，不读任何 state 快照，因此可以被长期持有。
	const pump = () => {
		if (activeRef.current) return;
		const next = queue.current.shift();
		if (!next) return;
		setLeaving(false);
		activeRef.current = next;
		setActive(next);
	};

	useEffect(() => {
		host = (options) => new Promise<boolean>((resolve) => {
			queue.current.push({ options, resolve });
			// 用微任务推一下：host 赋值发生在同一轮渲染里，立刻 pump 可能拿到旧 state。
			window.setTimeout(pump, 0);
		});
		return () => { host = null; };
		// pump 只读写 ref / setState，无需随渲染重建。
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, []);

	// 打开时把焦点放到确认按钮：键盘用户可以直接回车。
	useEffect(() => {
		if (!active) return;
		confirmRef.current?.focus();
	}, [active]);

	const settle = (value: boolean) => {
		const done = activeRef.current;
		if (!done) return;
		setLeaving(true);
		// 120ms 让淡出动画走完再卸载，避免弹窗"啪"地消失。
		window.setTimeout(() => {
			done.resolve(value);
			// 先清 ref 再排下一轮：pump 依赖它判断「席位空出来了」。
			activeRef.current = null;
			setActive(null);
			window.setTimeout(pump, 0);
		}, 120);
	};

	useEffect(() => {
		if (!active) return;
		const onKeyDown = (event: KeyboardEvent) => {
			if (event.key === 'Escape') { event.stopPropagation(); settle(false); }
			if (event.key === 'Enter') { event.preventDefault(); settle(true); }
		};
		window.addEventListener('keydown', onKeyDown, true);
		return () => window.removeEventListener('keydown', onKeyDown, true);
	}, [active]);

	if (!active) return null;
	const { options } = active;
	const danger = options.tone === 'danger';
	const onlyConfirm = options.cancelText === null;

	return createPortal(
		<div className={`confirm-dialog-overlay${leaving ? ' is-leaving' : ''}`} role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) settle(false); }}>
			<section
				className={`confirm-dialog${danger ? ' is-danger' : ''}`}
				role="alertdialog"
				aria-modal="true"
				aria-labelledby="confirm-dialog-title"
				aria-describedby={options.message ? 'confirm-dialog-message' : undefined}
			>
				<header>
					<span className="confirm-dialog-icon" aria-hidden="true">
						{danger ? <AlertTriangle size={17} /> : <Info size={17} />}
					</span>
					<h2 id="confirm-dialog-title">{options.title}</h2>
				</header>
				{options.message && <p id="confirm-dialog-message">{options.message}</p>}
				<footer>
					{!onlyConfirm && (
						<button type="button" className="confirm-dialog-cancel" onClick={() => settle(false)}>
							{options.cancelText || '取消'}
						</button>
					)}
					<button
						type="button"
						ref={confirmRef}
						className={`confirm-dialog-ok${danger ? ' is-danger' : ''}`}
						onClick={() => settle(true)}
					>
						{options.confirmText || '确定'}
					</button>
				</footer>
			</section>
		</div>,
		document.body,
	);
}
