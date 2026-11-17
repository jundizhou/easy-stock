const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

function zsxqExecutable(root, platform = process.platform) {
  return path.join(root, 'bin', platform === 'win32' ? 'zsxq-cli.exe' : 'zsxq-cli');
}

function resolveZsxqCliPath({ resourcesRoot, isPackaged = false, env = process.env, platform = process.platform, home = os.homedir() }) {
  const bundled = zsxqExecutable(path.join(resourcesRoot, 'zsxq-runtime'), platform);
  // Packaged builds always use their own native CLI, never a user's npm wrapper.
  // Returning its expected path also makes a damaged install fail explicitly.
  if (isPackaged || isFile(bundled)) return bundled;
  const configured = env.A_STOCK_ZSXQ_CLI || env.ZSXQ_CLI_PATH;
  if (configured && isFile(configured)) return configured;

  const pathValue = Object.entries(env).find(([key]) => key.toUpperCase() === 'PATH')?.[1] || '';
  const directories = [...pathValue.split(platform === 'win32' ? ';' : path.delimiter), '/usr/local/bin', '/opt/homebrew/bin'];
  if (home) {
    directories.push(...['.npm-global/bin', '.local/bin', '.volta/bin', 'n/bin', '.hermes/node/bin'].map(dir => path.join(home, dir)));
    const nvm = path.join(home, '.nvm', 'versions', 'node');
    try { directories.push(...fs.readdirSync(nvm).sort().reverse().map(version => path.join(nvm, version, 'bin'))); } catch { /* nvm is optional */ }
  }
  if (platform === 'win32') {
    if (env.APPDATA) directories.push(path.join(env.APPDATA, 'npm'));
    if (env.LOCALAPPDATA) directories.push(path.join(env.LOCALAPPDATA, 'Programs', 'nodejs'));
  }
  const names = platform === 'win32' ? ['zsxq-cli.exe', 'zsxq-cli.cmd', 'zsxq-cli'] : ['zsxq-cli'];
  for (const directory of directories.filter(Boolean)) {
    for (const name of names) {
      const candidate = path.join(directory, name);
      if (isFile(candidate)) return candidate;
    }
  }
  return '';
}

function isFile(file) {
  try { return fs.statSync(file).isFile(); } catch { return false; }
}

module.exports = { zsxqExecutable, resolveZsxqCliPath };
