import { describe, expect, it } from 'vitest';
import { buildStreamUrl, resolveBackendConfig } from './backend';

describe('backend configuration', () => {
  it('prefers Electron bridge configuration', async () => {
    const config = await resolveBackendConfig({
      bridge: {
        getBackendConfig: async () => ({
          backendUrl: 'http://127.0.0.1:20001',
          token: 'desktop-token',
        }),
      },
      env: { VITE_A_STOCK_BACKEND_URL: 'http://127.0.0.1:20081' },
    });

    expect(config).toEqual({
      backendUrl: 'http://127.0.0.1:20001',
      token: 'desktop-token',
    });
  });

  it('falls back to Vite development environment', async () => {
    const config = await resolveBackendConfig({
      env: { VITE_A_STOCK_BACKEND_URL: 'http://127.0.0.1:20081' },
    });

    expect(config).toEqual({
      backendUrl: 'http://127.0.0.1:20081',
      token: '',
    });
  });

  it('builds websocket URLs with symbols, interval, and token', () => {
    const url = buildStreamUrl(
      { backendUrl: 'http://127.0.0.1:20001', token: 'desktop-token' },
      ['000001.SZ', '600000.SH'],
      3000,
    );

    expect(url).toBe('ws://127.0.0.1:20001/api/v1/ws/stream?symbols=000001.SZ%2C600000.SH&interval_ms=3000&token=desktop-token');
  });
});
