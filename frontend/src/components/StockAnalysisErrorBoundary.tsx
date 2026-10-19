import { Component, type ErrorInfo, type ReactNode } from 'react';
import { CircleAlert, RefreshCw } from 'lucide-react';
import { logRuntimeEvent, runtimeErrorDetails } from '../lib/runtime-log';

// Keep search and history usable when a saved report cannot be rendered.
export class StockAnalysisErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
	state = { failed: false };

	static getDerivedStateFromError() {
		return { failed: true };
	}

	componentDidCatch(error: Error, info: ErrorInfo) {
		logRuntimeEvent('error', 'stock-analysis', { event: 'analysis_render_error', error: runtimeErrorDetails(error), component_stack: info.componentStack });
	}

	render() {
		if (!this.state.failed) return this.props.children;
		return <div className="stock-ai-error" role="alert">
			<CircleAlert size={22} aria-hidden="true" />
			<div><strong>分析结果暂时无法显示</strong><span>请重试显示，或从上方选择其他记录、重新分析。</span></div>
			<button type="button" onClick={() => this.setState({ failed: false })}><RefreshCw size={14} aria-hidden="true" />重试显示</button>
		</div>;
	}
}
