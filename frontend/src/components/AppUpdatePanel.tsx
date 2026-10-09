import { CheckCircle2, Download, ExternalLink, FolderOpen, HardDriveDownload, LoaderCircle, RefreshCw, RotateCcw, ShieldCheck } from 'lucide-react';
import { useState } from 'react';
import type { AppUpdateStatus } from '../lib/backend';
import { useAppUpdateStatus } from '../lib/use-app-update-status';
import { SettingsSection } from './SettingsSection';

export function updatePrimaryAction(status: AppUpdateStatus): 'check' | 'download' | 'install' | 'release' {
	if (status.state === 'available') return status.installMode === 'manual' ? 'release' : 'download';
	if (status.state === 'downloaded') return status.installMode === 'manual' ? 'release' : 'install';
	return 'check';
}

export function AppUpdatePanel() {
	const bridge = window.aStock;
	const { status, setStatus, error: statusError } = useAppUpdateStatus();
	const [actionError, setActionError] = useState('');

	const run = async (action?: () => Promise<AppUpdateStatus | void>) => {
		if (!action) return;
		setActionError('');
		try {
			const next = await action();
			if (next) setStatus(next);
		} catch (error) {
			setActionError(error instanceof Error ? error.message : '更新操作失败');
		}
	};

	const busy = status.state === 'checking' || status.state === 'downloading' || status.state === 'installing';
	const action = updatePrimaryAction(status);
	const primaryAction = action === 'release'
		? { label: '前往下载新版', icon: <ExternalLink size={15} />, action: bridge?.openUpdateRelease }
		: action === 'download'
		? { label: '下载更新', icon: <Download size={15} />, action: bridge?.downloadUpdate }
		: action === 'install'
			? { label: '重启并安装', icon: <RotateCcw size={15} />, action: bridge?.installUpdate }
			: { label: status.state === 'checking' ? '正在检查' : '检查更新', icon: status.state === 'checking' ? <LoaderCircle className="spin" size={15} /> : <RefreshCw size={15} />, action: bridge?.checkForUpdates };

	return (
		<SettingsSection className="app-update-section" title="版本与自动更新" description="Windows 支持应用内更新；macOS 未配置 Apple Developer ID 时通过发布页手动下载安装。" icon={<HardDriveDownload size={18} />}>
			<div className={`app-update-status ${status.state}`}>
				<div className="app-update-version">
					<span><strong>v{status.currentVersion}</strong><small>当前版本</small></span>
					{status.latestVersion && status.latestVersion !== status.currentVersion && <><em>→</em><span><strong>v{status.latestVersion}</strong><small>最新版本</small></span></>}
				</div>
				<div className="app-update-message">{status.state === 'downloaded' ? <CheckCircle2 size={15} /> : busy ? <LoaderCircle className="spin" size={15} /> : <ShieldCheck size={15} />}<span>{actionError || statusError || status.message}</span></div>
				{status.state === 'downloading' && <div className="app-update-progress" aria-label={`下载进度 ${Math.round(status.progress)}%`}><span style={{ width: `${status.progress}%` }} /><em>{Math.round(status.progress)}%</em></div>}
				{status.releaseNotes && <details className="app-update-notes"><summary>{status.releaseName || '查看更新说明'}</summary><p>{status.releaseNotes}</p></details>}
				<div className="app-update-actions">
					<button type="button" className="primary" onClick={() => void run(primaryAction.action)} disabled={!status.supported || busy}>{primaryAction.icon}{primaryAction.label}</button>
					{status.latestVersion && action !== 'release' && <button type="button" onClick={() => void run(bridge?.openUpdateRelease)}><ExternalLink size={14} />发布页</button>}
					<button type="button" onClick={() => void run(bridge?.openUpdateBackups)} disabled={!status.supported}><FolderOpen size={14} />备份目录</button>
				</div>
				{status.installMode === 'manual' && status.latestVersion && status.latestVersion !== status.currentVersion && <p className="settings-field-note app-update-manual-note">当前 macOS 安装包未使用 Apple Developer ID 签名，系统暂不允许应用内替换。退出 easy-stock，前往发布页下载新版 DMG 后覆盖安装，不会删除本地模型配置、文章、登录状态或数据库。</p>}
			</div>
			<p className="settings-field-note">{status.installMode === 'manual' ? '应用程序与用户数据分开存放，覆盖安装只替换 easy-stock 应用本身，不会清除本地模型密钥、导入文章、AI 摘要、Agent 记忆、浏览器/微信登录态或数据库。' : '应用内安装前会停止后台同步并在应用数据目录外创建完整备份，保留模型配置与密钥、导入文章、AI 摘要、Agent 记忆、浏览器/微信登录态及本地数据库；仅排除可重建缓存，最近保留 3 份。'}</p>
		</SettingsSection>
	);
}
