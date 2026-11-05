// 增量补齐 Hermes 运行时；代理下载地址可通过 HERMES_AGENT_SOURCE 设置。
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { HERMES_AGENT_SOURCE, prepareHermesRuntime } from './hermes-runtime.mjs';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const hermesRoot = path.join(desktopRoot, 'resources', 'hermes-runtime');
console.log('Preparing Hermes runtime via', process.env.HERMES_AGENT_SOURCE?.trim() || HERMES_AGENT_SOURCE);
const manifest = prepareHermesRuntime({ runtimeRoot: hermesRoot });
console.log(`Hermes ${manifest.version} (${manifest.mode}) ready at ${hermesRoot}`);
