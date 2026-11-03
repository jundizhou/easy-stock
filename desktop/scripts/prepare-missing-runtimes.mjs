// 增量补齐缺失运行时（hermes-runtime + wechat-download-api）。
// 仅用于 GitHub codeload 不可达时，通过国内代理源（ghproxy.net）补包。
// 环境变量由调用方 export：
//   HERMES_AGENT_SOURCE            hermes tar.gz 的直接 URL（含代理前缀）
//   WECHAT_DOWNLOAD_API_REPOSITORY wechat 仓库地址（含代理前缀）
// 这两个值会在对应 *.mjs import 时读取，因此必须在 node 启动前就 export 好。
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { prepareHermesRuntime } from './hermes-runtime.mjs';
import { prepareWechatRuntime } from './wechat-runtime.mjs';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const resourcesRoot = path.join(desktopRoot, 'resources');
const hermesRoot = path.join(resourcesRoot, 'hermes-runtime');

console.log('[1/2] Preparing Hermes runtime via', process.env.HERMES_AGENT_SOURCE || '(default github)');
const hermesManifest = prepareHermesRuntime({ runtimeRoot: hermesRoot });
console.log(`    Hermes ${hermesManifest.version} (${hermesManifest.mode}) ready at ${hermesRoot}`);

console.log('[2/2] Preparing WeChat download-api via', process.env.WECHAT_DOWNLOAD_API_REPOSITORY || '(default github)');
const wechatManifest = prepareWechatRuntime({ resourcesRoot, runtimeRoot: hermesRoot });
console.log(`    WeChat ${wechatManifest.revision.slice(0, 12)} ready`);

console.log('Done: desktop/resources now contains hermes-runtime + wechat-download-api');
