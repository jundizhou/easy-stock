const { spawn } = require('node:child_process');
const net = require('node:net');
const path = require('node:path');

function buildBackendEnv({ addr, token, baseEnv = process.env, extraEnv = {} }) {
  return {
    ...baseEnv,
    A_STOCK_ADDR: addr,
    A_STOCK_TOKEN: token,
    ...extraEnv,
  };
}

function resolveBackendCommand({ backendBin, backendDir, isPackaged }) {
  if (backendBin) {
    return {
      command: backendBin,
      args: [],
      cwd: backendDir,
    };
  }
  if (isPackaged) {
    throw new Error('packaged backend binary not found');
  }
  return {
    command: 'go',
    args: ['run', './cmd/server'],
    cwd: backendDir,
  };
}

async function findFreePort(host = '127.0.0.1', startPort = 20000, endPort = 29999) {
  for (let port = startPort; port <= endPort; port += 1) {
    if (await canListen(port, host)) {
      return port;
    }
  }
  throw new Error(`no free local port in ${startPort}-${endPort}`);
}

function canListen(port, host) {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.unref();
    server.on('error', (error) => {
      if (error.code === 'EADDRINUSE' || error.code === 'EACCES') {
        resolve(false);
        return;
      }
      reject(error);
    });
    server.listen(port, host, () => {
      server.close(() => resolve(true));
    });
  });
}

function startBackend({ command, args, cwd, addr, token, extraEnv }) {
  return spawn(command, args, {
    cwd,
    env: buildBackendEnv({ addr, token, extraEnv }),
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  });
}

function hasExited(child) {
  return child.exitCode != null || child.signalCode != null;
}

async function terminateChild(child, timeoutMS = 10000, { platform = process.platform, spawnImpl = spawn } = {}) {
  // `killed` only means a signal was sent; it does not mean the process exited.
  if (!child?.pid || hasExited(child)) return;
  let exited = false;
  let exitTimer;
  let forceTimer;
  let onExit;
  const exit = new Promise((resolve, reject) => {
    onExit = () => { exited = true; resolve(); };
    child.once('exit', onExit);
    exitTimer = setTimeout(() => {
      if (platform === 'win32') {
        reject(new Error('本机后台进程树未能及时停止'));
        return;
      }
      try { child.kill('SIGKILL'); } catch (error) { reject(error); return; }
      forceTimer = setTimeout(() => reject(new Error('本机后台服务未能及时停止')), 2000);
    }, timeoutMS);
  });
  // Attach immediately: the exit deadline can expire while taskkill is running.
  exit.catch(() => {});
  try {
    if (platform === 'win32') {
      await new Promise((resolve, reject) => {
        let killer;
        let timer;
        let settled = false;
        let details = '';
        const finish = (error) => {
          if (settled) return;
          settled = true;
          clearTimeout(timer);
          if (error) reject(error); else resolve();
        };
        try {
          const command = path.win32.join(process.env.SystemRoot || 'C:\\Windows', 'System32', 'taskkill.exe');
          killer = spawnImpl(command, ['/PID', String(child.pid), '/T', '/F'], {
            stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true,
          });
          killer.stdout?.on('data', (chunk) => { details = (details + chunk).slice(-4096); });
          killer.stderr?.on('data', (chunk) => { details = (details + chunk).slice(-4096); });
          killer.once('error', (error) => finish(error));
          killer.once('close', (code) => {
            if (code !== 0 && !exited && !hasExited(child)) {
              finish(new Error(`停止本机后台进程树失败（${code}）：${details.trim()}`));
            } else finish();
          });
          timer = setTimeout(() => {
            finish(new Error('停止本机后台进程树超时'));
            try { killer.kill(); } catch {}
          }, timeoutMS);
        } catch (error) { finish(error); }
      });
    } else {
      child.kill();
    }
    await exit;
  } finally {
    clearTimeout(exitTimer);
    clearTimeout(forceTimer);
    child.removeListener('exit', onExit);
  }
}

async function waitForHealth(backendUrl, timeoutMS = 15000, child) {
  const startedAt = Date.now();
  let lastError;
  let processError;
  const controller = new AbortController();
  const onError = (error) => { processError = error; controller.abort(); };
  const onExit = (code, signal) => onError(new Error(`本地后台服务已退出（${signal || `退出码 ${code}`}），请检查 backend.log`));
  child?.once('error', onError);
  child?.once('exit', onExit);
  try {
    if (child && child.exitCode !== null && child.exitCode !== undefined) onExit(child.exitCode);
    while (Date.now() - startedAt < timeoutMS) {
      if (processError) throw processError;
      try {
        const remaining = Math.max(1, timeoutMS - (Date.now() - startedAt));
        const response = await fetch(new URL('/api/health', backendUrl), { signal: AbortSignal.any([controller.signal, AbortSignal.timeout(remaining)]) });
        if (processError) throw processError;
        if (response.ok) return;
        lastError = new Error(`health status ${response.status}`);
      } catch (error) {
        if (processError) throw processError;
        lastError = error;
      }
      await new Promise((resolve) => setTimeout(resolve, Math.min(250, Math.max(0, timeoutMS - (Date.now() - startedAt)))));
    }
    throw lastError || new Error('backend health check timed out');
  } finally {
    child?.removeListener('error', onError);
    child?.removeListener('exit', onExit);
    controller.abort();
  }
}

module.exports = {
  buildBackendEnv,
  findFreePort,
  resolveBackendCommand,
  startBackend,
  terminateChild,
  waitForHealth,
};
