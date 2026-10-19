import { spawn } from 'node:child_process';
import electron from 'electron';

const child = spawn(electron, ['.'], {
	stdio: 'inherit',
	env: { ...process.env, ELECTRON_RENDERER_URL: process.env.ELECTRON_RENDERER_URL || 'http://127.0.0.1:20073' },
});
child.on('exit', (code) => process.exit(code ?? 0));
