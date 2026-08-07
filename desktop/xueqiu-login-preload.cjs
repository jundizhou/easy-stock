const { ipcRenderer } = require('electron');

function mountLoginCompleteControl() {
  if (!location.hostname.endsWith('xueqiu.com') || document.getElementById('a-stock-xueqiu-login-control')) return;
  const host = document.createElement('div');
  host.id = 'a-stock-xueqiu-login-control';
  const shadow = host.attachShadow({ mode: 'closed' });
  shadow.innerHTML = `
    <style>
      :host { all: initial; }
      .panel {
        position: fixed;
        right: 24px;
        bottom: 24px;
        z-index: 2147483647;
        display: flex;
        align-items: center;
        gap: 12px;
        padding: 12px 12px 12px 16px;
        border: 1px solid rgba(30, 112, 225, .22);
        border-radius: 14px;
        background: rgba(255, 255, 255, .96);
        box-shadow: 0 12px 36px rgba(26, 61, 105, .22);
        color: #1f2d3d;
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
        backdrop-filter: blur(12px);
      }
      .copy { display: grid; gap: 3px; min-width: 190px; }
      strong { font-size: 13px; line-height: 1.2; }
      small { color: #77879a; font-size: 11px; line-height: 1.35; }
      button {
        min-width: 128px;
        height: 40px;
        padding: 0 16px;
        border: 0;
        border-radius: 10px;
        background: #1677e8;
        color: #fff;
        font: 650 13px/1 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
        cursor: pointer;
        box-shadow: 0 6px 16px rgba(22, 119, 232, .24);
      }
      button:hover { background: #0868d5; }
      button:disabled { cursor: default; opacity: .72; }
      .panel.error { border-color: rgba(203, 55, 67, .28); }
      .panel.error button { background: #c93846; box-shadow: 0 6px 16px rgba(201, 56, 70, .18); }
      .panel.success { border-color: rgba(22, 128, 90, .28); }
      .panel.success button { background: #16805a; box-shadow: 0 6px 16px rgba(22, 128, 90, .18); }
    </style>
    <div class="panel">
      <span class="copy"><strong>已完成雪球登录？</strong><small>确认登录和滑块验证完成后点击</small></span>
      <button type="button">我已完成登录</button>
    </div>
  `;
  const panel = shadow.querySelector('.panel');
  const button = shadow.querySelector('button');
  const hint = shadow.querySelector('small');
  button.addEventListener('click', async () => {
    button.disabled = true;
    button.textContent = '正在检测…';
    panel.classList.remove('error', 'success');
    try {
      const status = await ipcRenderer.invoke('xueqiu-login-complete');
      if (status?.configured) {
        panel.classList.add('success');
        button.textContent = '登录态已保存';
        hint.textContent = '窗口即将自动关闭';
        return;
      }
      panel.classList.add('error');
      button.textContent = '重新检测';
      hint.textContent = status?.message || '暂未检测到登录状态，请确认登录后重试';
    } catch (error) {
      panel.classList.add('error');
      button.textContent = '重新检测';
      hint.textContent = error instanceof Error ? error.message : '登录态检测失败，请重试';
    } finally {
      button.disabled = false;
    }
  });
  document.documentElement.appendChild(host);
}

if (document.readyState === 'loading') {
  window.addEventListener('DOMContentLoaded', mountLoginCompleteControl, { once: true });
} else {
  mountLoginCompleteControl();
}
