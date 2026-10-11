import type { BackendConfig, StockAIAnalysis } from '../lib/backend';
import { StockAIAnalysisWorkspace, type StockAIWorkspaceMode } from './StockAIAnalysisWorkspace';
import { PortfolioInspectionWorkspace } from './PortfolioInspectionWorkspace';

export type StockAiTab = 'analysis' | 'inspection';

type Props = {
	config: BackendConfig | null;
	stockAIRefreshKey: number;
	portfolioRefreshKey: number;
	tab: StockAiTab;
	onTabChange: (tab: StockAiTab) => void;
	stockAIMode: StockAIWorkspaceMode;
	onStockAIModeChange: (mode: StockAIWorkspaceMode) => void;
	stockAIInitialAnalysis: StockAIAnalysis | null;
	onStockAIInitialAnalysisConsumed: () => void;
	stockAIInitialSymbol: string;
	onStockAIInitialSymbolConsumed: () => void;
	onAskAI: (analysis: StockAIAnalysis) => void;
	onOpenSettings: () => void;
	/** portfolio 巡检里的「查看个股报告」联动：切到个股分析 tab 并把报告交给分析工作台。 */
	onOpenStockAnalysisFromPortfolio: (analysis: StockAIAnalysis) => void;
};

/**
 * 个股分析 + 持仓巡检 的合并外壳（tab 化）。
 *
 * 两者都围绕「逐股 AI 研判」：个股分析是单只标的的多周期评分与情景推演，持仓巡检是组合级
 * 逐股扫描与优化。原本是两个独立左侧板块，功能高度重叠（都调 StockAIAnalysis、都能跳到个股报告），
 * 因此合并为一个「个股分析」板块，内部用 tab 切换，不再占两个侧栏入口。
 *
 * 壳只做 tab 切换与联动转发，不侵入两个子工作台的现有逻辑。
 */
export function StockAiWorkspace({
	config,
	stockAIRefreshKey,
	portfolioRefreshKey,
	tab,
	onTabChange,
	stockAIMode,
	onStockAIModeChange,
	stockAIInitialAnalysis,
	onStockAIInitialAnalysisConsumed,
	stockAIInitialSymbol,
	onStockAIInitialSymbolConsumed,
	onAskAI,
	onOpenSettings,
	onOpenStockAnalysisFromPortfolio,
}: Props) {
	return (
		<section className="stock-ai-workspace" aria-label="个股分析与持仓巡检">
			<div className="stock-ai-tab-bar" role="tablist" aria-label="个股分析 / 持仓巡检">
				<button
					type="button"
					role="tab"
					aria-selected={tab === 'analysis'}
					className={tab === 'analysis' ? 'active' : ''}
					onClick={() => onTabChange('analysis')}
				>
					个股分析
				</button>
				<button
					type="button"
					role="tab"
					aria-selected={tab === 'inspection'}
					className={tab === 'inspection' ? 'active' : ''}
					onClick={() => onTabChange('inspection')}
				>
					持仓巡检
				</button>
			</div>
			{tab === 'analysis' ? (
				<StockAIAnalysisWorkspace
					config={config}
					refreshKey={stockAIRefreshKey}
					mode={stockAIMode}
					onModeChange={onStockAIModeChange}
					initialAnalysis={stockAIInitialAnalysis}
					onInitialAnalysisConsumed={onStockAIInitialAnalysisConsumed}
					initialSymbol={stockAIInitialSymbol}
					onInitialSymbolConsumed={onStockAIInitialSymbolConsumed}
					onAskAI={onAskAI}
					onOpenSettings={onOpenSettings}
				/>
			) : (
				<PortfolioInspectionWorkspace
					config={config}
					refreshKey={portfolioRefreshKey}
					onOpenSettings={onOpenSettings}
					onOpenStockAnalysis={(analysis) => {
						onTabChange('analysis');
						onOpenStockAnalysisFromPortfolio(analysis);
					}}
				/>
			)}
		</section>
	);
}
