const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');

const CACHE_DIRECTORY_NAMES = new Set([
  'Cache',
  'Code Cache',
  'GPUCache',
  'Crashpad',
  'DawnCache',
  'GrShaderCache',
  'ShaderCache',
  'easy-stock-updater',
]);

// Windows keeps files locked while lingering processes (antivirus scans,
// slow-exiting child runtimes) still hold handles; the codes below can clear
// within milliseconds-to-seconds, so retry before giving up on a file.
const COPY_RETRY_ERROR_CODES = new Set(['EBUSY', 'EPERM', 'EACCES']);
const COPY_RETRY_DELAYS_MS = [250, 500, 1000, 2000, 4000];

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function resolveBackupRoot(userDataPath, configuredPath = process.env.A_STOCK_UPDATE_BACKUP_DIR) {
  const userData = path.resolve(userDataPath);
  const backupRoot = path.resolve(configuredPath || path.join(path.dirname(userData), `${path.basename(userData)}-update-backups`));
  if (backupRoot === userData || backupRoot.startsWith(`${userData}${path.sep}`)) {
    throw new Error('更新备份目录不能位于应用数据目录内');
  }
  return backupRoot;
}

function shouldExclude(relativePath) {
  return relativePath.split(path.sep).some((part) => CACHE_DIRECTORY_NAMES.has(part));
}

function safeVersion(value) {
  return String(value || 'unknown').replace(/[^0-9A-Za-z._-]+/g, '-');
}

function timestamp(value = new Date()) {
  return value.toISOString().replace(/[:.]/g, '-');
}

function sha256(filePath) {
  const hash = crypto.createHash('sha256');
  const file = fs.openSync(filePath, 'r');
  const buffer = Buffer.allocUnsafe(1024 * 1024);
  try {
    let bytesRead;
    do {
      bytesRead = fs.readSync(file, buffer, 0, buffer.length, null);
      if (bytesRead) hash.update(buffer.subarray(0, bytesRead));
    } while (bytesRead);
  } finally {
    fs.closeSync(file);
  }
  return hash.digest('hex');
}

async function copyFileWithRetry(sourcePath, targetPath) {
  for (let attempt = 0; ; attempt += 1) {
    try {
      fs.copyFileSync(sourcePath, targetPath);
      return;
    } catch (error) {
      if (!COPY_RETRY_ERROR_CODES.has(error?.code) || attempt >= COPY_RETRY_DELAYS_MS.length) throw error;
      await delay(COPY_RETRY_DELAYS_MS[attempt]);
    }
  }
}

async function copyUserData(userDataPath, destinationPath) {
  const files = [];
  const skipped = [];
  if (!fs.existsSync(userDataPath)) return { files, skipped };

  const copyDirectory = async (sourceDirectory, targetDirectory, relativeDirectory = '') => {
    fs.mkdirSync(targetDirectory, { recursive: true, mode: 0o700 });
    for (const entry of fs.readdirSync(sourceDirectory, { withFileTypes: true })) {
      const relativePath = relativeDirectory ? path.join(relativeDirectory, entry.name) : entry.name;
      if (shouldExclude(relativePath)) continue;
      const sourcePath = path.join(sourceDirectory, entry.name);
      const targetPath = path.join(targetDirectory, entry.name);
      if (entry.isDirectory()) {
        await copyDirectory(sourcePath, targetPath, relativePath);
        continue;
      }
      if (entry.isSymbolicLink()) {
        try {
          fs.symlinkSync(fs.readlinkSync(sourcePath), targetPath);
          files.push({ path: relativePath, type: 'symlink' });
        } catch (error) {
          // Windows without developer mode rejects symlink creation; the file
          // behind it is still copied when it is a regular file elsewhere.
          skipped.push({ path: relativePath, type: 'skipped', reason: readableErrorReason(error) });
        }
        continue;
      }
      if (!entry.isFile()) continue;
      try {
        await copyFileWithRetry(sourcePath, targetPath);
      } catch (error) {
        // A file that stays locked must not abort the whole update; record it
        // so the manifest shows the backup is partial. Anything else is a real
        // backup failure.
        if (!COPY_RETRY_ERROR_CODES.has(error?.code)) throw error;
        skipped.push({ path: relativePath, type: 'skipped', reason: readableErrorReason(error) });
        continue;
      }
      const sourceStat = fs.statSync(sourcePath);
      fs.chmodSync(targetPath, sourceStat.mode & 0o777);
      files.push({ path: relativePath, type: 'file', size: sourceStat.size, sha256: sha256(targetPath) });
    }
  };

  await copyDirectory(userDataPath, destinationPath);
  return { files, skipped };
}

