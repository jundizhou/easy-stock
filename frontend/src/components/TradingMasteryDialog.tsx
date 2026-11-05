import { BookMarked, X } from 'lucide-react';
import { useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';
import type { BackendConfig } from '../lib/backend';
import { TradingMastery } from './TradingMastery';

type Props = {
	config: BackendConfig | null;
	onAskAI: (traderName: string) => void;
	onClose: () => void;
};

export function TradingMasteryDialog({ config, onAskAI, onClose }: Props) {
	const dialogRef = useRef<HTMLDialogElement>(null);
	useEffect(() => {
		const dialog = dialogRef.current;
		dialog?.showModal();
		return () => { if (dialog?.open) dialog.close(); };
	}, []);

	return createPortal(
		<dialog
			ref={dialogRef}
			className="mastery-dialog"
			aria-labelledby="mastery-dialog-title"
			onCancel={(event) => { event.preventDefault(); onClose(); }}
			onMouseDown={(event) => {
			if (event.target !== event.currentTarget) return;
			const bounds = event.currentTarget.getBoundingClientRect();
			if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) onClose();
			}}
		>
			<header className="mastery-dialog-header">
				<h2 id="mastery-dialog-title"><BookMarked size={19} aria-hidden="true" />游资心法</h2>
				<button type="button" onClick={onClose} aria-label="关闭游资心法"><X size={20} aria-hidden="true" /></button>
			</header>
			<div className="mastery-dialog-body"><TradingMastery config={config} onAskAI={onAskAI} /></div>
		</dialog>,
		document.body,
	);
}
