import fs from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';

const releaseRoot = path.resolve(process.argv[2] || '');
if (!releaseRoot) throw new Error('Usage: node merge-mac-updater-metadata.mjs <release-root>');
const metadataFiles = fs.readdirSync(releaseRoot)
  .filter((name) => /^latest-mac-(?:arm64|x64)\.yml$/.test(name))
  .sort();
if (metadataFiles.length !== 2) throw new Error(`Expected arm64 and x64 macOS metadata, found: ${metadataFiles.join(', ')}`);
const documents = metadataFiles.map((name) => yaml.load(fs.readFileSync(path.join(releaseRoot, name), 'utf8')));
if (!documents.every((document) => document.version === documents[0].version)) throw new Error('macOS updater metadata versions do not match');
const files = documents.flatMap((document) => document.files || []);
if (files.length !== 2) throw new Error('macOS updater metadata must contain one ZIP per architecture');
const merged = {
  ...documents[0],
  files,
  path: files[0].url,
  sha512: files[0].sha512,
};
fs.writeFileSync(path.join(releaseRoot, 'latest-mac.yml'), yaml.dump(merged, { lineWidth: -1, noRefs: true }));
console.log(`Merged macOS updater metadata for ${files.map((file) => file.url).join(' and ')}`);
