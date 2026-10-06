const fs = require('node:fs');
const path = require('node:path');
const net = require('node:net');
const { spawn } = require('node:child_process');

// Quant Terminal 策略层编排（自研量化终端，stdlib Python）
const QUANT_TERMINAL_DEFAULT_PORT = 8766;
const QUANT_TERMINAL_DEFAULT_SCRIPT = process.env.A_STOCK_QUANT_SCRIPT
  || 'C:/quant/quant_terminal.py';

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

async function waitForQuantTerminal({ port = QUANT_TERMINAL_DEFAULT_PORT, host = '127.0.0.1', timeoutMS = 20000 } = {}) {
  const startedAt = Date.now();
  while (Date.now() - startedAt < timeoutMS) {
    if (await tcpPortReady(port, host, 1500)) return;
    await new Promise((r) => setTimeout(r, 400));
  }
  throw new Error(`Quant Terminal 在 ${host}:${port} 启动超时`);
}

function resolveQuantPython({ runtimeRoot, platform = process.platform, configured } = {}) {
  if (configured && fs.existsSync(configured)) return configured;
  if (runtimeRoot) {
    const p = platform === 'win32'
      ? path.join(runtimeRoot, 'python', 'python.exe')
      : path.join(runtimeRoot, 'venv', 'bin', 'python');
    if (fs.existsSync(p)) return p;
  }
  // 兜底：优先使用本机已知 Python，再退回 PATH（量化终端为纯 stdlib，任意 Python 可运行）。
  const candidates = [
    'C:/ProgramData/anaconda6/python.exe',
    '/usr/bin/python3',
    'python',
    'python3',
  ];
  for (const c of candidates) {
    try { if (fs.existsSync(c)) return c; } catch { /* ignore */ }
  }
  return 'python';
}

function appendNoProxy(value) {
  const entries = String(value || '').split(',').map((s) => s.trim()).filter(Boolean);
  for (const h of ['127.0.0.1', 'localhost']) if (!entries.includes(h)) entries.push(h);
  return entries.join(',');
}

function startQuantTerminal({ python, scriptPath = QUANT_TERMINAL_DEFAULT_SCRIPT, port = QUANT_TERMINAL_DEFAULT_PORT, cwd, spawnImpl = spawn } = {}) {
  if (!fs.existsSync(scriptPath)) throw new Error(`Quant Terminal 脚本不存在：${scriptPath}`);
  return spawnImpl(python, [scriptPath], {
    cwd: cwd || path.dirname(scriptPath),
    env: {
      ...process.env,
      PYTHONNOUSERSITE: '1',
      PYTHONDONTWRITEBYTECODE: '1',
      NO_PROXY: appendNoProxy(process.env.NO_PROXY || process.env.no_proxy || ''),
    },
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  });
}

function createQuantTerminalGuard({ getProcess, restart, port = QUANT_TERMINAL_DEFAULT_PORT, intervalMS = 30000, logger } = {}) {
  let timer = null;
  let stopped = false;
  async function tick() {
    if (stopped) return;
    const child = getProcess();
    if (!child || child.exitCode !== null) {
      logger?.warn('quant-guard', '进程不存在或已退出，尝试重启');
      try { await restart(); } catch (e) { logger?.error('quant-guard', '重启失败', e); }
    }
  }
  return {
    start() { if (timer) return; stopped = false; timer = setInterval(() => void tick(), intervalMS); timer.unref?.(); },
    stop() { stopped = true; if (timer) { clearInterval(timer); timer = null; } },
  };
}

module.exports = {
  QUANT_TERMINAL_DEFAULT_PORT,
  QUANT_TERMINAL_DEFAULT_SCRIPT,
  tcpPortReady,
  waitForQuantTerminal,
  resolveQuantPython,
  startQuantTerminal,
  createQuantTerminalGuard,
};