function readableErrorReason(error) {
  const message = error instanceof Error ? error.message : String(error || '未知错误');
  return message.replace(/(?:[A-Za-z]:\\|\/)[^\s*]+/g, '[本机路径]');
}

function pruneBackups(backupRoot, keep = 3) {
  const backups = fs.readdirSync(backupRoot, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && !entry.name.includes('.partial-'))
    .map((entry) => {
      const backupPath = path.join(backupRoot, entry.name);
      return { path: backupPath, modifiedAt: fs.statSync(backupPath).mtimeMs };
    })
    .sort((left, right) => right.modifiedAt - left.modifiedAt);
  for (const backup of backups.slice(Math.max(1, keep))) {
    fs.rmSync(backup.path, { recursive: true, force: true });
  }
}

async function createUpdateBackup({ userDataPath, backupRoot, fromVersion, toVersion, now = new Date(), keep = 3 }) {
  const userData = path.resolve(userDataPath);
  const resolvedBackupRoot = resolveBackupRoot(userData, backupRoot);
  fs.mkdirSync(resolvedBackupRoot, { recursive: true, mode: 0o700 });
  const backupName = `${timestamp(now)}-v${safeVersion(fromVersion)}-to-v${safeVersion(toVersion)}`;
  const finalPath = path.join(resolvedBackupRoot, backupName);
  const stagingPath = `${finalPath}.partial-${process.pid}`;
  if (fs.existsSync(finalPath) || fs.existsSync(stagingPath)) {
    throw new Error('同名更新备份已存在，请稍后重试');
  }

  try {
    fs.mkdirSync(stagingPath, { recursive: true, mode: 0o700 });
    const dataPath = path.join(stagingPath, 'data');
    const { files, skipped } = await copyUserData(userData, dataPath);
    const manifest = {
      schemaVersion: 1,
      createdAt: now.toISOString(),
      fromVersion: String(fromVersion || ''),
      toVersion: String(toVersion || ''),
      userDataDirectoryName: path.basename(userData),
      files,
      skipped,
    };
    const manifestTemporaryPath = path.join(stagingPath, 'manifest.json.tmp');
    fs.writeFileSync(manifestTemporaryPath, `${JSON.stringify(manifest, null, 2)}\n`, { mode: 0o600 });
    fs.renameSync(manifestTemporaryPath, path.join(stagingPath, 'manifest.json'));
    fs.renameSync(stagingPath, finalPath);
    try {
      pruneBackups(resolvedBackupRoot, keep);
    } catch {
      // Retention cleanup must never fail the update itself.
    }
    return { path: finalPath, backupRoot: resolvedBackupRoot, manifest };
  } catch (error) {
    fs.rmSync(stagingPath, { recursive: true, force: true });
    throw error;
  }
}

function listUpdateBackups(backupRoot) {
  if (!fs.existsSync(backupRoot)) return [];
  return fs.readdirSync(backupRoot, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && !entry.name.includes('.partial-'))
    .map((entry) => {
      const backupPath = path.join(backupRoot, entry.name);
      const manifestPath = path.join(backupPath, 'manifest.json');
      if (!fs.existsSync(manifestPath)) return null;
      try {
        return { path: backupPath, manifest: JSON.parse(fs.readFileSync(manifestPath, 'utf8')) };
      } catch {
        return null;
      }
    })
    .filter(Boolean)
    .sort((left, right) => String(right.manifest.createdAt).localeCompare(String(left.manifest.createdAt)));
}

module.exports = {
  CACHE_DIRECTORY_NAMES,
  createUpdateBackup,
  listUpdateBackups,
  resolveBackupRoot,
  shouldExclude,
};
