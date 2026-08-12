import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';

const releaseRoot = path.resolve(process.argv[2] || '');
const metadataNames = process.argv.slice(3);
if (!releaseRoot || !metadataNames.length) {
  throw new Error('Usage: node verify-updater-artifacts.mjs <release-root> <latest.yml> [latest-mac.yml]');
}

for (const metadataName of metadataNames) {
  const metadataPath = path.join(releaseRoot, metadataName);
  if (!fs.existsSync(metadataPath)) throw new Error(`Updater metadata not found: ${metadataName}`);
  const metadata = fs.readFileSync(metadataPath, 'utf8');
  const entries = parseFiles(metadata);
  if (!entries.length) throw new Error(`Updater metadata contains no files: ${metadataName}`);
  for (const entry of entries) {
    const assetPath = path.join(releaseRoot, entry.url);
    if (!fs.existsSync(assetPath)) throw new Error(`${metadataName} references missing asset: ${entry.url}`);
    const digest = crypto.createHash('sha512').update(fs.readFileSync(assetPath)).digest('base64');
    if (digest !== entry.sha512) throw new Error(`${metadataName} SHA-512 mismatch: ${entry.url}`);
    if (entry.url.endsWith('.exe') && !fs.existsSync(`${assetPath}.blockmap`)) {
      throw new Error(`Windows differential update blockmap is missing: ${entry.url}.blockmap`);
    }
  }
  console.log(`Updater metadata verified: ${metadataName}`);
}

function parseFiles(metadata) {
  const lines = metadata.split(/\r?\n/);
  const entries = [];
  let current;
  for (const line of lines) {
    const url = line.match(/^\s*- url:\s*(.+?)\s*$/)?.[1];
    if (url) {
      current = { url: unquote(url), sha512: '' };
      entries.push(current);
      continue;
    }
    const sha512 = line.match(/^\s+sha512:\s*(.+?)\s*$/)?.[1];
    if (sha512 && current) current.sha512 = unquote(sha512);
  }
  return entries.filter((entry) => entry.url && entry.sha512);
}

function unquote(value) {
  return value.replace(/^(?:"(.*)"|'(.*)')$/, '$1$2');
}
