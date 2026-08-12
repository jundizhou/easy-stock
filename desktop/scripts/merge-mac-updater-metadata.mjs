import fs from 'node:fs';
import path from 'node:path';

const releaseRoot = path.resolve(process.argv[2] || '');
if (!releaseRoot) throw new Error('Usage: node merge-mac-updater-metadata.mjs <release-root>');
const metadataFiles = fs.readdirSync(releaseRoot)
  .filter((name) => /^latest-mac-(?:arm64|x64)\.yml$/.test(name))
  .sort();
if (metadataFiles.length !== 2) throw new Error(`Expected arm64 and x64 macOS metadata, found: ${metadataFiles.join(', ')}`);
const documents = metadataFiles.map((name) => parseMetadata(fs.readFileSync(path.join(releaseRoot, name), 'utf8')));
if (!documents.every((document) => document.version === documents[0].version)) throw new Error('macOS updater metadata versions do not match');
if (!documents.every((document) => document.fileBlocks.length === 1)) {
  throw new Error('macOS updater metadata must contain one ZIP per architecture');
}
const mergedFiles = documents.flatMap((document) => document.fileBlocks);
const merged = replaceFilesBlock(documents[0].source, documents[0].filesStart, documents[0].filesEnd, mergedFiles);
fs.writeFileSync(path.join(releaseRoot, 'latest-mac.yml'), merged);
console.log(`Merged macOS updater metadata for ${documents.map((document) => document.urls[0]).join(' and ')}`);

function parseMetadata(source) {
  const version = source.match(/^version:\s*(.+?)\s*$/m)?.[1];
  if (!version) throw new Error('macOS updater metadata version is missing');
  const filesHeader = /^files:\s*$/m.exec(source);
  if (!filesHeader) throw new Error('macOS updater metadata files list is missing');
  const filesStart = filesHeader.index + filesHeader[0].length;
  const remainder = source.slice(filesStart);
  const nextTopLevel = /^\S.*$/m.exec(remainder);
  const filesEnd = nextTopLevel ? filesStart + nextTopLevel.index : source.length;
  const filesBlock = source.slice(filesStart, filesEnd);
  const starts = [...filesBlock.matchAll(/^\s*-\s+url:\s*(.+?)\s*$/gm)];
  const fileBlocks = starts.map((match, index) => {
    const start = match.index;
    const end = starts[index + 1]?.index ?? filesBlock.length;
    return filesBlock.slice(start, end).trimEnd();
  });
  const urls = starts.map((match) => unquote(match[1]));
  return { source, version: unquote(version), filesStart, filesEnd, fileBlocks, urls };
}

function replaceFilesBlock(source, start, end, fileBlocks) {
  return `${source.slice(0, start)}\n${fileBlocks.join('\n')}`.trimEnd() + `\n${source.slice(end).trimStart()}`;
}

function unquote(value) {
  return value.replace(/^(?:"(.*)"|'(.*)')$/, '$1$2');
}
