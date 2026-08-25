const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

test('preload exposes updater IPC calls and removes status listeners cleanly', async () => {
  const invocations = [];
  const listeners = new Map();
  let exposed;
  const ipcRenderer = {
    invoke: async (channel, ...args) => {
      invocations.push([channel, ...args]);
      return { channel };
    },
    on: (channel, handler) => listeners.set(channel, handler),
    removeListener: (channel, handler) => {
      assert.equal(listeners.get(channel), handler);
      listeners.delete(channel);
    },
  };
  const contextBridge = { exposeInMainWorld: (_name, api) => { exposed = api; } };
  const source = fs.readFileSync(path.resolve(__dirname, '..', 'preload.cjs'), 'utf8');
  vm.runInNewContext(source, {
    require: (moduleName) => {
      assert.equal(moduleName, 'electron');
      return { contextBridge, ipcRenderer };
    },
  });

  await exposed.getUpdateStatus();
  await exposed.checkForUpdates();
  await exposed.downloadUpdate();
  await exposed.installUpdate();
  assert.deepEqual(invocations.slice(-4).map(([channel]) => channel), [
    'app-update-status',
    'app-update-check',
    'app-update-download',
    'app-update-install',
  ]);
  await exposed.getRuntimeLogStatus();
  await exposed.openRuntimeLogs();
  await exposed.logRuntimeEvent({ level: 'warn', feature: 'test', message: 'failed' });
  assert.deepEqual(invocations.slice(-3), [
    ['runtime-log-status'],
    ['runtime-open-logs'],
    ['runtime-log', { level: 'warn', feature: 'test', message: 'failed' }],
  ]);

  let received;
  const unsubscribe = exposed.onUpdateStatus((status) => { received = status; });
  listeners.get('app-update-status-changed')({}, { state: 'downloaded' });
  assert.equal(received.state, 'downloaded');
  unsubscribe();
  assert.equal(listeners.has('app-update-status-changed'), false);
});
