const fs = require('node:fs');
const path = require('node:path');

function hasSettings(userDataPath) {
  if (!userDataPath) return false;
  try {
    return fs.statSync(path.join(userDataPath, 'settings.json')).isFile();
  } catch {
    return false;
  }
}

function resolveUserDataPath({ appDataPath, currentUserDataPath, configuredPath = '' }) {
  const explicitPath = String(configuredPath || '').trim();
  if (explicitPath) return path.resolve(explicitPath);
  if (hasSettings(currentUserDataPath)) return currentUserDataPath;

  const legacyUserDataPath = path.join(appDataPath, 'desktop');
  if (hasSettings(legacyUserDataPath)) return legacyUserDataPath;
  return currentUserDataPath;
}

module.exports = { hasSettings, resolveUserDataPath };
