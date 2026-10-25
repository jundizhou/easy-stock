const { redactRuntimeLog } = require('./runtime-logger.cjs');

function escapeHTML(value) {
  return String(value).replace(/[&<>"']/g, (character) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[character]));
}

function startupPage(logDirectory, error) {
  const detail = error ? redactRuntimeLog(error.message || String(error)) : '正在启动本地服务，首次启动可能需要一些时间…';
  const html = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>easy-stock</title><style>body{margin:0;background:#f4f5f7;color:#25324a;font:16px system-ui,sans-serif;display:grid;min-height:100vh;place-items:center}main{max-width:720px;padding:48px}h1{font-size:26px}p,pre{line-height:1.7;white-space:pre-wrap;overflow-wrap:anywhere}small{color:#59677f}button{padding:10px 18px;margin-top:20px;cursor:pointer}body{-webkit-app-region:drag}button{-webkit-app-region:no-drag}</style><main><h1>${error ? 'easy-stock 启动失败' : '正在启动 easy-stock'}</h1><p>${escapeHTML(detail)}</p>${error ? `<p>请重新启动应用。如果问题仍然存在，请在 Issue 中附上 desktop.log 和 backend.log（发送前检查并移除个人信息）。</p><small>日志目录：${escapeHTML(logDirectory)}</small><br><button id="logs">打开日志目录</button><script>document.getElementById('logs').addEventListener('click',()=>window.aStock?.openRuntimeLogs())</script>` : '<small>启动过程中请勿重复打开应用。</small>'}</main></html>`;
  return `data:text/html;charset=utf-8,${encodeURIComponent(html)}`;
}

async function startDesktopWindow({ window, logDirectory, boot, load, onError }) {
  try {
    await window.loadURL(startupPage(logDirectory));
    if (window.isDestroyed()) return false;
    await boot();
    if (window.isDestroyed()) return false;
    await load();
    return true;
  } catch (error) {
    onError(error);
    if (!window.isDestroyed()) await window.loadURL(startupPage(logDirectory, error));
    return false;
  }
}

module.exports = { startDesktopWindow, startupPage };
