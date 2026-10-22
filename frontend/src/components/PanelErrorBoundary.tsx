import { Component, type ReactNode } from 'react';

type Props = { title: string; children: ReactNode };
type State = { failed: boolean };

// 单个模块渲染出错时只替换它自己，不让整个应用白屏。
export class PanelErrorBoundary extends Component<Props, State> {
	state: State = { failed: false };

	static getDerivedStateFromError(): State {
		return { failed: true };
	}

	render() {
		if (this.state.failed) {
			return (
				<section className="panel-error" role="alert">
					<strong>{this.props.title}暂时无法显示</strong>
					<small>其他模块不受影响；刷新页面后会重新加载。</small>
				</section>
			);
		}
		return this.props.children;
	}
}
