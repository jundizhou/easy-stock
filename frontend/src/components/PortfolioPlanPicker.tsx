import { Check, Pencil, Plus, Trash2, WalletCards, X } from 'lucide-react';
import { useState } from 'react';
import { activePortfolioPlan, portfolioPlanNameLimit, type PortfolioPlans } from '../lib/portfolio-draft';

type Props = {
	state: PortfolioPlans;
	disabled: boolean;
	onSelect: (id: string) => void;
	onAdd: () => void;
	onRename: (name: string) => boolean;
	onRemove: () => boolean | Promise<boolean>;
};

export function PortfolioPlanPicker({ state, disabled, onSelect, onAdd, onRename, onRemove }: Props) {
	const active = activePortfolioPlan(state);
	const [editing, setEditing] = useState(false);
	const [name, setName] = useState(active.name);
	const [confirming, setConfirming] = useState(false);
	return <section className="portfolio-plan-picker" aria-label="持仓方案">
		<header><strong><WalletCards size={16} />持仓方案</strong>{disabled && <small>巡检启动或运行中，完成后可切换方案</small>}</header>
		<div className="portfolio-plan-controls">
			<div className="portfolio-plan-options" role="group" aria-label="选择持仓方案">
				{state.plans.map((plan) => <button type="button" key={plan.id} className={plan.id === active.id ? 'active' : ''} aria-pressed={plan.id === active.id} disabled={disabled} onClick={() => onSelect(plan.id)} title={plan.name}><span>{plan.name}</span><small>{plan.draft.holdings.length} 只</small></button>)}
				<button type="button" className="portfolio-plan-add" onClick={onAdd} disabled={disabled} aria-label="添加持仓方案" title="添加持仓方案"><Plus size={18} /></button>
			</div>
			<div className="portfolio-plan-actions">
				<button type="button" disabled={disabled} onClick={() => { setName(active.name); setEditing(true); setConfirming(false); }} aria-label="重命名当前方案" title="重命名当前方案"><Pencil size={15} /></button>
				<button type="button" disabled={disabled || state.plans.length <= 1} onClick={() => { setConfirming(true); setEditing(false); }} aria-label="删除当前方案" title={state.plans.length <= 1 ? '至少保留一份持仓方案' : '删除当前方案'}><Trash2 size={15} /></button>
			</div>
		</div>
		{editing && <form className="portfolio-plan-edit" onSubmit={(event) => { event.preventDefault(); if (onRename(name)) setEditing(false); }}>
			<input aria-label="方案名称" value={name} onChange={(event) => setName(event.target.value)} maxLength={portfolioPlanNameLimit} autoFocus disabled={disabled} onKeyDown={(event) => { if (event.key === 'Escape') setEditing(false); }} />
			<button type="submit" disabled={disabled || !name.trim()}><Check size={14} />保存名称</button>
			<button type="button" onClick={() => setEditing(false)}><X size={14} />取消</button>
		</form>}
		{confirming && <div className="portfolio-plan-delete" role="alert"><span>删除“{active.name}”及其持仓配置？定时巡检将一并停止，已生成的巡检记录会保留。</span><button type="button" disabled={disabled} onClick={async () => { if (await onRemove()) setConfirming(false); }}>确认删除方案</button><button type="button" onClick={() => setConfirming(false)}>取消</button></div>}
	</section>;
}
