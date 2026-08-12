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
