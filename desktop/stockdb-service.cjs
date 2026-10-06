const fs = require('node:fs');
const net = require('node:net');
const { spawn, execFile } = require('node:child_process');

// free-stockdb 引擎服务编排（数据/计算底座）
// 端口与可执行文件位置可通过环境变量覆盖，便于打包时指向 resources 内嵌二进制。
const STOCKDB_DEFAULT_PORT = 7899;
const STOCKDB_DEFAULT_BIN = process.env.A_STOCK_STOCKDB_BIN
  || 'C:/quant/tools/free-stockdb/stockdb/stockdb.exe';
const STOCKDB_MEMORY_LIMIT_MB = 8192; // 8GB：超过则重启，防 LevelDB 内存膨胀锁死
const STOCKDB_LIVENESS_PATH = '/分钟k?json=1&code=000001&count=1';

function tcpPortReady(port, host = '127.0.0.1', timeoutMS = 2000) {
  return new Promise((resolve) => {
    const s = net.connect(port, host);
    let done = false;
    const finish = (v) => { if (done) return; done = true; s.destroy(); resolve(v); };
    s.on('connect', () => finish(true));
    s.on('error', () => finish(false));
    setTimeout(() => finish(false), timeoutMS);
  });
}

async function waitForStockDb({ port = STOCKDB_DEFAULT_PORT, host = '127.0.0.1', timeoutMS = 20000 } = {}) {
  const startedAt = Date.now();
  while (Date.now() - startedAt < timeoutMS) {
    if (await tcpPortReady(port, host, 1500)) return;
    await new Promise((r) => setTimeout(r, 400));
  }
  throw new Error(`free-stockdb 在 ${host}:${port} 启动超时`);
}

function startStockDb({ exePath = STOCKDB_DEFAULT_BIN, cwd, port = STOCKDB_DEFAULT_PORT, spawnImpl = spawn } = {}) {
  if (!exePath || !fs.existsSync(exePath)) throw new Error(`free-stockdb 二进制不存在：${exePath}`);
  return spawnImpl(exePath, [], {
    cwd: cwd || require('node:path').dirname(exePath),
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  });
}

// 真查询探活：free-stockdb 半死态（CLOSE_WAIT / 内存膨胀）下 TCP 仍可达但查询超时，
// 故以真实 HTTP 查询 + 超时判定存活，区别于纯 TCP 探活。
async function probeStockDbLiveness({ port = STOCKDB_DEFAULT_PORT, host = '127.0.0.1', timeoutMS = 5000 } = {}) {
  const url = `http://${host}:${port}${STOCKDB_LIVENESS_PATH}`;
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), timeoutMS);
  try {
    const res = await fetch(url, { signal: ctrl.signal, headers: { 'User-Agent': 'easy-stock-orchestrator' } });
    return res.ok || res.status === 400; // 任意 HTTP 响应即视为存活（含参数错误的 400）
  } catch {
    return false;
  } finally {
    clearTimeout(timer);
  }
}

// Windows 下读取进程工作集（MB）；非 Windows 返回 null（内存膨胀问题主要见于 Windows）。
function readStockDbMemoryMB(pid) {
  if (process.platform !== 'win32' || !pid) return Promise.resolve(null);
  return new Promise((resolve) => {
    const child = execFile(
      'powershell',
      ['-NoProfile', '-Command', `(Get-Process -Id ${pid} -ErrorAction SilentlyContinue).WorkingSet64`],
      { windowsHide: true },
      (err, stdout) => {
        if (err) return resolve(null);
        const v = parseInt(String(stdout).replace(/[^\d]/g, ''), 10);
        resolve(Number.isFinite(v) ? Math.round(v / (1024 * 1024)) : null);
      },
    );
    child.on('error', () => resolve(null));
  });
}

// 看护：进程退出 / 探活超时(半死态) / 内存超阈值 → 触发 restart 回调。
function createStockDbGuard({ getProcess, restart, port = STOCKDB_DEFAULT_PORT, memoryLimitMB = STOCKDB_MEMORY_LIMIT_MB, intervalMS = 30000, logger } = {}) {
  let timer = null;
  let stopped = false;
  async function safeRestart() {
    try { await restart(); } catch (e) { logger?.error('stockdb-guard', '重启失败', e); }
  }
  async function tick() {
    if (stopped) return;
    const child = getProcess();
    if (!child || child.exitCode !== null) {
      logger?.warn('stockdb-guard', '进程不存在或已退出，尝试重启');
      return safeRestart();
    }
    if (!(await probeStockDbLiveness({ port }))) {
      logger?.warn('stockdb-guard', '探活超时（半死态），重启');
      return safeRestart();
    }
    const mem = await readStockDbMemoryMB(child.pid);
    if (mem !== null && mem > memoryLimitMB) {
      logger?.warn('stockdb-guard', `内存 ${(mem / 1024).toFixed(1)}GB 超阈值，重启`);
      return safeRestart();
    }
  }
  return {
    start() { if (timer) return; stopped = false; timer = setInterval(() => void tick(), intervalMS); timer.unref?.(); },
    stop() { stopped = true; if (timer) { clearInterval(timer); timer = null; } },
  };
}

module.exports = {
  STOCKDB_DEFAULT_PORT,
  STOCKDB_DEFAULT_BIN,
  STOCKDB_MEMORY_LIMIT_MB,
  tcpPortReady,
  waitForStockDb,
  startStockDb,
  probeStockDbLiveness,
  readStockDbMemoryMB,
  createStockDbGuard,
};
