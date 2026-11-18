import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { prepareZsxqRuntime, verifyZsxqRuntime } from './zsxq-runtime.mjs';

const runtimeRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../resources/zsxq-runtime');
try { verifyZsxqRuntime(runtimeRoot); }
catch { await prepareZsxqRuntime({ runtimeRoot }); }
console.log('Knowledge planet CLI runtime ready');
