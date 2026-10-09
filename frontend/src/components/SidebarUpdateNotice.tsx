import { CircleArrowUp } from 'lucide-react';
import { hasNewAppVersion, useAppUpdateStatus } from '../lib/use-app-update-status';

export function SidebarUpdateNotice({ onOpenUpdates }: { onOpenUpdates: () => void }) {
	const { status } = useAppUpdateStatus();
	if (!hasNewAppVersion(status)) return null;
	const version = status.latestVersion?.replace(/^v/, '');
	const label = `发现新版本 v${version}，打开版本更新设置`;
	return <button type="button" className="sidebar-update-notice" onClick={onOpenUpdates} aria-label={label} title={label}>
		<CircleArrowUp size={14} aria-hidden="true" /><span>新版本 v{version}</span>
	</button>;
}
