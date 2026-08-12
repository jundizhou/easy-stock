const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');
const { execFileSync } = require('node:child_process');

function sha512Base64(filePath) {
  return execFileSync(process.execPath, ['-e', `const fs=require('fs'),crypto=require('crypto');process.stdout.write(crypto.createHash('sha512').update(fs.readFileSync(process.argv[1])).digest('base64'))`, filePath], { encoding: 'utf8' });
}

test('release metadata points to existing assets with matching sha512', () => {
  const releaseRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-release-'));
  const assets = ['easy-stock-v0.4.0-macos-arm64.zip', 'easy-stock-v0.4.0-windows-x64-setup.exe'];
  for (const asset of assets) fs.writeFileSync(path.join(releaseRoot, asset), `fixture:${asset}`);
  fs.writeFileSync(path.join(releaseRoot, 'latest-mac.yml'), `version: 0.4.0\nfiles:\n  - url: ${assets[0]}\n    sha512: ${sha512Base64(path.join(releaseRoot, assets[0]))}\n`);
  fs.writeFileSync(path.join(releaseRoot, 'latest.yml'), `version: 0.4.0\nfiles:\n  - url: ${assets[1]}\n    sha512: ${sha512Base64(path.join(releaseRoot, assets[1]))}\n`);

  for (const metadataName of ['latest-mac.yml', 'latest.yml']) {
    const metadata = fs.readFileSync(path.join(releaseRoot, metadataName), 'utf8');
    const url = metadata.match(/^\s*- url:\s*(.+)$/m)?.[1]?.trim();
    const expectedHash = metadata.match(/^\s*sha512:\s*(.+)$/m)?.[1]?.trim();
    assert.ok(url, `${metadataName} should contain an asset URL`);
    const assetPath = path.join(releaseRoot, url);
    assert.ok(fs.existsSync(assetPath), `${url} should exist`);
    assert.equal(expectedHash, sha512Base64(assetPath));
  }
});

test('merges macOS updater metadata without installed npm dependencies', () => {
  const releaseRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-mac-metadata-'));
  const scriptPath = path.resolve(__dirname, '..', 'scripts', 'merge-mac-updater-metadata.mjs');
  const fixtures = [
    ['arm64', 'easy-stock-v0.4.0-macos-arm64.zip', 'arm-hash', 101],
    ['x64', 'easy-stock-v0.4.0-macos-x64.zip', 'x64-hash', 202],
  ];
  for (const [arch, url, hash, size] of fixtures) {
    fs.writeFileSync(path.join(releaseRoot, `latest-mac-${arch}.yml`), [
      'version: 0.4.0',
      'files:',
      `  - url: ${url}`,
      `    sha512: ${hash}`,
      `    size: ${size}`,
      `path: ${url}`,
      `sha512: ${hash}`,
      'releaseDate: 2026-08-12T00:00:00.000Z',
      '',
    ].join('\n'));
  }

  execFileSync(process.execPath, [scriptPath, releaseRoot], {
    cwd: os.tmpdir(),
    env: { PATH: process.env.PATH },
  });

  const merged = fs.readFileSync(path.join(releaseRoot, 'latest-mac.yml'), 'utf8');
  assert.match(merged, /version: 0\.4\.0/);
  assert.match(merged, /easy-stock-v0\.4\.0-macos-arm64\.zip/);
  assert.match(merged, /easy-stock-v0\.4\.0-macos-x64\.zip/);
  assert.equal((merged.match(/^\s*- url:/gm) || []).length, 2);
  assert.match(merged, /path: easy-stock-v0\.4\.0-macos-arm64\.zip/);
});

test('separates user downloads from internal updater assets', () => {
  const sourceRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-release-assets-'));
  const outputRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-publish-assets-'));
  const version = '0.4.0';
  const names = [
    `easy-stock-v${version}-macos-arm64.dmg`,
    `easy-stock-v${version}-macos-x64.dmg`,
    `easy-stock-v${version}-macos-arm64.zip`,
    `easy-stock-v${version}-macos-arm64.zip.blockmap`,
    `easy-stock-v${version}-macos-x64.zip`,
    `easy-stock-v${version}-macos-x64.zip.blockmap`,
    `easy-stock-v${version}-windows-x64-setup.exe`,
    `easy-stock-v${version}-windows-x64-setup.exe.blockmap`,
    'latest-mac.yml',
    'latest.yml',
  ];
  for (const name of names) fs.writeFileSync(path.join(sourceRoot, name), name);

  const scriptPath = path.resolve(__dirname, '..', 'scripts', 'prepare-publish-assets.mjs');
  execFileSync(process.execPath, [scriptPath, sourceRoot, outputRoot, `v${version}`]);

  assert.deepEqual(fs.readdirSync(path.join(outputRoot, 'github')).sort(), [
    `easy-stock-v${version}-macos-arm64.dmg`,
    `easy-stock-v${version}-macos-x64.dmg`,
    `easy-stock-v${version}-windows-x64-setup.exe`,
  ]);
  assert.deepEqual(fs.readdirSync(path.join(outputRoot, 'updater')).sort(), [
    `easy-stock-v${version}-macos-arm64.zip`,
    `easy-stock-v${version}-macos-arm64.zip.blockmap`,
    `easy-stock-v${version}-macos-x64.zip`,
    `easy-stock-v${version}-macos-x64.zip.blockmap`,
    `easy-stock-v${version}-windows-x64-setup.exe`,
    `easy-stock-v${version}-windows-x64-setup.exe.blockmap`,
    'latest-mac.yml',
    'latest.yml',
  ].sort());
});
