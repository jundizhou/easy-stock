import fs from 'node:fs';
import path from 'node:path';

const sourceRoot = path.resolve(process.argv[2] || '');
const outputRoot = path.resolve(process.argv[3] || '');
const releaseTag = process.argv[4] || '';
if (!sourceRoot || !outputRoot || !/^v\d+\.\d+\.\d+(?:[-+].+)?$/.test(releaseTag)) {
  throw new Error('Usage: node prepare-publish-assets.mjs <source-root> <output-root> <release-tag>');
}

const version = releaseTag.slice(1);
const githubNames = [
  `easy-stock-v${version}-macos-arm64.dmg`,
  `easy-stock-v${version}-macos-x64.dmg`,
  `easy-stock-v${version}-windows-x64-setup.exe`,
];
const updaterNames = [
  `easy-stock-v${version}-macos-arm64.zip`,
  `easy-stock-v${version}-macos-arm64.zip.blockmap`,
  `easy-stock-v${version}-macos-x64.zip`,
  `easy-stock-v${version}-macos-x64.zip.blockmap`,
  `easy-stock-v${version}-windows-x64-setup.exe`,
  `easy-stock-v${version}-windows-x64-setup.exe.blockmap`,
  'latest-mac.yml',
  'latest.yml',
];

for (const name of [...new Set([...githubNames, ...updaterNames])]) {
  const source = path.join(sourceRoot, name);
  if (!fs.statSync(source, { throwIfNoEntry: false })?.isFile()) throw new Error(`Required release asset is missing: ${name}`);
}

for (const [directory, names] of [['github', githubNames], ['updater', updaterNames]]) {
  const targetRoot = path.join(outputRoot, directory);
  fs.rmSync(targetRoot, { recursive: true, force: true });
  fs.mkdirSync(targetRoot, { recursive: true });
  for (const name of names) fs.copyFileSync(path.join(sourceRoot, name), path.join(targetRoot, name));
}

console.log(`Prepared ${githubNames.length} GitHub assets and ${updaterNames.length} updater assets for ${releaseTag}`);
