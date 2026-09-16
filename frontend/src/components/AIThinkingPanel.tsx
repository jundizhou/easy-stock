import { BrainCircuit, ChevronRight } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';

export function AIThinkingPanel({ content, pending }: { content?: string; pending: boolean }) {
	const [expanded, setExpanded] = useState<boolean | null>(null);
	const bodyRef = useRef<HTMLDivElement>(null);
	const followEnd = useRef(true);
	const open = expanded ?? pending;

	useEffect(() => {
		if (open && followEnd.current && bodyRef.current) {
			bodyRef.current.scrollTop = bodyRef.current.scrollHeight;
		}
	}, [content, open]);

	if (!content?.trim()) return null;

	return (
		<details className="ai-thinking" open={open}>
			<summary onClick={(event) => { event.preventDefault(); setExpanded(!open); }}>
				<BrainCircuit size={15} aria-hidden="true" />
				<strong>思考过程</strong>
				<span>{pending ? '实时更新' : open ? '收起' : '展开'}</span>
				<ChevronRight className="ai-thinking-chevron" size={14} aria-hidden="true" />
			</summary>
			<div className="ai-thinking-content" ref={bodyRef} onScroll={(event) => {
				const element = event.currentTarget;
				followEnd.current = element.scrollHeight - element.scrollTop - element.clientHeight < 32;
			}}>{content}</div>
		</details>
	);
}
