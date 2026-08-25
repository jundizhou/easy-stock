const fs = require('node:fs');
const path = require('node:path');
const util = require('node:util');

const DEFAULT_MAX_BYTES = 5 * 1024 * 1024;
const DEFAULT_BACKUPS = 5;
const MAX_MESSAGE_LENGTH = 16 * 1024;

function redactRuntimeLog(value) {
  return String(value || '')
    .replace(/\bBearer\s+[A-Za-z0-9._~+/=-]+/gi, 'Bearer <redacted>')
    .replace(/([?&](?:token|api[_-]?key|key|cookie|credential|authorization)=)[^&\s]+/gi, '$1<redacted>')
    .replace(/\b(token|api[_-]?key|cookie|credential|authorization)\b["']?\s*[:=]\s*["']?([^"'\s,;&}]+)/gi, '$1=<redacted>')
    .slice(0, MAX_MESSAGE_LENGTH);
}

function formatRuntimeValue(value) {
  if (value instanceof Error) return value.stack || value.message || String(value);
  if (typeof value === 'string') return value;
  return util.inspect(value, { depth: 4, breakLength: 160, maxArrayLength: 40 });
}

function createRotatingLogger({
  directory,
  fileName,
  component,
  maxBytes = DEFAULT_MAX_BYTES,
  backups = DEFAULT_BACKUPS,
  mirror,
  now = () => new Date(),
}) {
  if (!directory) throw new Error('runtime log directory is required');
  if (!fileName || path.basename(fileName) !== fileName) throw new Error('runtime log file name must be a base name');
  const logDirectory = path.resolve(directory);
  const logPath = path.join(logDirectory, fileName);
  fs.mkdirSync(logDirectory, { recursive: true, mode: 0o700 });

  const rotateIfNeeded = (incomingBytes) => {
    let currentSize = 0;
    try {
      currentSize = fs.statSync(logPath).size;
    } catch {}
    if (currentSize === 0 || currentSize + incomingBytes <= maxBytes) return;
    if (backups === 0) {
      try { fs.rmSync(logPath, { force: true }); } catch {}
      return;
    }
    for (let index = backups; index >= 1; index -= 1) {
      const source = index === 1 ? logPath : `${logPath}.${index - 1}`;
      const target = `${logPath}.${index}`;
      try { fs.rmSync(target, { force: true }); } catch {}
      try { fs.renameSync(source, target); } catch (error) {
        if (error.code !== 'ENOENT') throw error;
      }
    }
  };

  const write = (level, feature, ...values) => {
    const message = redactRuntimeLog(values.map(formatRuntimeValue).join(' '));
    const entry = `${JSON.stringify({
      time: now().toISOString(),
      level,
      component,
      feature: String(feature || component || 'runtime').slice(0, 80),
      message,
    })}\n`;
    const bytes = Buffer.byteLength(entry);
    try {
      rotateIfNeeded(bytes);
      fs.appendFileSync(logPath, entry, { encoding: 'utf8', mode: 0o600 });
    } catch (error) {
      mirror?.error?.('[runtime-logger]', error);
    }
    const mirrorMethod = level === 'error' ? 'error' : level === 'warn' ? 'warn' : 'log';
    mirror?.[mirrorMethod]?.(...values);
  };

  return {
    directory: logDirectory,
    path: logPath,
    event: (level, feature, ...values) => write(level, feature, ...values),
    debug: (...values) => write('debug', component, ...values),
    info: (...values) => write('info', component, ...values),
    log: (...values) => write('info', component, ...values),
    warn: (...values) => write('warn', component, ...values),
    error: (...values) => write('error', component, ...values),
  };
}

module.exports = {
  DEFAULT_BACKUPS,
  DEFAULT_MAX_BYTES,
  createRotatingLogger,
  redactRuntimeLog,
};
