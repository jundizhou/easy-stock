const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const { createRotatingLogger, redactRuntimeLog } = require('../runtime-logger.cjs');

test('runtime logger redacts secrets before writing', () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-logs-'));
  const logger = createRotatingLogger({ directory, fileName: 'desktop.log', component: 'desktop' });

	logger.error('Authorization: Bearer live-secret token=abc123 api_key:sk-test cookie=session-value {"credential":"json-secret"}');

  const content = fs.readFileSync(path.join(directory, 'desktop.log'), 'utf8');
	assert.doesNotMatch(content, /live-secret|abc123|sk-test|session-value|json-secret/);
  assert.match(content, /<redacted>/);
});

test('runtime logger rotates and bounds retained files', () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-logs-'));
  const logger = createRotatingLogger({
    directory,
    fileName: 'renderer.log',
    component: 'renderer',
    maxBytes: 220,
    backups: 2,
  });

  for (let index = 0; index < 12; index += 1) logger.info(`entry-${index}-${'x'.repeat(80)}`);

  const files = fs.readdirSync(directory).filter((name) => name.startsWith('renderer.log')).sort();
  assert.deepEqual(files, ['renderer.log', 'renderer.log.1', 'renderer.log.2']);
  assert.match(files.map((name) => fs.readFileSync(path.join(directory, name), 'utf8')).join(''), /entry-11/);
});

test('redactor removes secret query parameters without removing route context', () => {
  const redacted = redactRuntimeLog('GET /api/v1/settings?token=secret&mode=test');
  assert.equal(redacted, 'GET /api/v1/settings?token=<redacted>&mode=test');
});

test('runtime logger keeps writing when the console mirror fails', () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-logs-'));
  const failures = [];
  const logger = createRotatingLogger({
    directory,
    fileName: 'desktop.log',
    component: 'desktop',
    // 父进程 stdout 管道断开时 Electron 主进程会收到 EPIPE，日志不应因此中断或崩溃。
    mirror: {
      log() { throw Object.assign(new Error('write EPIPE'), { code: 'EPIPE' }); },
      warn() { failures.push('warn'); },
      error() { throw Object.assign(new Error('write EPIPE'), { code: 'EPIPE' }); },
    },
  });

  assert.doesNotThrow(() => logger.info('mirror-down-info'));
  assert.doesNotThrow(() => logger.warn('mirror-down-warn'));
  assert.doesNotThrow(() => logger.error('mirror-down-error'));

  const content = fs.readFileSync(path.join(directory, 'desktop.log'), 'utf8');
  assert.match(content, /mirror-down-info/);
  assert.match(content, /mirror-down-warn/);
  assert.match(content, /mirror-down-error/);
  assert.deepEqual(failures, ['warn']);
});
