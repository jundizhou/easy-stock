import { useEffect, useState } from 'react';
import type { AppUpdateStatus } from './backend';

const developmentStatus: AppUpdateStatus = {
	state: 'disabled',
	supported: false,
	currentVersion: '开发模式',
	message: '安装版会自动检查正式更新源',
	progress: 0,
};

export function hasNewAppVersion(status: AppUpdateStatus): boolean {
	if (!status.supported || status.state === 'disabled' || !status.latestVersion) return false;
	const parse = (version: string) => /^v?(\d+)\.(\d+)\.(\d+)(?:-([\w.-]+))?(?:\+[\w.-]+)?$/.exec(version);
	const latest = parse(status.latestVersion);
	const current = parse(status.currentVersion);
	if (!latest || !current) return false;
	for (let index = 1; index <= 3; index += 1) {
		if (Number(latest[index]) !== Number(current[index])) return Number(latest[index]) > Number(current[index]);
	}
	return Boolean(current[4] && !latest[4]);
}

export function useAppUpdateStatus() {
	const bridge = window.aStock;
	const [status, setStatus] = useState<AppUpdateStatus>(developmentStatus);
	const [error, setError] = useState('');

	useEffect(() => {
		let active = true;
		let receivedEvent = false;
		const unsubscribe = bridge?.onUpdateStatus?.((next) => {
			if (!active) return;
			receivedEvent = true;
			setStatus(next);
			setError('');
		});
		void bridge?.getUpdateStatus?.().then((next) => {
			if (active && !receivedEvent) setStatus(next);
		}).catch((cause) => {
			if (active && !receivedEvent) setError(cause instanceof Error ? cause.message : '读取版本状态失败');
		});
		return () => {
			active = false;
			unsubscribe?.();
		};
	}, [bridge]);

	return { status, setStatus, error };
}
