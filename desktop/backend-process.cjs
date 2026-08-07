const { spawn } = require('node:child_process');
const net = require('node:net');

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
  });
}

async function waitForHealth(backendUrl, timeoutMS = 15000) {
  const startedAt = Date.now();
  let lastError;
  while (Date.now() - startedAt < timeoutMS) {
    try {
      const response = await fetch(new URL('/api/health', backendUrl));
      if (response.ok) {
        return;
      }
      lastError = new Error(`health status ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw lastError || new Error('backend health check timed out');
}

module.exports = {
  buildBackendEnv,
  findFreePort,
  resolveBackendCommand,
  startBackend,
  waitForHealth,
};
