import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const packageManifest = JSON.parse(fs.readFileSync(path.join(desktopRoot, 'package.json'), 'utf8'));
const expectedTag = `v${packageManifest.version}`;
const actualTag = process.argv[2] || process.env.GITHUB_REF_NAME || '';

if (actualTag !== expectedTag) {
	throw new Error(`Release tag ${actualTag || '(empty)'} does not match desktop version ${expectedTag}`);
}
console.log(`Release tag verified: ${actualTag}`);
