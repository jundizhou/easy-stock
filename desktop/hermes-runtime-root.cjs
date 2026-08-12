const fs = require('node:fs');
const path = require('node:path');

function runtimePython(runtimeRoot, platform = process.platform) {
  return platform === 'win32'
    ? path.join(runtimeRoot, 'python', 'python.exe')
    : path.join(runtimeRoot, 'venv', 'bin', 'python');
}

function validRuntimeRoot(runtimeRoot, platform = process.platform) {
  if (!runtimeRoot) return false;
  try {
    return fs.statSync(runtimePython(runtimeRoot, platform)).isFile();
  } catch {
    return false;
  }
}

function readGitDir(projectRoot) {
  const dotGit = path.join(projectRoot, '.git');
  try {
    const stat = fs.statSync(dotGit);
    if (stat.isDirectory()) return dotGit;
    const value = fs.readFileSync(dotGit, 'utf8').trim();
    const match = /^gitdir:\s*(.+)$/i.exec(value);
    if (!match) return '';
    return path.resolve(projectRoot, match[1].trim());
  } catch {
    return '';
  }
}

function commonGitDir(projectRoot) {
  const gitDir = readGitDir(projectRoot);
  if (!gitDir) return '';
  const commonDirFile = path.join(gitDir, 'commondir');
  try {
    const commonDir = fs.readFileSync(commonDirFile, 'utf8').trim();
    return path.resolve(gitDir, commonDir);
  } catch {
    return gitDir;
  }
}

function resolveHermesRuntimeRoot({ configuredRoot = '', bundledRoot = '', projectRoot = '', platform = process.platform } = {}) {
  const candidates = [];
  if (configuredRoot) candidates.push(path.resolve(configuredRoot));
  if (bundledRoot) candidates.push(path.join(bundledRoot, 'hermes-runtime'));
  if (projectRoot) {
    candidates.push(path.join(projectRoot, 'desktop', 'resources', 'hermes-runtime'));
    const commonDir = commonGitDir(projectRoot);
    if (commonDir) candidates.push(path.join(path.dirname(commonDir), 'desktop', 'resources', 'hermes-runtime'));
  }
  return candidates.find((candidate) => validRuntimeRoot(candidate, platform)) || '';
}

module.exports = { commonGitDir, resolveHermesRuntimeRoot, runtimePython, validRuntimeRoot };
